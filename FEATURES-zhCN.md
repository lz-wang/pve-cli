# pvectl 功能说明

`pvectl` 是一个小而明确的个人 HomeLab Proxmox VE CLI。它通过
`go-proxmox` 调用 Proxmox VE API，重点覆盖日常 VM/QEMU 和 LXC 工作流。

## 全局 CLI 能力

- 通过 `--output` 或 `-o` 支持 `table`、`json`、`yaml` 输出。
- 通过 `--profile` 支持选择 profile。
- 通过 `--config` 支持指定配置文件路径。
- 通过 `--timeout` 支持覆盖 API 请求超时。
- 对异步操作支持 `--wait` 和 `--wait-timeout`。
- 通过 `--insecure` 支持覆盖 TLS 校验行为。
- 命令结果输出到 stdout。
- task ID、等待进度和日志输出到 stderr。

## 配置

`pvectl config` 管理本地 YAML 配置和 profile 选择。

- `config init` 初始化默认 HomeLab profile。
- `config set-profile NAME` 创建或更新指定 profile。
- `config use-profile NAME` 切换当前 profile。
- `config current-profile` 打印当前 profile 名称。
- `config view` 打印当前配置文件。

配置文件保存 Proxmox endpoint、token ID、token-secret 环境变量名、TLS
行为、timeout 和默认输出格式。它只保存 `token_secret_env`，不会保存 token
secret 值。

## 诊断

`pvectl doctor` 校验本地配置；除非使用 `--offline`，否则也会验证 Proxmox
API 连接。

检查项包括：

- 配置路径和配置文件是否存在
- YAML 解析
- 当前选中的 profile
- profile 必填字段
- token-secret 环境变量是否存在
- timeout 和默认输出设置
- endpoint 形态和 TLS 模式
- API 连接
- 节点列表权限
- 通过 `--node` 可选验证指定节点

Doctor 输出结构化诊断行，并且不会打印 token secret。

`doctor` 检查的是 `pvectl` 自身能否工作；`check`（见下文）检查 HomeLab 当前
是否健康。

## HomeLab Status

`pvectl status` 聚合输出一份紧凑的 HomeLab 概览：

- 节点摘要（total/online/offline，含每个节点的行）
- guest 摘要（total、running、stopped、VM 与 LXC 数量）
- 存储摘要（total、active，含每个存储的行）
- 备份摘要（数量、跨备份存储的最新备份时间；shared 存储只在一个节点查询
  并只统计一次）
- 查询失败的部分以 `issues` 呈现，而不是让整个命令失败

## HomeLab 健康检查

`pvectl check` 以适合 cron 的退出码语义检查 HomeLab 健康状况：

- 离线节点报告 `fail`
- inactive 存储报告 `fail`，disabled 存储报告 `warn`
- 存储使用率超过 `--storage-warn`（默认 85%）报告 `warn`，超过
  `--storage-fail`（默认 95%）报告 `fail`
- 通过 `--backup-tag` 和 `--backup-max-age` 可选开启备份覆盖率检查，只针对
  带该 tag 的 guest；备份存储不可查询时报告 `backup status unavailable`，
  而不是误导性的 `no backup found`
- `--node` 指向不存在的节点时报告 `fail`，而不是输出虚假的正常结果
- `fail` 使退出码非零；`--strict` 让 `warn` 也非零

## 版本

`pvectl version` 打印构建和运行时元数据。它不读取配置文件，也不连接
Proxmox VE。

支持常规输出格式：

- `pvectl version`
- `pvectl version -o json`
- `pvectl version -o yaml`

## 节点

`pvectl node ls` 列出 Proxmox VE 节点。

节点输出包含 status、CPU、memory、disk 和 uptime 字段。

`pvectl node get NODE` 显示节点详情：status、CPU、memory、disk、uptime、PVE
版本、内核版本、load average 以及 CPU 型号/核数/插槽数。

## Task 检查

PVE task 是一等资源：

- `task ls` 列出最近的 task；省略 `--node` 时跨节点聚合并容忍部分节点失败
- `task ls --type vzdump`、`task ls --status running`、`task ls --limit N`
- `task get UPID` 显示单个 task 状态
- `task log UPID` 与 `task log UPID --tail N` 显示 task 日志
- `task wait UPID --wait-timeout DURATION` 阻塞等待完成

task status 取值：`running`、`ok`、`error`、`unknown`。

## Guest 聚合视图

`pvectl guest` 聚合 VM/QEMU 和 LXC guest。

- `guest ls` 列出所有 guest。
- `guest ls --node NODE` 按节点过滤。
- `guest ls --type vm` 只显示 VM。
- `guest ls --type lxc` 只显示容器。
- `guest ls --status running` 按 guest 状态过滤。
- `guest ls --tag TAG` 按 tag 过滤；可重复，配合 `--tag-match all|any`（默认
  `all`）。Proxmox VE 用 `;` 连接 guest tags，`pvectl` 先按该格式解码再匹配。
- `guest get VMID` 解析并显示指定 ID 的 guest。
- `guest get VMID --type vm` 或 `--type lxc` 用于消除重复 ID 歧义。

Guest 输出包含 kind、VMID/CTID、name、node、status、CPU、memory、disk、
uptime 和 tags 等 Proxmox VE 可提供的字段。

### 批量 Guest 操作

`guest start`、`guest shutdown`、`guest reboot`、`guest stop` 作用于所有匹配
选择条件的 guest：

- 选择条件至少需要 `--node`、`--status`、`--tag` 之一
- 选择结果是变更操作的输入，必须 fail closed：任一节点无法查询时整体拒绝
  执行，而不是带着部分集群视图继续
- `--type all|vm|lxc` 收窄 guest 类型
- `--dry-run` 打印将受影响的 guest 后退出
- 命中超过一个 guest 时需要本地输入 `yes` 确认；`--force` 跳过
- `--jobs N`（默认 2）限制并发
- `--wait`/`--wait-timeout` 等待每个 guest 的 task
- 单个 guest 失败不会中止整批；每个 guest 的结果以 `BulkGuestResult` 行写入
  stdout，进度输出到 stderr，只有至少一个 guest 失败时退出码才非零

## VM/QEMU 管理

`pvectl vm` 管理 QEMU 虚拟机。

读取操作：

- `vm ls`
- `vm ls --node NODE`
- `vm get VMID`
- `vm get VMID --node NODE`

生命周期操作：

- `vm start VMID`
- `vm shutdown VMID`
- `vm stop VMID`
- `vm reboot VMID`

维护操作：

- `vm clone SOURCE_VMID --name NAME --target NODE`
- `vm config VMID --set key=value`
- `vm migrate VMID --target NODE`
- `vm resize VMID --disk DISK --size SIZE`
- `vm backup VMID --storage STORAGE`
- `vm delete VMID`

省略 `--node` 时，工具可以遍历集群节点来解析 VM 所在位置。

## LXC 管理

`pvectl lxc` 为 LXC 容器提供与 VM 类似的命令形态。

读取操作：

- `lxc ls`
- `lxc ls --node NODE`
- `lxc get CTID`
- `lxc get CTID --node NODE`

生命周期操作：

- `lxc start CTID`
- `lxc shutdown CTID`
- `lxc stop CTID`
- `lxc reboot CTID`

维护操作：

- `lxc clone SOURCE_CTID --hostname HOSTNAME --target NODE`
- `lxc config CTID --set key=value`
- `lxc migrate CTID --target NODE`
- `lxc resize CTID --disk DISK --size SIZE`
- `lxc backup CTID --storage STORAGE`
- `lxc delete CTID`

省略 `--node` 时，工具可以遍历集群节点来解析容器所在位置。

## 克隆

VM 和 LXC clone 命令从已有 guest 创建新 guest。

通用选项：

- `--node` 选择源节点。
- `--newid` 设置新的 VMID/CTID；省略时由 Proxmox 分配。
- `--target` 选择目标节点，必填。
- `--storage` 选择目标存储。
- `--full` 请求完整克隆。
- `--pool` 设置目标资源池。
- `--snapname` 从指定快照克隆。
- `--description` 设置描述。
- `--wait` 等待 task 完成。
- `--wait-timeout` 设置 task 等待超时。
- `-o json` 让脚本能够捕获 `new_vmid`。

VM 专用选项：

- `--name`
- `--format`

LXC 专用选项：

- `--hostname`

## Guest 配置更新

`vm config` 和 `lxc config` 将通用 `key=value` 选项传给 Proxmox guest
config API。

示例：

```bash
pvectl vm config 101 --set memory=4096 --set cores=4 --wait
pvectl lxc config 201 --set memory=2048 --set cores=2 --wait
```

## 扩容

`vm resize` 和 `lxc resize` 调整 guest 磁盘大小。

示例：

```bash
pvectl vm resize 101 --disk scsi0 --size +20G --wait
pvectl lxc resize 201 --disk rootfs --size +10G --wait
```

## 迁移

`vm migrate` 和 `lxc migrate` 将 guest 迁移到另一个节点。

支持选项：

- `--node` 选择源节点。
- `--target` 选择目标节点，必填。
- `--online` 请求在线迁移。
- `--wait` 等待 task 完成。

## 快照

VM 和 LXC 快照命令位于 `snapshot` 分组下。

- `vm snapshot ls VMID`
- `vm snapshot create VMID SNAPNAME`
- `vm snapshot rollback VMID SNAPNAME`
- `vm snapshot delete VMID SNAPNAME`
- `lxc snapshot ls CTID`
- `lxc snapshot create CTID SNAPNAME`
- `lxc snapshot rollback CTID SNAPNAME`
- `lxc snapshot delete CTID SNAPNAME`

Rollback 和 delete 被视为危险操作，除非传入 `--force`，否则需要本地确认。

## 备份

备份支持刻意保持轻量。

`backup ls` 列出指定节点和存储上的备份文件：

- `backup ls --node NODE --storage STORAGE`
- `backup ls --node NODE --storage STORAGE --vmid VMID`
- `backup ls --node NODE --storage STORAGE --kind vm`
- `backup ls --node NODE --storage STORAGE --kind lxc`
- `backup ls --node NODE --storage STORAGE --latest`

`vm backup` 和 `lxc backup` 创建一次性 guest 备份：

- `--storage` 必填。
- `--mode` 支持 `snapshot`、`suspend`、`stop`。
- `--compress` 支持 `zstd`、`lzo`、`gzip`、`none`。
- `--notes-template` 设置备份备注模板。
- `--bwlimit` 设置带宽限制，单位 KiB/s。
- `--protected` 接受 `0` 或 `1`。
- `--wait` 等待完成。

`vm restore` 和 `lxc restore` 将 vzdump 备份归档恢复到新的、尚不存在的
VMID/CTID：

- `--node` 和 `--vmid` 必填
- `--storage` 可选指定目标存储
- 目标 VMID 已存在时拒绝执行；没有覆盖开关
- VMID 预检必须能看到所有节点；任一节点的 guest 清单无法查询时，restore
  会在发起之前中止
- `--wait` 期间已提交的 task 失败时，结果行（含 task ID）仍会写入 stdout，
  之后命令再以非零码退出
- 归档名带 vzdump 类型前缀（`vzdump-qemu-`/`vzdump-lxc-`）时必须与命令类型
  匹配

## VM QEMU Guest Agent（仅 VM）

`vm agent` 查询 QEMU guest agent。LXC 容器没有该 API。

- 所有 agent 命令的 `--node` 可选；省略时跨集群定位 VM，与其他面向
  VMID 的命令一致
- `vm agent ping VMID` 验证 agent 是否可用
- `vm agent network VMID` 列出 guest 内部看到的网卡和地址（回答"克隆出来的
  VM 拿到了什么 IP"）
- `vm agent exec VMID -- COMMAND [ARG...]` 在 guest 内执行 executable+argv，
  不隐式包装 shell；支持 `--input` 传入 stdin 数据、`--timeout` 限制等待；
  guest 命令退出码会被保留并反映在命令退出码中

文件写入/读取、fs freeze、密码重置刻意不在范围内。

## VM Cloud-init

Cloud-init 命令使用 PVE 原生 cloud-init 配置。

- 所有 cloud-init 命令的 `--node` 可选；省略时跨集群定位 VM
- `vm cloud-init get VMID` 显示归一化的 cloud-init 配置；密码永不回显，只报
  告 `password_configured`
- `vm cloud-init set VMID` 更新 `--user`、`--ssh-key-file`、`--ipconfig0..3`、
  `--nameserver`、`--searchdomain` 和 `--password-env`
- 密码只接受通过 `--password-env` 指定的环境变量名，绝不接受 flag 值
- `vm cloud-init update VMID` 重新生成 cloud-init 镜像，让下次启动生效

## 网络清单（只读）

`network` 查看节点网络接口。

- `network ls --node NODE`
- `network ls --type bridge`
- `network ls --active`
- 省略 `--node` 时跨节点聚合并容忍部分节点失败
- `network get IFACE --node NODE`

网络变更（create/update/delete/apply/reload）是非目标。

## 防火墙清单（只读）

`firewall` 查看防火墙状态和规则。

- `firewall status --node NODE`
- `firewall ls --node NODE`
- `firewall status --node NODE --type vm --vmid VMID`
- `firewall ls --node NODE --type vm --vmid VMID`
- `firewall ls --node NODE --type lxc --vmid CTID`

`--type node` 是默认值。防火墙规则变更是非目标。

## 存储清单

存储命令是只读的。

`storage ls` 列出存储状态：

- `storage ls`
- `storage ls --node NODE`
- `storage ls --content backup`
- `storage ls --type dir`
- `storage ls --active`
- `storage ls --enabled`

`storage usage` 是复用 `StorageRow` schema 的紧凑日常用量视图。

`storage get STORAGE --node NODE` 显示某节点上的一个存储。

`storage content ls` 列出通用存储内容：

- `storage content ls --node NODE --storage STORAGE`
- `storage content ls --node NODE --storage STORAGE --content iso`
- `storage content ls --node NODE --storage STORAGE --content backup`
- `storage content ls --node NODE --storage STORAGE --vmid VMID`

## 危险操作

以下操作可能改变或破坏 guest 状态，因此保持显式操作：

- `vm delete`
- `lxc delete`
- `vm snapshot rollback`
- `lxc snapshot rollback`
- `vm snapshot delete`
- `lxc snapshot delete`
- 命中超过一个 guest 的批量 `guest start/shutdown/reboot/stop`

Delete prompt 要求输入精确的 VMID/CTID，除非传入 `--force`。Snapshot
rollback 和 delete prompt 要求输入精确的 snapshot name，除非传入 `--force`。
批量操作要求输入 `yes`，除非传入 `--force`。

`--force` 会跳过本地确认 prompt。
