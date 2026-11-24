# 变更日志

[English](../../CHANGELOG.md) | **简体中文**

本文件记录项目的所有重要变更。格式遵循
[Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循
[语义化版本](https://semver.org/lang/zh-CN/)。

## 未发布

### 新增

- 创建实例支持 `disks` 额外空数据盘，表单提供设备名、挂载路径与容量的引导和校验。
- 容量输入统一校验：内存、根磁盘、存储卷、ISO 安装均显示单位提示，支持
  MiB/GiB/TiB，纯数字按 MiB 处理，并在输入框下直接显示错误。
- 按用户任务重构信息架构：工作台（总览、实例、存储）、资源（镜像与自动化）、
  集群 → 节点、管理。迁移历史并入实例页；应答配置与 SSH 公钥、脚本、镜像/ISO
  合并到同一标签页，并更名为「镜像与自动化」。集群页改为分标签（概览与节点、
  实例、存储、镜像与安装介质、网络、配置文件），不再罗列节点本地物理网卡。
- 侧栏按 Fleet / 集群 → 节点 / 管理 分组，增加面包屑并自动展开当前节点；节点页
  说明自身作用域，只读角色不再看到必然失败的变更按钮。
- 管理员可查看并吊销某个账号的全部会话。

### 修复

- 批量电源确认、注销集群标题、删除镜像失败提示等文案错误。
- 资产/实例/清单请求失败不再伪装成空数据；部分集群失败会明确标注，实例总览
  在部分集群不可达时降级返回而非整页 503。
- 迁移去重改为查询数据库，共享存储的两个中心不能同时发起同一实例的迁移。
- 生产环境为认证端点增加按客户端限流；仅信任本地反向代理的转发协议头；任务审计
  记录真实操作者。
- 轮询在故障恢复后回到基础频率并在后台标签页暂停；ISO 上传可取消且跨页面保持进度；
  并发实例操作按行跟踪忙碌状态。

- 防泄漏从“靠人记得”改为机制强制：加固后的 `.gitignore` 按整目录拒绝本地材料、
  按凭据形态拒绝密钥类文件；`scripts/secret-scan.sh` 在提交前检查真实内容（私钥、
  凭据赋值、Bearer token、运维网段与主机名）；CI 用同一扫描器遍历所有已跟踪文件和
  每个提交。证书已移出仓库至 `~/.config/incus-fleet/certs`，详见 docs/en/security.md。
- 集群私有互联网络：fleet 在每个成员上创建宿主网桥，用点对点 VXLAN 隧道把成员连起来，并由一个网关成员提供 DHCP/NAT，使所有节点上的实例共享一张私有网络，且无需改动客户机配置。包含启用/对账/关闭接口和集群页面板。
- 本地账号体系：`admin`/`operator`/`viewer` 角色、bcrypt 密码、跨中心共享的服务端会话、
  失败锁定；共享管理 token 在首个本地账号创建后自动失效。
- 虚拟机图形控制台：不依赖宿主 QEMU 的 VNC/SPICE 能力，fleet 通过 `raw.qemu.conf` 追加第二个
  QEMU 监视器（QMP control 模式），提供 `desktop/prepare`、`desktop/screenshot`（PNG 帧缓冲轮询）
  与 `desktop/input`（带修饰键的和弦键盘、绝对指针、滚轮）。浏览器画布自动刷新；首次准备会让
  运行中的虚拟机重启一次，之后调用幂等。
- Rust agent 兼容 node-exporter 的主机指标（`node_cpu_seconds_total`、`node_memory_*`、
  `node_load1`、网络与 OOM 计数），与实例指标并存。
- 跨集群迁移后自动在客户机内续租 DHCP，移动后的虚拟机立即从目标集群网桥获取地址，
  而不是继续沿用源集群租约最长一小时。

- 新增 `rqlite` 作为第三种数据库驱动，让多个 fleet 实例共享一份复制的 SQLite，无需运行 PostgreSQL。包含把 rqlite 的 JSON 浮点数还原为 int64 时间戳的包装驱动、不依赖事务的原子任务抢占，以及驱动为共享型时自动启用数据库会话。
- migrations 表新增 `owner` 列，实例重启时只把自己中断的任务标记为失败。
- 从 UI 添加集群成员：SSH 准备、可选安装 Incus、加入集群、sudo 白名单和 fleet SSH 公钥授权。
- 本地应用主机与远程成员的按成员节点网络管理。
- 类型化主机操作路由（`hostops`），支持 `node_access.mode`（`auto`、`agent`、`ssh`、`local`）
  并显式报告后端。
- 持久化 host task 与审计记录，提供 `GET /api/v1/tasks`、`GET /api/v1/tasks/{id}` 和
  `GET /api/v1/audit`，节点网络页提供 **History** 对话框。
- schema 迁移版本跟踪，以及 host task、审计、agent、注册 token 和证书表。
- Wi-Fi 与节点网络任务计划已脱敏；密码和 join token 不会持久化。
- Rust 主机 agent MVP：protobuf `incusfleet.agent.v1` 契约、一次性注册 token、中心 mTLS
  WebSocket gateway、facts/心跳上报和只读网络快照。
- 管理端点 `POST /api/v1/agents/enrollment-tokens`、`GET /api/v1/agents`、
  `GET /api/v1/agents/{id}/snapshot` 和 `POST /api/v1/agents/{id}/network`。
- 特权网络 helper 与 agent 派发：链路、地址、路由、防火墙和 Wi-Fi 变更，fleet 路由使用
  protocol 99，并按任务保存回滚快照。
- ifupdown/ifupdown-ng、systemd-networkd、netplan、wicked 和 NetworkManager 持久化适配器；
  confirm/rollback 恢复捕获的状态。
- 签名 Incus bootstrap spec、join/init preseed 生成、apt/dnf/zypper/apk/pacman 安装适配器、
  版本 pin 和幂等 bootstrap 状态恢复。
- 用于 agent 驱动 Incus 准备的 `POST /api/v1/agents/{id}/bootstrap`。
- Agent 每 10 秒上报主机遥测，中心 1 小时环形缓冲，query/snapshot/export 端点，以及节点详情页
  的主机指标图表。
- cgroup v2 实例级 CPU/内存/pids/IO 指标、有界直方图原语，以及可选的 Linux eBPF 主机 TCP
  重传/OOM 计数器（procfs 回退）。
- Prometheus/VictoriaMetrics remote write（有界重试队列）、PromQL range 查询代理，以及带 token
  鉴权的 HTTP 服务发现和中心代理抓取端点。
- PostgreSQL/SQLite 主机任务 lease 原语、agent 证书轮换/吊销 API、带自动回滚的签名 agent
  升级，以及 JSONL/CSV 审计导出。
- 主机指标面板可在中心环形缓冲和 Prometheus/VictoriaMetrics 查询代理之间切换。
- HA dispatcher 使用数据库 lease 消费 queued `agent.network` 任务；通过 `POST /api/v1/tasks` 入队。
  agent 在离过期 30 天内自动续期证书，签名升级默认重启 systemd 服务（可用 `--no-restart` 关闭）。
- 新增 agent `/metrics` 监听 + bearer token 的 direct HTTP SD 模式，保留中心 proxy 模式。
- `incus-fleet-agent ebpf-self-test` 可验证 aya 加载器和 tracepoint 挂载；eBPF Makefile 现在能
  找到 Debian multiarch 头文件。
- 按 cgroup（含嵌套 systemd cgroup）归因的 eBPF 指标和 block 延迟直方图，输出为
  `fleet_instance_tcp_retransmits_total`、`fleet_instance_oom_kills_total` 和
  `fleet_instance_block_latency_seconds_*`。
- direct HTTP SD 已用 Prometheus 3.8 抓取 agent `/metrics` 做端到端验证；新增 `metrics-serve`
  用于独立抓取验证，并保持 bearer token 大小写敏感。
- Raft center mesh：leader 选举、带心跳续租的 agent 归属复制与中继派发、共享 PostgreSQL 会话，以及
  带主动健康检查的有序集群 endpoint 故障切换。提供强制单机恢复和 learner 重新加入 API。
- 中心提供一键 Linux agent 安装器（`agent.install`）：校验 agent/helper、按架构选择二进制、安装
  CA/sudoers/systemd、自动注册并启动服务。UI 显示该命令，不再假设 agent 已手动安装。
- UI 的 **Add node** 支持 agent-first：显示一次性注册 token 并轮询上线、调用签名 agent
  bootstrap、展示步骤进度，并显式回退 SSH。`GET /api/v1/agents` 支持 `cluster`/`member` 过滤。
- remote-write 磁盘持久化 spool、重启重放、队列/字节上限、旧文件淘汰、按中心实例隔离的 spool
  分区，以及每分区 flock 所有权和过期分区接管。默认分区无冲突（`hostname-pid`）。HA dispatcher
  新增 `agent.bootstrap` 与 `agent.snapshot` handler。
- sockops flow→cgroup 映射实现可靠的 IPv4/IPv6 TCP 重传归因，并用容器丢包验证；使用
  `BPF_F_ALLOW_MULTI` 挂载，不会替换已有 sockops 程序。OOM 计数按实例聚合；root 上下文的全局
  OOM 通过 `bpf_task_from_pid` 和 CO-RE cgroup-id 读取精确归因，并在隔离 VM 中受控验证；没有
  这些 kfunc 的内核保留 ring-buffer `/proc` 回退。
- Linux 原生 netlink 链路/地址/fleet 路由写入、可选 detach 网络回滚计时器，以及现有 UI 的
  agent 后端节点网络路由。

- 设备编辑:`PATCH /clusters/{cluster}/instances/{instance}/devices/{device}` 把修改合并进已
  挂载设备(切换网络、静态地址、容量),表单未覆盖的键原样保留;设备面板新增可展开的
  完整配置视图。
- 远程桌面工具栏新增 开机/重启/关机 按钮与客户机电源状态,关机的虚拟机可以直接在控制台里启动。
- 终端对话框现在显示服务端的 WebSocket 关闭原因(缺少 incus agent、实例未运行、控制台被占用),
  并提示虚拟机串口控制台的注意事项。

- 为无 agent 的虚拟机提供一键安装官方 incus-agent:fleet 通过 SSH(经成员主机跳板)进入客户机,
  挂载 Incus 自带的 `config` 9p 共享盘,执行官方 `install.sh`,并验证 agent 开始上报状态。客户机
  凭据仅存于请求中,不落库。目前仅支持 Linux/systemd 客户机。

### 修复

- 实例网卡编辑不再失败或静默无效:旧的 `nictype`/`parent` 网卡切换到托管网络时会自动删除
  冲突的键,而不是同时发送两者导致被拒绝;编辑表单对物理网卡禁用设备级地址(其地址来自
  局域网 DHCP),不再悄悄丢弃修改。
- 集群 TLS 证书字段同时接受内联 PEM 正文和文件路径。
- `.gitignore` 排除 `*.crt` 证书文件。
- 大文件上传不再死于 60 秒处理器超时；镜像与 ISO 导入获得更长的读写窗口及所需的
  响应写入器解包。

## 0.1.0 - 2026-11-14

多集群控制平面的首个 tag 发布。

### 新增

- 面向多个独立 Incus 集群的初始控制平面。
- 集群总览：健康状态、成员、存储、实例数量与宿主机 CPU/内存水位。
- 容器与虚拟机清单及详情视图。
- 跨集群 move/copy 迁移与回滚。
- 有状态虚拟机在线迁移。
- SQLite 与 PostgreSQL 持久化。
- 管理员 token 认证、HttpOnly 会话，以及供自动化使用的 Bearer token。
- 单个 Go 二进制，内嵌 Vue 3 UI(简体中文与英文)。
- systemd、Caddy 与示例配置等部署资产。
- 镜像导入、跨集群分发、缓存镜像刷新、克隆与重建。
- ISO 安装镜像上传与空白虚拟机启动，用于手动安装系统。
- 浏览器 Exec 终端(持久 Shell)、串行控制台与 SPICE 远程桌面。
- 实例文件管理、快照、设备挂载、密码与密钥注入。
- 共享资产库：SSH 公钥与 Shell 脚本。
- 跨集群批量注入密钥、执行脚本与电源操作。
- 无需客户机 agent 的宿主机与实例级资源监控(CPU、内存、磁盘、网络)。
- 双语文档：README、指南、API 参考、规范与社区文档均提供英文基准版和简体中文镜像,
  截图亦分 UI 语言提供。

### 变更

- 采用 GNU Affero 通用公共许可证第 3 版。
- 对文档、示例、测试与截图脱敏，移除真实基础设施信息。
