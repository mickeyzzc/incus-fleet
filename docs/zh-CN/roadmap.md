# 路线图

[English](../roadmap.md) | 简体中文

本文档记录 incus-fleet 的规划工作，已做脱敏：不含任何内部地址、主机名、凭证或拓扑信息。
详细内部审计记录不入库。

当前版本已覆盖：镜像导入与分发、ISO 安装、实例创建/快照/克隆/重建、跨集群迁移、
在线终端与基于 QMP 的图形桌面控制台、脚本/密码/密钥运维、共享资产库、批量操作，以及集群/实例/主机
资源监控。以下工作在此基础之上扩展。

## 1. 认证、用户与访问控制 —— **已交付**

随 `v0.1.0-userauth` 交付。共享 admin token 现在只是引导凭证，创建第一个本地账号后
自动退役。`INCUS_FLEET_BOOTSTRAP_ADMIN_USERNAME` / `..._PASSWORD` 用于创建首个管理员。

已交付：

- 本地账号（bcrypt 哈希），创建/更新/禁用/删除、强制改密、失败次数锁定。
- `admin` / `operator` / `viewer` 三角色，在每条变更路由上服务端强制校验，而非仅隐藏 UI。
- 带身份的 server-side 会话，通过数据库在多个 center 间共享。
- 自助修改密码与当前会话查看。
- 最后一个管理员不能被删除或降级。

仍在规划：

- **API token**：按用户、带权限与有效期的 token。
- **会话吊销 UI**：列出并吊销某账号的全部会话。
- **登录审计流**：持久化登录历史与失败原因。
- **可选 SSO**：OIDC 接入，映射到同一套角色。

迁移路径：共享 admin token 保留一个版本作为引导凭证，随后要求创建第一个管理员账号并弃用 token。

## 2. UI 信息架构（跨集群 / 单集群 / 节点）

当前导航混用了不同作用域，将重组为三个明确层级。指导原则：
**对象出现在能操作它的最小作用域；跨节点、跨集群的资源上提到统一的「Fleet」视图。**

### Fleet 作用域（跨集群）

- **Fleet 仪表盘**：所有集群的聚合健康、容量、告警。
- **Fleet 实例**：所有集群实例统一表格，带集群/实例/节点列；支持筛选、批量电源/密钥/脚本
  操作、发起迁移（由当前「所有实例」页演进）。
- **Fleet 资产**：共享 SSH 密钥、脚本、镜像、ISO，可分发到任意集群。
- **Fleet 迁移**：所有迁移任务，支持取消/重试与进度展示。
- **Fleet 拓扑 / 网络**：跨集群可达性与 overlay（见第 5 节）。
- **Fleet 用户、角色、会话、审计**：第 1 节的管理入口。

**已交付部分**：集群概览只列出**受管网络**，物理网卡不再出现在集群层级；
物理网卡、地址、路由、防火墙与 Wi-Fi 全部位于节点页。

### 集群作用域（单集群）

- 集群概览：成员、健康、计数、资源容量。
- 该集群的实例（创建、快照、克隆、重建、控制台/桌面）。
- 存储池/卷、profile、托管网络、支持的 network forward/负载均衡/ACL、集群本地镜像。
- 集群成员：添加/移除、角色、故障域。

### 节点作用域（单成员）

- 主机网络：物理网卡、地址、路由、防火墙、Wi-Fi —— **从集群视图移出**。
  网卡状态是节点本地信息，不应出现在集群概览上。
- 主机资源与指标（CPU、内存、磁盘、TCP 重传、OOM、块设备延迟），由 agent 提供（见第 4 节）。
- 该节点的操作历史/审计。
- 节点升级与 agent 生命周期（注册、撤销、轮换证书）。
- 按节点的实例分布视图。

### 需要统筹的跨作用域资源

天然属于 fleet 的资源应有统一权威视图，而不是在每个集群下重复：

- **镜像 / ISO**：单一目录，带所在集群标签、分发/复制操作。
- **卷 / 存储**：fleet 存储视图，可下钻到集群与池。
- **网络 / IP / 地址**：fleet IPAM 与可达性视图，节点级网卡细节仍留在节点作用域。
- **任务 / 审计**：统一的持久任务队列与审计流，可按集群/节点/用户过滤。

## 3. Bug 修复与加固 backlog（来自 2026-11-20 审计）

以下发现均已对照源码核实，公开 tracking 不含内部细节。

### 高优先级

- 共享 DB 驱动的 schema 迁移做到原子/幂等：崩溃或并发首启不能永久破坏启动。
- 所有带参数的查询都做占位符重绑定，使 PostgreSQL 部署的集群注册与资产 CRUD 可用。
- 限制 SPICE 监听与桥接：不再无认证监听所有网卡，桥接不允许拨任意端口。
  （Web UI 的图形控制台已改为通过 unix socket 的 QMP 监视器驱动；SPICE 仅保留
  给原生客户端场景。）
- 修复 agent 撤销：心跳不能把已撤销 agent 改回 online。
- host task lease 写回按 owner  fencing；worker 崩溃后回收 `running` 任务；续租失败硬中止。
- 迁移去重从进程内存移到数据库。
- 迁移进度更新单调化，避免尾随回调复活失败任务或清空错误。
- 迁移执行与应用生命周期绑定：关闭 store 前等待在途迁移；终态用新 context 写入。
- 修复 agent 后端防火墙规则因注释错位丢失协议字段。
- 让 `statusRecorder` 转发 `SetWriteDeadline`，避免长节点加入请求被全局写超时截断。
- 前端：会话过期跳登录；破坏性删镜像加确认；资产删除失败要报错；轮询 rejection 要捕获；
  批量选择在轮询间保持；fleet 状态自动刷新。

### 中优先级

- 限制 rqlite 连接池与发现开销。
- remote-write fsync 移出 agent 读循环；上报丢弃的遥测。
- host 操作改用轻量成员查询，而非每次完整 summary。
- 健康检查不要拆掉仍有在途操作的 client。
- agent 断连/重连状态更新按连接作用域隔离。
- 增加遥测/审计保留策略与会话清理调度。
- 加固请求体/错误处理：5xx 不返回内部错误原文，增加 HSTS。
- 每集群流量聚合避免每实例 N+1 调用。
- 修复幂等性：重复密钥注入、脚本同名冲突、地址配置与临时文件竞争、Wi-Fi PSK 文件权限。
- auth 与注册接口增加限流与校验加固。
- 前端后续：轮询退避/在途保护、迁移取消/重试、二次确认、表单校验、i18n key 对齐、
  可访问性、响应式表格、相对时间、分页边界、全局错误处理、终端重连、加载/空状态。

## 4. Rust agent：主机监控、node-exporter 兼容、eBPF —— **部分交付**

已交付：agent 已部署到两个集群，遥测链路端到端可用。`GET /api/v1/agents` 显示两个成员
在线；center 同时在原生 `fleet_*` 之外接受 node-exporter 兼容的 `node_*` 指标，
现有 Prometheus 面板无需修改即可使用。节点页现在显示实时 CPU、负载、内存与 agent
运行时间，不再显示 "telemetry is not enabled"。

已交付内容：

- Agent 注册（CA + 一次性 token）、两台主机的 systemd 单元。
- `node_*` 兼容：`node_cpu_seconds_total`、`node_memory_MemTotal_bytes`、
  `node_memory_MemAvailable_bytes`、`node_memory_MemUsed_bytes`、`node_load1`、
  `node_boot_time_seconds`、`node_network_{receive,transmit}_bytes_total`、
  `node_netstat_Tcp_RetransSegs`、`node_vmstat_oom_kill`。
- 复用既有 WebSocket 遥测路径摄取，center 的名称白名单扩展为接受 `node_`。

仍在规划：

agent 已采集基础 `fleet_*` 指标（CPU 比率、内存、负载、uptime、网络字节、TCP 重传、
OOM、采集耗时），并有 eBPF 采集器（sockops、tcp 重传、oom、块 I/O 延迟，可回退
procfs/cgroup），以及带 token 保护的 `/metrics` 端点。

规划工作：

- **node-exporter 兼容层**：在现有 `fleet_*` 之外，按标准 `node_*` 名称与标签暴露常见主机
  指标（文件系统、CPU、内存、网络、磁盘、systemd、时间），让已有 Prometheus 面板与告警
  规则无需修改即可使用。提供兼容开关并文档化映射关系。
- **指标完整性**：逐 CPU、挂载点使用率、磁盘 IOPS/延迟、TCP 连接状态、进程数、熵、时间同步
  等 node-exporter 基线指标。
- **远端摄取**：fleet center 已通过 agent WebSocket 接收 `TelemetryBatch`；保留该路径，并新增
  到 Prometheus/VictoriaMetrics 的可配 remote-write。两套命名体系的序列校验与标签清洗保持一致。
- **eBPF 扩展**：保留现有探针；在内核支持时增加 TCP 建连延迟/错误与 DNS 延迟追踪，带能力探测
  与优雅 procfs 回退。提供可选预编译 eBPF 对象；基础运行绝不依赖它。
- **安全**：保留 mTLS 身份与抓取 token 保护；增加抓取标签基数白名单。
- **UI**：在第 2 节节点作用域视图展示主机指标，支持下钻与 PromQL 历史图表。

兼容目标：Prometheus node-exporter 指标名与标签约定，条件允许时在 CI 用真实 node-exporter 校验。

## 5. 跨节点 / 跨集群网络 —— **已交付（迁移 DHCP 交接）**

已交付:跨集群迁移不再把 guest 滞留在旧子网。传输完成后,fleet 会要求 guest 释放 DHCP
租约并从它现在所属的集群重新获取,地址、默认路由与连通性无需人工干预即可恢复。

已在生产集群上双向实测:

| 方向 | 迁移前 | 迁移后(自动) | 网关 |
| --- | --- | --- | --- |
| c63 → c62 | `203.0.113.18/24` | `198.51.100.18/24` | 0% 丢包 |
| c62 → c63 | `198.51.100.18/24` | `203.0.113.18/24` | 0% 丢包 |

该步骤覆盖 `dhclient`、`udhcpc`、`systemd-networkd`、`NetworkManager`,且是尽力而为:
使用静态地址或没有 DHCP 客户端的 guest 只记录警告,迁移仍然成功。

仍在规划:

### 调研结论

- Incus **bridge** 网络是节点本地的：每个成员各自跑 dnsmasq 与 NAT，集群间没有内置 L2 拉伸。
- 唯一原生 overlay 是 **OVN**（Geneve），Incus 持续维护，但需要外部 ovn-central/ovn-host、
  DB 仲裁节点与共享 L2 底层，对少量节点过重。
- K8s CNI（Calico/Cilium/Flannel/Weave/Kube-OVN）离开 K8s 难以适配 Incus，运维成本高。
- WireGuard mesh（WireGuard、Nebula、NetBird、ZeroTier）提供扁平 L3 可达；ZeroTier 还提供 L2。
  建议跑在主机层配合子网路由，而非每个 guest 内。
- 跨集群迁移的真正痛点是 **guest DHCP 租约滞留**（RFC 2131：客户端保留旧地址直到过期；
  Incus bridge 默认租约 1 小时），无需 overlay 即可解决。

### 决策

1. **不自研 overlay**：该问题已有成熟方案；自研要永久承担密钥管理、MTU/分片、组播、故障切换、
   NAT 穿透与可观测性。
2. 保持每集群原生 bridge 且**子网不重叠**；用静态/固定 DHCP 分配修复跨子网迁移，可选配合
   DHCP relay 与统一 DNS zone，并缩短租约作为缓解。
3. 在主机层增加可选 **WireGuard mesh**（Nebula 或 NetBird 自托管，或原生 WireGuard），提供跨集群
   管理与业务可达，作为 fleet 网络能力暴露。
4. **OVN** 作为未来可选能力，面向多租户逻辑网络需求，使用 Incus 集成形式而非独立部署。
5. 构建 fleet 级 **IPAM、可达性与拓扑可视化**，以及迁移感知的 DHCP/DNS 处理 —— 这才是控制面
   真正能补的空白。

## 6. 交付原则

- 每个面向用户的改动都附带浏览器级操作测试与脱敏用户手册条目；截图脱敏后再入文档。
- 三种数据库部署形态（单节点 SQLite、多节点 rqlite、多节点 PostgreSQL）对每个新功能都保持可用。
- 所有新增后台任务的正确性不能以进程内存协调为前提（共享 DB 场景硬要求）。
- 安全相关改动（认证、SPICE、token）发布前做专项审查与 secret 扫描。
