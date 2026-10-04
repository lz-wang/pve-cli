# Output Schema

JSON and YAML output are script-facing and stable within v1.x. New fields may be
added in minor releases, but existing field names and types will not be removed,
renamed, or changed without a new major version. Table output is for humans and
should not be parsed by scripts.

Fields marked optional use `omitempty` and may be absent when the value is empty
or zero.

## NodeRow

| Field | Type |
| --- | --- |
| `name` | string |
| `status` | string |
| `cpu` | number |
| `mem` | uint64 |
| `max_mem` | uint64 |
| `disk` | uint64 |
| `max_disk` | uint64 |
| `uptime` | uint64 |

## GuestRow

Used by `guest`, `vm`, and `lxc` list/detail commands.

| Field | Type |
| --- | --- |
| `kind` | string |
| `vmid` | uint64 |
| `name` | string |
| `node` | string |
| `status` | string |
| `cpus` | int |
| `cpu` | number |
| `mem` | uint64 |
| `max_mem` | uint64 |
| `max_disk` | uint64 |
| `uptime` | uint64 |
| `tags` | string, optional |

## CloneResult

| Field | Type |
| --- | --- |
| `kind` | string |
| `source_vmid` | uint64 |
| `new_vmid` | uint64 |
| `source_node` | string |
| `target_node` | string |
| `name` | string |
| `task` | string, optional |

## SnapshotRow

| Field | Type |
| --- | --- |
| `kind` | string |
| `vmid` | uint64 |
| `node` | string |
| `name` | string |
| `description` | string |
| `parent` | string |
| `snaptime` | int64 |
| `vmstate` | int |
| `state` | string |

## BackupRow

| Field | Type |
| --- | --- |
| `node` | string |
| `storage` | string |
| `kind` | string |
| `vmid` | uint64 |
| `volid` | string |
| `format` | string |
| `size` | uint64 |
| `used` | uint64, optional |
| `ctime` | uint64 |
| `protected` | string, optional |
| `encrypted` | string, optional |
| `verify_state` | string, optional |
| `notes` | string, optional |

## BackupResult

| Field | Type |
| --- | --- |
| `kind` | string |
| `vmid` | uint64 |
| `node` | string |
| `storage` | string |
| `mode` | string |
| `task` | string, optional |

## StorageRow

| Field | Type |
| --- | --- |
| `node` | string |
| `storage` | string |
| `type` | string |
| `active` | bool |
| `enabled` | bool |
| `shared` | bool |
| `content` | string |
| `used` | uint64 |
| `avail` | uint64 |
| `total` | uint64 |
| `used_fraction` | number |

## StorageContentRow

| Field | Type |
| --- | --- |
| `node` | string |
| `storage` | string |
| `content` | string |
| `vmid` | uint64, optional |
| `volid` | string |
| `format` | string, optional |
| `size` | uint64 |
| `used` | uint64, optional |
| `ctime` | uint64, optional |
| `protected` | string, optional |
| `encrypted` | string, optional |
| `verify_state` | string, optional |
| `notes` | string, optional |

## DoctorRow

| Field | Type |
| --- | --- |
| `check` | string |
| `status` | string |
| `message` | string |

Known `status` values are `ok`, `warn`, `fail`, and `skip`.

When a plaintext `token_secret` is used, doctor adds a `TOKEN_SECRET` row with
status `ok` and marks `TOKEN_SECRET_ENV` as `skip`. Environment-based profiles
continue to use `TOKEN_SECRET_ENV`. Credential values are not included in
diagnostic messages.

## CheckRow

Used by `check`. `status` uses the same `ok`/`warn`/`fail`/`skip` vocabulary as
`DoctorRow`.

| Field | Type |
| --- | --- |
| `check` | string |
| `status` | string |
| `resource` | string, optional |
| `message` | string |

## TaskRow

Used by `task ls` and `task get`.

| Field | Type |
| --- | --- |
| `upid` | string |
| `node` | string |
| `type` | string |
| `id` | string, optional |
| `user` | string, optional |
| `status` | string |
| `exit_status` | string, optional |
| `start_time` | int64, optional |
| `end_time` | int64, optional |

Known `status` values are `running`, `ok`, `error`, and `unknown`.

## TaskLogRow

| Field | Type |
| --- | --- |
| `line` | int |
| `text` | string |

## RestoreResult

Used by `vm restore` and `lxc restore`.

| Field | Type |
| --- | --- |
| `kind` | string |
| `vmid` | uint64 |
| `node` | string |
| `archive` | string |
| `storage` | string, optional |
| `task` | string, optional |

## StatusReport

Used by `status`.

| Field | Type |
| --- | --- |
| `nodes` | NodeSummary |
| `guests` | GuestSummary |
| `storages` | StorageSummary |
| `backups` | BackupSummary |
| `issues` | StatusIssue[], optional |

NodeSummary: `total` (int), `online` (int), `offline` (int), `rows`
(NodeRow[], optional).

GuestSummary: `total`, `running`, `stopped`, `vm`, `lxc` (all int).

StorageSummary: `total` (int), `active` (int), `rows` (StorageRow[], optional).

BackupSummary: `count` (int), `latest_ctime` (uint64, optional).

StatusIssue: `component` (string), `message` (string).

## NodeDetail

Used by `node get`.

| Field | Type |
| --- | --- |
| `name` | string |
| `status` | string |
| `cpu` | number |
| `mem` | uint64 |
| `max_mem` | uint64 |
| `disk` | uint64 |
| `max_disk` | uint64 |
| `uptime` | uint64 |
| `pve_version` | string |
| `kernel_version` | string |
| `load_average` | string |
| `cpu_model` | string |
| `cpu_cores` | int |
| `cpu_sockets` | int |

## BulkGuestResult

Used by bulk `guest start/shutdown/reboot/stop`. One row per affected guest;
partial failures still write every row to stdout.

| Field | Type |
| --- | --- |
| `kind` | string |
| `vmid` | uint64 |
| `node` | string |
| `name` | string |
| `action` | string |
| `status` | string (`ok` or `error`) |
| `task` | string, optional |
| `error` | string, optional |

## AgentNetworkRow

Used by `vm agent network`.

| Field | Type |
| --- | --- |
| `name` | string |
| `hardware_address` | string, optional |
| `addresses` | string[], optional |

## AgentExecResult

Used by `vm agent exec`.

| Field | Type |
| --- | --- |
| `exit_code` | int |
| `signal` | int, optional |
| `stdout` | string, optional |
| `stderr` | string, optional |
| `truncated` | bool, optional |

## CloudInitConfig

Used by `vm cloud-init get`. The `cipassword` value is never echoed; only
`password_configured` is reported.

| Field | Type |
| --- | --- |
| `vmid` | uint64 |
| `node` | string |
| `user` | string, optional |
| `password_configured` | bool |
| `ssh_keys` | string, optional |
| `ip_configs` | CloudInitIPConfig[], optional |
| `nameserver` | string, optional |
| `searchdomain` | string, optional |
| `type` | string, optional |
| `custom` | CloudInitCustom[], optional |

CloudInitIPConfig: `device` (string, for example `ipconfig0`), `config`
(string). CloudInitCustom: `device` (string), `volume` (string).

## NetworkRow

Used by `network ls` and `network get`.

| Field | Type |
| --- | --- |
| `node` | string |
| `name` | string |
| `type` | string, optional |
| `active` | bool |
| `autostart` | bool |
| `address` | string, optional |
| `cidr` | string, optional |
| `gateway` | string, optional |
| `bridge_ports` | string, optional |
| `bond_slaves` | string, optional |
| `vlan_aware` | bool |
| `comments` | string, optional |

## FirewallStatusRow

Used by `firewall status`.

| Field | Type |
| --- | --- |
| `scope` | string (`node`, `vm`, or `lxc`) |
| `node` | string |
| `vmid` | uint64, optional |
| `enabled` | bool |

## FirewallRuleRow

Used by `firewall ls`.

| Field | Type |
| --- | --- |
| `scope` | string |
| `node` | string |
| `vmid` | uint64, optional |
| `position` | int |
| `enabled` | bool |
| `direction` | string, optional |
| `action` | string, optional |
| `interface` | string, optional |
| `source` | string, optional |
| `destination` | string, optional |
| `protocol` | string, optional |
| `source_port` | string, optional |
| `dest_port` | string, optional |
| `log` | string, optional |
| `comment` | string, optional |

## VersionInfo

| Field | Type |
| --- | --- |
| `version` | string |
| `commit` | string |
| `date` | string |
| `go_version` | string |
| `os` | string |
| `arch` | string |
