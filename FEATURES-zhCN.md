# pve v3 功能清单

面向个人 HomeLab 的资源式、单二进制 Proxmox VE CLI。
全部命令、参数与默认值见[使用指南](docs/usage.md)，JSON/YAML 契约见
[输出 Schema](docs/output-schema.md)。

## 多台独立 HomeLab 主机

- 状态、健康检查、来宾列表、存储用量、任务列表及备份审计支持多 profile 只读聚合、受限并发及明确的部分失败结果。
- `backup audit` 报告缺失或过期的归档候选，以及被排除的磁盘、宿主机路径和设备；身份、异机副本与恢复验证证据保持未知。
  暂不支持 PBS 快照覆盖审计，存在 PBS 备份存储时报告结果不完整。
- `backup restore-plan` 在传输前读取归档配置，检查声明磁盘容量、存储映射、网桥及外部依赖。
  容量采用精确数值计算，无法读取资源清单时返回非零退出码。逐磁盘映射仅用于规划。
- `backup delete` 精确删除单个未受保护的 vzdump 归档，提供预览、确认、元数据复查与删除后验证。
- `config ssh set/clear` 独立维护 SSH 配置；`doctor --ssh` 只读检查节点绑定、连通性及所需主机工具。

## CLI 与配置

- 可执行文件为 `pve`，Go Module 为 `github.com/lz-wang/pve-cli/v3`，使用 urfave/cli/v2。
- 帮助按 Dashboard、Guests、Infrastructure、Local 分组。
- 各命令支持表格、JSON/YAML 结果；stdout 输出结果，stderr 输出进度及 UPID。
- 全局配置路径、Profile、API 超时、TLS 和详细日志选项；本地异步等待。
- `config ls/show/add/set/use`、引导配置、隐藏 Token 输入、当前 Profile 选择、独立凭据文件和环境变量引用；show 仅显示引用与绝对配置路径。
- 严格 YAML Schema；仅允许一个凭据源；Unix 凭据文件须私有且属于当前用户；采用原子写入和私有目录。
- Bash、Zsh、Fish 命令/参数/枚举补全及只读 Node、Storage、VMID 动态补全；有超时限制和静默回退。

## 来宾查询与生命周期

- `vm/lxc ls/get` 和聚合 `guest ls/get` 支持精确 ID/名称、显式数字名称、重名候选及完整清单校验。
- `lxc net set` 按属性安全更新网络设备：保留未指定属性、校验目标网桥存在、
  digest 并发冲突检测。
- `lxc exec` 经可选宿主机 SSH 通道（`pct exec`）在容器内执行命令：文本模式
  标准流透传，JSON 模式有界缓冲与截断标记，保留远端真实退出码，SSH 连接
  失败独立分类。
- 跨独立 PVE 主机的 LXC 迁移：八维度只读预检（不可观察事实如实报
  unknown）、原生 remote_migrate 执行（默认离线、保留源、不启动目标、目标
  Token 全面脱敏、--yes 门控与凭据转移提示）、双端迁移后校验且不自动回滚。
- 跨主机备份物流：SSH 流式归档复制（SHA-256 校验、目标拒绝覆盖、完整
  part 校验即续传）；恢复预检（归档可读性、存储容量、网桥映射校验与恢复后
  自动改写）；backup copy-restore 组合流水线，阶段结果独立、不自动回滚、
  续传规则明确。
- 离线应急维护 `lxc mount`/`lxc unmount`（宿主机 SSH 通道）：仅限已停止
  容器、拒绝重复挂载、报告真实宿主机路径并逐操作复核挂载状态——无法确认
  的结果如实报 unknown 并附人工清理提示，绝不伪装成功；卸载幂等；生命周期
  验收（挂载、读取、锁状态、中断恢复）本地验证，真机验收仅限专用测试容器。
- 名称、状态、标签过滤，标签 all/any 匹配；按 VMID、名称、节点、状态、内存、CPU 稳定排序，缺失指标置后。
- 单来宾启动、关闭、重启和强制停止，异步操作返回可追踪结果。
- VM/LXC 混合批量生命周期操作支持选择范围约束、dry-run、确认、1–8 个 worker、默认 20 个目标上限和逐来宾结果。
- Context 取消阻止尚未提交的操作；部分失败独立保留，按 VMID 稳定输出。

## 部署与维护

- LXC 可下载系统模板索引与本地 `vztmpl` 清单，模板类别/名称过滤，防重复下载及等待后文件校验。
- LXC 创建预检 ID、模板、rootdir 存储和网络；默认非特权且不启动；支持 SSH 公钥和密码环境变量。
- VM 创建支持 CPU、内存、SCSI 磁盘、网桥、可选 ISO 启动与开机自启；不启动或自动安装系统。
- VM/LXC 原生配置 get/set，在 `config` 中保留原始字段及类型；隐藏凭据值并拒绝重复更新键。
- VM 磁盘区分系统盘、附加盘、安装介质和未使用卷；add 检查槽位及存储；remove 保留 unused 卷；delete 仅删除明确的未使用卷，需精确确认并验证存储结果；保留 resize。
- VM/LXC 克隆、扩容、迁移及快照查询、创建、回滚、删除。
- 快照回滚/删除预检存在性和锁，支持只读 dry-run 与确认；VM 回滚要求已停止。
- VM cloud-init get/set/regenerate；部署使用已准备的模板克隆流程。
- VM QEMU Agent ping、网络清单及不隐式使用 Shell 的 executable/argv exec。
- 备份清单、过滤和最近备份查询；一次性 VM/LXC 备份及恢复到未占用的新 VMID，不自动覆盖现有来宾。

## 基础设施与健康检查

- 节点列表/详情及重启/关机预检，显示运行来宾数量和影响，要求精确节点名确认；不自动停止来宾或等待节点恢复。
- 存储列表/详情/内容/容量，支持内容/类型/状态过滤、名称/节点/利用率/容量排序及阈值；未知容量置后，共享存储按节点展示且不重复累加容量。
- 只读节点网络清单和 Node/VM/LXC 防火墙状态/规则。
- Task ls/get/log/wait、状态/类型过滤、日志 tail 与运行中任务取消请求；取消返回 cancel_requested。
- 统一 submitted/succeeded/failed/unknown 操作状态，保留可恢复 UPID，等待受 Context 控制，非幂等请求不自动重复提交。
- Doctor 检查本地配置、凭据和 API 连通性，不输出秘密。
- 健康检查覆盖节点状态、CPU/内存阈值、存储 Warn/Fail 阈值及标签来宾的备份时效与最近时间；不可访问时为 unknown，严格模式将 warning/unknown 视为失败。
- Status 聚合节点/来宾/存储/备份，提供 complete/component_errors、已观测数量及 PARTIAL 表格标记。

## 契约与交付

- 退出码：0 成功，1 操作/健康失败，2 参数/配置错误，3 API 访问失败，4 定位/冲突，5 结果未知。
- `--yes` 仅跳过本地确认；非终端危险操作必须显式提供。Dry-run 零修改，等待超时不取消远端任务。
- 类型化且不回显凭据的 HTTP 错误，不输出可能包含密码的原始错误响应。
- 结构化 `APIError` 保留 HTTP 状态码、CLI 语义分类、脱敏后的服务端消息、
  字段级校验错误与操作上下文（操作、资源、节点、阶段）；错误分类不再依赖
  HTTP 状态行描述文本。
- `config set` 写入后回读状态核验：新增 `verified`/`api_error` 字段区分
  已确认、不一致与结果未知；不确定状态绝不自动重试。
- 备份复制恢复流程支持分别指定归档存储 `--target-storage` 和来宾磁盘存储 `--restore-storage`。
- 恢复后的网桥映射必须使用非空 Digest 和受 Digest 保护的配置更新。
  缺少保护时不写入配置，恢复结果报告为 partial。
- 提交异常保留来宾资源类型。备份报告实际模式未知，恢复报告后置配置已跳过。
- 备份结果区分 `requested_mode` 与 `effective_mode`，实际模式仅来自任务
  日志证据，无法判定时为 `unknown`。
- Mock Service、命令、HTTP 契约、JSON/YAML Golden 和竞态测试。
- 从源码生成命令/Schema 参考并检查同步。
- Linux/macOS/Windows amd64/arm64 构建、校验和、基于 Changelog 的 GitHub Release 与 Homebrew Formula 发布。

保持个人 HomeLab 范围：不提供常驻服务、Web UI、多用户控制平面、任意 REST 透传、定时备份策略或完整 HA/Ceph/SDN 管理。
