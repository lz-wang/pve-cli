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

// DirStoragePath resolves a host-visible directory for dir-type storages.
// Only dir storages are supported in this version: the backup archive must
// be reachable as a plain file on the host filesystem. PBS storages must be
// handled by their native mechanisms, not file copies.
type DirStoragePath interface {
	StorageDirPath(ctx context.Context, storage string) (string, error)
}

// BackupCopyOptions configures one cross-profile backup archive copy.
type BackupCopyOptions struct {
	Volid         string // source "storage:backup/file.ext"
	TargetStorage string // dir storage on the target host
}

// BackupTransferService streams a backup archive from one PVE host to
// another over the two SSH transports. Data flows source `cat` → local
// fan-out (sha256 + progress) → pipe → target `dd`; a checksum comparison
// and an atomic rename publish the file.
type BackupTransferService struct {
	sourceAPI  BackupReader
	sourcePath DirStoragePath
	sourceNode string
	sourceSSH  CommandRunner
	targetPath DirStoragePath
	targetSSH  CommandRunner
	Progress   io.Writer
}

func NewBackupTransferService(sourceAPI BackupReader, sourceNode string, sourceSSH CommandRunner, targetPath DirStoragePath, targetSSH CommandRunner, progress io.Writer) *BackupTransferService {
	sourcePath, _ := sourceAPI.(DirStoragePath)
	return &BackupTransferService{
		sourceAPI:  sourceAPI,
		sourcePath: sourcePath,
		sourceNode: sourceNode,
		sourceSSH:  sourceSSH,
		targetPath: targetPath,
		targetSSH:  targetSSH,
		Progress:   progress,
	}
}

// Copy transfers the archive once. The target refuses to overwrite an
// existing file; a leftover .pve-cli-part of the full size is resumed by
// verification instead of re-transfer. A checksum mismatch fails and cleans
// the partial file up.
func (s *BackupTransferService) Copy(ctx context.Context, options BackupCopyOptions) (output.BackupCopyResult, error) {
	result := output.BackupCopyResult{Resource: "backup", SourceVolid: options.Volid, TargetStorage: options.TargetStorage}

	sourceStorage, subpath, err := parseBackupVolid(options.Volid)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(options.TargetStorage) == "" {
		return result, fmt.Errorf("%w: --target-storage is required", ErrInvalid)
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

	sourceDir, err := s.sourcePath.StorageDirPath(ctx, sourceStorage)
	if err != nil {
		return result, err
	}
	targetDir, err := s.targetPath.StorageDirPath(ctx, options.TargetStorage)
	if err != nil {
		return result, err
	}

	sourceFile := sourceDir + "/" + subpath
	finalPath := targetDir + "/" + subpath
	partPath := finalPath + ".pve-cli-part"
	result.TargetPath = finalPath

	// The target never overwrites an existing archive.
	if run, err := s.targetSSH.Run(ctx, nil, nil, nil, "test", "-e", finalPath); err != nil {
		return result, err
	} else if run.ExitCode == 0 {
		return result, fmt.Errorf("%w: target file %s already exists; delete it first or choose another storage", ErrConflict, finalPath)
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

	if _, err := s.runTarget(ctx, "mv", partPath, finalPath); err != nil {
		return result, err
	}
	result.Status = "succeeded"
	return result, nil
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
