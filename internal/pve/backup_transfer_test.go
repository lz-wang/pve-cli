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

type transferTestAPI struct {
	*fakeBackend
	dirPaths map[string]string
	failPath map[string]error
}

func (a *transferTestAPI) StorageDirPath(_ context.Context, storage string) (string, error) {
	if err := a.failPath[storage]; err != nil {
		return "", err
	}
	path, ok := a.dirPaths[storage]
	if !ok {
		return "", fmt.Errorf("%w: storage %s is not a dir storage", ErrInvalid, storage)
	}
	return path, nil
}

const transferPayload = "vzdump-payload-bytes"

func newTransferService(sourceSSH, targetSSH *scriptedRunner, progress io.Writer) (*BackupTransferService, *transferTestAPI) {
	sourceAPI := &transferTestAPI{fakeBackend: &fakeBackend{
		nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
		backupRows: map[string]map[string][]output.BackupRow{
			"n150": {"local": {{
				Node: "n150", Storage: "local", Kind: "lxc", VMID: 101,
				VolID: "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", Size: uint64(len(transferPayload)),
			}}},
		},
	}, dirPaths: map[string]string{"local": "/var/lib/vz"}}
	targetAPI := &transferTestAPI{fakeBackend: &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
	}, dirPaths: map[string]string{"backup-dst": "/mnt/backups"}}
	return NewBackupTransferService(sourceAPI, "n150", sourceSSH, targetAPI, targetSSH, progress), targetAPI
}

func dataSHA(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func TestBackupCopyTransfersVerifiesAndRenames(t *testing.T) {
	const payload = "vzdump-payload-bytes"
	sourceSSH := newScriptedRunner().on("cat /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: payload})
	targetSSH := newScriptedRunner().
		on("test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 1}).
		on("stat -c %s /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 1}).
		on(fmt.Sprintf("sha256sum /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part"), ssh.Result{ExitCode: 0, Stdout: dataSHA(payload) + "  file\n"})
	var progress bytes.Buffer
	svc, _ := newTransferService(sourceSSH, targetSSH, &progress)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{
		Volid:         "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
		TargetStorage: "backup-dst",
	})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if result.Status != "succeeded" || result.Bytes != uint64(len(payload)) || result.SHA256 != dataSHA(payload) {
		t.Fatalf("result = %+v", result)
	}
	if result.TargetPath != "/mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst" {
		t.Fatalf("target path = %q", result.TargetPath)
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
	if !targetSSH.saw("ln /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst") {
		t.Fatal("atomic link publish missing")
	}
	if targetSSH.saw("mv ") {
		t.Fatal("mv must never publish — it overwrites")
	}
	if !targetSSH.saw("rm -f /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part") {
		t.Fatal("part cleanup missing after publish")
	}
}

// P1-6: if the final path appears between the existence check and the
// publish, the hard link fails and nothing is overwritten.
func TestBackupCopyPublishRefusesConcurrentTarget(t *testing.T) {
	const payload = "vzdump-payload-bytes"
	final := "/mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst"
	sourceSSH := newScriptedRunner().on("cat /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: payload})
	targetSSH := newScriptedRunner().
		on("test -e "+final, ssh.Result{ExitCode: 1}).
		on("stat -c %s "+final+".pve-cli-part", ssh.Result{ExitCode: 1}).
		on("sha256sum "+final+".pve-cli-part", ssh.Result{ExitCode: 0, Stdout: dataSHA(payload) + "  file\n"}).
		on("ln "+final+".pve-cli-part "+final, ssh.Result{ExitCode: 1})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{
		Volid:         "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
		TargetStorage: "backup-dst",
	})
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
func TestBackupCopyExistingIdenticalFileIsIdempotent(t *testing.T) {
	const payload = "vzdump-payload-bytes"
	final := "/mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst"
	sourceSSH := newScriptedRunner().
		on("sha256sum /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: dataSHA(payload) + "  file\n"})
	targetSSH := newScriptedRunner().
		on("test -e "+final, ssh.Result{ExitCode: 0}).
		on("sha256sum "+final, ssh.Result{ExitCode: 0, Stdout: dataSHA(payload) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{
		Volid:         "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
		TargetStorage: "backup-dst",
	})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if result.Status != "succeeded" || !result.Resumed || result.SHA256 != dataSHA(payload) {
		t.Fatalf("result = %+v, want a verified idempotent continuation", result)
	}
	if targetSSH.saw("dd of=") || targetSSH.saw("ln ") {
		t.Fatal("an identical published file must not be re-transferred or re-published")
	}
}

func TestBackupCopyRefusesOverwrite(t *testing.T) {
	const payload = "vzdump-payload-bytes"
	final := "/mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst"
	sourceSSH := newScriptedRunner().
		on("sha256sum /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: dataSHA(payload) + "  file\n"})
	targetSSH := newScriptedRunner().
		on("test -e "+final, ssh.Result{ExitCode: 0}).
		on("sha256sum "+final, ssh.Result{ExitCode: 0, Stdout: strings.Repeat("f", 64) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	_, err := svc.Copy(context.Background(), BackupCopyOptions{
		Volid:         "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
		TargetStorage: "backup-dst",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want conflict for a different existing file", err)
	}
	if targetSSH.saw("dd of=") || targetSSH.saw("ln ") {
		t.Fatal("must not transfer over or replace an existing different file")
	}
}

func TestBackupCopyResumesCompletePart(t *testing.T) {
	sourceSSH := newScriptedRunner().
		on("cat /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: transferPayload}).
		on("sha256sum /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	targetSSH := newScriptedRunner().
		on("test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 1}).
		on("stat -c %s /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 0, Stdout: fmt.Sprint(len(transferPayload))}).
		on("sha256sum /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 0, Stdout: dataSHA(transferPayload) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{
		Volid:         "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
		TargetStorage: "backup-dst",
	})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !result.Resumed {
		t.Fatal("expected resumed transfer")
	}
	if targetSSH.saw("cat /var/lib") && false {
		t.Fatal("unreachable")
	}
	if targetSSH.saw("dd of=") {
		t.Fatal("complete part must not re-transfer")
	}
	if !targetSSH.saw("ln /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part") {
		t.Fatal("atomic link publish missing on resume")
	}
}

func TestBackupCopyHashMismatchCleansUp(t *testing.T) {
	const payload = "vzdump-payload-bytes"
	sourceSSH := newScriptedRunner().on("cat /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: payload})
	targetSSH := newScriptedRunner().
		on("test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 1}).
		on("stat -c %s /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 1}).
		on("sha256sum /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 0, Stdout: strings.Repeat("0", 64) + "  file\n"})
	svc, _ := newTransferService(sourceSSH, targetSSH, nil)

	result, err := svc.Copy(context.Background(), BackupCopyOptions{
		Volid:         "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
		TargetStorage: "backup-dst",
	})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q", result.Status)
	}
	if !targetSSH.saw("rm -f /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part") {
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
	if _, err := svc.Copy(context.Background(), BackupCopyOptions{Volid: "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", TargetStorage: "not-dir"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want non-dir target rejection", err)
	}
}

func TestBackupCopyReportsProgress(t *testing.T) {
	sourceSSH := newScriptedRunner().on("cat /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: "data"})
	targetSSH := newScriptedRunner().
		on("test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 1}).
		on("stat -c %s /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 1}).
		on("sha256sum /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 0, Stdout: dataSHA("data") + "  f\n"})
	var progress bytes.Buffer
	svc, _ := newTransferService(sourceSSH, targetSSH, &progress)

	if _, err := svc.Copy(context.Background(), BackupCopyOptions{
		Volid:         "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
		TargetStorage: "backup-dst",
	}); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !strings.Contains(progress.String(), "transferred") {
		t.Fatalf("progress output missing: %q", progress.String())
	}
}
