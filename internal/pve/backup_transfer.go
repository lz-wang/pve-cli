package pve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

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
// existing file; a leftover .pve-cli-part of the full size is resumed by
// verification instead of re-transfer. A checksum mismatch fails and cleans
// the partial file up, and the published archive must be recognized by the
// target storage inventory before the copy reports success.
func (s *BackupTransferService) Copy(ctx context.Context, options BackupCopyOptions) (output.BackupCopyResult, error) {
	result := output.BackupCopyResult{Resource: "backup", SourceVolid: options.Volid, TargetStorage: options.TargetStorage}

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
	partPath := finalPath + ".pve-cli-part"
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
			return result, fmt.Errorf("%w: target file %s already exists and does not match the source archive; delete it first or choose another storage", ErrConflict, finalPath)
		}
		if invErr := s.verifyTargetInventory(ctx, targetVolid); invErr != nil {
			return result, invErr
		}
		result.Bytes = source.Size
		result.SHA256 = sourceSum
		result.Resumed = true
		result.Status = "succeeded"
		return result, nil
	}

	// Limited resume: a part file with the complete size skips re-transfer
	// and is validated by checksum instead.
	resume := false
	if run, err := s.targetSSH.Run(ctx, nil, nil, nil, "stat", "-c", "%s", partPath); err == nil && run.ExitCode == 0 {
		if size := parseLeadingUint(run.Stdout); size == source.Size && size > 0 {
			resume = true
		}
	}
	result.Resumed = resume

	var sourceSum string
	if resume {
		result.Bytes = source.Size
		if sourceSum, err = s.remoteChecksum(ctx, s.sourceSSH, sourceFile); err != nil {
			return result, err
		}
	} else {
		if _, err := s.runTarget(ctx, "mkdir", "-p", path.Dir(finalPath)); err != nil {
			return result, err
		}
		hasher := sha256.New()
		progress := &countingWriter{progress: s.Progress}
		transferred, err := s.stream(ctx, sourceFile, partPath, hasher, progress)
		if err != nil {
			_, _ = s.runTarget(ctx, "rm", "-f", partPath)
			result.Status = "failed"
			result.Error = err.Error()
			return result, err
		}
		result.Bytes = transferred
		sourceSum = hex.EncodeToString(hasher.Sum(nil))
	}

	targetSum, err := s.remoteChecksum(ctx, s.targetSSH, partPath)
	if err != nil {
		_, _ = s.runTarget(ctx, "rm", "-f", partPath)
		return result, err
	}
	if sourceSum != targetSum {
		_, _ = s.runTarget(ctx, "rm", "-f", partPath)
		err := fmt.Errorf("%w: checksum mismatch after transfer: source %s, target %s", ErrConflict, sourceSum, targetSum)
		result.Status = "failed"
		result.Error = err.Error()
		return result, err
	}
	result.SHA256 = sourceSum

	// Publish without any overwrite window: hard-linking the part file is
	// atomic and fails when the final path appeared concurrently (closing
	// the test-then-publish TOCTOU gap that mv would silently overwrite).
	if run, err := s.runTarget(ctx, "ln", partPath, finalPath); err != nil {
		return result, err
	} else if run.ExitCode != 0 {
		return result, fmt.Errorf("%w: publishing %s failed (ln exited with %d): the target file may have been created concurrently; nothing was overwritten", ErrConflict, finalPath, run.ExitCode)
	}
	// Cleanup is best effort: the archive is already published, and a
	// leftover part file is picked up by the idempotent continuation above.
	_, _ = s.runTarget(ctx, "rm", "-f", partPath)

	// A file PVE cannot list is not a completed copy: the published archive
	// must be recognized by the target storage inventory (an unrecognized
	// extension or foreign directory would be invisible to PVE).
	if invErr := s.verifyTargetInventory(ctx, targetVolid); invErr != nil {
		result.Status = "failed"
		result.Error = invErr.Error()
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

func parseLeadingUint(text string) uint64 {
	var value uint64
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			break
		}
		value = value*10 + uint64(ch-'0')
	}
	return value
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
