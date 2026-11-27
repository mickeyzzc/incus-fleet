# 插件系统

[English](../en/plugins.md) | **简体中文**

> 状态：**P0–P5 已实现**。manifest 解析与注册表、带作用域的 API token、同源代理、
> 基于任务的安装编排器、持久化接线注册表、事件 webhook、数据驱动 UI、内置目录与
> 签名社区包均已上线。各阶段状态见 §10，当前限制见 §11。
> 文档已脱敏：所有地址与主机名使用 RFC 5737 文档段和 `example.com`；
> 不含凭据、内部拓扑或部署专有值。

## 1. 插件系统是什么 —— 不是什么

incus-fleet 提供三项内置能力：

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

插件包是 `.fleetplugin` 归档，内容：

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
    path_prefix: /plugin-ui/victoria-metrics/
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

`internal/plugin.ParseManifest` 强制校验：

- `schema` 必须是 `fleet-plugin/v1`，`id` 必须是小写 DNS 标签，`tier` 为
  `official` 或 `community`。
- `install.defaults.network` 为 `managed` 或 `overlay`；`data_disk.filesystem`
  为 `ext4` 或 `xfs`；挂载点必须是绝对路径，必需数据盘容量必须为正。模板类型
  支持 `text`、`choice`、`port`、`bool`。
- 代理前缀必须以 `/` 开头和结尾且两者不同；webhook 路径必须是绝对路径并至少
  列出一个事件。
- 社区插件不得声明 `wiring`；每条接线都必须有键与人类可读的效果说明。

`manifest.yaml` 是规范格式，旧版 `plugin.json` 目录项（仅接入或带安装脚本）
仍受支持。`scripts/pack-plugin.sh` 会校验两种格式，并在有签名密钥时写入
`manifest.yaml.sig` 旁车文件：对 manifest 原始字节做 base64 HMAC-SHA256，
可带 `sha256:` 前缀。密钥来自 `plugins.signing_key_file` 或
`INCUS_FLEET_PLUGINS_SIGNING_KEY`。配置密钥后，未签名的社区包会被目录跳过、
被任务安装接口拒绝；未配置密钥时没有信任锚，本地开发包仍可使用。内置 manifest
不签名。

## 4. 一键安装：编排步骤

安装器（`internal/plugininstall`）把每次安装保存为类型为 `plugin.install` 的持久化
host task。十个步骤顺序执行；每步成功后把检查点写入 `HostTask.Progress`，
并追加一条 `HostTaskEvent`：

1. **validate** —— 重新校验 manifest；若配置签名密钥则验证签名；社区插件不得
   带 `approvals`。
2. **placement** —— 使用请求的 `cluster`/`member`，否则选第一个在线集群与成员。
3. **image** —— 把 `artifact.image.reference` 解析为目标集群上已有镜像的指纹；
   任务本身不导入镜像。
4. **data_disk** —— 当 `data_disk_gb`（或 manifest 中必需数据盘的默认值）大于 0 时，
   在 `storage_pool`（默认 `default`）上创建自定义卷 `plugin-<id>-data`，创建实例时
   挂到 `mount_point`。
5. **format_mount** —— 通过 `instances/{i}/ops` 的 `run-script` 对请求中的
   `instance` 名称执行 `plugin.DataDiskScript`：按需建文件系统、挂载卷并写入 fstab。
6. **create_instance** —— 创建名为 `instance`（默认 `fleet-<id>`）的容器，
   CPU、内存、根盘与网络取自请求或 manifest。
7. **inject_config** —— 渲染 manifest `templates`（未知键报错），用临时脚本写入
   `/etc/fleet-plugin/config.env` 并通过 `ops` 执行。
8. **health** —— 延迟 `initial_delay_seconds` 后探测
   `<upstream><health.endpoint>`，按 `interval_seconds` 重试最多
   `failure_threshold` 次；2xx 后才激活接线。
9. **activate_wiring** —— 为已安装插件注册描述符，随后持久化并激活每个已批准键，
   同时记录静态默认值。
10. **publish** —— 写入 `installed_plugins` 记录，签发绑定 `(插件 id, 实例 id)`、
    仅有 `read:plugins` 作用域的插件 token，并为声明的事件注册出站 webhook 订阅者。

失败或取消时按已完成步骤逆序回滚：删除已安装记录、撤销接线、删除实例与数据卷，
并把检查点倒回数据盘步骤，使续装重放这些步骤。`Resume` 只接受失败或已取消的任务；
同一插件 id 同时只能有一个排队/运行中的任务。未启用 HA 时任务在服务端进程内执行；
启用 HA 时由常规 host task 调度器领取。

### 任务接口

| 方法 | 路径 | 作用域 | 用途 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/plugin-installs` | `manage:plugins` | 入队安装；返回 `202` 与 `{"task": ...}` |
| `GET` | `/api/v1/plugin-installs` | `read:plugins` | 列出安装任务 |
| `GET` | `/api/v1/plugin-installs/{id}` | `read:plugins` | 单个任务（含事件） |
| `POST` | `/api/v1/plugin-installs/{id}/cancel` | `manage:plugins` | 在下一个检查点协作取消；`204` |
| `POST` | `/api/v1/plugin-installs/{id}/resume` | `manage:plugins` | 重新入队失败/已取消任务 |

POST 请求体可带 `id`（必填，目录插件 id）、`name`、`cluster`、`member`、
`instance`、`upstream`、`cpu`、`memory`、`disk`、`network`、`values`（模板覆盖）、
`data_disk_gb`、`filesystem`、`mount_point`、`storage_pool`、`approvals`（接线键）、
`defaults`（要恢复的静态值）、`health_delay_ms`、`health_interval_ms`、
`health_failure_threshold`、`signature`（manifest 字节的 base64 HMAC）。任务状态字段
有 `id`、`plugin_id`、`state`、`progress`、`attempt`、`error`、`created_at`、
`updated_at`，详情接口还含 `events`。状态取值为 `queued`、`dispatched`、`running`、
`succeeded`、`failed`、`canceled`。常见错误：`400 unknown_plugin`、
`403 invalid_signature`、`409 install_in_progress`、`409 invalid_state`。

旧接口 `POST /api/v1/plugins/install` 保留：它同步部署并返回 `201` 与一次性 token，
目前仍是 Web 向导使用的路径；它没有检查点，也不支持取消/续装。

### 安装表单（由 manifest 生成）

| 字段 | 来源 | 例子 |
|---|---|---|
| CPU / 内存 | manifest 预设 + 覆盖 | 2 vCPU / 4096 MiB |
| 根盘 | manifest 默认 | 20 GiB |
| 数据盘大小与文件系统 | manifest + 操作员 | 200 GiB、ext4、挂 `/data` |
| 存储池 | fleet 已知存储池下拉 | `ssd-pool` / `hdd-pool` |
| 网络 | manifest 默认值或请求 `network` | `managed` 或 `overlay` |
| 插件参数 | manifest `templates` | 保留期、端口、监听地址、凭据 |

## 5. 接线注册表

fleet 核心子系统对若干配置项动态读取：静态配置给默认值，接线注册表给生效覆盖值。
已批准的覆盖保存在 `plugin_wiring` 表（迁移 15），启动时加载；内存注册表只保存
当前生效值。

```
GET /api/v1/plugins/{id}/wiring          # read:plugins；查看已批准接线
PUT /api/v1/plugins/{id}/wiring          # manage:plugins；修改已批准接线
```

`GET` 返回 `key`、`value`、`default_value`、`effect`、`enabled`、`approved`。
`PUT` 接受 `{"values":[{"key","value","enabled"}]}`：键必须被 manifest 声明且安装时
批准，值必须是绝对 http(s) URL（末尾 `/` 会被去掉）。传 `"enabled": false`
即恢复捕获的默认值并通知消费者。变更会作为 `plugin.wiring.update` 记入审计，
并发布 `plugin.wiring_changed` 事件。

当前被消费的键，以及内置 manifest 声明的键：

| 键 | 效果 | 消费方 |
|---|---|---|
| `metrics.remote_write_url` | 遥测 remote-write 目标 | fleet 启动/remote writer |
| `metrics.query_url` + `metrics.query_type` | PromQL 查询代理后端 | 指标代理 |
| `logs.query_url` | 日志检索代理后端 | 日志查询代理 |
| `bootstrap.answer_file_url` | 无人值守安装的应答文件来源 | `hive` 声明，尚无消费方 |
| `notifications.sink_url` | 事件 webhook 目标 | `bell` 声明，尚无消费方 |

设计规则：

- 只能设置 manifest 声明且安装时批准的键。
- 同一键由第一个声明它的插件生效；值会规范化为 http(s) URL。
- 接线变更后 fleet 重载相应子系统，无需重启进程。
- 卸载与「恢复默认」都会把捕获的默认值写回并禁用该行；批准记录保留，
  重新安装无需二次批准。
- 接线变更与其他特权操作一样进审计。

## 6. 首批插件

内置 manifest 只有描述符——fleet 从不运行插件代码。目录暴露以下 id，每个声明
一条接线并带一个安装端口；镜像引用使用 `example.com` 占位符，任务安装前需先在
集群上导入镜像（见[一键安装插件](plugins-one-click.md)）：

| 插件 | 目录 id | 端口 | 声明的接线 |
|---|---|---|---|
| **VictoriaMetrics** | `vm` | 8428 | `metrics.query_url` |
| **VictoriaLogs** | `vmlogs` | 9428 | `logs.query_url` |
| **MiBeeHive** | `hive` | 8080 | `bootstrap.answer_file_url` |
| **MiBeeBell** | `bell` | 9090 | `notifications.sink_url` |

安装 `vm` 会把 PromQL 代理切到 VictoriaMetrics；安装 `vmlogs` 会启用日志检索代理。
这两个 manifest 都不会打开 remote-write 或 Rust agent 日志上报（见 §11）。另外两个键
只是声明，便于安装页展示将接管的能力，fleet 尚未消费。

能力互补：fleet 继续管实例/ISO/无人值守安装；hive 补网络启动（DHCP/TFTP）与应答文件分发。
hive 可经 fleet 的 batch scripts/ops 原语可选地向选定节点分发 kite-agent，但绝不自行开 SSH 通道。

## 7. 身份、凭据与权限

带 scope 的 per-user API token 已在 P0/P1 实现，是插件系统的前置条件（见
`docs/en/architecture.md`）：引导 bearer token 在首个本地账号创建后退役，session 只带
单一角色（`admin`/`operator`/`viewer`）并隐含对应 scope。不存在临时的本地账号凭据。

已实现的 token 设计：

- per-user API token 哈希存储，带显式 scope 列表、有效期、轮换、撤销与最近使用时间。
- 插件专用 token 绑定 `(插件 id, 实例 id)`，只带 `read:plugins` 作用域；卸载立即撤销。
  任务安装器与旧安装接口都会签发该 token。
- `requireScope` 与 `requireAuth`/`requireWriter`/`requireAdmin` 并列；插件读取用
  `read:plugins`，变更用 `manage:plugins`。
- 插件→fleet 调用使用 scoped token；fleet→插件 webhook 带由签名密钥派生的 HMAC
  签名。同源代理在转发前剥离 Fleet 会话 Cookie。
- 特权接线变更以操作者用户名记入审计。

## 8. Web UI 与前端改造点

管理端点都从 `httpapi.Deps` 挂载，并由 `requireScope` 保护：接受带 scope 的
API token，或角色隐含该 scope 的浏览器会话；入站事件端点改用 HMAC 与可选 bearer
token 鉴权。令牌管理由 `read:own-tokens` 保护，
所有角色都隐含该 scope——任何账号都能管理自己的凭据；只有管理员才能操作他人
令牌或申请超出自身的 scope。

| 方法 | 路径 | 作用域 | 用途 |
|---|---|---|---|
| `GET` | `/api/v1/plugins` | `read:plugins` | 插件列表及管理器是否启用 |
| `GET` | `/api/v1/plugins/catalog` | `read:plugins` | 目录项（内置 + `plugins.dir` 下的包） |
| `GET` | `/api/v1/plugins/{id}` | `read:plugins` | 单个插件描述 |
| `POST` | `/api/v1/plugins/install` | `manage:plugins` | 旧版同步安装；`201` 返回一次性 token |
| `POST` | `/api/v1/plugins/{id}/toggle` | `manage:plugins` | 启用/停用已安装插件 |
| `GET`、`PUT` | `/api/v1/plugins/{id}/wiring` | `read:plugins`、`manage:plugins` | 查看/修改已批准接线 |
| `POST` | `/api/v1/plugin-installs` | `manage:plugins` | 入队任务安装 |
| `GET` | `/api/v1/plugin-installs`、`/api/v1/plugin-installs/{id}` | `read:plugins` | 列出/查看安装任务 |
| `POST` | `/api/v1/plugin-installs/{id}/cancel`、`.../resume` | `manage:plugins` | 取消/续装任务 |
| `DELETE` | `/api/v1/plugins/{id}` | `manage:plugins` | 卸载；恢复接线默认值并撤销 token |
| `POST` | `/api/v1/plugins/events` | HMAC + 可选 bearer | 入站插件事件（仅在启用时挂载路由） |
| `GET` | `/api/v1/logs/query` | `requireAuth` | 日志检索代理；未配置后端时 `503 unavailable` |
| `GET` | `/plugin-ui/{id}/…` | `read:plugins` | 插件 UI 的同源代理（唯一的代理命名空间） |
| `GET`、`POST`、`DELETE` | `/api/v1/tokens`、`/api/v1/tokens/{id}` | `read:own-tokens` | 令牌管理；scope 词表在 `/api/v1/tokens/scopes` |

配置键（均在 `plugins:` 下）：`enabled`（默认 false）、`dir`（默认 `data/plugins`）、
`health_interval`（默认 `15s`）、`signing_key_file`（或环境变量
`INCUS_FLEET_PLUGINS_SIGNING_KEY`），以及 `events.enabled`（默认 false）、
`events.path`（默认 `/api/v1/plugins/events`）、`events.secret_file`（启用事件时必填）、
`events.bearer_token_file`（可选）。`plugins.entries` 可声明 `id`、`enabled`、
`upstream` 和/或 `upstream_port`、`bearer_token_file`、`role`、`ui`（`path` 必须位于
`/plugin-ui/` 下）。

- 前端用 `router.addRoute` 为每个已安装插件注册数据驱动路由，放在 catch-all 之前；
  `PluginPage.vue` 渲染代理目标；图标组件映射与 `i18n.mergeLocaleMessage` 处理图标与标题。
- 硬编码核心导航保留作兜底，保证既有行为与测试稳定。
- 同源插件 UI 的 CSP 调整：`frame-ancestors 'self'`；`default-src` 与 `connect-src`
  保持 `'self'`。外部源必须代理，禁止直接加载。

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

**状态（P3）：**侧栏单入口与徽标、商店/已安装双视图、三步安装向导、任务面板、固定
五标签详情页，以及统一状态词（`installing`、`healthy`、`degraded`、`stopped`、
`failed`）均已实现。尚未由服务端支撑的界面部分见 §11。

## 9. 商店（社区插件）

目录把内置描述符与解压到 `plugins.dir/<id>/` 下的社区包合并：

- 作者用 `scripts/pack-plugin.sh` 按 §3 打包 `.fleetplugin` 归档，提交到商店仓库
  （PR + 签名验证）。
- 安装页展示发布者、版本、来源、权限与接线声明，再请用户确认。
- 社区插件不能声明 `wiring`；只获得通用能力（实例/卷/网络/配置/代理/事件）。
  配置签名密钥后，社区 `manifest.yaml` 必须带有效的 `manifest.yaml.sig`。
- 例子：MiBeeNVR 的示例包位于 `plugins/examples/mibee-nvr/`，无需修改 fleet 核心
  即可安装。

## 10. 实现状态

- **P0 —— 契约与凭据。** ✅ **已完成。** `internal/plugin`（manifest 解析/校验、注册表、生命周期、事件分发、接线注册表）；`plugins:` 配置段；带 scope 与有效期的 per-user API token（迁移 10、`auth.TokenStore`、`requireScope`），含绑定 `(插件 id, 实例 id)` 的插件专用 token。
- **P1 —— 服务端接入点。** ✅ **已完成。** 插件路由与同源代理经 `httpapi.Deps` 挂载；`GET /api/v1/plugins`、`GET /api/v1/plugins/{id}`、`/plugin-ui/…` 与 `/api/v1/tokens` 系列已上线。CSP 放宽为 `frame-ancestors 'self'`、`X-Frame-Options: SAMEORIGIN` 以允许渲染代理页面。
- **P2 —— 编排器。** ✅ **已完成。** `internal/plugininstall` 以带进度事件的 `plugin.install` host task 执行带检查点的安装，支持协作取消、续装、逆序回滚、manifest 模板、数据盘格式化与配置注入、健康检查通过后才激活接线；迁移 15 持久化接线；`manifest.yaml` 优先并兼容 `plugin.json`；社区包支持 HMAC 签名校验。
- **P3 —— 数据驱动 UI。** ✅ **已完成。** 插件 store + `router.addRoute`、`PluginPage.vue`、图标映射、i18n 合并；侧栏单入口与徽标；商店/已安装双视图；三步安装向导、任务面板、固定五标签详情页、统一状态词；按 §8.1 提供加载/空/错误/无权限状态。
- **P4 —— 事件与按需遥测。** ✅ **已完成。** agent、host task、迁移与实例状态事件会分发给已注册订阅者；出站签名 webhook、按需启用的入站 `POST /api/v1/plugins/events` 与 `GET /api/v1/logs/query` 代理已上线。日志上报与日志检索默认关闭，只有已安装插件的 `logs.query_url` 接线启用检索代理。Rust agent 的遥测 envelope 未改动。
- **P5 —— 首批插件与商店。** ✅ **已完成。** `vm`、`vmlogs`、`hive`、`bell` 的内置描述符；MiBeeNVR 社区示例；`scripts/pack-plugin.sh` 生成目录签名旁车文件。

## 11. 已知限制

- Web 向导仍调用旧版同步接口 `POST /api/v1/plugins/install`。§4 的任务接口已提供，
  但浏览器端尚未接入；任务面板目前展示向导的本地进度，而非轮询任务 id。
- 详情页的「配置保存」（`PUT /api/v1/plugins/{id}/config`）与「按 key 恢复默认」
  （`DELETE /api/v1/plugins/{id}/wiring/{key}`）调用的服务端路由并不存在。当前请用
  `PUT /api/v1/plugins/{id}/wiring` 并传 `"enabled": false` 恢复默认值。
  `GET /api/v1/plugins/{id}` 返回的描述符也没有接线、事件、健康历史与 config 字段，
  因此这三个标签只显示空状态。
- 接线行与批准记录会持久化，但重启时只重新注册内置描述符。已安装的社区插件需要
  重新注册描述符，其接线才会加载、接线接口才会响应；动态注册的事件订阅者也只在
  内存中存在。
- `hive` 与 `bell` 的接线键仅是声明：fleet 尚无子系统消费
  `bootstrap.answer_file_url` 或 `notifications.sink_url`。内置 manifest 均未声明
  `metrics.remote_write_url`，remote-write 仍需静态配置。
- 需要数据盘的插件应在请求中给出明确的 `instance` 名称：格式化/挂载步骤在创建实例
  之前对该名称执行。任务会解析镜像别名，但不导入镜像。
- agent 日志上报尚未实现：Rust agent 仍只上报指标，没有日志 envelope 或发送端可供修改。
- 只有在未配置签名密钥时社区目录包才允许无签名；在生产环境接收第三方包前，请先配置
  `plugins.signing_key_file`（或环境变量覆盖）。

## 12. 不在范围内

- 编译进 fleet 二进制或与 fleet 共享进程的插件。
- 不经同源代理的跨域 UI 嵌入。
- fleet Raft mesh 内的通用复制 KV 或消息总线（初始阶段）——插件自己管状态；fleet 只存注册表与接线元数据。
- 插件数据的自动备份/恢复（由插件自己负责）。
