# 插件系统规范（设计草案）

[English](../plugins.md) | **简体中文**

> 状态：**设计草案，尚未实现**。本文档规定 incus-fleet 的插件系统。
> 文档已脱敏：所有地址与主机名使用 RFC 5737 文档段和 `example.com`；
> 不含凭据、内部拓扑或部署专有值。

## 1. 插件系统是什么 —— 不是什么

incus-fleet 将获得三项内置能力：

1. **插件规范** —— 任何外部服务都能实现的 manifest 格式。
2. **Web 插件管理页** —— 浏览目录、查看权限、安装、配置、启停/卸载、查看接线与健康状态。
3. **一键安装与接线编排器** —— 一键把插件部署为受管 Incus 实例，并自动与 fleet 能力对接。

插件本体**不内置到 fleet**。插件代码在自己的仓库、以自己的镜像发布、独立升级。
fleet 核心只包含：规范解析器、管理 UI、编排器、同源代理、接线注册表。

由此形成两层插件：

| 层级 | 谁维护 | 能力 | 例子 |
|---|---|---|---|
| **官方 / 深度接线** | 与 fleet 一同维护 | 可声明 `wiring` 效果，改变 fleet 核心子系统行为（指标采集目标、查询后端、通知出口等） | VictoriaMetrics、VictoriaLogs、MiBeeHive、MiBeeBell |
| **社区 / 商店** | 第三方按公开规范制作 | 只用通用能力：部署实例、分配磁盘、注入配置、代理 UI、接收事件 webhook | MiBeeNVR、监控面板、自定义工具 |

深度接线是可选的，且必须显式授权：安装页始终展示插件将接管哪些 fleet 行为。
社区插件不能改变核心行为，只能消费公开 API。

## 2. 设计原则

- **默认外部化。** 插件就是跑任意镜像的普通 Incus 实例；fleet 不检查负载内容，只管生命周期与接线。
- **复用现有原语。** 安装由 fleet 已有 API 组合而成：镜像导入、实例创建、存储卷、keys/scripts/batch、`instances/{i}/ops`、overlay 网络、健康检查。不造新执行器。
- **同源 UI。** 当前 CSP 是 `frame-ancestors 'none'`、`default-src 'self'`；插件 UI 一律经 fleet 同源反向代理呈现（并把 `frame-ancestors` 精确放宽为 `'self'`），绝不跨域加载。
- **静态配置默认权威。** 接线注册表只在插件显式声明、操作员安装时批准的配置项上覆盖静态配置。
- **权限可见。** 每个 manifest 声明能力；安装页与安装后都展示。
- **失败安全。** 安装是有检查点、可恢复、可回滚的长任务；接线只在健康检查通过后激活。

## 3. Manifest 格式

插件包是签名归档（`.fleetplugin`），内容：

```
manifest.yaml       # 必需，见下方 schema
icon.svg            # 可选，64x64
logo.svg            # 可选
README.md           # 商店列表必需
LICENSE             # 必需
images/             # 可选镜像产物/导入元数据
templates/          # 可选安装配置模板
```

`manifest.yaml`（地址均为 RFC 5737 文档段）：

```yaml
schema: fleet-plugin/v1
id: victoria-metrics                 # 小写 DNS 标签，全局唯一
name: VictoriaMetrics
version: 1.0.0
publisher: fleet-official             # 或社区发布者 ID
tier: official                        # official | community
description: 集群与 agent 遥测的时序存储
homepage: https://example.com/plugins/victoria-metrics

artifact:
  image:
    source: registry                  # registry | local-tarball
    reference: example.com/plugins/victoria-metrics:1.0.0
    architectures: [amd64, arm64]
  size_hints:
    min_memory_mb: 2048
    min_disk_gb: 20

install:
  defaults:
    cpu: 2
    memory_mb: 4096
    root_disk_gb: 20
    network: managed                 # managed | overlay
  data_disk:
    required: true
    default_gb: 200
    filesystem: ext4                 # ext4 | xfs，自动格式化
    mount_point: /data
    storage_pool_selector: true      # UI 列出 fleet 已知存储池
  templates:                         # 安装时渲染进实例配置
    - name: retention
      label: 数据保留
      type: choice
      options: [7d, 30d, 90d]
      default: 30d
    - name: listen_port
      type: port
      default: 8428

capabilities:                         # 原样展示在安装页
  deploy_instance: true
  proxy_ui:
    path_prefix: /plugins/victoria-metrics/
    upstream_port: 8428
  proxy_api:
    path_prefix: /api/v1/plugins/victoria-metrics/
    upstream_port: 8428
    paths: [/api/v1/query, /api/v1/query_range]
  event_webhook:
    path: /events
    events: [agent.connected, agent.disconnected, host.task.completed]
  wiring:                             # 仅官方插件，需批准
    - key: metrics.remote_write_url
      effect: "fleet 遥测 remote_write 指向本插件"
    - key: metrics.query_url
      effect: "fleet PromQL 代理查询本插件"

health:
  endpoint: /health
  initial_delay_seconds: 10
  interval_seconds: 15
  failure_threshold: 5

permissions:
  host: [api_proxy, event_delivery]
  fleet: [read:clusters, read:instances, read:agents]
```

社区插件的 manifest 中出现任何 `wiring` 声明，校验时直接拒绝。

## 4. 一键安装：编排步骤

安装插件是一个长任务（复用现有 host task / lease 机制），带进度、取消与回滚：

1. **校验** —— 解析并验证签名；检查架构、fleet 版本兼容性、申请的权限。
2. **选位** —— 操作员选定目标集群、存储池、网络，填写模板值（CPU/内存/磁盘/格式/挂载/数据位置）；UI 展示权限与接线摘要供确认。
3. **导入镜像** —— `POST /clusters/{cluster}/images/...`（或把打包的 tarball 载入目标集群存储）。
4. **分配数据盘** —— `POST .../storage-pools/{pool}/volumes`；大小与文件系统取自 manifest 默认值或操作员覆盖。
5. **格式化挂载** —— `POST .../instances/{instance}/ops` 的 `run-script`：建文件系统、建挂载点、写 fstab、挂载。
6. **创建实例** —— `POST /clusters/{cluster}/instances`：CPU/内存、根盘、挂数据卷、网络（overlay 或受管网络）、需要时静态地址。
7. **注入配置** —— keys/scripts/`ops` 下发渲染后的配置、端点 URL、插件专用凭据（见 §7）。
8. **启动与健康检查** —— 启动实例；轮询健康端点直至 healthy；插件内部服务完成注册。
9. **激活接线** —— 仅在健康检查通过后，把已批准的接线值写入接线注册表（§5）；fleet 子系统重载并开始使用插件。
10. **发布 UI 与导航** —— 注册代理路由与导航描述符；插件出现在 fleet Web UI。

失败时按 9→3 回滚：撤销接线、移除代理路由、停/删实例、按需删卷/镜像。
每一步都是检查点；修复底层问题后可续装。

### 安装表单（由 manifest 生成）

| 字段 | 来源 | 例子 |
|---|---|---|
| CPU / 内存 | manifest 预设 + 覆盖 | 2 vCPU / 4096 MiB |
| 根盘 | manifest 默认 | 20 GiB |
| 数据盘大小与文件系统 | manifest + 操作员 | 200 GiB、ext4、挂 `/data` |
| 存储池 | fleet 已知存储池下拉 | `ssd-pool` / `hdd-pool` |
| 网络 / 地址 | fleet overlay 或受管网络 + 静态 IP | overlay + 静态地址 |
| 插件参数 | manifest `templates` | 保留期、端口、监听地址、凭据 |

## 5. 接线注册表

fleet 核心子系统对若干配置项动态读取：静态配置给默认值，接线注册表给生效覆盖值。

```
GET/PUT /api/v1/plugins/{id}/wiring      # admin；查看/修改已批准接线
```

初始接线键（随插件增加）：

| 键 | 效果 |
|---|---|
| `metrics.remote_write_url` | 遥测 remote-write 目标（VictoriaMetrics 插件） |
| `metrics.query_url` + `metrics.query_type` | PromQL 查询代理后端 |
| `logs.query_url` | 日志检索代理后端（VictoriaLogs 插件） |
| `notifications.sink_url` | 事件 webhook 目标（bell 插件） |

> 注：接线注册表当前实现为 `internal/plugin.Registry` 的内存契约（`SetWiring` /
> `WiringValue` / `ClearWiring`），尚未持久化到数据库。进程重启后清空，静态配置回到权威。
> 持久化与「卸载后恢复默认」的落库安排在 P2。

设计规则：

- 只能设置 manifest 声明且安装时批准的键。
- 接线变更后 fleet 重载相应子系统，无需重启进程。
- 卸载插件时恢复静态配置的默认值。
- 接线变更与其他特权操作一样进审计。

## 6. 首批插件（规划中）

| 插件 | 一键安装后的效果 |
|---|---|
| **VictoriaMetrics** | 部署 VM；fleet 遥测 remote-write 与 PromQL 代理指向它；出现「指标」导航；fleet 内存 1h 指标环退场（不再是显示源）。 |
| **VictoriaLogs** | 部署 VictoriaLogs；**仅在插件安装接线时**启用 agent 日志上报链路（默认关闭，当前 Rust agent 只报指标）；出现「日志检索」导航。 |
| **MiBeeHive** | 部署 hive；注入 fleet 端点与限定凭据；hive 注册 IT/运维页面（PXE 应答、DNS 管理、内部 apt/yum 源与缓存、kite-agent 自动化）；hive 取得集群/节点清单，开始 PXE 应答分发。 |
| **MiBeeBell** | 后续阶段：部署 bell；fleet/hive/VM 事件投递到 bell webhook；出现「通知中心」导航。 |

能力互补：fleet 继续管实例/ISO/无人值守安装；hive 补网络启动（DHCP/TFTP）与应答文件分发。
hive 可经 fleet 的 batch scripts/ops 原语可选地向选定节点分发 kite-agent，但绝不自行开 SSH 通道。

## 7. 身份、凭据与权限

fleet 现状（见 `docs/architecture.md`）：bearer token 是引导凭据，首个本地账号创建后退役；
session 只带单一角色（`admin`/`operator`/`viewer`）；没有 per-user API token 与 scope。

**决定：直接实现带 scope 与有效期的 per-user API token，作为插件系统的前置条件。**
不设「专用本地账号」过渡方案——临时弱凭据会留下迁移债务和审计盲区。该能力安排在 P0/P1，
先于编排器允许安装任何插件。

Token 设计：

- per-user API token：哈希存储、显式 scope 列表、有效期、轮换、撤销、最近使用时间；从现有用户/管理界面签发。
- 插件专用 token 基于它：每次安装签发绑定 `(插件 id, 实例 id)` 的 token，只携带 manifest 声明且安装时批准的 scope；卸载立即撤销。
- `requireScope` 中间件与现有 `requireAuth`/`requireWriter`/`requireAdmin` 并列；session 在单一角色之外增加 scope。
- 插件→fleet 调用走同源代理、用 scoped token；fleet→插件调用用每次安装生成的共享密钥（webhook 用 HMAC，代理 API 用 bearer）。
- 每次插件 API 调用都带插件 id 与 scope 进审计，特权动作可追溯到具体安装，而非共享账号。

## 8. Web UI 与前端改造点

已实现端点（P1）：

| 方法 | 路径 | 用途 |
|---|---|---|
| `GET` | `/api/v1/plugins` | 已注册插件列表及管理器是否启用 |
| `GET` | `/api/v1/plugins/{id}` | 单个插件描述 |
| `GET` | `/plugin-ui/{id}/…` | 插件 UI 的同源代理（唯一的代理命名空间） |
| `GET` | `/api/v1/tokens` | 列出令牌（管理员看全部，其他账号只看自己的） |
| `POST` | `/api/v1/tokens` | 签发令牌，响应中一次性返回密钥 |
| `DELETE` | `/api/v1/tokens/{id}` | 撤销令牌 |
| `GET` | `/api/v1/tokens/scopes` | scope 词表 |

所有插件端点由 `requireScope` 保护：接受带 scope 的 API 令牌，或角色隐含该 scope 的
浏览器会话。令牌管理本身由 `read:own-tokens` 保护，所有角色都隐含该 scope——任何账号
都能管理自己的凭据；只有管理员才能操作他人令牌或申请超出自身的 scope。P2 规划：`GET /api/v1/plugins/catalog`（商店列表）与安装端点。
- 前端增加插件 store（持久导航 + 数据驱动路由）：`router.addRoute` 为每个已安装插件的 UI 路径注册路由，放在 catch-all 之前；通用 `PluginPage.vue` 渲染代理目标；图标组件映射与 `i18n.mergeLocaleMessage` 处理插件图标与标题。
- 硬编码核心导航保留作兜底，保证既有行为与测试稳定。
- 同源插件 UI 的 CSP 调整：`frame-ancestors 'self'`；`default-src` 与 `connect-src` 保持 `'self'`。外部源必须代理，禁止直接加载。

### 8.1 交互设计与信息架构

**铁律：新增能力不算交付，除非它的入口与页面内容已按交互重新设计，而不是往现有界面上叠加。**
每加一个功能，都要重新推导：用户从哪里开始、第一眼看到什么、可以忽略什么。

**入口（最多两处，绝不为每个插件加顶级菜单项）**

- 侧栏只有一个 **插件** 入口，带一个状态徽标（有更新 / 需处理）。
  已安装插件不各占顶级导航项；一律从插件详情页进入，日常高频的可额外固定一个快捷入口。
- 主操作 **安装插件** 只出现在插件列表页顶部，全站其他地方不提供安装按钮。

**两个顶层视图，绝不混成一个列表**

| 视图 | 内容 | 用途 |
|---|---|---|
| **商店** | 卡片网格：名称、发布者、一句话价值、权限数量、层级标签 | 发现与选择 |
| **已安装** | 表格：状态徽标、版本、目标集群、健康、接线摘要 | 管理既有插件 |

**安装向导：三步，每步只做一个决定**

1. *选位* —— 目标集群、存储池、网络；模板值预填，默认路径无需输入。
2. *资源* —— CPU / 内存 / 数据盘 + 预设档位；文件系统、挂载点、其余模板收进「高级」。
3. *权限与接线* —— manifest 的效果用人话呈现（「将接管 fleet 的指标采集目标」），
   每条都写清后果；单个确认勾选；然后安装，显示分步进度，失败可续装。

原始 manifest 键名、scope 标识、接线键名，任何情况下都不是 UI 的主标签。

**插件详情页：五个标签，顺序固定**

*概览*（状态、健康历史、实例链接、版本、启停/重装）· *配置*（仅操作员可改的模板值）
· *接线*（fleet 能力 → 当前目标的表格，带「恢复默认」）· *事件*（近期投递与错误）
· *危险区*（卸载、输入名称确认、明说会销毁哪些数据）。

**状态、措辞与失败规则**

- 全产品一套状态词：*安装中 / 健康 / 降级 / 已停止 / 失败*，同一词同一色。
- 每个空状态都指出唯一的下一个动作。
- 每个失败都说明：什么失败了、在哪一步、接下来怎么办。不放原始堆栈，不写「发生错误」。
- 破坏性操作必须点名被销毁的对象并要求输入名称确认，不允许只点「确定」。

**认知负担预算**

- 每个视图最多一个主按钮；次要操作收进溢出菜单。
- 顶级导航项最多七个。
- 不允许嵌套模态框。长操作进任务面板显示进度，不阻塞对话框。
- 数字必须带单位和参照；单独一个百分比不构成状态。

**强制执行**

新增用户可见能力的 PR 必须附一份信息架构说明：入口、页面结构、加载 / 空 / 错误 / 无权限
四种状态，以及它替换或合并了哪个既有控件。只加一个按钮或一张表的 PR，评审直接打回。

## 9. 商店（社区插件）

商店是社区插件包的签名列表：

- 作者按 §3 打包 `.fleetplugin` 归档，提交到商店仓库（PR + 签名验证）。
- 安装页展示发布者、版本、来源、权限与接线声明，再请用户确认。
- 社区插件不能声明 `wiring`；只获得通用能力（实例/卷/网络/配置/代理/事件）。
- 例子：MiBeeNVR 可打包成商店插件，在 fleet 上一键安装，无需修改 fleet 核心。

## 10. 分阶段实施计划

- **P0 —— 契约与凭据。** ✅ **已完成。** `internal/plugin`（manifest 解析/校验、注册表、生命周期、事件分发、接线注册表）；`plugins:` 配置段；带 scope 与有效期的 per-user API token（迁移 10、`auth.TokenStore`、`requireScope`），含绑定 `(插件 id, 实例 id)` 的插件专用 token。
- **P1 —— 服务端接入点。** ✅ **已完成。** 插件路由与同源代理经 `httpapi.Deps` 挂载；`GET /api/v1/plugins`、`/api/v1/plugins/{id}`、`/plugins/…`、`/plugin-ui/…` 与 `/api/v1/tokens` 系列已上线。CSP 放宽为 `frame-ancestors 'self'`、`X-Frame-Options: SAMEORIGIN` 以允许渲染代理页面。P1 未完成部分：实例创建、健康轮询、安装后激活接线（依赖 P2 编排器）。
- **P2 —— 编排器。** 带检查点的安装任务、manifest 驱动模板、卷/实例/配置工作流、回滚；用现有 fleet API 完成选址。
- **P3 —— 数据驱动 UI。** 插件 store、`router.addRoute`、`PluginPage.vue`、图标映射、i18n 合并、CSP 调整。**交互按 §8.1 设计**：商店与已安装分成两个视图、三步安装向导、固定五标签详情页、全局唯一状态词，以及每页的四种状态（加载/空/错误/无权限）都要设计——不是在既有界面上叠加。
- **P4 —— 事件与按需遥测。** agent/集群/任务事件分发。**agent 日志/遥测上报默认关闭，仅由插件安装接线启用**——安装 VictoriaMetrics 才开指标 remote-write，安装 VictoriaLogs 才开 Rust agent 日志上报链路；没有安装插件时 agent 不上报任何额外数据，fleet 保持基线行为。
- **P5 —— 首批插件与商店。** VictoriaMetrics、VictoriaLogs、MiBeeHive，后续 MiBeeBell；社区商店与签名强制校验。

## 11. 不在范围内

- 编译进 fleet 二进制或与 fleet 共享进程的插件。
- 不经同源代理的跨域 UI 嵌入。
- fleet Raft mesh 内的通用复制 KV 或消息总线（初始阶段）——插件自己管状态；fleet 只存注册表与接线元数据。
- 插件数据的自动备份/恢复（由插件自己负责）。
