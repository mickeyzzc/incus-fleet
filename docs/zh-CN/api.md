# HTTP API

[English](../api.md) | **简体中文**

基础路径：`/api/v1`。除特别说明外，所有请求体与响应体均为 JSON。同一台服务器
还在 `/` 提供 Vue SPA。

## 认证

服务器支持本地账号与基于角色的访问控制。有三种认证方式：

1. **浏览器会话。** 用用户名和密码调用 `POST /api/v1/auth/login`；响应会设置一个名为
   `incus_fleet_session` 的 `HttpOnly`、`SameSite=Lax` cookie。该 cookie 的有效期为
   12 小时。当请求本身走 TLS，或携带 `X-Forwarded-Proto: https`（由 Caddy 发送）
   时，cookie 会被标记为 `Secure`。
2. **Bearer token。** 在受保护的端点上发送 `Authorization: Bearer <admin token>`。
   这是共享引导凭证：**仅在尚无本地账号时有效**，创建第一个账号后自动退役。
3. **引导环境变量。** `INCUS_FLEET_BOOTSTRAP_ADMIN_USERNAME` 与
   `INCUS_FLEET_BOOTSTRAP_ADMIN_PASSWORD` 在启动时创建第一个管理员；已有账号后忽略。

### 角色

| 角色 | 读取 | 变更集群资源 | 管理用户 |
| --- | --- | --- | --- |
| `admin` | 是 | 是 | 是 |
| `operator` | 是 | 是 | 否 |
| `viewer` | 是 | 否（403） | 否 |

变更类端点在服务端校验角色；只读账号即使直接调用 API 也会收到
`403 {"error":{"code":"forbidden"}}`。

### 用户管理

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| GET | `/users` | admin | 列出账号 |
| POST | `/users` | admin（首个账号可用引导 token） | 创建账号 |
| PATCH | `/users/{id}` | admin | 修改角色、密码或禁用状态 |
| DELETE | `/users/{id}` | admin | 删除账号（拒绝删除最后一个管理员） |
| POST | `/auth/password` | 任意已登录账号 | 修改自己的密码 |
| GET | `/auth/sessions` | 任意已登录账号 | 查看当前会话 |

最后一个启用的管理员既不能被删除也不能被降级：两种操作都返回 `409 conflict`。

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

{"username": "admin", "password": "<password>"}
```

```json
{
  "authenticated": true,
  "expires_at": "2026-11-12T09:30:00Z",
  "username": "admin",
  "role": "admin"
}
```

凭据错误返回 `401` `invalid_credentials`；账号锁定返回 `429` `account_locked`；
账号禁用返回 `403` `account_disabled`。

引导路径改为发送 `{"token": "<admin token>"}`；一旦存在本地账号即返回 `401`。

### 会话

```http
GET /api/v1/auth/session
```

```json
{"authenticated": true, "username": "admin", "role": "admin", "expires_at": "..."}
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
| PATCH | `/clusters/{cluster}/instances/{instance}/devices/{device}` | 是 | 编辑单个设备(合并进现有配置;网卡地址留空回退 DHCP) |
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
| POST | `/clusters/{cluster}/instances/{instance}/agent/install` | 是 | 通过 SSH 在虚拟机内安装官方 incus-agent（客户机凭据仅在请求体内，不落库） |
| GET | `/clusters/{cluster}/instances/{instance}/vga` | 是 | 图形控制台（WebSocket） |
| POST | `/clusters/{cluster}/instances/{instance}/desktop/prepare` | 是 | 启用 QMP 图形控制台（安装监视器 socket） |
| GET | `/clusters/{cluster}/instances/{instance}/desktop/screenshot` | 是 | 当前帧缓冲（PNG） |
| POST | `/clusters/{cluster}/instances/{instance}/desktop/input` | 是 | 注入一个键盘或指针事件 |
| GET | `/clusters/{cluster}/instances/{instance}/spice?port=` | 是 | SPICE 通道桥接（WebSocket，旧版） |
| POST | `/clusters/{cluster}/instances/{instance}/spice/prepare` | 是 | 配置虚拟机的 SPICE 监听器（旧版） |
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

## 添加集群成员

`POST /api/v1/clusters/{cluster}/members` 会准备一台新主机并加入集群。fleet 服务通过 SSH
连接主机，可选地从 apt 仓库安装 Incus，通过主机本地 socket 提交加入请求，并等待成员上报
`Online`。

```json
{
  "name": "node-c",
  "address": "198.51.100.11",
  "ssh_user": "node-admin",
  "ssh_port": 22,
  "ssh_password": "optional-ssh-password",
  "sudo_password": "optional-sudo-password",
  "install_incus": true,
  "install_vms": true,
  "ignore_host_key": true,
  "storage_source": "/srv/incus",
  "incus_repo": "https://pkgs.zabbly.com/incus/lts-7.0",
  "incus_repo_key": "https://pkgs.zabbly.com/key.asc"
}
```

- `name` 可选；省略时使用节点 hostname。
- `ssh_user` 默认取 `node_access.ssh_user`；除非提供 `ssh_password`，否则使用 fleet SSH 密钥。
- `sudo_password` 仅在 SSH 账户需要密码执行 sudo 时使用一次。加入成功后，fleet 会为该账户安装免密白名单。
- `install_incus` 会添加 apt 源（`incus_repo`/`incus_repo_key`）并安装软件包。安装的版本必须与集群的 Incus 版本一致。
- `ignore_host_key` 跳过 SSH 主机密钥校验，适用于尚未加入 `known_hosts` 的节点。
- `storage_source` 填充每个成员的 `storage-pool/default/source`；默认 `/srv/incus`。

`201` 返回加入的成员和准备步骤：

```json
{
  "member": {"name": "node-c", "status": "Online", "roles": [], "url": "https://198.51.100.11:8443"},
  "steps": [
    {"name": "connect", "status": "ok", "detail": "SSH connection established to 198.51.100.11"},
    {"name": "install", "status": "ok", "detail": "Incus installed from https://pkgs.zabbly.com/incus/lts-7.0"},
    {"name": "token", "status": "ok", "detail": "Join token generated for node-c"},
    {"name": "join", "status": "ok", "detail": "Join request accepted by cluster c2"},
    {"name": "online", "status": "ok", "detail": "Member node-c is online"},
    {"name": "sudoers", "status": "ok", "detail": "Passwordless node network sudo rules installed"},
    {"name": "fleet_key", "status": "ok", "detail": "Fleet SSH key authorized for node-admin"}
  ]
}
```

加入后的配置步骤（`sudoers`、`fleet_key` 等）如果返回 `warning`/`skipped`，不会让请求失败，
因为成员已经加入集群。错误映射为 `400 invalid_request`、`404 not_found`、`409 conflict`
（成员名已存在）、`503 cluster_unavailable` 或 `500 internal_error`。

请求可能需要数分钟；handler 会延长写超时，服务器的全局超时仍作用于请求上下文。密码只在
内存中使用，不会持久化或记录到日志。

## 节点网络（按集群成员）

这些端点管理某个集群成员的主机网络。fleet 所在主机通过 `sudo -n` 本地执行；其他成员通过
`node_access` 的 SSH 身份访问。两条路径都需要 `docs/deployment.md` 中的 sudo 白名单。

```json
// GET /api/v1/clusters/c2/members/node-b/node?wifi=wlan0
{
  "host": "node-b",
  "nics": [
    {"name": "eth0", "state": "up", "kind": "ethernet", "mac": "aa:bb:cc:dd:ee:ff", "mtu": 1500,
     "addresses": [{"family": "inet", "address": "198.51.100.10", "prefixlen": 24}]},
    {"name": "wlan0", "state": "up", "kind": "wifi", "addresses": [],
     "wifi": {"connected": true, "ssid": "Office", "signal": -55, "frequency": 2437}}
  ],
  "routes": [{"destination": "default", "gateway": "198.51.100.1", "device": "eth0", "metric": "100"}],
  "firewall": [{"handle": "7", "rule": "tcp dport 22 accept comment \"fleet_rule\""}],
  "wifi_scan": [{"ssid": "Office", "signal": -45, "frequency": 2437, "secure": true}],
  "wifi_device": "wlan0",
  "capabilities": ["nics", "routes", "firewall"]
}
```

`wifi` 查询参数选择要扫描的接口；省略时使用第一个无线接口。扫描结果按 SSID 去重（保留信号
最强的 BSS），并要求接口处于 up 状态。

以下路径都相对于 `/api/v1/clusters/{cluster}/members/{member}/node`：

| 方法与路径 | 用途 |
|---|---|
| `POST /networks/{name}/state` `{"up": bool}` | 启用/禁用网卡 |
| `PATCH /networks/{name}` `{"dhcp": bool, "address": "10.0.0.50/24", "gateway": "...", "persist": bool}` | 静态地址或 DHCP；`persist` 写入 `/etc/network/interfaces.d/<name>` |
| `POST /networks/{name}/wifi` `{"ssid": "...", "password": "..."}` | 通过 wpa_supplicant 关联并获取 DHCP 租约 |
| `GET/POST /routes`、`DELETE /routes/{destination}` | 路由表和静态路由 |
| `GET/POST /firewall`、`DELETE /firewall/{handle}` | fleet_filter nftables 表规则 |

Handler 错误映射为 `400 invalid_request`（输入错误）、`404 not_found`（未知集群或成员）和
`503 node_unavailable`（缺少 SSH 访问、sudo 规则、二进制，或主机故障）。

快照包含 `backend`（`local`、`ssh`，启用 agent 后为 `agent`）。变更型响应会设置
`X-Incus-Fleet-Backend`，如果操作被记录还会设置 `X-Incus-Fleet-Task`。后端由
`node_access.mode`（`auto`、`agent`、`ssh` 或 `local`）决定。默认 `auto` 下，读操作可以回退
SSH；写操作只有在 `node_access.ssh_write_fallback=true` 时才回退（对于尚未部署 agent 的旧
部署，默认是 true）。

## Agent

主机 agent 是可选的。注册 token 一次性使用，并绑定到一个集群成员。agent gateway 监听独立的
`agent.listen` TLS 端口（默认 `:8444`），不属于浏览器 API；普通 HTTP 端口只暴露下面的管理
端点。

| 方法与路径 | 用途 |
|---|---|
| `POST /api/v1/agents/enrollment-tokens` | 创建一次性 token；要求集群已存在 |
| `GET /api/v1/agents?cluster=&member=` | 列出已注册 agent，可按 cluster/member 过滤 |
| `GET /api/v1/agents/{id}/snapshot` | 向在线 agent 派发只读 `nodenet.snapshot` |
| `GET /api/v1/agent/install.sh` | 公开的一键安装脚本；由 `agent.install` 启用 |
| `GET /api/v1/agent/install/agent` | agent 二进制，带 `X-Checksum-Sha256` |
| `GET /api/v1/agent/install/helper` | 特权 helper 二进制，带 `X-Checksum-Sha256` |
| `GET /api/v1/agent/install/ca.crt` | agent gateway CA 证书 |

```json
// POST /api/v1/agents/enrollment-tokens
{"cluster": "c1", "member": "node-a", "ttl_seconds": 900}
// 201
{"token": "one-time-value", "cluster": "c1", "member": "node-a",
 "expires_at": "2026-11-18T00:15:00Z"}
```

安装器端点不需要登录，因为一次性注册 token 本身就是凭据；`agent.install.enabled=false` 时返回
`503`。生成的脚本内嵌 gateway URL、资源基地址、两个 SHA-256 校验值、CA 证书、sudoers 白名单和
systemd unit；它会创建服务用户、安装二进制、完成注册并启动服务。

token 只返回一次；数据库只存它的 SHA-256。注册请求本身是 agent gateway 上的
`POST /api/v1/agent/enroll`，携带 PKIX 公钥、cluster、member 和 token。gateway 返回 agent ID、
客户端证书和 CA 证书。agent 随后用 mTLS 连接 `GET /api/v1/agent/connect`，通过 WebSocket
发送 protobuf `Envelope` 帧。证书会自动续期：离过期 30 天内时，agent 用当前 mTLS 身份调用
`POST /api/v1/agent/renew`；gateway 校验当前指纹，为提交的 PKIX 公钥签发新证书并返回新 PEM 和 CA。

`GET /api/v1/agents/{id}/snapshot` 返回：

```json
{
  "snapshot": {
    "host": "node-a",
    "nics": [{"name": "eth0", "state": "up", "kind": "ethernet",
              "addresses": [{"family": "inet", "address": "192.0.2.10", "prefixlen": 24}]}],
    "routes": [{"destination": "default", "gateway": "192.0.2.1", "device": "eth0"}],
    "capabilities": ["nics", "routes"]
  }
}
```

`503 agent_offline` 表示 agent 没有在线 WebSocket；`504 agent_timeout` 表示派发超时前没有响应；
`502 agent_error` 表示 agent 拒绝或执行任务失败。

`POST /api/v1/agents/{id}/bootstrap` 构建、签名并向 agent 的特权 helper 派发 Incus
安装/初始化/加入 spec。spec 使用 agent CA 私钥签名，helper 执行前用 agent identity 目录中的
CA 证书验证。`mode=join` 会从现有集群申请一次性 token 并渲染 join preseed；`mode=init` 会
渲染新集群 preseed（dir 存储池和 `incusbr0` 网桥）。

```json
// 加入现有集群；只有仓库已预配置时才安装 Incus
{"mode": "join", "address": "198.51.100.11", "storage_source": "/srv/incus",
 "install_mode": "package", "version_pin": "7.0", "packages": ["incus"]}
// 初始化新集群
{"mode": "init", "address": "192.0.2.10", "network_cidr": "10.0.0.1/24",
 "install_mode": "existing"}
// 200
{"result": {"ok": true, "task_id": "...", "mode": "bootstrap",
            "message": "Incus bootstrap complete", "steps": ["install", "service", "preseed", "ready"]}}
```

helper 按 task ID 幂等：已完成的 install/service/preseed/ready 步骤记录在
`/var/lib/incus-fleet-helper/bootstrap-<task_id>.json`，重试时跳过。`install_mode=package` 使用
发行版已配置的仓库，支持 apt、dnf、zypper、apk 和 pacman；当前 helper 会拒绝 `repo_url`，不会
添加未验证仓库。安装后会校验 Incus 版本 pin。`confirm`/`rollback` 不会卸载软件包。

`POST /api/v1/agents/{id}/network` 通过特权 helper 派发一个允许列表内的网络变更。支持的 action
为 `link-state`、`address`、`route-add`、`route-delete`、`firewall-add`、`firewall-delete`、
`wifi-connect`、`confirm` 和 `rollback`。

```json
// 关闭 eth0
{"action": "link-state", "interface": "eth0", "up": false}
// 配置静态地址并按检测到的管理器持久化；rollback_after_seconds 可选，启动独立回滚计时器
{"action": "address", "interface": "eth0", "address": "10.0.0.50/24",
 "gateway": "10.0.0.1", "persist": true, "task_id": "task-1",
 "rollback_after_seconds": 120}
// 确认或回滚之前的操作
{"action": "confirm", "task_id": "task-1"}
```

响应为 `{"result": {"ok": true, "action": "...", "task_id": "...", "backend": "helper",
"persistence": "ifupdown"}}`。`confirm` 删除回滚快照；`rollback` 恢复该接口捕获的地址、路由、
链路状态和持久化文件。任务走允许列表；helper 会拒绝未知 action、接口名、地址和 task ID。
Wi-Fi 密码只传给 helper，不会写入任务/审计记录。

## 指标

中心在**内存中保留最近 1 小时**的 agent 遥测（`fleet_` 前缀，10 秒间隔），不会写入数据库。
`cluster`、`member`、`agent_id` 标签由中心注入，agent 不能覆盖。

| 方法与路径 | 用途 |
|---|---|
| `GET /api/v1/metrics/query?cluster=&member=&agent=&metric=&from=&to=` | 返回时间范围内的 series |
| `GET /api/v1/metrics/snapshot` | 返回每个匹配 series 的最新点 |
| `GET /api/v1/metrics/export?format=prometheus\|json\|csv` | 导出当前缓冲 |

`from`/`to` 支持 RFC3339、`1h` 这类相对时长（相对当前时间）或 Unix 秒；默认最近 1 小时。
指标形状如下：

```json
{
  "series": [
    {
      "name": "fleet_host_cpu_usage_ratio",
      "labels": {"cluster": "c1", "member": "node-a", "agent_id": "..."},
      "type": "gauge",
      "points": [{"at": "2026-11-18T00:00:00Z", "value": 0.25}]
    }
  ]
}
```

Prometheus 导出带 `# TYPE` 行和毫秒时间戳。CSV 为长表：
`timestamp,metric,cluster,member,agent_id,value`。如果 `metrics.enabled=false`，三个端点都返回
`503 unavailable`。重启中心会清空 1 小时历史，这是有意的设计。

agent 导出主机指标（`fleet_host_cpu_usage_ratio`、`fleet_host_memory_bytes`、
`fleet_host_load`、`fleet_host_uptime_seconds`、`fleet_host_network_bytes_total`），以及 cgroup v2
回退的实例指标（`fleet_instance_cpu_seconds_total`、`fleet_instance_memory_bytes`、
`fleet_instance_processes`、`fleet_instance_io_bytes_total`）。可选 eBPF 对象增加主机计数器
（`fleet_host_tcp_retransmits_total`、`fleet_host_oom_kills_total`）和按 cgroup 归因的实例
series（`fleet_instance_tcp_retransmits_total`、`fleet_instance_oom_kills_total`、
`fleet_instance_block_latency_seconds_bucket|_sum|_count`）。
`fleet_agent_telemetry_mode{mode="ebpf"|"procfs"}` 表示当前路径。block 延迟 bucket 使用 2 的幂
纳秒（约 2ns 到 1s）加 `+Inf`。TCP 重传通过 sockops 维护的 IPv4/IPv6 flow→cgroup 映射归因，
因此即使重传定时器在 softirq 上下文运行也能找到所属容器。OOM kill 通过 `bpf_task_from_pid` 和
CO-RE cgroup-id 读取从 victim pid 归因，memcg OOM 和 root 上下文的全局 OOM 都会落到被杀的实例，
并按实例聚合嵌套 cgroup。没有这些 kfunc 的内核回退当前 cgroup 和 ring-buffer `/proc` 路径。

## 外部指标

中心可以把同一份遥测转发到 Prometheus 或 VictoriaMetrics，并代理 PromQL 查询供 UI 使用。
外部集成默认关闭；参见[部署文档](deployment.md#prometheusvictoriametrics)。

| 方法与路径 | 认证 | 用途 |
|---|---|---|
| `GET /api/v1/metrics/query_range?query=&from=&to=&step=` | 管理员会话/Bearer | PromQL range 查询代理 |
| `GET /api/v1/sd/agents` | HTTP SD token | Prometheus HTTP 服务发现 |
| `GET /api/v1/agents/{id}/metrics/prometheus` | HTTP SD token | 中心代理的 agent 指标抓取 |

remote write 使用 Prometheus 1.0 线格式（snappy 压缩 protobuf），地址为
`metrics.remote_write.url`。Prometheus 需要 `--web.enable-remote-write-receiver`；
VictoriaMetrics 单机使用 `/api/v1/write`，集群使用
`/insert/<accountID>/prometheus/api/v1/write`。

`query_range` 的 `from`/`to` 支持 RFC3339、相对时长（`1h`）或 Unix 秒；`step` 是 `1m` 这样的
时长。中心转发到 Prometheus `/api/v1/query_range` 或 VictoriaMetrics
`/prometheus/api/v1/query_range` 并返回上游 JSON。查询仅限管理员，受配置超时限制。

HTTP SD 响应遵循 Prometheus 格式：

```json
[
  {
    "targets": ["fleet.example"],
    "labels": {
      "__scheme__": "https",
      "__metrics_path__": "/api/v1/agents/AGENT_ID/metrics/prometheus",
      "cluster": "c1",
      "member": "node-a",
      "agent_id": "AGENT_ID"
    }
  }
]
```

SD token 与管理员 token 分离，通过 `Authorization: Bearer <token>` 发送。只返回在线 agent。当前
`mode=proxy`（默认）由 Prometheus 抓取中心，中心返回最新内存样本和 `fleet_agent_up`。
`mode=direct` 返回 agent 自己的监听地址（来自 `metrics_address` fact）；agent 必须用
`--metrics-listen` 启动，并使用与抓取配置相同的 bearer token。没有上报地址的 agent 会被跳过。

```yaml
# direct 模式的 Prometheus 抓取配置
scrape_configs:
  - job_name: incus-fleet
    authorization:
      credentials_file: /etc/prometheus/fleet-metrics.token
    http_sd_configs:
      - url: https://fleet.example/api/v1/sd/agents
        authorization:
          credentials_file: /etc/prometheus/fleet-sd.token
```

agent 身份生命周期：

| 方法与路径 | 用途 |
|---|---|
| `POST /api/v1/agents/{id}/certificate` | 为 agent 的 PKIX 公钥签发新证书 |
| `POST /api/v1/agents/{id}/revoke` | 吊销 agent；gateway 拒绝后续连接 |
| `POST /api/v1/tasks` | 为 HA dispatcher 入队异步 `agent.network` 任务 |

```json
// 轮换
{"public_key": "-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----\n"}
// 200
{"certificate": "-----BEGIN CERTIFICATE-----\n...\n", "fingerprint": "...",
 "expires_at": "2027-02-16T00:00:00Z"}
// 吊销
POST /api/v1/agents/AGENT_ID/revoke -> 204
```

## 主机任务与审计

每个变更型节点操作都会记录为一个 host task 和一条不可变审计记录。任务和审计与迁移历史
存放在同一个 SQLite/PostgreSQL 数据库；schema 在启动时自动创建或升级。

| 方法与路径 | 用途 |
|---|---|
| `GET /api/v1/tasks?cluster=&member=&limit=` | 最近的任务，按时间倒序；`limit` 1-200，默认 50 |
| `POST /api/v1/tasks` | 入队 `{cluster,member,type,plan}`；返回 `202` 和 queued 任务 |
| `GET /api/v1/tasks/{id}` | 一个任务及其状态事件 |
| `GET /api/v1/audit?cluster=&member=&limit=` | 主机审计记录，按时间倒序 |
| `GET /api/v1/audit/export?format=jsonl\|csv` | 完整审计导出，不受 UI 上限 |

```json
// GET /api/v1/tasks/{id}
{
  "task": {
    "id": "b4f0...",
    "cluster": "c1",
    "member": "node-a",
    "type": "node.route.add",
    "state": "succeeded",
    "plan": "destination=10.0.1.0/24 gateway=192.0.2.1 device=eth0",
    "plan_hash": "...",
    "result": "succeeded",
    "created_by": "admin",
    "created_at": "2026-11-18T00:00:00Z",
    "started_at": "2026-11-18T00:00:00Z",
    "finished_at": "2026-11-18T00:00:01Z",
    "updated_at": "2026-11-18T00:00:01Z"
  },
  "events": [
    {"state": "running", "message": "started", "at": "2026-11-18T00:00:00Z"},
    {"state": "succeeded", "message": "succeeded", "at": "2026-11-18T00:00:01Z"}
  ]
}
```

dispatcher 支持 `agent.network`（`NodenetApplySpec` plan）、`agent.snapshot`（无 plan）和
`agent.bootstrap`（带 `task_id`、`signature` 的签名 `bootstrap.Spec` plan）。网络示例：

```json
POST /api/v1/tasks
{"cluster":"c1","member":"node-a","type":"agent.network",
 "plan":{"action":"route-add","destination":"10.0.1.0/24","gateway":"192.0.2.1","device":"eth0"}}
// 202
{"task":{"id":"...","state":"queued","lease_owner":""}}
```

任务由运行 dispatcher（`ha.enabled=true`）的中心实例领取，并通过成员的在线 agent 执行。lease
防止两个实例重复执行。`agent.bootstrap` 要求预签名 spec，handler 原样派发给 agent 并记录步骤数；
`agent.snapshot` 是连通性探测，返回网卡/路由数量。

任务状态为 `queued`、`dispatched`、`accepted`、`running`、`succeeded`、`failed`、`canceled`
和 `unknown`。`unknown` 表示控制面与执行器失联，但操作可能已经完成；它不会被当作失败。审计
记录包含 actor、来源 IP、backend、action、task ID、result 和脱敏后的 plan。Wi-Fi 密码、SSH
密码、sudo 密码、Incus join token 和私钥永远不会写入任务或审计。

## 中心 mesh

| 方法与路径 | 用途 |
|---|---|
| `GET /api/v1/mesh/status` | leader、term、本地状态和 voter 集合 |
| `POST /api/v1/mesh/force-single` | 破坏性恢复：`{"confirm":true}` 重置为单 voter |
| `POST /api/v1/mesh/join` | 添加恢复的节点为 voter 或非 voter |
| `POST /api/v1/mesh/promote` | 将追平的非 voter 提升为 voter |
| `POST /api/v1/mesh/remove` | 从 voter 集合移除节点 |
| `POST /internal/mesh/dispatch` | 内部认证中继；由 `X-Fleet-Mesh-Secret` 保护 |
| `POST /internal/mesh/owner` | 内部转发的 owner 注册；同样由该密钥保护 |
| `POST /internal/mesh/admin` | 内部转发给 leader 的 join/promote/remove；同样由该密钥保护 |

`clusters[].endpoints` 可配置额外的 Incus API 地址用于故障切换；manager 会在后台重连离线集群。

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

### 实例操作

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

### 图形控制台（desktop）

desktop 端点驱动 fleet 为虚拟机追加的第二个 QEMU 监视器。写操作先调
`POST .../desktop/prepare`：写入 `raw.qemu.conf`（QMP control 模式的 unix socket
监视器）与对应的 AppArmor 规则，配置变化时运行中的虚拟机会重启一次。重复调用是
幂等的，响应会说明发生了什么：

```json
{"ready": true, "restarted": false}
```

读操作轮询 `GET .../desktop/screenshot`，返回当前帧缓冲的 `image/png`。QEMU 7.1+
直接输出 PNG；更旧的版本回退为 PPM 并在成员主机上内联转换，跨 SSH 的只有压缩后
的画面。

输入经 `POST .../desktop/input` 注入（成功返回 204）：

```json
{"type": "key", "key": "ArrowDown"}
{"type": "key", "key": "F1", "modifiers": ["Control", "Alt"], "repeat": 2}
{"type": "pointer", "x": 0.5, "y": 0.25, "button": "left", "down": true}
{"type": "pointer", "x": 0.5, "y": 0.25, "move": true}
```

`key` 使用浏览器 `KeyboardEvent.key` 取值加可选修饰键（`Control`、`Alt`、`Shift`、
`Meta`）；按键会被翻译为 QMP qcode 事件,符号键自动带 shift 和弦。`pointer` 接受
归一化到 0..1 的坐标（映射到仿真平板的 0..0x7fff 轴），可选 `button`（`left`、
`middle`、`right`、`wheel-up`、`wheel-down` 等）与 `move: true` 表示无按键的拖动。

上面的旧版 SPICE 端点保留给偏好原生 SPICE 客户端的场景；内置 Web UI 现在使用
desktop 端点，不涉及任何额外 TCP 监听。

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
