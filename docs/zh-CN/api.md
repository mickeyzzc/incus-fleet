# HTTP API

[English](../api.md) | **简体中文**

基础路径：`/api/v1`。除特别说明外，所有请求体与响应体均为 JSON。同一台服务器
还在 `/` 提供 Vue SPA。

## 认证

服务器只有一个共享的管理员 token。有两种认证方式：

1. **浏览器会话。** 用 token 调用 `POST /api/v1/auth/login`；响应会设置一个名为
   `incus_fleet_session` 的 `HttpOnly`、`SameSite=Lax` cookie。该 cookie 的有效期为
   12 小时。当请求本身走 TLS，或携带 `X-Forwarded-Proto: https`（由 Caddy 发送）
   时，cookie 会被标记为 `Secure`。
2. **Bearer token。** 在受保护的端点上发送 `Authorization: Bearer <admin token>`。
   该方式面向自动化场景，无需登录步骤。

`GET /api/v1/healthz`、`GET /api/v1/version`、`POST /auth/login` 和
`POST /auth/logout` 是公开的。`/clusters`、`/instances`、`/migrations`、`/keys`、
`/scripts`、`/batch` 下的所有端点，以及 `GET /auth/session`，都需要认证。

```bash
curl -sS https://fleet.example/api/v1/clusters \
  -H "Authorization: Bearer $INCUS_FLEET_AUTH_TOKEN"
```

### 登录

```http
POST /api/v1/auth/login
Content-Type: application/json

{"token": "<admin token>"}
```

```json
{
  "authenticated": true,
  "expires_at": "2026-11-12T09:30:00Z"
}
```

token 无效时返回 `401`，错误码为 `invalid_token`。

### 会话

```http
GET /api/v1/auth/session
```

```json
{"authenticated": true}
```

### 登出

```http
POST /api/v1/auth/logout
```

返回 `204 No Content` 并使该 cookie 过期。登出是幂等的。

## 端点

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| GET | `/healthz` | 否 | 存活/就绪探针 |
| GET | `/version` | 否 | 构建版本 |
| POST | `/auth/login` | 否 | 用管理员 token 换取会话 cookie |
| POST | `/auth/logout` | 否 | 移除当前会话 |
| GET | `/auth/session` | 是 | 报告当前请求是否已认证 |
| GET | `/clusters` | 是 | 所有已配置集群及其健康摘要 |
| GET | `/clusters/{cluster}` | 是 | 单个集群摘要 |
| POST | `/clusters/{cluster}/reconnect` | 是 | 重新连接集群并返回其摘要 |
| GET | `/clusters/{cluster}/instances?type=` | 是 | 单个集群上的实例 |
| POST | `/clusters/{cluster}/instances` | 是 | 从本地镜像创建实例 |
| GET | `/clusters/{cluster}/instances/{instance}` | 是 | 实例详情 |
| PATCH | `/clusters/{cluster}/instances/{instance}` | 是 | 更新限制、根磁盘、NIC、描述 |
| POST | `/clusters/{cluster}/instances/{instance}/state` | 是 | 启动、停止或重启实例 |
| GET | `/clusters/{cluster}/instances/{instance}/metrics` | 是 | 实时资源快照（CPU、内存、磁盘、网络） |
| DELETE | `/clusters/{cluster}/instances/{instance}?force=` | 是 | 删除实例 |
| GET | `/clusters/{cluster}/instances/{instance}/snapshots` | 是 | 列出实例快照 |
| POST | `/clusters/{cluster}/instances/{instance}/snapshots` | 是 | 创建快照 |
| POST | `/clusters/{cluster}/instances/{instance}/snapshots/{snapshot}/restore` | 是 | 恢复快照 |
| DELETE | `/clusters/{cluster}/instances/{instance}/snapshots/{snapshot}` | 是 | 删除快照 |
| POST | `/clusters/{cluster}/instances/{instance}/devices` | 是 | 挂载磁盘或 NIC 设备 |
| DELETE | `/clusters/{cluster}/instances/{instance}/devices/{device}` | 是 | 卸载设备 |
| GET | `/clusters/{cluster}/instances/{instance}/console` | 是 | 交互式控制台（WebSocket） |
| GET | `/clusters/{cluster}/instances/{instance}/exec` | 是 | 交互式 exec 会话（WebSocket） |
| GET | `/clusters/{cluster}/instances/{instance}/files?action=list&path=` | 是 | 列出实例内的目录 |
| GET | `/clusters/{cluster}/instances/{instance}/files?path=` | 是 | 下载单个文件 |
| POST | `/clusters/{cluster}/instances/{instance}/files?path=` | 是 | 上传文件（multipart `file` 字段） |
| POST | `/clusters/{cluster}/instances/{instance}/files?action=mkdir&path=` | 是 | 创建目录 |
| DELETE | `/clusters/{cluster}/instances/{instance}/files?path=` | 是 | 删除单个文件 |
| GET | `/clusters/{cluster}/images` | 是 | 集群上的本地镜像 |
| DELETE | `/clusters/{cluster}/images/{fingerprint}` | 是 | 删除镜像 |
| GET | `/clusters/{cluster}/resources` | 是 | 存储池与网络及其用量 |
| POST | `/clusters/{cluster}/storage-pools` | 是 | 创建存储池 |
| PATCH | `/clusters/{cluster}/storage-pools/{pool}` | 是 | 更新存储池描述/配置 |
| DELETE | `/clusters/{cluster}/storage-pools/{pool}` | 是 | 删除存储池 |
| GET | `/clusters/{cluster}/storage-pools/{pool}/volumes` | 是 | 列出存储池上的自定义卷 |
| POST | `/clusters/{cluster}/storage-pools/{pool}/volumes` | 是 | 创建自定义卷 |
| DELETE | `/clusters/{cluster}/storage-pools/{pool}/volumes/{volume}` | 是 | 删除自定义卷 |
| POST | `/clusters/{cluster}/networks` | 是 | 创建托管网络 |
| PATCH | `/clusters/{cluster}/networks/{network}` | 是 | 更新托管网络 |
| DELETE | `/clusters/{cluster}/networks/{network}` | 是 | 删除托管网络 |
| GET | `/clusters/{cluster}/profiles` | 是 | 列出实例 Profile |
| POST | `/clusters/{cluster}/profiles` | 是 | 创建 Profile |
| PATCH | `/clusters/{cluster}/profiles/{profile}` | 是 | 更新 Profile |
| DELETE | `/clusters/{cluster}/profiles/{profile}` | 是 | 删除 Profile |
| PATCH | `/clusters/{cluster}/members/{member}` | 是 | 更新成员描述、故障域、分组 |
| GET | `/instances?type=` | 是 | 所有集群上的实例 |
| GET | `/migrations?limit=` | 是 | 最近的迁移任务（默认 50） |
| POST | `/migrations` | 是 | 提交迁移（`202 Accepted`） |
| GET | `/migrations/{id}` | 是 | 单个迁移任务 |
| POST | `/clusters/{cluster}/images/import?type=&alias=` | 是 | 上传镜像（`multipart/form-data`） |
| POST | `/clusters/{cluster}/images/{fingerprint}/copy` | 是 | 把本地镜像分发到另一个集群 |
| POST | `/clusters/{cluster}/images/{fingerprint}/refresh` | 是 | 从来源重新拉取缓存镜像 |
| GET | `/clusters/{cluster}/iso` | 是 | 列出安装用 ISO 卷 |
| POST | `/clusters/{cluster}/iso?pool=` | 是 | 上传 ISO 安装镜像（`multipart/form-data`） |
| DELETE | `/clusters/{cluster}/iso/{pool}/{name}` | 是 | 删除 ISO 卷 |
| POST | `/clusters/{cluster}/iso/{pool}/{name}/launch` | 是 | 创建一个启动该 ISO 的空白虚拟机 |
| POST | `/clusters/{cluster}/instances/{instance}/clone` | 是 | 在所属集群内克隆实例 |
| POST | `/clusters/{cluster}/instances/{instance}/rebuild` | 是 | 用另一个镜像重建根磁盘 |
| POST | `/clusters/{cluster}/instances/{instance}/ops` | 是 | 运行脚本、设置密码或注入密钥 |
| GET | `/clusters/{cluster}/instances/{instance}/vga` | 是 | 图形控制台（WebSocket） |
| GET | `/clusters/{cluster}/instances/{instance}/spice?port=` | 是 | SPICE 通道桥接（WebSocket） |
| POST | `/clusters/{cluster}/instances/{instance}/spice/prepare` | 是 | 配置虚拟机的 SPICE 监听器 |
| GET | `/keys` | 是 | 列出共享 SSH 公钥 |
| POST | `/keys` | 是 | 存储公钥 |
| PATCH | `/keys/{id}` | 是 | 更新已存储的公钥 |
| DELETE | `/keys/{id}` | 是 | 删除已存储的公钥 |
| GET | `/scripts` | 是 | 列出共享脚本 |
| POST | `/scripts` | 是 | 存储脚本 |
| PATCH | `/scripts/{id}` | 是 | 更新已存储的脚本 |
| DELETE | `/scripts/{id}` | 是 | 删除已存储的脚本 |
| POST | `/batch/keys` | 是 | 把已存储的密钥注入多个实例 |
| POST | `/batch/scripts` | 是 | 在多个实例上运行同一个已存储脚本 |
| POST | `/batch/power` | 是 | 启动、停止或重启多个实例 |

`type` 可取 `any`（默认）、`container` 或 `virtual-machine`。

## 健康检查与版本

```json
// GET /api/v1/healthz
{"status": "ok", "version": "1.2.3", "time": "2026-11-11T09:30:00Z"}

// GET /api/v1/version
{"version": "1.2.3"}
```

## 集群

```json
// GET /api/v1/clusters
{
  "clusters": [
    {
      "name": "c1",
      "display_name": "Primary site",
      "endpoint": "https://192.0.2.10:8443",
      "online": true,
      "server_name": "host-a",
      "server_version": "7.0.2",
      "clustered": true,
      "architectures": ["x86_64"],
      "members": [
        {
          "name": "host-a",
          "url": "https://192.0.2.10:8443",
          "status": "Online",
          "message": "Fully operational",
          "database": true,
          "roles": ["database-leader"],
          "architecture": "x86_64"
        }
      ],
      "instances": {
        "total": 3,
        "containers": 2,
        "virtual_machines": 1,
        "running": 2
      },
      "storage": {"pool": "default", "used": 250000000000, "total": 1000000000000},
      "networks": ["incusbr0"]
    }
  ]
}
```

离线集群保持相同结构，只是 `online: false` 并带有一个 `error` 字符串。当集群
无法检查时，存储和实例计数可能为零。

`POST /clusters/{cluster}/reconnect` 直接返回更新后的摘要对象（不做外层包装）。

## 实例

```json
// GET /api/v1/instances?type=any
{
  "instances": [
    {
      "name": "web",
      "cluster": "c1",
      "type": "container",
      "status": "Running",
      "status_code": 103,
      "location": "host-a",
      "project": "default",
      "architecture": "x86_64",
      "description": "web frontend",
      "profiles": ["default"],
      "ipv4": ["10.0.0.5"],
      "ipv6": [],
      "memory": 134217728,
      "processes": 12,
      "created_at": "2026-11-10T08:00:00Z",
      "started_at": "2026-11-11T09:00:00Z",
      "storage_pool": "default",
      "migration_stateful": false,
      "live_migratable": false
    }
  ]
}
```

实例详情额外包含：

- 来自客户机状态的 `os`、`os_version`、`kernel_version`、`hostname`；
- `ephemeral`；
- `config` —— 仅包含经过筛选的安全键（`image.*`、`limits.cpu`、`limits.memory`、
  `migration.stateful`、`security.secureboot`）；
- `devices` —— 展开后的设备；
- `state` —— 可用时的 Incus 原始状态。

只有当虚拟机的 `migration.stateful=true` 时，`live_migratable` 才为 true。它并不
保证该虚拟机当前正在运行；迁移预检会单独检查这一点。

### 实例管理

创建（`POST /clusters/{cluster}/instances`）接受 `name`（必填，
`[A-Za-z0-9][A-Za-z0-9_.-]*`，最长 63 个字符）、`type`（默认 `container`，或
`virtual-machine`）、`image`（必填，本地镜像的指纹）、`description`、`profiles`、
`cpu`（核数、范围或列表）、`memory`（例如 `512MiB`）、`root_disk_size`、
`storage_pool`（默认 `default`）、`network`、`ipv4`、`ipv6`（静态地址需要
`network`）以及 `start`（默认 `true`）。等待时间：最长 15 分钟。

`PATCH .../instances/{instance}` 接受可为 null 的字段；空字符串会清除取值。NIC
变更以 `device` 为目标（默认 `eth0`）。

`POST .../state` 请求体：`{"action": "start|stop|restart", "force": false}`。

### 快照

```json
// GET .../instances/web/snapshots
{
  "snapshots": [
    { "name": "snap0", "created_at": "2026-11-10T08:00:00Z", "size": 1024, "stateful": true }
  ]
}
```

创建请求体：`{"name": "backup", "stateful": false}`。有状态快照要求实例为有状态
虚拟机。恢复会回滚实例（`InstancePut.Restore`）。创建快照和恢复的等待时间：最长
15 分钟。

### 设备

挂载请求体（磁盘）：

```json
{ "name": "data", "type": "disk", "pool": "default", "source": "data", "path": "/mnt/data", "size": "10GiB" }
```

挂载请求体（NIC）：`{ "name": "eth1", "type": "nic", "network": "incusbr0", "ipv4": "10.0.0.9" }`。
磁盘需要 `path`；NIC 需要一个已存在的 `network`。设备名不得与已有设备冲突。
两者都返回更新后的实例详情。

### 终端会话

`GET .../console` 和 `GET .../exec` 会升级为 WebSocket（使用会话 cookie 或
bearer）。exec 的第一帧是 JSON：
`{"command": ["bash"], "cwd": "/root", "width": 100, "height": 30}`。UI 会启动一个
常驻的交互式 shell（`command: ["bash"]`），并把每一条提交的行转发到 stdin，因此
环境变量和工作目录会在多次命令之间保留；会话结束时 UI 会重新打开会话，并重放
待执行的那一行。

- 二进制帧双向承载原始终端字节；
- 来自服务器的文本帧是 base64 编码的输出，在投递前会被解码；
- 来自客户端的文本帧是控制消息：`{"type":"resize","width":W,"height":H}` 或
  `{"type":"signal","signal":"SIGINT"}`。

任意一端关闭或 Incus 操作结束时，会话结束。

### 实例指标

`GET .../metrics` 返回 `{"metrics": {...}}`，其中包含单个实例的实时状态：
`status`、`pid`、`processes`、`cpu.usage`（累计纳秒）、`memory`（用量/峰值/总量/
交换，单位为字节）、`disk`（每个挂载点的用量/总量）以及 `network`（每个接口的
累计计数器，不含回环）。计数器自实例启动起累计；客户端通过对相邻采样做差来推算
速率。

### 文件

所有路径必须是绝对路径。`GET .../files?action=list&path=/var/log` 返回
`{"files": [{"name", "size", "mode", "is_dir", "modtime"}]}`（目录在前）。下载会以
附件形式流式返回文件。上传使用 `multipart/form-data`，包含一个 `file` 字段
（请求体上限 32 MiB，权限 0644）。`action=mkdir` 创建目录。删除操作移除一个文件。

### 集群资源与管理

`GET .../resources` 返回存储池（含 `config`、用量、`used_by`）和网络（含 `config`、
托管标志、地址）。创建存储池接受 `name` + `driver`，以及可选的 `description`/
`config`；更新会合并 config，并删除被设为空字符串的键。网络遵循相同的合并规则；
只有托管网络可以更新或删除。

Profile 支持完整的 CRUD；当请求中提供 `devices` 时，更新会整体替换设备映射。

`PATCH .../members/{member}` 接受 `description`、`failure_domain` 和 `groups`
（为 nil 时保留当前分组）。

### 卷与镜像

`GET .../storage-pools/{pool}/volumes` 只列出自定义卷。创建接受 `name`、
`description`、`size`（会校验的字节字符串）以及额外的 `config`。`DELETE` 删除
一个自定义卷。`DELETE .../images/{fingerprint}` 删除本地镜像；UI 的清理操作会
删除所有 `cached=true` 的镜像。

## 迁移

### 提交

```http
POST /api/v1/migrations
Content-Type: application/json

{
  "source_cluster": "c1",
  "target_cluster": "c2",
  "instance": "web",
  "mode": "move",
  "live": false,
  "include_snapshots": false,
  "auto_start": true
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `source_cluster` | 是 | 必须存在且已连接 |
| `target_cluster` | 是 | 必须与源集群不同 |
| `instance` | 是 | 必须存在于源集群，且不存在于目标集群 |
| `mode` | 否 | `move`（默认）或 `copy` |
| `live` | 否 | 仅用于 `move`；要求虚拟机正在运行且 `migration.stateful=true` |
| `include_snapshots` | 否 | 默认 `false`；为 `false` 时仅复制实例本身（不含快照） |
| `auto_start` | 否 | 默认 `true`；当源实例此前在运行时启动目标实例 |

返回 `202 Accepted` 及迁移对象。任务异步执行。

### 迁移对象

```json
{
  "id": "8f14e45fceea167a5a36dedd4bea2543",
  "source_cluster": "c1",
  "target_cluster": "c2",
  "instance": "web",
  "instance_type": "container",
  "mode": "move",
  "live": false,
  "status": "transferring",
  "progress": "transferring instance",
  "error": "",
  "created_at": "2026-11-11T09:30:00Z",
  "updated_at": "2026-11-11T09:30:04Z"
}
```

状态推进顺序：

```mermaid
flowchart LR
    pending --> preflight --> stopping --> transferring --> verifying --> starting --> done
```

并非每个阶段都会出现。对于 `auto_start=false` 的已停止实例，会跳过 `starting`；
在线迁移以及源实例本来就已停止时，会跳过 `stopping`。失败时停在 `failed`，
`progress` 指明失败的阶段，`error` 包含详细信息。

`GET /api/v1/migrations?limit=20` 返回 `{"migrations": [...]}`。`limit` 必须是
正整数。`GET /api/v1/migrations/{id}` 返回单个对象。

## 镜像

### 导入

```http
POST /api/v1/clusters/{cluster}/images/import?type=container&alias=debian/13
Content-Type: multipart/form-data
```

Multipart 字段：

- `image` —— 镜像元数据 tar 包，或统一镜像 tar 包（必填）；
- `rootfs` —— `.root` 根文件系统，仅用于拆分导出。

`type` 接受 `container`（默认）或 `virtual-machine`；`alias` 可选。请求体上限为
8 GiB。导入成功返回 `{"fingerprint": "<sha256>"}`；如果 Incus 守护进程未上报
指纹，响应为 `204 No Content`。

### 分发

`POST /clusters/{cluster}/images/{fingerprint}/copy`，请求体为
`{"target_cluster": "c2", "alias": "debian/13"}`，会把镜像直接流式传输到另一个
集群，并返回 `204 No Content`。目标必须与源不同且处于已连接状态。

### 刷新

`POST /clusters/{cluster}/images/{fingerprint}/refresh` 让 Incus 从镜像来源重新
拉取该缓存镜像，然后返回 `204 No Content`。别名会保留。

## ISO 安装镜像

| 方法 | 路径 | 结果 |
| --- | --- | --- |
| GET | `.../iso` | `{"isos": [{"name", "pool", "description", "size"}]}` |
| POST | `.../iso?pool=default` | 上传（`image` multipart 字段，上限 12 GiB）→ `201 {"name","pool"}` |
| DELETE | `.../iso/{pool}/{name}` | `204 No Content` |
| POST | `.../iso/{pool}/{name}/launch` | 用该 ISO 创建虚拟机 → `201` 实例详情 |

启动请求体：

```json
{
  "name": "db-installer",
  "storage_pool": "default",
  "network": "incusbr0",
  "root_disk": "20GiB",
  "start": true
}
```

`iso_name` 来自 URL 路径，无需在请求体中重复。为兼容安装程序，fleet 会放宽
Secure Boot，并自动配置 SPICE。

## 实例操作

### 克隆

`POST .../instances/{instance}/clone`，请求体为
`{"target": "web-2", "start": true}`，会在同一集群内复制一个完整的实例（磁盘和
配置），并返回 `201` 及新实例的详情。`start` 默认为 `true`。

### 重建

`POST .../instances/{instance}/rebuild`，请求体为 `{"image": "debian/13"}`，会用
指定的镜像（别名或指纹）替换根磁盘。实例必须处于停止状态。数据卷不会改变。返回
更新后的实例详情。

### 运维操作

`POST .../instances/{instance}/ops` 以 root 身份在客户机内执行一个操作：

```json
{"action": "run-script", "script": "apt-get update && apt-get -y upgrade"}
{"action": "set-password", "user": "root", "password": "<new password>"}
{"action": "inject-key", "user": "root", "key": "ssh-ed25519 AAAA... user@example"}
```

响应为 `{"exit_code": 0, "stdout": "...", "stderr": "..."}`。脚本通过 `sh -s`
执行；密码交给 `chpasswd`；密钥追加到 `~/.ssh/authorized_keys`。载荷写入 stdin，
绝不会出现在进程参数中。

### 图形控制台

`POST .../instances/{instance}/spice/prepare`，请求体为 `{"port": 0, "apply": true}`，
用于配置虚拟机的 QEMU SPICE TCP 监听器。端口为 0 时会根据实例名在 5910-5989
范围内派生一个稳定端口。响应为：

```json
{"port": 5931, "applied": true, "restart_required": false}
```

`apply: true` 会重启正在运行的虚拟机，使 QEMU 参数生效；因此第一个会话可能需要
一次重启。`GET .../spice?port=5931` 会升级为 WebSocket，并把一个 SPICE 通道桥接
到虚拟机的 TCP 监听器。`GET .../vga` 会升级为 Incus 图形控制台 WebSocket。

SPICE 监听器使用 `disable-ticketing=on`，并绑定在集群成员的所有网络接口上。请
用防火墙限制 5910-5989 端口范围；不要将其暴露到不可信网络。

## 共享资产

### SSH 公钥

`GET /keys` 返回 `{"keys": [...]}`。每个密钥包含 `id`、`name`、`public_key`、
`comment` 和 `created_at`。`POST /keys` 接受 `name`、`public_key` 以及可选的
`comment`，返回 `201` 及存储后的对象。`public_key` 可以包含多行以换行分隔的
密钥；相同内容只会以内容派生的 id 存储一份。`PATCH /keys/{id}` 更新密钥，
`DELETE /keys/{id}` 返回 `204 No Content`。

### 脚本

`GET /scripts` 返回 `{"scripts": [...]}`，包含 `id`、`name`、`description`、`body`、
`created_at` 和 `updated_at`。`POST /scripts` 需要 `name` 和 `body`，返回 `201`。
`PATCH /scripts/{id}` 更新脚本；`DELETE /scripts/{id}` 返回 `204 No Content`。

### 批量操作

三个端点都接受一个 `targets` 数组，元素为 `{"cluster", "instance", "user"}` 对象。
`user` 可选，注入密钥时默认为 `root`；脚本始终以 root 运行。

```json
// POST /batch/keys
{"targets": [{"cluster": "c1", "instance": "web"}], "key_ids": ["key-1a2b3c4d5e6f7788"]}

// POST /batch/scripts
{"targets": [{"cluster": "c1", "instance": "web"}], "script_id": "script-..."}

// POST /batch/power
{"targets": [{"cluster": "c1", "instance": "web"}], "action": "restart", "force": true}
```

每次调用返回 `{"items": [...]}`，其中每个 item 为
`{"cluster", "instance", "ok", "exit_code", "stdout", "stderr", "error"}`。各实例
独立执行：一个失败不会影响其余实例。所有 item 都成功时响应为 `200 OK`，只要有
item 报告错误则为 `207 Multi-Status`。

## 错误

所有错误使用同一个信封结构：

```json
{
  "error": {
    "code": "invalid_request",
    "message": "migrations: live migration requires a running instance"
  }
}
```

| HTTP | 错误码 | 含义 |
| --- | --- | --- |
| 400 | `invalid_request` | JSON 格式错误、`type` 不合法、`limit` 不合法、迁移字段无效 |
| 401 | `unauthorized` | 缺少或无效的会话 / bearer token |
| 401 | `invalid_token` | 登录时管理员 token 错误 |
| 404 | `not_found` | 未知的集群、实例或迁移 |
| 409 | `conflict` | 实例已存在于目标集群，或该实例已有迁移在运行 |
| 503 | `cluster_unavailable` | 无法连接已配置的集群 |
| 500 | `internal_error` | 意外失败；`message` 包含底层错误详情 |

`207 Multi-Status` 不是错误信封：批量端点在至少有一个目标失败时，用它返回每个
实例的结果。

## 限制与行为

- JSON 请求体限制为 1 MiB。
- Multipart 上传各有独立上限：镜像 8 GiB，ISO 文件 12 GiB，实例文件上传
  32 MiB。
- 迁移执行默认超时为 6 小时。
- 源实例停止操作使用 300 秒超时。
- 实例 `exec` 与运维操作使用 10 分钟超时；镜像传输使用 30 分钟。
- 应用进程重启会把未完成的迁移标记为失败；它绝不会续传。
- 同一源实例的并发迁移会按应用进程拒绝。若两个应用实例同时对接相同的集群，
  仍可能发生竞争；Incus 会拒绝重叠的工作。
