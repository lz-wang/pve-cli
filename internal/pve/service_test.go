package pve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func TestNodeServiceGet(t *testing.T) {
	backend := &fakeBackend{
		nodeDetails: map[string]output.NodeDetail{
			"pve1": {Name: "pve1", Status: "online", PVEVersion: "8.4.1", CPUCores: 12},
		},
	}
	svc := NewNodeService(backend)

	row, err := svc.Get(context.Background(), "pve1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Name != "pve1" || row.PVEVersion != "8.4.1" {
		t.Fatalf("row = %#v", row)
	}

	if _, err := svc.Get(context.Background(), " "); err == nil {
		t.Fatal("expected empty node error")
	}
	if _, err := svc.Get(context.Background(), "missing"); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestGuestServiceResolveExplicitNode(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		vms: map[string]map[int]*fakeGuest{
			"pve2": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve2"}}},
		},
	}
	svc := NewVMService(backend, TaskRunner{}, nil, false)

	row, err := svc.Get(context.Background(), 100, "pve2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Node != "pve2" {
		t.Fatalf("node = %q", row.Node)
	}
	if backend.vmCalls != 1 {
		t.Fatalf("vm calls = %d", backend.vmCalls)
	}
}

func TestGuestServiceResolveByTraversal(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		vms: map[string]map[int]*fakeGuest{
			"pve2": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve2"}}},
		},
	}
	svc := NewVMService(backend, TaskRunner{}, nil, false)

	row, err := svc.Get(context.Background(), 100, "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Node != "pve2" {
		t.Fatalf("node = %q", row.Node)
	}
	if backend.vmCalls != 2 {
		t.Fatalf("vm calls = %d", backend.vmCalls)
	}
}

func TestGuestServiceListPartialFailure(t *testing.T) {
	backend := &fakeBackend{
		nodes:   []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		vmErrs:  map[string]error{"pve1": errors.New("forbidden")},
		vmRows:  map[string][]output.GuestRow{"pve2": {{Kind: "vm", VMID: 100, Node: "pve2"}}},
		vms:     map[string]map[int]*fakeGuest{},
		lxcs:    map[string]map[int]*fakeGuest{},
		lxcRows: map[string][]output.GuestRow{},
	}
	svc := NewVMService(backend, TaskRunner{}, nil, false)

	rows, err := svc.List(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Node != "pve2" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestTaskRunnerWaitsAndReportsFailure(t *testing.T) {
	var stderr bytes.Buffer
	task := &fakeTask{upid: "UPID:pve1:1", failed: true, exitStatus: "ERROR"}
	runner := TaskRunner{Wait: true, ErrWriter: &stderr}

	err := runner.Handle(context.Background(), task)
	if err == nil {
		t.Fatal("expected task failure")
	}
	if !task.waited {
		t.Fatal("expected task to be waited")
	}
	if stderr.String() == "" {
		t.Fatal("expected task output on stderr")
	}
}

func TestGuestServiceCloneReturnsNewIDAndWaits(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:clone"}
	guest := &fakeGuest{
		row:       output.GuestRow{Kind: "vm", VMID: 9000, Node: "pve1", Name: "tmpl"},
		task:      task,
		cloneID:   101,
		cloneName: "app-vm",
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {9000: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	result, err := svc.Clone(context.Background(), 9000, "", CloneOptions{Name: "app-vm", Target: "pve2"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if result.NewVMID != 101 || result.Task != "UPID:pve1:clone" {
		t.Fatalf("result = %#v", result)
	}
	if !task.waited {
		t.Fatal("expected clone task to be waited")
	}
	if guest.cloneOptions.Target != "pve2" || guest.cloneOptions.Name != "app-vm" {
		t.Fatalf("clone options = %#v", guest.cloneOptions)
	}
}

func TestGuestServiceConfigPassesValuesAndWaits(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:config"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.Config(context.Background(), 101, "", map[string]string{"memory": "4096", "cores": "4"})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !task.waited {
		t.Fatal("expected config task to be waited")
	}
	if guest.configValues["memory"] != "4096" || guest.configValues["cores"] != "4" {
		t.Fatalf("config values = %#v", guest.configValues)
	}
}

func TestGuestServiceDeleteWaits(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:delete"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.Delete(context.Background(), 101, "")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !guest.deleted {
		t.Fatal("expected guest to be deleted")
	}
	if !task.waited {
		t.Fatal("expected delete task to be waited")
	}
}

func TestGuestServiceVMRebootWaits(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:reboot"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.Reboot(context.Background(), 101, "")
	if err != nil {
		t.Fatalf("reboot: %v", err)
	}
	if !guest.rebooted {
		t.Fatal("expected guest to be rebooted")
	}
	if !task.waited {
		t.Fatal("expected reboot task to be waited")
	}
}

func TestGuestServiceLXCRebootWaits(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:lxcreboot"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		lxcs:  map[string]map[int]*fakeGuest{"pve1": {201: guest}},
	}
	svc := NewLXCService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.Reboot(context.Background(), 201, "")
	if err != nil {
		t.Fatalf("reboot: %v", err)
	}
	if !guest.rebooted {
		t.Fatal("expected guest to be rebooted")
	}
	if !task.waited {
		t.Fatal("expected reboot task to be waited")
	}
}

func TestGuestServiceMigratePassesOptions(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:migrate"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.Migrate(context.Background(), 101, "", MigrateOptions{Target: "pve2", Online: true})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if guest.migrateOptions.Target != "pve2" || !guest.migrateOptions.Online {
		t.Fatalf("migrate options = %#v", guest.migrateOptions)
	}
}

func TestGuestServiceResizePassesDiskAndSize(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:resize"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.Resize(context.Background(), 101, "", "scsi0", "+20G")
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if guest.resizeDisk != "scsi0" || guest.resizeSize != "+20G" {
		t.Fatalf("resize = %s/%s", guest.resizeDisk, guest.resizeSize)
	}
}

func TestGuestServiceOperationTaskFailure(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:reboot", failed: true, exitStatus: "ERROR"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.Reboot(context.Background(), 101, "")
	if err == nil {
		t.Fatal("expected task failure")
	}
	if got := err.Error(); got != "task UPID:pve1:reboot failed: ERROR" {
		t.Fatalf("error = %q", got)
	}
}

func TestGuestServiceListSnapshots(t *testing.T) {
	guest := &fakeGuest{
		row: output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		snapshots: []output.SnapshotRow{
			{Kind: "vm", VMID: 101, Node: "pve1", Name: "before-upgrade"},
		},
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{}, nil, false)

	rows, err := svc.ListSnapshots(context.Background(), 101, "")
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "before-upgrade" {
		t.Fatalf("snapshots = %#v", rows)
	}
}

func TestGuestServiceCreateSnapshotWaits(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:snapshot"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.CreateSnapshot(context.Background(), 101, "", " before-upgrade ")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if guest.createdSnapshot != "before-upgrade" {
		t.Fatalf("created snapshot = %q", guest.createdSnapshot)
	}
	if !task.waited {
		t.Fatal("expected create task to be waited")
	}
}

func TestGuestServiceRollbackSnapshotWaits(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:rollback"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.RollbackSnapshot(context.Background(), 101, "", "before-upgrade")
	if err != nil {
		t.Fatalf("rollback snapshot: %v", err)
	}
	if guest.rollbackSnapshot != "before-upgrade" {
		t.Fatalf("rollback snapshot = %q", guest.rollbackSnapshot)
	}
	if !task.waited {
		t.Fatal("expected rollback task to be waited")
	}
}

func TestGuestServiceSnapshotNameRequired(t *testing.T) {
	svc := NewVMService(&fakeBackend{}, TaskRunner{}, nil, false)

	if err := svc.CreateSnapshot(context.Background(), 101, "", " "); err == nil {
		t.Fatal("expected empty create snapshot name error")
	}
	if err := svc.RollbackSnapshot(context.Background(), 101, "", " "); err == nil {
		t.Fatal("expected empty rollback snapshot name error")
	}
}

func TestGuestServiceSnapshotTaskFailure(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:rollback", failed: true, exitStatus: "ERROR"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {101: guest}},
	}
	svc := NewVMService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.RollbackSnapshot(context.Background(), 101, "", "before-upgrade")
	if err == nil {
		t.Fatal("expected task failure")
	}
	if got := err.Error(); got != "task UPID:pve1:rollback failed: ERROR" {
		t.Fatalf("error = %q", got)
	}
}

type fakeBackend struct {
	nodes            []output.NodeRow
	nodeDetails      map[string]output.NodeDetail
	vms              map[string]map[int]*fakeGuest
	lxcs             map[string]map[int]*fakeGuest
	vmRows           map[string][]output.GuestRow
	lxcRows          map[string][]output.GuestRow
	backupRows       map[string]map[string][]output.BackupRow
	storageRows      map[string][]output.StorageRow
	storageByName    map[string]map[string]output.StorageRow
	storageContent   map[string]map[string][]output.StorageContentRow
	backupTask       Task
	backupNode       string
	backupOptions    BackupOptions
	backupErrs       map[string]error
	vmErrs           map[string]error
	lxcErrs          map[string]error
	storageErrs      map[string]error
	taskErrs         map[string]error
	taskRows         map[string][]output.TaskRow
	taskByName       map[string]output.TaskRow
	taskLogPages     map[string][][]output.TaskLogRow
	taskHandle       Task
	restoreOptions   RestoreOptions
	restoreTask      Task
	agentErrs        map[string]error
	agentNetwork     []output.AgentNetworkRow
	agentExecOptions AgentExecOptions
	agentExecResult  output.AgentExecResult
	cloudInitConfigs map[int]output.CloudInitConfig
	networkRows      map[string][]output.NetworkRow
	networkErrs      map[string]error
	firewallStatus   map[string]output.FirewallStatusRow
	firewallRules    map[string][]output.FirewallRuleRow
	vmCalls          int
	lxcCalls         int
	nodeCalls        int
	vmListCalls      map[string]int
	lxcListCalls     map[string]int
	backupCalls      int
	backupListCalls  map[string]int
	storageCalls     map[string]int

	// mu guards the call counters: bulk execution queries the backend from
	// several goroutines at once, and go test -race runs over these fakes.
	mu sync.Mutex
}

func (b *fakeBackend) Nodes(context.Context) ([]output.NodeRow, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nodeCalls++
	return b.nodes, nil
}

func (b *fakeBackend) Node(_ context.Context, name string) (output.NodeDetail, error) {
	if detail, ok := b.nodeDetails[name]; ok {
		return detail, nil
	}
	return output.NodeDetail{}, ErrNotFound
}

func (b *fakeBackend) VMs(_ context.Context, node string) ([]output.GuestRow, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.vmListCalls == nil {
		b.vmListCalls = make(map[string]int)
	}
	b.vmListCalls[node]++
	if err := b.vmErrs[node]; err != nil {
		return nil, err
	}
	return b.vmRows[node], nil
}

func (b *fakeBackend) VM(_ context.Context, node string, vmid int) (Guest, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.vmCalls++
	if guest := b.vms[node][vmid]; guest != nil {
		return guest, nil
	}
	return nil, ErrNotFound
}

func (b *fakeBackend) LXCs(_ context.Context, node string) ([]output.GuestRow, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lxcListCalls == nil {
		b.lxcListCalls = make(map[string]int)
	}
	b.lxcListCalls[node]++
	if err := b.lxcErrs[node]; err != nil {
		return nil, err
	}
	return b.lxcRows[node], nil
}

func (b *fakeBackend) LXC(_ context.Context, node string, vmid int) (Guest, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lxcCalls++
	if guest := b.lxcs[node][vmid]; guest != nil {
		return guest, nil
	}
	return nil, ErrNotFound
}

func (b *fakeBackend) Backups(_ context.Context, node, storage string) ([]output.BackupRow, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.backupListCalls == nil {
		b.backupListCalls = make(map[string]int)
	}
	b.backupListCalls[node+"/"+storage]++
	if err := b.backupErrs[node+"/"+storage]; err != nil {
		return nil, err
	}
	if b.backupRows == nil {
		return nil, nil
	}
	return b.backupRows[node][storage], nil
}

func (b *fakeBackend) BackupGuest(_ context.Context, node string, options BackupOptions) (Task, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.backupCalls++
	b.backupNode = node
	b.backupOptions = options
	return b.backupTask, nil
}

func (b *fakeBackend) Storages(_ context.Context, node string) ([]output.StorageRow, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.storageCalls == nil {
		b.storageCalls = make(map[string]int)
	}
	b.storageCalls[node]++
	if err := b.storageErrs[node]; err != nil {
		return nil, err
	}
	return b.storageRows[node], nil
}

func (b *fakeBackend) Storage(_ context.Context, node, storage string) (output.StorageRow, error) {
	if b.storageByName == nil {
		return output.StorageRow{}, ErrNotFound
	}
	row, ok := b.storageByName[node][storage]
	if !ok {
		return output.StorageRow{}, ErrNotFound
	}
	return row, nil
}

func (b *fakeBackend) StorageContents(_ context.Context, node, storage string) ([]output.StorageContentRow, error) {
	if b.storageContent == nil {
		return nil, nil
	}
	return b.storageContent[node][storage], nil
}

func (b *fakeBackend) Tasks(_ context.Context, node string, _ TaskListBackendOptions) ([]output.TaskRow, error) {
	if err := b.taskErrs[node]; err != nil {
		return nil, err
	}
	return b.taskRows[node], nil
}

func (b *fakeBackend) Task(_ context.Context, _, upid string) (output.TaskRow, error) {
	if row, ok := b.taskByName[upid]; ok {
		return row, nil
	}
	return output.TaskRow{}, ErrNotFound
}

func (b *fakeBackend) TaskLog(_ context.Context, _, upid string, page TaskLogPage) ([]output.TaskLogRow, error) {
	pages := b.taskLogPages[upid]
	idx := page.Start / taskLogPageSize
	if idx >= len(pages) {
		return nil, nil
	}
	return pages[idx], nil
}

func (b *fakeBackend) TaskHandle(string) (Task, error) {
	if b.taskHandle == nil {
		return nil, ErrNotFound
	}
	return b.taskHandle, nil
}

func (b *fakeBackend) Restore(_ context.Context, options RestoreOptions) (Task, error) {
	b.restoreOptions = options
	return b.restoreTask, nil
}

func (b *fakeBackend) AgentPing(_ context.Context, node string, vmid int) error {
	if err := b.agentErrs["ping/"+node]; err != nil {
		return err
	}
	return nil
}

func (b *fakeBackend) AgentNetwork(_ context.Context, node string, vmid int) ([]output.AgentNetworkRow, error) {
	if err := b.agentErrs["network/"+node]; err != nil {
		return nil, err
	}
	return b.agentNetwork, nil
}

func (b *fakeBackend) AgentExec(_ context.Context, node string, vmid int, options AgentExecOptions) (output.AgentExecResult, error) {
	b.agentExecOptions = options
	if err := b.agentErrs["exec/"+node]; err != nil {
		return output.AgentExecResult{}, err
	}
	return b.agentExecResult, nil
}

func (b *fakeBackend) VirtualMachineCloudInit(_ context.Context, node string, vmid int) (output.CloudInitConfig, error) {
	if detail, ok := b.cloudInitConfigs[vmid]; ok {
		return detail, nil
	}
	return output.CloudInitConfig{}, ErrNotFound
}

func (b *fakeBackend) Networks(_ context.Context, node string, options NetworkListOptions) ([]output.NetworkRow, error) {
	if err := b.networkErrs[node]; err != nil {
		return nil, err
	}
	rows := b.networkRows[node]
	return filterNetworkRows(rows, options), nil
}

func (b *fakeBackend) Network(_ context.Context, node, iface string) (output.NetworkRow, error) {
	for _, row := range b.networkRows[node] {
		if row.Name == iface {
			return row, nil
		}
	}
	return output.NetworkRow{}, ErrNotFound
}

func (b *fakeBackend) FirewallStatus(_ context.Context, scope FirewallScope) (output.FirewallStatusRow, error) {
	row, ok := b.firewallStatus[firewallScopeKey(scope)]
	if !ok {
		return output.FirewallStatusRow{}, ErrNotFound
	}
	return row, nil
}

func (b *fakeBackend) FirewallRules(_ context.Context, scope FirewallScope) ([]output.FirewallRuleRow, error) {
	return b.firewallRules[firewallScopeKey(scope)], nil
}

func firewallScopeKey(scope FirewallScope) string {
	return scope.Type + "/" + scope.Node + "/" + fmt.Sprint(scope.VMID)
}

func (b *fakeBackend) RegenerateVirtualMachineCloudInit(context.Context, string, int) error {
	return nil
}

type fakeGuest struct {
	row              output.GuestRow
	task             Task
	actionErr        error
	cloneID          int
	cloneName        string
	cloneOptions     CloneOptions
	configValues     map[string]string
	deleted          bool
	rebooted         bool
	migrateOptions   MigrateOptions
	resizeDisk       string
	resizeSize       string
	snapshots        []output.SnapshotRow
	createdSnapshot  string
	rollbackSnapshot string
	deletedSnapshot  string
}

func (g *fakeGuest) Row() output.GuestRow {
	return g.row
}

func (g *fakeGuest) Start(context.Context) (Task, error) {
	if g.actionErr != nil {
		return nil, g.actionErr
	}
	return g.task, nil
}

func (g *fakeGuest) Shutdown(context.Context) (Task, error) {
	if g.actionErr != nil {
		return nil, g.actionErr
	}
	return g.task, nil
}

func (g *fakeGuest) Stop(context.Context) (Task, error) {
	if g.actionErr != nil {
		return nil, g.actionErr
	}
	return g.task, nil
}

func (g *fakeGuest) Reboot(context.Context) (Task, error) {
	g.rebooted = true
	if g.actionErr != nil {
		return nil, g.actionErr
	}
	return g.task, nil
}

func (g *fakeGuest) Clone(_ context.Context, options CloneOptions) (CloneResult, Task, error) {
	g.cloneOptions = options
	name := options.Name
	if name == "" {
		name = options.Hostname
	}
	if name == "" {
		name = g.cloneName
	}
	return CloneResult{
		Kind:       g.row.Kind,
		SourceVMID: g.row.VMID,
		NewVMID:    uint64(g.cloneID),
		SourceNode: g.row.Node,
		TargetNode: options.Target,
		Name:       name,
		Task:       g.task.UPID(),
	}, g.task, nil
}

func (g *fakeGuest) Config(_ context.Context, values map[string]string) (Task, error) {
	g.configValues = values
	return g.task, nil
}

func (g *fakeGuest) Delete(context.Context) (Task, error) {
	g.deleted = true
	return g.task, nil
}

func (g *fakeGuest) Migrate(_ context.Context, options MigrateOptions) (Task, error) {
	g.migrateOptions = options
	return g.task, nil
}

func (g *fakeGuest) Resize(_ context.Context, disk, size string) (Task, error) {
	g.resizeDisk = disk
	g.resizeSize = size
	return g.task, nil
}

func (g *fakeGuest) Snapshots(context.Context) ([]output.SnapshotRow, error) {
	return g.snapshots, nil
}

func (g *fakeGuest) CreateSnapshot(_ context.Context, name string) (Task, error) {
	g.createdSnapshot = name
	return g.task, nil
}

func (g *fakeGuest) RollbackSnapshot(_ context.Context, name string) (Task, error) {
	g.rollbackSnapshot = name
	return g.task, nil
}

func (g *fakeGuest) DeleteSnapshot(_ context.Context, name string) (Task, error) {
	g.deletedSnapshot = name
	return g.task, nil
}

type fakeTask struct {
	upid       string
	waited     bool
	failed     bool
	exitStatus string
}

func (t *fakeTask) UPID() string {
	return t.upid
}

func (t *fakeTask) WaitFor(context.Context, int) error {
	t.waited = true
	return nil
}

func (t *fakeTask) ExitStatus() string {
	return t.exitStatus
}

func (t *fakeTask) Failed() bool {
	return t.failed
}
