# v3 JSON and YAML output schema

Use the per-command `-o json` or `-o yaml` option. Results go to stdout;
task IDs, wait progress and diagnostic text go to stderr. Config inspection
uses human-readable tables without credential values.

Operations carry `resource`, optional `vmid/node/task`, `status`, and optional
`error`. Clone, backup and restore include the same operation semantics plus
resource-specific metadata. `submitted` means accepted without a completed
wait; `succeeded` and `failed` reflect confirmed outcomes. `unknown` includes
wait timeout/interruption and failed post-operation verification. A task ID is
preserved whenever PVE returned one. `cancel_requested` does not imply stopped.
Task inventory retains its own `running/ok/error/unknown` vocabulary.

`StatusReport.complete=false` means component queries failed. Component errors
are grouped by name and also appear as issue rows. Counts in such reports are
observed counts, not authoritative cluster totals. Unknown resource capacity
must not be interpreted as an actual zero-percent utilization.

Guest configuration uses a dynamic `config` object containing native API keys.
Values retain original JSON types, including numeric strings. Credential-key
values are `[REDACTED]`. Other documented row types have stable field names and
Go scalar types listed below. `uint64`/`int64`/`int` serialize as JSON numbers;
`DoctorStatus` is a string; `map[string]interface {}` is a dynamic object.
Slices are arrays; omitted optional fields are absent in both JSON and YAML.

The following reference is generated from the actual output structs.

<!-- GENERATED_REFERENCE -->

## NodeRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `name` | `string` | no |
| `status` | `string` | no |
| `cpu` | `float64` | no |
| `mem` | `uint64` | no |
| `max_mem` | `uint64` | no |
| `disk` | `uint64` | no |
| `max_disk` | `uint64` | no |
| `uptime` | `uint64` | no |

## NodeDetail

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `name` | `string` | no |
| `status` | `string` | no |
| `cpu` | `float64` | no |
| `mem` | `uint64` | no |
| `max_mem` | `uint64` | no |
| `disk` | `uint64` | no |
| `max_disk` | `uint64` | no |
| `uptime` | `uint64` | no |
| `pve_version` | `string` | no |
| `kernel_version` | `string` | no |
| `load_average` | `string` | no |
| `cpu_model` | `string` | no |
| `cpu_cores` | `int` | no |
| `cpu_sockets` | `int` | no |

## GuestRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `kind` | `string` | no |
| `vmid` | `uint64` | no |
| `name` | `string` | no |
| `node` | `string` | no |
| `status` | `string` | no |
| `cpus` | `int` | no |
| `cpu` | `float64` | no |
| `mem` | `uint64` | no |
| `max_mem` | `uint64` | no |
| `max_disk` | `uint64` | no |
| `uptime` | `uint64` | no |
| `tags` | `string` | yes |

## OperationResult

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `resource` | `string` | no |
| `vmid` | `int` | yes |
| `node` | `string` | yes |
| `task` | `string` | yes |
| `status` | `string` | no |
| `error` | `string` | yes |

## CloneResult

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `resource` | `string` | no |
| `status` | `string` | no |
| `error` | `string` | yes |
| `vmid` | `int` | no |
| `node` | `string` | no |
| `kind` | `string` | no |
| `source_vmid` | `uint64` | no |
| `new_vmid` | `uint64` | no |
| `source_node` | `string` | no |
| `target_node` | `string` | no |
| `name` | `string` | no |
| `task` | `string` | yes |

## SnapshotRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `kind` | `string` | no |
| `vmid` | `uint64` | no |
| `node` | `string` | no |
| `name` | `string` | no |
| `description` | `string` | no |
| `parent` | `string` | no |
| `snaptime` | `int64` | no |
| `vmstate` | `int` | no |
| `state` | `string` | no |

## SnapshotPlan

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `resource` | `string` | no |
| `vmid` | `int` | no |
| `node` | `string` | no |
| `snapshot` | `string` | no |
| `action` | `string` | no |
| `impact` | `string` | no |

## BackupRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `node` | `string` | no |
| `storage` | `string` | no |
| `kind` | `string` | no |
| `vmid` | `uint64` | no |
| `volid` | `string` | no |
| `format` | `string` | no |
| `size` | `uint64` | no |
| `used` | `uint64` | yes |
| `ctime` | `uint64` | no |
| `protected` | `string` | yes |
| `encrypted` | `string` | yes |
| `verify_state` | `string` | yes |
| `notes` | `string` | yes |

## BackupResult

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `resource` | `string` | no |
| `status` | `string` | no |
| `error` | `string` | yes |
| `kind` | `string` | no |
| `vmid` | `uint64` | no |
| `node` | `string` | no |
| `storage` | `string` | no |
| `mode` | `string` | no |
| `task` | `string` | yes |

## RestoreResult

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `resource` | `string` | no |
| `status` | `string` | no |
| `error` | `string` | yes |
| `kind` | `string` | no |
| `vmid` | `uint64` | no |
| `node` | `string` | no |
| `archive` | `string` | no |
| `storage` | `string` | yes |
| `task` | `string` | yes |

## StatusReport

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `complete` | `bool` | no |
| `component_errors` | `map[string][]string` | yes |
| `nodes` | `NodeSummary` | no |
| `guests` | `GuestSummary` | no |
| `storages` | `StorageSummary` | no |
| `backups` | `BackupSummary` | no |
| `issues` | `[]output.StatusIssue` | yes |

## NodeSummary

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `total` | `int` | no |
| `online` | `int` | no |
| `offline` | `int` | no |
| `rows` | `[]output.NodeRow` | yes |

## GuestSummary

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `total` | `int` | no |
| `running` | `int` | no |
| `stopped` | `int` | no |
| `vm` | `int` | no |
| `lxc` | `int` | no |

## StorageSummary

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `total` | `int` | no |
| `active` | `int` | no |
| `rows` | `[]output.StorageRow` | yes |

## BackupSummary

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `count` | `int` | no |
| `latest_ctime` | `uint64` | yes |

## StatusIssue

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `component` | `string` | no |
| `message` | `string` | no |

## BulkGuestResult

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `kind` | `string` | no |
| `vmid` | `uint64` | no |
| `node` | `string` | no |
| `name` | `string` | no |
| `action` | `string` | no |
| `status` | `string` | no |
| `task` | `string` | yes |
| `error` | `string` | yes |

## AgentNetworkRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `name` | `string` | no |
| `hardware_address` | `string` | yes |
| `addresses` | `[]string` | yes |

## AgentExecResult

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `exit_code` | `int` | no |
| `signal` | `int` | yes |
| `stdout` | `string` | yes |
| `stderr` | `string` | yes |
| `truncated` | `bool` | yes |

## CloudInitIPConfig

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `device` | `string` | no |
| `config` | `string` | no |

## CloudInitCustom

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `device` | `string` | no |
| `volume` | `string` | no |

## CloudInitConfig

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `vmid` | `uint64` | no |
| `node` | `string` | no |
| `user` | `string` | yes |
| `password_configured` | `bool` | no |
| `ssh_keys` | `string` | yes |
| `ip_configs` | `[]output.CloudInitIPConfig` | yes |
| `nameserver` | `string` | yes |
| `searchdomain` | `string` | yes |
| `type` | `string` | yes |
| `custom` | `[]output.CloudInitCustom` | yes |

## NetworkRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `node` | `string` | no |
| `name` | `string` | no |
| `type` | `string` | yes |
| `active` | `bool` | no |
| `autostart` | `bool` | no |
| `address` | `string` | yes |
| `cidr` | `string` | yes |
| `gateway` | `string` | yes |
| `bridge_ports` | `string` | yes |
| `bond_slaves` | `string` | yes |
| `vlan_aware` | `bool` | no |
| `comments` | `string` | yes |

## FirewallStatusRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `scope` | `string` | no |
| `node` | `string` | no |
| `vmid` | `uint64` | yes |
| `enabled` | `bool` | no |

## FirewallRuleRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `scope` | `string` | no |
| `node` | `string` | no |
| `vmid` | `uint64` | yes |
| `position` | `int` | no |
| `enabled` | `bool` | no |
| `direction` | `string` | yes |
| `action` | `string` | yes |
| `interface` | `string` | yes |
| `source` | `string` | yes |
| `destination` | `string` | yes |
| `protocol` | `string` | yes |
| `source_port` | `string` | yes |
| `dest_port` | `string` | yes |
| `log` | `string` | yes |
| `comment` | `string` | yes |

## StorageRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `node` | `string` | no |
| `storage` | `string` | no |
| `type` | `string` | no |
| `active` | `bool` | no |
| `enabled` | `bool` | no |
| `shared` | `bool` | no |
| `content` | `string` | no |
| `used` | `uint64` | no |
| `avail` | `uint64` | no |
| `total` | `uint64` | no |
| `used_fraction` | `float64` | no |

## StorageContentRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `node` | `string` | no |
| `storage` | `string` | no |
| `content` | `string` | no |
| `vmid` | `uint64` | yes |
| `volid` | `string` | no |
| `format` | `string` | yes |
| `size` | `uint64` | no |
| `used` | `uint64` | yes |
| `ctime` | `uint64` | yes |
| `protected` | `string` | yes |
| `encrypted` | `string` | yes |
| `verify_state` | `string` | yes |
| `notes` | `string` | yes |

## DoctorRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `check` | `string` | no |
| `status` | `DoctorStatus` | no |
| `message` | `string` | no |

## CheckRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `latest_backup` | `uint64` | yes |
| `value` | `float64` | yes |
| `threshold` | `float64` | yes |
| `check` | `string` | no |
| `status` | `DoctorStatus` | no |
| `resource` | `string` | yes |
| `message` | `string` | no |

## TaskRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `upid` | `string` | no |
| `node` | `string` | no |
| `type` | `string` | no |
| `id` | `string` | yes |
| `user` | `string` | yes |
| `status` | `string` | no |
| `exit_status` | `string` | yes |
| `start_time` | `int64` | yes |
| `end_time` | `int64` | yes |

## TaskLogRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `line` | `int` | no |
| `text` | `string` | no |

## VersionInfo

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `version` | `string` | no |
| `commit` | `string` | no |
| `date` | `string` | no |
| `go_version` | `string` | no |
| `os` | `string` | no |
| `arch` | `string` | no |

## GuestConfig

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `resource` | `string` | no |
| `vmid` | `int` | no |
| `node` | `string` | no |
| `config` | `map[string]interface {}` | no |

## TemplateRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `name` | `string` | no |
| `section` | `string` | no |
| `size` | `uint64` | no |
| `node` | `string` | no |
| `storage` | `string` | yes |
| `volid` | `string` | yes |
| `available` | `bool` | no |

## DiskRow

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `vmid` | `int` | no |
| `node` | `string` | no |
| `slot` | `string` | no |
| `volume` | `string` | no |
| `device` | `string` | no |
| `size` | `string` | yes |
| `role` | `string` | no |

## DiskResult

| Field | Go / wire value type | May be omitted |
|---|---|---|
| `resource` | `string` | no |
| `vmid` | `int` | yes |
| `node` | `string` | yes |
| `task` | `string` | yes |
| `status` | `string` | no |
| `error` | `string` | yes |
| `slot` | `string` | no |
| `volume` | `string` | yes |
