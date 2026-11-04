package pve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

// scriptedRunner replays canned results per "command args" key and records
// the call order. Sequences (onSeq) replay in order before falling back to
// the single canned result.
type scriptedRunner struct {
	mu       sync.Mutex
	calls    []string
	stdinOf  map[string]io.Reader
	script   map[string]ssh.Result
	sequence map[string][]ssh.Result
	defaults ssh.Result
}

func newScriptedRunner() *scriptedRunner {
	return &scriptedRunner{script: map[string]ssh.Result{}, sequence: map[string][]ssh.Result{}, defaults: ssh.Result{ExitCode: 0}}
}

func (r *scriptedRunner) on(command string, result ssh.Result) *scriptedRunner {
	r.script[command] = result
	return r
}

// onSeq queues ordered results for a command key.
func (r *scriptedRunner) onSeq(command string, results ...ssh.Result) *scriptedRunner {
	r.sequence[command] = append(r.sequence[command], results...)
	return r
}

func (r *scriptedRunner) Run(_ context.Context, stdin io.Reader, stdout, _ io.Writer, command string, args ...string) (ssh.Result, error) {
	r.mu.Lock()
	key := strings.Join(append([]string{command}, args...), " ")
	r.calls = append(r.calls, key)
	if stdin != nil {
		if r.stdinOf == nil {
			r.stdinOf = map[string]io.Reader{}
		}
		r.stdinOf[key] = stdin
	}
	result := r.defaults
	if queued := r.sequence[key]; len(queued) > 0 {
		result = queued[0]
		r.sequence[key] = queued[1:]
	} else if scripted, ok := r.script[key]; ok {
		result = scripted
	}
	r.mu.Unlock()

	// Behave like the real transport: stream stdout into the sink and
	// consume stdin to EOF (like dd) so pipelined transfers never break.
	if stdin != nil {
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(io.Discard, stdin)
			close(done)
		}()
		<-done
	}
	if stdout != nil && result.Stdout != "" {
		if _, err := stdout.Write([]byte(result.Stdout)); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (r *scriptedRunner) saw(commandPart string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range r.calls {
		if strings.Contains(call, commandPart) {
			return true
		}
	}
	return false
}

// transferTestAPI implements the backup storage contract check: a storage is
// a dir storage with backup content only when listed with a nil error.
type transferTestAPI struct {
	*fakeBackend
	verify map[string]error
}

func (a *transferTestAPI) VerifyBackupStorage(_ context.Context, storage string) error {
	if err, ok := a.verify[storage]; ok {
		return err
	}
	return fmt.Errorf("%w: storage %s is not a supported dir storage", ErrInvalid, storage)
}

const transferPayload = "vzdump-payload-bytes"

// The default PVE dir-storage layout: backup archives live in the physical
// dump/ directory, not in a "backup/" subdirectory of the storage root.
const (
	transferVolid = "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst"
	transferName  = "vzdump-lxc-101-2026_06_06-00_00_00.tar.zst"
	transferFile  = "/var/lib/vz/dump/" + transferName
	targetVolid   = "backup-dst:backup/" + transferName
	targetFile    = "/mnt/backups/dump/" + transferName
	targetPart    = targetFile + ".pve-cli-part"
)

func newTransferAPIs() (*transferTestAPI, *transferTestAPI) {
	sourceAPI := &transferTestAPI{fakeBackend: &fakeBackend{
		nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
		backupRows: map[string]map[string][]output.BackupRow{
			"n150": {"local": {{
				Node: "n150", Storage: "local", Kind: "lxc", VMID: 101,
				VolID: transferVolid, Size: uint64(len(transferPayload)),
			}}},
		},
	}, verify: map[string]error{"local": nil}}
	targetAPI := &transferTestAPI{fakeBackend: &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
	}, verify: map[string]error{"backup-dst": nil}}
	return sourceAPI, targetAPI
}

// defaultTransferScripts resolves both volume paths through `pvesm path` —
// the physical dump/ directories of the default dir-storage layout — and
// lets the target inventory list the published archive.
func defaultTransferScripts(sourceSSH, targetSSH *scriptedRunner) {
	sourceSSH.on("pvesm path "+transferVolid, ssh.Result{ExitCode: 0, Stdout: transferFile + "\n"})
	targetSSH.on("pvesm path "+targetVolid, ssh.Result{ExitCode: 0, Stdout: targetFile + "\n"})
	targetSSH.on("pvesm list backup-dst --content backup", ssh.Result{ExitCode: 0, Stdout: "Volid                                                    Format Type Size Filename\n" + targetVolid + " tar.zst backup " + fmt.Sprint(len(transferPayload)) + " " + transferName + "\n"})
}

func newTransferService(sourceSSH, targetSSH *scriptedRunner, progress io.Writer) (*BackupTransferService, *transferTestAPI) {
	sourceAPI, targetAPI := newTransferAPIs()
	defaultTransferScripts(sourceSSH, targetSSH)
	return NewBackupTransferService(sourceAPI, "n150", sourceSSH, targetAPI, targetSSH, progress), targetAPI
}

func dataSHA(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// P0-1: the transfer resolves the real host paths through `pvesm path` —
// the default dir-storage layout keeps archives in dump/ — and never
// treats the logical "backup/" volume namespace as a filesystem directory.
func TestBackupCopyResolvesPhysicalDumpPaths(t *testing.T) {
	sourceSSH := newScriptedRunner().on("cat "+transferFile, ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+targetPart, ssh.Result{ExitCode: 1}).
		on("sha256sum "+targetPart, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	var progress bytes.Buffer
	svc, _ := newTransferService(sourceSSH, targetSSH, &progress)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if result.TargetPath != targetFile {
		t.Fatalf("target path = %q, want the pvesm-resolved dump path", result.TargetPath)
	}
	if !sourceSSH.saw("cat "+transferFile) || sourceSSH.saw("cat /var/lib/vz/backup/") {
		t.Fatalf("source commands = %v", sourceSSH.calls)
	}
	if !targetSSH.saw("dd of="+targetPart) || targetSSH.saw("dd of=/mnt/backups/backup/") {
		t.Fatalf("target commands = %v", targetSSH.calls)
	}
}

// content-dirs overrides redirect the physical archive directory; the
// transfer must follow whatever `pvesm path` reports.
func TestBackupCopyFollowsCustomContentDirs(t *testing.T) {
	customFile := "/mnt/vzdump-archive/" + transferName
	customPart := customFile + ".pve-cli-part"
	sourceAPI, targetAPI := newTransferAPIs()
	sourceSSH := newScriptedRunner().
		on("pvesm path "+transferVolid, ssh.Result{ExitCode: 0, Stdout: customFile + "\n"}).
		on("cat "+customFile, ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("pvesm path "+targetVolid, ssh.Result{ExitCode: 0, Stdout: customFile + "\n"}).
		on("test -e "+customFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+customPart, ssh.Result{ExitCode: 1}).
		on("sha256sum "+customPart, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"}).
		on("pvesm list backup-dst --content backup", ssh.Result{ExitCode: 0, Stdout: targetVolid + " tar.zst backup 123\n"})
	svc := NewBackupTransferService(sourceAPI, "n150", sourceSSH, targetAPI, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if result.TargetPath != customFile {
		t.Fatalf("target path = %q, want the content-dirs path", result.TargetPath)
	}
	if !targetSSH.saw("dd of=" + customPart) {
		t.Fatalf("target commands = %v", targetSSH.calls)
	}
}

// A completed copy is recognizable by the target PVE storage inventory:
// the published archive must be listed by `pvesm list --content backup`.
func TestBackupCopyVerifiesTargetInventoryRecognition(t *testing.T) {
	sourceSSH := newScriptedRunner().on("cat "+transferFile, ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+targetPart, ssh.Result{ExitCode: 1}).
		on("sha256sum "+targetPart, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if result.Status != "succeeded" {
		t.Fatalf("result = %+v", result)
	}
	if !targetSSH.saw("pvesm list backup-dst --content backup") {
		t.Fatal("inventory recognition check missing")
	}
}

// A file that exists on disk but is not listed by the storage inventory is
// not a completed copy — e.g. an unrecognized archive extension.
func TestBackupCopyFailsWhenInventoryMissesArchive(t *testing.T) {
	sourceAPI, targetAPI := newTransferAPIs()
	sourceSSH := newScriptedRunner().
		on("pvesm path "+transferVolid, ssh.Result{ExitCode: 0, Stdout: transferFile + "\n"}).
		on("cat "+transferFile, ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("pvesm path "+targetVolid, ssh.Result{ExitCode: 0, Stdout: targetFile + "\n"}).
		on("test -e "+targetFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+targetPart, ssh.Result{ExitCode: 1}).
		on("sha256sum "+targetPart, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"}).
		on("ln "+targetPart+" "+targetFile, ssh.Result{ExitCode: 0}).
		on("pvesm list backup-dst --content backup", ssh.Result{ExitCode: 0, Stdout: "Volid\nbackup-dst:backup/some-other-archive.tar.zst tar.zst backup 1 x\n"})
	svc := NewBackupTransferService(sourceAPI, "n150", sourceSSH, targetAPI, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "inventory") {
		t.Fatalf("err = %v, want an inventory-recognition conflict", err)
	}
	if result.Status == "succeeded" {
		t.Fatalf("result = %+v", result)
	}
}

// The storage contract check runs before any host command: a non-dir or
// backup-incapable storage fails the copy up front.
func TestTransferContractRefusesUnsupportedStorage(t *testing.T) {
	sourceAPI, targetAPI := newTransferAPIs()
	targetAPI.verify = map[string]error{"backup-dst": fmt.Errorf("%w: storage backup-dst has type \"pbs\"", ErrInvalid)}
	sourceSSH, targetSSH := newScriptedRunner(), newScriptedRunner()
	defaultTransferScripts(sourceSSH, targetSSH)
	svc := NewBackupTransferService(sourceAPI, "n150", sourceSSH, targetAPI, targetSSH, nil)

	_, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want a storage-contract rejection", err)
	}
	if sourceSSH.saw("cat") || targetSSH.saw("dd") {
		t.Fatal("no host command may run against an unsupported storage")
	}
}

// A source storage that fails the contract check is refused before any
// host command as well.
func TestTransferContractRefusesUnsupportedSourceStorage(t *testing.T) {
	sourceAPI, targetAPI := newTransferAPIs()
	sourceAPI.verify = map[string]error{"local": fmt.Errorf("%w: storage local does not accept backup content", ErrInvalid)}
	sourceSSH, targetSSH := newScriptedRunner(), newScriptedRunner()
	defaultTransferScripts(sourceSSH, targetSSH)
	svc := NewBackupTransferService(sourceAPI, "n150", sourceSSH, targetAPI, targetSSH, nil)

	_, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want a source contract rejection", err)
	}
	if sourceSSH.saw("cat") {
		t.Fatal("no host command may run against an unsupported source storage")
	}
}

func TestBackupCopyTransfersVerifiesAndRenames(t *testing.T) {
	sourceSSH := newScriptedRunner().on("cat "+transferFile, ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+targetPart, ssh.Result{ExitCode: 1}).
		on("sha256sum "+targetPart, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	var progress bytes.Buffer
	svc, _ := newTransferService(sourceSSH, targetSSH, &progress)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if result.Status != "succeeded" || result.Bytes != uint64(len(transferPayload)) || result.SHA256 != dataSHA(transferPayload) {
		t.Fatalf("result = %+v", result)
	}
	if result.Resumed {
		t.Fatal("not a resumed transfer")
	}
	if !targetSSH.saw("mkdir -p") {
		t.Fatal("target directory not created")
	}
	if !targetSSH.saw("dd of=") {
		t.Fatal("dd transfer missing")
	}
	// P1-6: publication is a hard link (atomic, fails on an existing target)
	// followed by part cleanup — never mv, which would overwrite a file
	// created between the existence check and the publish.
	if !targetSSH.saw("ln " + targetPart + " " + targetFile) {
		t.Fatal("atomic link publish missing")
	}
	if targetSSH.saw("mv ") {
		t.Fatal("mv must never publish — it overwrites")
	}
	if !targetSSH.saw("rm -f " + targetPart) {
		t.Fatal("part cleanup missing after publish")
	}
}

// P1-6: if the final path appears between the existence check and the
// publish, the hard link fails and nothing is overwritten.
func TestBackupCopyPublishRefusesConcurrentTarget(t *testing.T) {
	sourceSSH := newScriptedRunner().on("cat "+transferFile, ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+targetPart, ssh.Result{ExitCode: 1}).
		on("sha256sum "+targetPart, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"}).
		on("ln "+targetPart+" "+targetFile, ssh.Result{ExitCode: 1})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want conflict on the concurrent publish", err)
	}
	if !strings.Contains(err.Error(), "concurrently") || !strings.Contains(err.Error(), "nothing was overwritten") {
		t.Fatalf("err = %v, want the no-overwrite guarantee", err)
	}
	if targetSSH.saw("mv ") {
		t.Fatal("mv must never publish — it overwrites")
	}
	if result.Status == "succeeded" {
		t.Fatalf("result = %+v", result)
	}
}

// An existing, checksum-identical target file is the completion of a
// previous interrupted run: an idempotent continuation, not an overwrite.
// The published file must also be inventory-recognized.
func TestBackupCopyExistingIdenticalFileIsIdempotent(t *testing.T) {
	sourceSSH := newScriptedRunner().
		on("sha256sum "+transferFile, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 0}).
		on("sha256sum "+targetFile, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if result.Status != "succeeded" || !result.Resumed || result.SHA256 != dataSHA(transferPayload) {
		t.Fatalf("result = %+v, want a verified idempotent continuation", result)
	}
	if targetSSH.saw("dd of=") || targetSSH.saw("ln ") {
		t.Fatal("an identical published file must not be re-transferred or re-published")
	}
	if !targetSSH.saw("pvesm list backup-dst --content backup") {
		t.Fatal("inventory recognition check missing on the idempotent path")
	}
}

func TestBackupCopyRefusesOverwrite(t *testing.T) {
	sourceSSH := newScriptedRunner().
		on("sha256sum "+transferFile, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 0}).
		on("sha256sum "+targetFile, ssh.Result{ExitCode: 0, Stdout: strings.Repeat("f", 64) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	_, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want conflict for a different existing file", err)
	}
	if targetSSH.saw("dd of=") || targetSSH.saw("ln ") {
		t.Fatal("must not transfer over or replace an existing different file")
	}
}

func TestBackupCopyResumesCompletePart(t *testing.T) {
	sourceSSH := newScriptedRunner().
		on("sha256sum "+transferFile, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+targetPart, ssh.Result{ExitCode: 0, Stdout: fmt.Sprint(len(transferPayload))}).
		on("sha256sum "+targetPart, ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !result.Resumed {
		t.Fatal("expected resumed transfer")
	}
	if targetSSH.saw("dd of=") {
		t.Fatal("complete part must not re-transfer")
	}
	if !targetSSH.saw("ln " + targetPart) {
		t.Fatal("atomic link publish missing on resume")
	}
}

func TestBackupCopyHashMismatchCleansUp(t *testing.T) {
	sourceSSH := newScriptedRunner().on("cat "+transferFile, ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+targetPart, ssh.Result{ExitCode: 1}).
		on("sha256sum "+targetPart, ssh.Result{ExitCode: 0, Stdout: strings.Repeat("0", 64) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q", result.Status)
	}
	if !targetSSH.saw("rm -f " + targetPart) {
		t.Fatal("interrupted transfer must clean up the part file")
	}
}

func TestBackupCopyValidations(t *testing.T) {
	svc, _ := newTransferService(newScriptedRunner(), newScriptedRunner(), nil)

	if _, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: "garbage"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want invalid volid", err)
	}
	if _, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: "local:backup/missing.tar.zst", TargetStorage: "backup-dst"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want missing source backup", err)
	}
	if _, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "not-dir"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want non-dir target rejection", err)
	}
	if _, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "  "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want empty target storage rejection", err)
	}
}

func TestBackupCopyReportsProgress(t *testing.T) {
	sourceSSH := newScriptedRunner().on("cat "+transferFile, ssh.Result{ExitCode: 0, Stdout: "data"})
	targetSSH := newScriptedRunner().
		on("test -e "+targetFile, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+targetPart, ssh.Result{ExitCode: 1}).
		on("sha256sum "+targetPart, ssh.Result{ExitCode: 0, Stdout: dataSHA("data") + "  f\n"})
	var progress bytes.Buffer
	svc, _ := newTransferService(sourceSSH, targetSSH, &progress)

	if _, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: transferVolid, TargetStorage: "backup-dst"}); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !strings.Contains(progress.String(), "transferred") {
		t.Fatalf("progress output missing: %q", progress.String())
	}
}

// `pvesm path` must produce exactly one absolute path; anything else is a
// resolution failure, never a guess.
func TestResolveVolumePathRejectsUnusableOutput(t *testing.T) {
	runner := newScriptedRunner()

	runner.on("pvesm path "+transferVolid, ssh.Result{ExitCode: 1, Stderr: "storage 'local' does not support backup\n"})
	if _, err := resolveVolumePath(context.Background(), runner, transferVolid); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want unavailable for a failing pvesm path", err)
	}
	runner.on("pvesm path "+transferVolid, ssh.Result{ExitCode: 0, Stdout: "relative/path\n"})
	if _, err := resolveVolumePath(context.Background(), runner, transferVolid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want invalid for a non-absolute path", err)
	}
	runner.on("pvesm path "+transferVolid, ssh.Result{ExitCode: 0, Stdout: ""})
	if _, err := resolveVolumePath(context.Background(), runner, transferVolid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want invalid for empty output", err)
	}
}
