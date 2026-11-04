package pve

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

// BackupStorageContract validates through the PVE API that a storage is a
// host-filesystem (dir) storage that accepts backup content. Only dir
// storages are supported for backup copies: the archive must be reachable
// as a plain file on the host; PBS storages must use their native
// mechanisms, not file copies.
type BackupStorageContract interface {
	VerifyBackupStorage(ctx context.Context, storage string) error
}

// BackupCopyOptions configures one cross-profile backup archive copy.
type BackupCopyOptions struct {
	Volid         string // source "storage:backup/file.ext"
	TargetStorage string // dir storage on the target host
}

// PartToken generates the unique per-transfer suffix of the temporary part
// file. Unique names keep concurrent copies isolated: two runs never write
// into (or delete) each other's temporary state, and the atomic hard-link
// publish decides the winner. Tests pin it for deterministic scripts.
var PartToken = func() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

// BackupTransferService streams a backup archive from one PVE host to
// another over the two SSH transports. The physical archive paths are
// resolved on each host with `pvesm path`, so the default dump/ directory
// and custom content-dirs mappings are both honored — the logical volume
// namespace ("storage:backup/file") is never treated as a filesystem
// layout. Data flows source `cat` → local fan-out (sha256 + progress) →
// pipe → target `dd`; a checksum comparison, an atomic hard-link publish,
// and a target inventory check complete the copy.
type BackupTransferService struct {
	sourceAPI      BackupReader
	sourceNode     string
	sourceSSH      CommandRunner
	targetContract BackupStorageContract
	targetSSH      CommandRunner
	Progress       io.Writer
}

func NewBackupTransferService(sourceAPI BackupReader, sourceNode string, sourceSSH CommandRunner, targetContract BackupStorageContract, targetSSH CommandRunner, progress io.Writer) *BackupTransferService {
	return &BackupTransferService{
		sourceAPI:      sourceAPI,
		sourceNode:     sourceNode,
		sourceSSH:      sourceSSH,
		targetContract: targetContract,
		targetSSH:      targetSSH,
		Progress:       progress,
	}
}

// Copy transfers the archive once. The target refuses to overwrite an
// existing file; an identical, checksum-verified file is an idempotent
// reuse. Leftover .pve-cli-part files of other runs are never adopted, and
// this run's own part is cleaned up on every definite failure. A checksum
// or byte-count mismatch fails and cleans the partial file up, and the
// published archive must be recognized by the target storage inventory
// before the copy reports success.
func (s *BackupTransferService) Copy(ctx context.Context, options BackupCopyOptions) (output.BackupCopyResult, error) {
	// Everything before the first host write is a preflight: any failure
	// there proves nothing was executed, so the status starts failed and
	// only upgrades to unknown once the transfer can have had side effects.
	result := output.BackupCopyResult{Resource: "backup", SourceVolid: options.Volid, TargetStorage: options.TargetStorage, Status: "failed"}

	sourceStorage, subpath, err := parseBackupVolid(options.Volid)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(options.TargetStorage) == "" {
		return result, fmt.Errorf("%w: --target-storage is required", ErrInvalid)
	}

	// The storage contract checks run before any host command: only dir
	// storages with backup content are in scope for this version.
	sourceContract, ok := s.sourceAPI.(BackupStorageContract)
	if !ok {
		return result, fmt.Errorf("%w: source backend cannot verify backup storages", ErrUnavailable)
	}
	if err := sourceContract.VerifyBackupStorage(ctx, sourceStorage); err != nil {
		return result, err
	}
	if s.targetContract == nil {
		return result, fmt.Errorf("%w: target backend cannot verify backup storages", ErrUnavailable)
	}
	if err := s.targetContract.VerifyBackupStorage(ctx, options.TargetStorage); err != nil {
		return result, err
	}

	rows, err := s.sourceAPI.Backups(ctx, s.sourceNode, sourceStorage)
	if err != nil {
		return result, err
	}
	var source *output.BackupRow
	for i := range rows {
		if rows[i].VolID == options.Volid {
			source = &rows[i]
			break
		}
	}
	if source == nil {
		return result, fmt.Errorf("%w: backup %s not found on node %s", ErrNotFound, options.Volid, s.sourceNode)
	}

	sourceFile, err := resolveVolumePath(ctx, s.sourceSSH, options.Volid)
	if err != nil {
		return result, err
	}
	targetVolid := options.TargetStorage + ":" + subpath
	finalPath, err := resolveVolumePath(ctx, s.targetSSH, targetVolid)
	if err != nil {
		return result, err
	}
	// Unique part names per transfer: concurrent copies never share (or
	// delete) each other's temporary state, and the publish race is decided
	// by the atomic hard link instead of a shared fixed path.
	ownPart := finalPath + ".pve-cli-part-" + PartToken()
	result.TargetPath = finalPath

	// The target never overwrites an existing archive — but an identical,
	// checksum-verified file is the completion of a previous interrupted
	// run: an idempotent continuation, not an overwrite.
	if run, err := s.targetSSH.Run(ctx, nil, nil, nil, "test", "-e", finalPath); err != nil {
		return result, err
	} else if run.ExitCode == 0 {
		sourceSum, sumErr := s.remoteChecksum(ctx, s.sourceSSH, sourceFile)
		if sumErr != nil {
			return result, sumErr
		}
		targetSum, sumErr := s.remoteChecksum(ctx, s.targetSSH, finalPath)
		if sumErr != nil {
			return result, sumErr
		}
		if sourceSum != targetSum {
			result.Status = "failed"
			return result, fmt.Errorf("%w: target file %s already exists and does not match the source archive; delete it first or choose another storage", ErrConflict, finalPath)
		}
		if invErr := s.verifyTargetInventory(ctx, targetVolid); invErr != nil {
			// The identical file demonstrably exists on the target; only its
			// PVE visibility is unconfirmed. Partial keeps that fact alive —
			// a bare failed would invite an agent to clean the file up.
			result.Status = "partial"
			result.Error = "identical archive already present at " + finalPath + " but its inventory verification failed: " + invErr.Error()
			return result, invErr
		}
		result.Bytes = source.Size
		result.SHA256 = sourceSum
		result.Resumed = true
		result.Status = "succeeded"
		return result, nil
	}

	// A correct size alone never proves a leftover part stopped growing, so
	// orphan parts of other runs are never adopted: this run always
	// transfers to its own part file. Published archives are reused through
	// the checksum-verified branch above.
	if run, err := s.runTarget(ctx, "mkdir", "-p", path.Dir(finalPath)); err != nil {
		return result, err
	} else if run.ExitCode != 0 {
		return result, fmt.Errorf("%w: mkdir -p %s exited with %d: %s", ErrUnavailable, path.Dir(finalPath), run.ExitCode, strings.TrimSpace(run.Stderr))
	}
	// The transfer is the first step that can leave bytes on the target
	// host: from here on an interrupted run cannot prove its outcome, so
	// the status is unknown until a definite branch reports otherwise.
	result.Status = TaskStatusUnknown

	hasher := sha256.New()
	progress := &countingWriter{progress: s.Progress}
	transferred, err := s.stream(ctx, sourceFile, ownPart, hasher, progress)
	if err != nil {
		_, _ = s.runTarget(ctx, "rm", "-f", ownPart)
		result.Status = "failed"
		result.Error = err.Error()
		return result, err
	}
	result.Bytes = transferred
	// The inventory size is the archive's manifest: a shorter read (the
	// source truncated mid-transfer) can still checksum-match a partial
	// file, so byte equality with the manifest is its own publish gate.
	if transferred != source.Size {
		_, _ = s.runTarget(ctx, "rm", "-f", ownPart)
		err := fmt.Errorf("%w: transferred %d bytes but the backup inventory reports %d for %s; refusing to publish an incomplete archive", ErrConflict, transferred, source.Size, options.Volid)
		result.Status = "failed"
		result.Error = err.Error()
		return result, err
	}
	sourceSum := hex.EncodeToString(hasher.Sum(nil))

	targetSum, err := s.remoteChecksum(ctx, s.targetSSH, ownPart)
	if err != nil {
		_, _ = s.runTarget(ctx, "rm", "-f", ownPart)
		result.Status = "failed"
		result.Error = err.Error()
		return result, err
	}
	if sourceSum != targetSum {
		_, _ = s.runTarget(ctx, "rm", "-f", ownPart)
		err := fmt.Errorf("%w: checksum mismatch after transfer: source %s, target %s", ErrConflict, sourceSum, targetSum)
		result.Status = "failed"
		result.Error = err.Error()
		return result, err
	}
	result.SHA256 = sourceSum

	// Publish without any overwrite window: hard-linking the part file is
	// atomic and fails when the final path appeared concurrently (closing
	// the test-then-publish TOCTOU gap that mv would silently overwrite).
	if run, err := s.runTarget(ctx, "ln", ownPart, finalPath); err != nil {
		// A transport-level failure proves nothing about whether ln ran.
		return result, err
	} else if run.ExitCode != 0 {
		// A failed hard link proves nothing was published.
		result.Status = "failed"
		result.Error = fmt.Sprintf("publishing %s failed (ln exited with %d): the target file may have been created concurrently; nothing was overwritten", finalPath, run.ExitCode)
		return result, fmt.Errorf("%w: %s", ErrConflict, result.Error)
	}
	// Cleanup is best effort and strictly scoped to this run's own part:
	// parts of concurrent runs are never touched, and leftover parts of
	// interrupted runs stay on disk for manual inspection.
	_, _ = s.runTarget(ctx, "rm", "-f", ownPart)

	// A file PVE cannot list is not a completed copy: the published archive
	// must be recognized by the target storage inventory (an unrecognized
	// extension or foreign directory would be invisible to PVE). The
	// published fact itself is kept: partial reports an archive at the
	// published path whose verification did not finish.
	if invErr := s.verifyTargetInventory(ctx, targetVolid); invErr != nil {
		result.Status = "partial"
		result.Error = "archive published to " + finalPath + " but its inventory verification failed: " + invErr.Error()
		return result, invErr
	}
	result.Status = "succeeded"
	return result, nil
}

// resolveVolumePath asks the host for the physical path of a volume id via
// `pvesm path`. The storage's own configuration decides the directory — the
// default dump/ layout and content-dirs overrides alike — so the logical
// volume namespace is never mapped onto the filesystem by this CLI.
func resolveVolumePath(ctx context.Context, runner CommandRunner, volid string) (string, error) {
	run, err := runner.Run(ctx, nil, nil, nil, "pvesm", "path", volid)
	if err != nil {
		return "", err
	}
	if run.ExitCode != 0 {
		return "", fmt.Errorf("%w: pvesm path %s exited with %d: %s", ErrUnavailable, volid, run.ExitCode, strings.TrimSpace(run.Stderr))
	}
	resolved := strings.TrimSpace(run.Stdout)
	if resolved == "" || !strings.HasPrefix(resolved, "/") || strings.ContainsAny(resolved, "\n\r") {
		return "", fmt.Errorf("%w: pvesm path %s returned an unusable path %q", ErrInvalid, volid, strings.TrimSpace(run.Stdout))
	}
	return resolved, nil
}

// verifyTargetInventory confirms the target PVE storage inventory lists the
// published archive volid; only then is the archive usable by the target
// PVE.
func (s *BackupTransferService) verifyTargetInventory(ctx context.Context, targetVolid string) error {
	storage, _, _ := strings.Cut(targetVolid, ":")
	run, err := s.targetSSH.Run(ctx, nil, nil, nil, "pvesm", "list", storage, "--content", "backup")
	if err != nil {
		return err
	}
	if run.ExitCode != 0 {
		return fmt.Errorf("%w: pvesm list %s exited with %d: %s", ErrUnavailable, storage, run.ExitCode, strings.TrimSpace(run.Stderr))
	}
	for line := range strings.SplitSeq(run.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == targetVolid {
			return nil
		}
	}
	return fmt.Errorf("%w: published archive %s is not listed by the target storage inventory; the file exists at the published path but the target PVE does not recognize it as a backup", ErrConflict, targetVolid)
}

// stream pipes source `cat` into target `dd` with a local hash and progress
// fan-out. Both sides run concurrently; either failure aborts the copy.
func (s *BackupTransferService) stream(ctx context.Context, sourceFile, partPath string, hasher io.Writer, progress *countingWriter) (uint64, error) {
	pipeReader, pipeWriter := io.Pipe()
	fanOut := io.MultiWriter(hasher, pipeWriter, progress)

	type outcome struct {
		result ssh.Result
		err    error
	}
	targetDone := make(chan outcome, 1)
	go func() {
		result, err := s.targetSSH.Run(ctx, pipeReader, nil, nil, "dd", "of="+partPath, "bs=4M", "status=none")
		_ = pipeReader.Close()
		targetDone <- outcome{result, err}
	}()

	sourceRun, sourceErr := s.sourceSSH.Run(ctx, nil, fanOut, nil, "cat", sourceFile)
	_ = pipeWriter.Close() // unblock dd regardless of the source outcome
	target := <-targetDone

	if sourceErr != nil {
		return progress.written, fmt.Errorf("source read: %w", sourceErr)
	}
	if target.err != nil {
		return progress.written, fmt.Errorf("target write: %w", target.err)
	}
	if sourceRun.ExitCode != 0 {
		return progress.written, fmt.Errorf("%w: source cat exited with %d", ErrUnavailable, sourceRun.ExitCode)
	}
	if target.result.ExitCode != 0 {
		return progress.written, fmt.Errorf("%w: target dd exited with %d", ErrUnavailable, target.result.ExitCode)
	}
	return progress.written, nil
}

func (s *BackupTransferService) runTarget(ctx context.Context, command string, args ...string) (ssh.Result, error) {
	return s.targetSSH.Run(ctx, nil, nil, nil, command, args...)
}

func (s *BackupTransferService) remoteChecksum(ctx context.Context, runner CommandRunner, file string) (string, error) {
	run, err := runner.Run(ctx, nil, nil, nil, "sha256sum", file)
	if err != nil {
		return "", err
	}
	if run.ExitCode != 0 {
		return "", fmt.Errorf("%w: sha256sum exited with %d", ErrUnavailable, run.ExitCode)
	}
	sum, _, _ := strings.Cut(strings.TrimSpace(run.Stdout), " ")
	sum = strings.TrimSpace(sum)
	if len(sum) != 64 {
		return "", fmt.Errorf("%w: unparsable checksum output %q", ErrInvalid, strings.TrimSpace(run.Stdout))
	}
	return sum, nil
}

func parseBackupVolid(volid string) (storage, subpath string, err error) {
	volid = strings.TrimSpace(volid)
	storage, subpath, found := strings.Cut(volid, ":")
	if !found || storage == "" || subpath == "" {
		return "", "", fmt.Errorf("%w: invalid volume id %q, expected STORAGE:backup/file", ErrInvalid, volid)
	}
	return storage, subpath, nil
}

// countingWriter reports observable transfer progress.
type countingWriter struct {
	progress io.Writer
	written  uint64
	next     uint64
	mu       sync.Mutex
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.written += uint64(len(p))
	report := w.progress != nil && w.written >= w.next
	if report {
		w.next = w.written + 64<<20
	}
	written := w.written
	w.mu.Unlock()
	if report {
		_, _ = fmt.Fprintf(w.progress, "transferred %s\n", output.FormatBytes(written))
	}
	return len(p), nil
}
