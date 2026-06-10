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

## Guest 聚合视图

`pvectl guest` 是跨 VM/QEMU 和 LXC 的只读聚合视图。

- `guest ls` 列出所有 guest。
- `guest ls --node NODE` 按节点过滤。
- `guest ls --type vm` 只显示 VM。
- `guest ls --type lxc` 只显示容器。
- `guest ls --status running` 按 guest 状态过滤。
- `guest get VMID` 解析并显示指定 ID 的 guest。
- `guest get VMID --type vm` 或 `--type lxc` 用于消除重复 ID 歧义。

Guest 输出包含 kind、VMID/CTID、name、node、status、CPU、memory、disk 和
uptime 等 Proxmox VE 可提供的字段。

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
- `lxc snapshot ls CTID`
- `lxc snapshot create CTID SNAPNAME`
- `lxc snapshot rollback CTID SNAPNAME`

Rollback 被视为危险操作，除非传入 `--force`，否则需要本地确认。

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

## 存储清单

存储命令是只读的。

`storage ls` 列出存储状态：

- `storage ls`
- `storage ls --node NODE`
- `storage ls --content backup`
- `storage ls --type dir`
- `storage ls --active`
- `storage ls --enabled`

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

Delete prompt 要求输入精确的 VMID/CTID，除非传入 `--force`。Snapshot
rollback prompt 要求输入精确的 snapshot name，除非传入 `--force`。

`--force` 会跳过本地确认 prompt。
