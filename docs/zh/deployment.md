# 部署

[English](../en/deployment.md) | **简体中文**

本指南在每台管理主机上部署一个 incus-fleet 实例，每个实例连接所有 Incus 集群。另有一台独立的 Caddy 主机对 UI/API 做负载均衡。

示例使用文档专用地址和占位名称。请替换为你自己的值。切勿提交真实的 token、私钥或生产配置。

## 拓扑

```mermaid
flowchart LR
    caddy["Caddy<br/>会话粘滞 · TLS"]

    subgraph apps["应用主机 · 相互独立的实例"]
        direction TB
        fleetA["incus-fleet<br/>@ app-a<br/>HTTP :8080"]
        fleetB["incus-fleet<br/>@ app-b<br/>HTTP :8080"]
    end

    subgraph clusters["Incus 集群"]
        direction TB
        c1["c1<br/>https://192.0.2.10:8443"]
        c2["c2<br/>https://192.0.2.11:8443"]
        c1 ~~~ c2
    end

    caddy -->|粘滞 Cookie| apps
    apps -->|HTTPS · 客户端证书| clusters
```

每个应用实例都是独立的：独立的二进制、配置和证书。实例之间是否共享状态由数据库驱动决定。

> **UI 地图（0.2.0）。** 侧边栏分为 **Fleet**、**Resources**、**Clusters → Nodes** 和
> **Administration** 四组，新增顶层 **Instances** 和 **Volumes**（存储）页面。**Images & automation**
> 是包含镜像、ISO、answer profile 和脚本的分页页面。迁移历史位于 **Instances** 下的
> `view=migrations`，导航项按当前可见的集群范围显示。

## 选择数据库

支持三种驱动，按实例数量和对单点故障的容忍度选择。

| 驱动 | 实例数 | 状态 | 故障转移 | 额外软件 |
| --- | --- | --- | --- | --- |
| `sqlite`（默认） | 单个 | 本地文件 | 无 | 无 |
| `rqlite` | 多个 | 复制的 SQLite | 自动；需要 3 个投票节点 | `rqlited`（单个二进制） |
| `postgres` | 多个 | 共享服务器 | 需要 PostgreSQL 自身的高可用 | PostgreSQL |

**`sqlite`** —— 默认。单个实例、单个数据库文件。会话存在进程内存，所以 UI 必须固定到该实例（见下方 Caddy）。单管理主机的正确选择。

**`rqlite`** —— 一个 Go 二进制，在每个节点上保存复制的 SQLite 数据库。每个实例连任意节点即可，驱动会发现其余节点，因此 leader 切换无需改配置。请用三个投票节点：两个投票节点无法容忍任何故障，因为 Raft 需要多数派，而 2 个中的 2 个仍然是全部。会话和所有其他表都在共享库里，所以任意实例都能处理任意请求，实例消失时浏览器不会被登出。

**`postgres`** —— 共享的 PostgreSQL 服务器。行为与 `rqlite` 相同，区别是数据库服务本身需要独立的备份与高可用方案。

### 需要注意的限制

迁移任务（实例在集群之间移动）由提交它的实例内的 goroutine 执行。如果该实例在传输中途宕机，任务会被标记为失败，需要重新提交。共享数据库能保住记录和资产，但不会续跑传输。任何驱动都是如此。

选择 `rqlite` 或 `postgres` 会自动启用数据库会话。`internal/config` 里的 `Database.IsShared()` 是决定这一点的唯一位置。

## 前置条件

在每台应用主机上：

- 带 systemd 的 Linux；如果部署发布版二进制，则**不需要** Go。
- 能访问每个 Incus API 端点的 8443 端口。
- 一个专用服务账号和目录（示例使用 `/home/incus`）。
- SQLite（默认）、rqlite 集群或 PostgreSQL 服务器。
- `dnsmasq`，私有 overlay 网络的 DHCP 服务需要它（见下方节点网络一节）。

在每个 Incus 集群上：

- Incus 已初始化，且存储池与网络与你将要迁移的实例相匹配。
- 有权添加受信任的客户端证书（`incus-admin` 组或 root）。

## 1. 构建发布版二进制

每个 `v*` tag 都会触发 `Release` GitHub Actions 工作流：先用与 CI 相同的门禁校验该提交，
然后发布 `linux-amd64` 与 `linux-arm64` 两个架构的产物：

- 独立二进制 `incus-fleet-linux-<arch>`、`incus-fleet-helper-linux-<arch>`、
  `incus-fleet-agent-linux-<arch>`（中心程序已内嵌 Vue UI）；
- 每个组件均有 `.deb` 与 `.rpm` 包。中心包安装 `/usr/bin/incus-fleet`、systemd unit，
  以及 `/etc/incus-fleet/` 下的配置/sudoers/Caddy 示例；agent 包安装 agent 二进制、unit 与
  sudoers 示例。安装包**不会自动配置**：仍需按后续步骤创建服务账号、填写配置并启用服务；
- `incus-fleet-<tag>-linux-<arch>.tar.gz` 打包三个二进制与 `SHA256SUMS-<arch>`；
  release 还另外提供汇总的 `SHA256SUMS`。

`agent.install` 一键安装器从 `asset_url`（未设置时从 UI 源）下载 agent 与 helper 二进制——
使用该功能时请把这两个二进制与 release 产物放在一起。

如需本地构建：

```bash
make release
ls dist/incus-fleet-linux-amd64
```

把二进制复制到每台应用主机：

```bash
sudo install -d -o incus-fleet -g incus-fleet -m 0755 /home/incus/bin
sudo install -o incus-fleet -g incus-fleet -m 0755 \
  dist/incus-fleet-linux-amd64 /home/incus/bin/incus-fleet
```

如果账号尚不存在：

```bash
sudo useradd --system --home-dir /home/incus --create-home \
  --shell /usr/sbin/nologin incus-fleet
sudo install -d -o incus-fleet -g incus-fleet -m 0750 \
  /home/incus/bin /home/incus/certs /home/incus/data
```

## 2. 创建并信任客户端证书

为每台应用主机生成不同的证书。在可信的工作站上执行：

```bash
scripts/gen-client-cert.sh app-a ./certs
scripts/gen-client-cert.sh app-b ./certs
```

把 `app-a.crt`/`app-a.key` 复制到应用主机 A，把 `app-b.crt`/`app-b.key` 复制到应用主机 B，放在 `/home/incus/certs` 下，属主为 `incus-fleet`，密钥权限为 `0600`。

在**每个**应用实例将要管理的 Incus 集群上信任这两份证书：

```bash
sudo incus config trust add-certificate /path/to/app-a.crt --name incus-fleet-app-a
sudo incus config trust add-certificate /path/to/app-b.crt --name incus-fleet-app-b
incus config trust list
```

运行时不会使用任何信任令牌。如果轮换证书，请先添加新证书，待两个应用实例都更新后再移除旧证书。

## 3. 配置应用

复制示例并编辑：

```bash
sudo -u incus-fleet install -m 600 \
  deploy/examples/config.sqlite.yaml /home/incus/config.yaml
sudo -u incus-fleet "$EDITOR" /home/incus/config.yaml
```

`deploy/examples/config.sqlite.yaml`：

```yaml
server:
  listen: 0.0.0.0:8080
  auth_token: ""
  log_level: info

database:
  driver: sqlite
  dsn: /home/incus/data/fleet.db

clusters:
  - name: c1
    display_name: Primary site
    endpoint: https://192.0.2.10:8443
    project: default
    tls_cert: /home/incus/certs/app-a.crt
    tls_key: /home/incus/certs/app-a.key
    # tls_server_cert: /home/incus/certs/incus-server.crt

  - name: c2
    display_name: Secondary site
    endpoint: https://192.0.2.11:8443
    project: default
    tls_cert: /home/incus/certs/app-a.crt
    tls_key: /home/incus/certs/app-a.key
```

在应用主机 B 上，把证书路径指向 `app-b.crt`/`app-b.key`。

示例中的 `auth_token` 故意留空。请通过环境变量文件提供：

```bash
sudo -u incus-fleet bash -c 'umask 077; openssl rand -hex 32 > /home/incus/auth-token'
sudo -u incus-fleet install -m 600 /dev/null /home/incus/incus-fleet.env
sudo -u incus-fleet tee /home/incus/incus-fleet.env >/dev/null <<'EOF'
INCUS_FLEET_AUTH_TOKEN=replace-with-the-generated-token
# 全新部署时创建第一个本地管理员：
# INCUS_FLEET_BOOTSTRAP_ADMIN_USERNAME=admin
# INCUS_FLEET_BOOTSTRAP_ADMIN_PASSWORD=replace-with-a-strong-password
# Optional overrides:
# INCUS_FLEET_LISTEN=0.0.0.0:8080
# INCUS_FLEET_DATABASE_DRIVER=postgres
# INCUS_FLEET_DATABASE_DSN=postgres://...
EOF
sudo chmod 600 /home/incus/incus-fleet.env
sudo chown incus-fleet:incus-fleet /home/incus/incus-fleet.env
```

bootstrap admin 变量会在启动时创建第一个本地账号；一旦存在任何账号就会被忽略，因此可以安全地
保留在 env 文件中。共享的 `INCUS_FLEET_AUTH_TOKEN` 在第一个本地账号出现后也会失效。

启动前先校验：

```bash
sudo -u incus-fleet env $(grep -v '^#' /home/incus/incus-fleet.env | xargs) \
  /home/incus/bin/incus-fleet -config /home/incus/config.yaml -check
```

预期输出：

```
configuration valid: 2 cluster(s), database sqlite
```

### 用 PostgreSQL 代替 SQLite

初始化时通过修改 database 块来选择 PostgreSQL：

```yaml
database:
  driver: postgres
  dsn: postgres://incus_fleet:CHANGE_ME@127.0.0.1:5432/incus_fleet?sslmode=disable
```

schema 会自动创建。如果愿意，也可以把 DSN 放在环境变量文件中，而不是 YAML 中：

```bash
INCUS_FLEET_DATABASE_DRIVER=postgres
INCUS_FLEET_DATABASE_DSN=postgres://...
```

迁移历史属于运维数据。要把现有部署从 SQLite 迁移到 PostgreSQL：先准备新数据库，停止应用，切换 driver/DSN，再重新启动。历史会从头开始；实例状态从不存储在这个数据库中。（目前尚未提供 dump/restore 辅助工具。）

### 用 rqlite 替代单个 SQLite 文件

rqlite 在每个节点上保存一份复制的 SQLite 数据库，不需要 PostgreSQL 服务器。这是让多个应用实例共享一份存储的最轻量方式。

在所有要参与投票的节点上安装二进制：

```bash
curl -fsSL -o rqlite.tar.gz \
  https://github.com/rqlite/rqlite/releases/download/v10.7.3/rqlite-v10.7.3-linux-amd64.tar.gz
tar xzf rqlite.tar.gz
sudo install -m 0755 rqlite-v10.7.3-linux-amd64/rqlited /usr/local/bin/rqlited
sudo useradd --system --home-dir /var/lib/rqlite --shell /usr/sbin/nologin rqlite
sudo install -d -o rqlite -g rqlite -m 0750 /var/lib/rqlite
```

在 `/etc/rqlite/rqlite.env` 中配置每个节点（复制 `deploy/examples/rqlite.env.example`）。第一个节点留空 `RQLITE_JOIN_ARGS`；其余节点指向第一个节点的 **HTTP** 端口（`:4001`），与 `deploy/examples/rqlite.env.example` 和 `deploy/systemd/rqlited.service` 保持一致：

```bash
# 节点 1
RQLITE_NODE_ID=1
RQLITE_HTTP_ADDR=192.0.2.20:4001
RQLITE_RAFT_ADDR=192.0.2.20:4002

# 节点 2（及 3）
RQLITE_NODE_ID=2
RQLITE_HTTP_ADDR=192.0.2.21:4001
RQLITE_RAFT_ADDR=192.0.2.21:4002
RQLITE_JOIN_ARGS=-join 192.0.2.20:4001
```

安装并启动 unit，先启动节点 1：

```bash
sudo install -m 644 deploy/systemd/rqlited.service /etc/systemd/system/rqlited.service
sudo systemctl daemon-reload
sudo systemctl enable --now rqlited
curl -fsS http://192.0.2.20:4001/ready && echo ready
curl -fsS http://192.0.2.20:4001/nodes
```

让应用连接任意节点即可。驱动会发现集群其余节点并在失败时重试，因此 leader 切换无需重启：

```yaml
database:
  driver: rqlite
  dsn: http://192.0.2.20:4001
```

**要三个投票节点，不是两个。** Raft 选出 leader 需要多数派：两个投票节点时，失去任意一个就只剩 1/2，不是多数派，集群会停止接受写入直到它恢复。三个投票节点可以容忍失去一个。

把现有 SQLite 部署迁移到 rqlite 时，在切换应用之前先把现有记录通过 HTTP API 写入。不需要建表：应用启动时会自己建 schema。

```bash
sqlite3 /home/incus/data/fleet.db .dump | grep -v '^CREATE\|^BEGIN\|^COMMIT\|schema_migrations' \
  > /tmp/seed.sql
python3 - <<'PY'
import json, urllib.request
stmts = [s.strip() for s in open('/tmp/seed.sql').read().split(';') if s.strip()]
req = urllib.request.Request('http://192.0.2.20:4001/db/execute?transaction',
    data=json.dumps(stmts).encode(), headers={'Content-Type': 'application/json'})
print(urllib.request.urlopen(req).read()[:200])
PY
```

同样的种子数据导入方式也适用于 PostgreSQL 部署：用 `sqlite3 ... .dump` 导出 SQLite 记录，去掉
`CREATE`/`BEGIN`/`COMMIT`/`schema_migrations` 行，再把 INSERT 语句通过 `psql` 导入，而不是走
rqlite 的 HTTP API（迁移用 `pg_dump`/`psql`）。建表同样由应用启动时的 schema 迁移完成。

操作前请备份 SQLite 文件。每个节点都保存完整副本，因此重建集群时任意节点的 `/var/lib/rqlite` 都足以恢复。

### 插件与 API token

插件是外部服务，通过同源代理在 UI 中呈现。在配置中启用并声明它们（完整设计见
[插件规范](plugins.md)）：

```yaml
plugins:
  enabled: true
  entries:
    - id: victoria-metrics
      enabled: true
      upstream: https://192.0.2.30:8428
      bearer_token_file: /etc/incus-fleet/vm.token   # 发送给插件的 token
      role: viewer                                    # 访问它的最低 fleet 角色
      ui:
        path: /plugin-ui/victoria-metrics/            # 必须位于 /plugin-ui/ 下
```

每个 `ui.path` 必须是绝对路径且位于 `/plugin-ui/` 下；插件管理页 `/plugins` 是保留路径。
token 请放在仅 root 可读的文件中，不要写进 YAML。

按用户划分的 scoped API token 通过 `GET`/`POST`/`DELETE /api/v1/tokens` 管理（可用 scope 见
`GET /api/v1/tokens/scopes`）。`POST` 响应只在返回时携带一次明文 secret。插件会获得一个绑定到其
`(id, instance)`、仅含批准过的 manifest scope 的 token；卸载插件会立即吊销该 token。

## 4. 安装 systemd 单元

```bash
sudo install -m 644 deploy/systemd/incus-fleet.service /etc/systemd/system/incus-fleet.service
sudo systemctl daemon-reload
sudo systemctl enable --now incus-fleet
sudo systemctl status incus-fleet --no-pager
```

示例单元以 `incus-fleet` 身份运行，读取 `/home/incus/incus-fleet.env`，并且只写入 `/home/incus/data`。如果你使用不同的部署目录，请调整路径。

跟踪日志：

```bash
sudo journalctl -u incus-fleet -f
```

本地验证：

```bash
curl -fsS http://127.0.0.1:8080/api/v1/healthz
curl -fsS http://127.0.0.1:8080/api/v1/version
```

### 节点网络管理（可选）

每个集群成员都有自己的 **Node network** 页面（Cluster → Nodes → Manage node）。fleet 所在
主机通过 `sudo -n` 本地执行；远程成员优先使用在线 host agent，否则走 SSH。SSH 路径需要下面的
sudo 白名单；agent 路径使用 `deploy/agent-sudoers`。

```bash
sudo install -m 0440 -o root -g root deploy/nodesudoers /etc/sudoers.d/incus-fleet-nodesudoers
sudo visudo -cf /etc/sudoers.d/incus-fleet-nodesudoers   # 语法检查
sudo mkdir -p /etc/network/interfaces.d /etc/wpa_supplicant
sudo systemctl daemon-reload && sudo systemctl restart incus-fleet
```

白名单覆盖 `ip`、`nft`、`iw`、`dhcpcd`、`wpa_supplicant`、`hostname`、`systemctl` 和 `rm`，
以及仅限 `/etc/network/interfaces.d/*`、`/etc/wpa_supplicant/fleet.conf`、
`/etc/dnsmasq.d/fleet-overlay-*.conf` 和 `/etc/systemd/system/fleet-overlay-dhcp-*.service`
的 `tee`（接口名与 overlay 名在使用前会校验）。不同发行版的二进制路径可能不同；如果主机把它们
放在别处，请调整该文件（runner 按相同的 PATH 顺序解析二进制，并把绝对路径传给 sudo）。

远程成员使用专用 SSH 身份。在应用主机上以服务账户生成密钥，并把公钥授权给每个成员上的
管理用户：

```bash
sudo -u incus-fleet install -d -m 0700 /home/incus/.ssh
sudo -u incus-fleet ssh-keygen -t ed25519 -N '' -f /home/incus/.ssh/id_ed25519
# 在每个成员上执行（管理用户必须具备上面的 sudo 白名单）：
ssh-copy-id -i /home/incus/.ssh/id_ed25519.pub node-admin@192.0.2.20
```

在 `/home/incus/config.yaml` 中配置该身份：

```yaml
node_access:
  # auto：优先 agent，其次 local，最后 SSH；agent/ssh/local 强制单一后端。
  mode: auto
  # 没有 agent 时，变更操作是否允许回退 SSH。所有成员都部署 agent 后设为 false；
  # 读操作仍可用于 break-glass。
  ssh_write_fallback: true
  ssh_user: node-admin
  ssh_key: /home/incus/.ssh/id_ed25519
  ssh_port: 22
  # 生产环境请固定主机密钥：
  # ssh_known_hosts: /home/incus/.ssh/known_hosts
  ssh_insecure_ignore_host_key: true   # 仅实验环境；优先使用 known_hosts
```

SSH 账户需要对白名单中的二进制免密 sudo。如果该账户已经拥有全量免密 sudo，白名单是可选的。
**Add node** 工作流会为它使用的 SSH 账户自动安装这份白名单。

Linux 上 agent helper 对链路状态、静态地址和 fleet 路由（protocol 99）使用原生 netlink；命令
回退只用于非 Linux 开发/测试。给 agent 网络请求设置 `rollback_after_seconds` 会启动独立的回滚
计时器，`confirm` 会取消它。

示例 systemd 单元设置 `NoNewPrivileges=false` 和 `PrivateDevices=false`，因为 sudo 是 setuid
二进制，且 `ip`/`iw`/`nft` 需要真实的设备视图；`ReadWritePaths` 授予上述 `/etc` 路径，另加
`/etc/dnsmasq.d` 和 `/etc/systemd/system/fleet-overlay-dhcp-*.service`，供私有 overlay 写入
dnsmasq 配置和每个 overlay 的 systemd unit。请保留其余加固项。Wi-Fi 扫描/连接还需要在每个节点
安装 `wpasupplicant`（`apt-get install -y wpasupplicant`）。

升级后的首次启动会通过 schema 迁移创建主机任务和审计表；现有迁移历史会保留。API 提供
`GET /api/v1/tasks`、`GET /api/v1/tasks/{id}` 和 `GET /api/v1/audit`。审计查询单次最多 200
条；自动保留/导出会在 agent 生命周期工作中加入。

### 主机 agent（可选）

Rust 主机 agent 是可选的。它用一次性 token 注册，通过出站 mTLS WebSocket 连接中心，上报
facts/心跳，并提供只读 `nodenet.snapshot` 任务。节点不需要开放入站端口。

在中心主机上生成私有 CA 和 agent gateway 的服务端证书：

```bash
sudo install -d -m 0755 /etc/incus-fleet
sudo openssl ecparam -name prime256v1 -genkey -noout -out /etc/incus-fleet/agent-ca.key
sudo openssl req -x509 -new -key /etc/incus-fleet/agent-ca.key -sha256 -days 3650 \
  -subj "/CN=incus-fleet-agent-ca" -out /etc/incus-fleet/agent-ca.crt
sudo openssl ecparam -name prime256v1 -genkey -noout -out /etc/incus-fleet/agent-server.key
sudo openssl req -new -key /etc/incus-fleet/agent-server.key \
  -subj "/CN=fleet.example" -out /etc/incus-fleet/agent-server.csr
printf 'subjectAltName=DNS:fleet.example,IP:192.0.2.20\nextendedKeyUsage=serverAuth\n' \
  | sudo tee /etc/incus-fleet/agent-server.ext >/dev/null
sudo openssl x509 -req -in /etc/incus-fleet/agent-server.csr \
  -CA /etc/incus-fleet/agent-ca.crt -CAkey /etc/incus-fleet/agent-ca.key \
  -CAcreateserial -days 825 -sha256 -extfile /etc/incus-fleet/agent-server.ext \
  -out /etc/incus-fleet/agent-server.crt
```

在中心配置中启用 gateway：

```yaml
agent:
  enabled: true
  listen: 0.0.0.0:8444
  tls_cert: /etc/incus-fleet/agent-server.crt
  tls_key: /etc/incus-fleet/agent-server.key
  client_ca: /etc/incus-fleet/agent-ca.crt
  ca_key: /etc/incus-fleet/agent-ca.key
```

从 UI/API 创建一次性 token（只显示一次）。在中心构建 Linux 二进制并开放安装器：

```yaml
agent:
  # ... gateway TLS 配置 ...
  install:
    enabled: true
    gateway_url: https://fleet.example:8444
    asset_url: https://fleet.example
    agent_binary: /home/incus/bin/incus-fleet-agent
    helper_binary: /home/incus/bin/incus-fleet-helper
    # 可选：用下面的多架构映射替代上面两个单一路径：
    # agent_binaries:
    #   amd64: /home/incus/bin/incus-fleet-agent-linux-amd64
    #   arm64: /home/incus/bin/incus-fleet-agent-linux-arm64
    # helper_binaries:
    #   amd64: /home/incus/bin/incus-fleet-helper-linux-amd64
    #   arm64: /home/incus/bin/incus-fleet-helper-linux-arm64
```

```bash
cargo build --manifest-path agent/Cargo.toml --release
go build -o bin/incus-fleet-helper ./cmd/incus-fleet-helper
sudo install -m 0755 agent/target/release/incus-fleet-agent /home/incus/bin/incus-fleet-agent
sudo install -m 0755 bin/incus-fleet-helper /home/incus/bin/incus-fleet-helper
```

在全新节点上，Add node 对话框会显示一条命令：

```bash
curl -fsSL https://fleet.example/api/v1/agent/install.sh | sudo sh -s -- \
  --token '<一次性 token>' --cluster c1 --member node-a
```

安装器通过 `uname -m` 检测架构；配置了 `agent_binaries`/`helper_binaries` 时下载对应架构的
二进制，校验 SHA-256，安装 CA、sudoers 白名单、systemd unit 和服务用户，完成注册并启动
`incus-fleet-agent`。目标主机需要 `curl`、`sudo`/`visudo`、`sha256sum`、`runuser`、`useradd`、
`groupadd` 和 systemd。安装命令应从 UI 源站获取，即使用 `agent.install.asset_url`（未设置时使用 UI
源站），**不要**从 8444 端口的 `gateway_url` 获取 —— 该端口是 agent 的 mTLS WebSocket gateway，
不是资源服务器。中心位于反向代理后时应设置 `asset_url`；否则安装器会从请求推导资源地址。
如果中心使用私有 HTTPS 证书，请配置节点信任库，或在外层 `curl` 加 `--insecure`（注册本身仍会
校验 agent gateway CA）。

离线主机仍可手动安装：

```bash
sudo install -m 0755 agent/target/release/incus-fleet-agent /usr/local/bin/incus-fleet-agent
sudo install -m 0755 bin/incus-fleet-helper /usr/libexec/incus-fleet-helper
sudo install -m 0440 -o root -g root deploy/agent-sudoers \
  /etc/sudoers.d/incus-fleet-agent
sudo visudo -cf /etc/sudoers.d/incus-fleet-agent
sudo useradd --system --home /var/lib/incus-fleet-agent \
  --shell /usr/sbin/nologin incus-fleet-agent
sudo install -d -m 0700 -o incus-fleet-agent -g incus-fleet-agent /var/lib/incus-fleet-agent
sudo install -m 0644 deploy/systemd/incus-fleet-agent.service \
  /etc/systemd/system/incus-fleet-agent.service
sudo systemctl daemon-reload

read -r -s AGENT_TOKEN
printf '%s' "$AGENT_TOKEN" | sudo -u incus-fleet-agent \
  /usr/local/bin/incus-fleet-agent enroll \
  --center https://fleet.example:8444 \
  --ca /etc/incus-fleet/agent-ca.crt \
  --cluster c1 --member node-a \
  --state-dir /var/lib/incus-fleet-agent
sudo systemctl enable --now incus-fleet-agent
```

agent 上报 facts/心跳、只读快照，并可经 helper 执行允许列表内的网络变更。eBPF、Incus bootstrap
和遥测持久化可通过可选对象/依赖启用。示例单元使用 `NoNewPrivileges=false`（sudo 需要 setuid
转换）、`ProtectSystem=strict`、`MemoryMax=128M` 和 `CPUQuota=25%`；在最低配节点上验证资源
目标后再扩大使用范围。

#### 主机指标

启用中心的 1 小时缓冲：

```yaml
metrics:
  enabled: true
  retention: 1h
  interval: 10s
```

agent 每 10 秒上报主机 CPU/内存/负载/运行时间/网络计数器。中心不会把指标写入
SQLite/PostgreSQL；重启中心会清空缓冲。节点详情页显示最近 1 小时，API 可导出 JSON、
Prometheus text 或 CSV。Prometheus/VictoriaMetrics remote write、查询代理和 HTTP SD 已支持。

#### Agent 生命周期、HA 与审计

证书轮换和吊销通过管理员 API 完成。先从 agent 的 identity 目录导出公钥：

```bash
sudo openssl pkey -in /var/lib/incus-fleet-agent/identity/client.key \
  -pubout -out /tmp/agent-public.pem
```

```bash
# 为 agent 现有公钥签发新证书
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"public_key\":$(jq -Rs . < agent-public.pem)}" \
  https://fleet.example/api/v1/agents/AGENT_ID/certificate
# 吊销
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://fleet.example/api/v1/agents/AGENT_ID/revoke
```

gateway 每次连接都会检查吊销状态。agent 在证书离过期 30 天内时自动续期：用当前 mTLS 身份调用
`POST /api/v1/agent/renew`，把返回的证书写入 identity 目录后重连。

审计历史可以绕过 UI 的 200 条限制导出：

```bash
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  'https://fleet.example/api/v1/audit/export?format=jsonl' > audit.jsonl
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" \
  'https://fleet.example/api/v1/audit/export?format=csv' > audit.csv
```

主机任务已有数据库级 lease（`lease_owner`、`lease_until`、
`ClaimHostTask`/`RenewHostTaskLease`/`ReleaseHostTask`）。PostgreSQL 使用
`FOR UPDATE SKIP LOCKED`；SQLite 通过事务串行化。启用后台 dispatcher 消费 queued 任务：

```yaml
ha:
  enabled: true
  worker_id: app-a          # 每个中心实例唯一；默认 hostname-pid
  lease: 30s
  interval: 2s
```

让两个或更多中心实例连接同一个 PostgreSQL。每个实例同时只领取一个任务，运行期间续租。通过
`POST /api/v1/tasks` 入队异步 agent 网络操作；节点页面的同步操作行为不变。SQLite 下 lease 仍
可用，但只应有一个实例运行 dispatcher。

使用 detached Ed25519 签名升级 agent：

```bash
incus-fleet-agent upgrade \
  --binary incus-fleet-agent-linux-amd64 \
  --signature incus-fleet-agent-linux-amd64.sig \
  --public-key release-ed25519.pub \
  --target /usr/local/bin/incus-fleet-agent
```

安装器验证签名、保留 `incus-fleet-agent.prev`、以 0755 安装新二进制并运行 `--version`；健康检查
失败时恢复旧二进制。成功后如果 systemd 正在运行，会执行 `systemctl try-restart incus-fleet-agent`。
使用 `--no-restart`（或 `INCUS_FLEET_UPGRADE_NO_RESTART=1`）跳过重启。

#### Prometheus/VictoriaMetrics

启用 remote write 和查询代理：

```yaml
metrics:
  enabled: true
  remote_write:
    enabled: true
    url: https://prometheus.example/api/v1/write
    bearer_token_file: /etc/incus-fleet/prom.token
    timeout: 10s
    queue_size: 10000
    batch_size: 500
    spool_dir: /home/incus/data/remote-write
    spool_partition: app-a     # 每个中心实例唯一
    max_bytes: 67108864
  query:
    enabled: true
    url: https://prometheus.example
    type: prometheus          # 或 victoriametrics
    timeout: 15s
  http_sd:
    enabled: true
    mode: proxy
    token: replace-with-a-random-sd-token
    public_url: https://fleet.example
```

remote write 队列是磁盘持久化的。每个中心实例写入自己的分区 `spool_dir/<spool_partition>`。
`spool_partition` 默认依次取 `ha.worker_id`、无冲突的 `hostname-pid`；需要稳定名称时显式设置。
每个分区都有 `lock` 文件；活跃实例持有独占 flock，其他实例无法重放或删除其文件。新实例启动时
会持有根锁，接管所有未加锁的过期分区（例如崩溃或 PID 变化后），先重放其中的批次再打开自己的
分区。两个实例使用相同显式分区启动会立即失败，而不是重复发送。每个批次先写入再进入内存队列；
发送成功后删除文件，崩溃/重启遗留的文件会被重放。升级时 `spool_dir` 根目录下的旧文件会迁移进
当前分区。`queue_size` 限制内存队列，`max_bytes` 限制每个分区（默认 64 MiB），任一上限超出时
删除最旧文件。429/5xx 后退重试；4xx 批次作为永久拒绝丢弃。`spool_dir` 默认是文件型 SQLite
数据库旁的 `data/remote-write`。Prometheus 需要
`--web.enable-remote-write-receiver`；VictoriaMetrics 单机使用
`http://vm:8428/api/v1/write`，集群使用
`http://vminsert:8480/insert/<accountID>/prometheus/api/v1/write`。token 放在文件或环境变量中，
不要提交到 YAML。

Prometheus 可以通过以下配置发现中心代理抓取目标：

```yaml
scrape_configs:
  - job_name: incus-fleet
    http_sd_configs:
      - url: https://fleet.example/api/v1/sd/agents
        authorization:
          credentials_file: /etc/prometheus/fleet-sd.token
```

`mode=proxy` 由 Prometheus 抓取中心。`mode=direct` 需要给每个 agent 启动指标监听，并使用相同的
抓取 token：

```bash
# /etc/incus-fleet/agent-metrics.token 保存 bearer token
incus-fleet-agent run --state-dir /var/lib/incus-fleet-agent \
  --metrics-listen 0.0.0.0:9100 \
  --metrics-advertise 192.0.2.11:9100 \
  --metrics-token-file /etc/incus-fleet/agent-metrics.token
```

配置 `metrics.http_sd.mode: direct` 后，SD 端点返回 `metrics_address` 目标和
`__metrics_path__=/metrics`。Prometheus 抓取任务使用同一个 bearer token。没有上报地址的 agent
会被跳过；SD 端点只返回在线 agent。

不连接中心也可以验证抓取路径：

```bash
incus-fleet-agent metrics-serve \
  --listen 127.0.0.1:19100 \
  --token-file /etc/incus-fleet/agent-metrics.token \
  --interval-secs 2
curl -fsS -H 'Authorization: Bearer <token>' http://127.0.0.1:19100/metrics
```

真实 Prometheus 3.x 抓取任务通过 `authorization.credentials` 和
`http_sd_configs.authorization.credentials` 解析中心 SD 响应，抓取
`http://<metrics_address>/metrics`，并附加 `cluster`/`member`/`agent_id` 目标标签。bearer scheme
大小写不敏感，但 token 大小写敏感。

#### eBPF 遥测（可选）

agent 始终从 `/sys/fs/cgroup` 采集 cgroup v2 实例指标，并从 `/proc` 采集主机回退计数器。eBPF
是可选的，增加主机 TCP 重传和 OOM kill 计数。要求 Linux 内核 >= 5.8 且启用 BTF
（`/sys/kernel/btf/vmlinux`）、`CAP_BPF`/`CAP_PERFMON`（或 root），以及从
`agent/bpf/metrics.bpf.c` 构建的对象：

```bash
sudo apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r)
make agent-ebpf
sudo install -d -m 0755 /usr/lib/incus-fleet
sudo install -m 0644 agent/bpf/metrics.bpf.o /usr/lib/incus-fleet/metrics.bpf.o
```

在 agent 的 `ExecStart` 中加入 `--ebpf-object /usr/lib/incus-fleet/metrics.bpf.o`，并在 unit 中
启用 `AmbientCapabilities=CAP_BPF CAP_PERFMON`。如果对象、BTF 或权限缺失，agent 会记录原因并
继续使用 procfs/cgroup，不会丢失遥测。

启用前可以用 agent 自带的加载器验证对象：

```bash
sudo incus-fleet-agent ebpf-self-test \
  --object /usr/lib/incus-fleet/metrics.bpf.o --hold-secs 10
# initial_tcp_retransmits=0
# initial_oom_kills=0
# tcp_retransmits=...
# oom_kills=...
```

归因异常时用 `--debug-cgroups` 打印观测到的 cgroup id 和映射。人为丢包时 `tcp_retransmits`
有增量说明主机 tracepoint 已挂载；容器内 block I/O 或重传应增加 `instance=<name> ...
tcp_retransmits=...` 行。selftest 还会打印 `socket_cgroups`（sockops 已学习的 flow 映射数量）、
`oom_exact`/`oom_fallback` 和 `oom_debug` 计数。IPv4 和 IPv6 的 loopback/容器 flow 均受支持。
在隔离 VM 中制造受控全局 OOM 时，应看到 `oom_exact=1`、`oom_fallback=0` 和
`instance=<name> ... oom_kills=1`。

per-cgroup eBPF 指标通过读取每个 cgroup 的内核 file handle（包括嵌套的 systemd cgroup）归因，
容器 block I/O 会变成 `fleet_instance_block_latency_seconds_*`，以及重传/OOM 计数器。block 延迟
bucket 使用 2 的幂纳秒。VM 只做宿主侧 cgroup 指标。TCP 重传通过 sockops 维护的 IPv4/IPv6
flow→cgroup 映射归因，因此 softirq 中的重传定时器仍能保持容器归因。
sockops 使用 `BPF_PROG_ATTACH` + `BPF_F_ALLOW_MULTI` 挂载，可与其他 sockops 程序共存；如果内核不
支持该模式，agent 会跳过 sockops 而不是替换现有程序，并回退当前任务归因。OOM kill 按实例聚合
嵌套 cgroup。OOM tracepoint 在内核态通过 `bpf_task_from_pid` 和 CO-RE 读取
`task_struct->cgroups->dfl_cgrp->kn->id`，把 victim pid 精确归因到所属 cgroup，因此 root 上下文的
全局 OOM 会归到被杀的容器而不是 OOM killer。没有这些 kfunc 的内核使用当前 cgroup 路径，并通过
ring buffer 携带 victim pid，以 `/proc/<pid>/cgroup` 回退。主机计数器保持精确。
`fleet_agent_telemetry_mode` 报告 `ebpf` 或 `procfs`。

#### 通过 agent 引导 Incus

agent 上线后，中心可以通过 `POST /api/v1/agents/{id}/bootstrap` 准备 Incus。spec 使用与 agent
注册相同的 CA 私钥签名；helper 用 `/var/lib/incus-fleet-agent/identity/ca.pem` 验证。CA 私钥
只应保留在中心。

```bash
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"mode":"join","address":"192.0.2.11","storage_source":"/srv/incus",
       "install_mode":"existing","version_pin":"7.0"}' \
  https://fleet.example/api/v1/agents/AGENT_ID/bootstrap
```

`install_mode=package` 要求节点上已配置 Incus 软件仓库。helper 使用 apt、dnf、zypper、apk 或
pacman，并拒绝添加未验证的 `repo_url`。新集群使用 `"mode":"init"` 和 `network_cidr`；默认创建
`/srv/incus` 的 dir 存储池和 `incusbr0` 网桥。重复请求是安全的：已完成步骤记录在
`/var/lib/incus-fleet-helper/bootstrap-<task_id>.json`。

#### 从 UI 添加节点

**Add node** 对话框默认使用 **主机 agent** 流程，需要时回退到 **SSH**。

agent-first 加入步骤：

1. 填写精确的成员名称和集群地址。
2. 点击 **创建注册 token**。对话框显示一次性 token 和一条安装命令；该命令会下载 agent、安装
   systemd unit 和 sudoers 白名单、完成注册并启动服务。以 root 执行后，对话框会轮询 agent 上线状态。
3. agent 上线后点击 **通过 agent 引导**。中心签发 join spec，agent helper 安装 Incus（仅在勾选
   **通过包管理器安装 Incus** 时）、执行 preseed 加入集群，并返回完成的步骤。

成员名称必须与 agent 的 `--member` 一致。包安装使用节点上已配置的 Incus 仓库；helper 不会添加
未验证的仓库。随时可以点击 **改用 SSH** 切换到 SSH/nodejoin 路径。

SSH 路径需要：

- 能通过 SSH 连接目标，且账户可以执行 `sudo`（可以在对话框中输入一次性的 sudo 密码）。
- 目标能访问现有集群使用的 Incus apt 仓库。安装的 Incus 版本必须与集群版本一致。
- 成员本地存储池有可写的 source 路径（默认 `/srv/incus`）。
- 如果要运行 VM，对话框可以安装 `qemu`、`ovmf` 和 `swtpm`。

为了方便新节点接入，对话框默认跳过 SSH 主机密钥校验；生产环境请配置 `ssh_known_hosts` 并
取消该选项。SSH 和 sudo 密码只用于本次请求，不会被存储。

### 中心 mesh（Raft 高可用）

center 可以运行为 active-active mesh。使用奇数投票节点：三个 center，或两个 center 加一个轻量
witness（witness 只投票，不运行 gateway 和任务 dispatcher）。

```yaml
mesh:
  enabled: true
  node_id: app-a
  address: 192.0.2.20:9400          # Raft 传输地址
  control_address: 192.0.2.20:9401  # 内部派发中继地址
  data_dir: /home/incus/data/mesh
  peers:
    - id: app-a
      address: 192.0.2.20:9400
      control_address: 192.0.2.20:9401
    - id: app-b
      address: 192.0.2.21:9400
      control_address: 192.0.2.21:9401
    - id: witness
      address: 192.0.2.22:9400
      control_address: 192.0.2.22:9401
  witness: false                    # 仅 witness 主机设为 true
  force_single: false
```

在所有投票节点上把 `INCUS_FLEET_MESH_SECRET` 设为同一个值；不要提交到仓库。所有节点的 `peers`
必须相同且包含 witness。witness 使用同一个二进制，设置 `witness: true` 和 `agent.enabled: false`；
不要把浏览器或 agent 流量指向它。

mesh 复制以下状态：

- leader 选举和 fencing term。3 个投票节点可容忍 1 个故障而不失去写 quorum。
- agent 连接归属：agent 连接后，本地 center 把 owner 写入 Raft；如果 WebSocket 落在 follower 上，
  会把更新转发给 leader。owner 带 60 秒 lease，由 agent 心跳（默认 15 秒）续租，因此崩溃的 center
  不会留下永久过期的 owner。其他 center 通过 `control_address` 把任务中继给 owner。这样即使异步任务在另一台 center 上领取，同步节点操作
  也能正确派发。
- 使用 `rqlite` 或 PostgreSQL 时，会话在共享数据库中，leader 切换后用户不会被登出。单节点 SQLite 文件不能支撑多个 center。

agent 始终连接虚拟的 `agent.install.gateway_url`（Caddy/VIP），不知道当前 leader 是谁。所有
center 必须共享 agent CA 和服务端证书。

集群 endpoint 也支持故障切换：在 `endpoint` 之外配置 `endpoints`；列表存在时会替代 `endpoint`
用于拨号，因此第一项应与 `endpoint` 相同。manager 按顺序尝试，并每 10 秒对当前 client 做健康检查，
发现无响应时切到下一个 endpoint：

```yaml
clusters:
  - name: c1
    endpoint: https://192.0.2.10:8443
    endpoints:
      - https://192.0.2.10:8443
      - https://192.0.2.11:8443
```

#### 灾难恢复

失去 quorum（3 个节点挂 2 个）后会停止写入。要临时恢复单中心服务，先隔离失效节点、备份幸存
节点，然后调用：

该请求必须**直接发送给幸存节点**，不要经过负载均衡：

```bash
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"confirm":true}' \
  https://192.0.2.20:8080/api/v1/mesh/force-single
```

这会把幸存节点重置为单 voter 集群。Caddy/VIP 只指向该节点；agent 用同一个 URL 重连。其他节点
恢复后，清空它们的 mesh 数据目录，并以非 voter 身份加入，追平后再提升：

```bash
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"id":"app-b","address":"192.0.2.21:9400","control_address":"192.0.2.21:9401","voter":false}' \
  https://fleet.example/api/v1/mesh/join
curl -fsS -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"app-b"}' \
  https://fleet.example/api/v1/mesh/promote
```

`GET /api/v1/mesh/status` 可查看 leader、term 和 voter 集合。

## 5. 配置 Caddy

负载均衡策略取决于数据库驱动。

使用 `rqlite` 或 `postgres` 时，会话和所有状态都在共享数据库中，任意实例都能处理任意请求。用普通轮询即可；健康检查会摘掉故障实例，浏览器无需重新登录：

```caddyfile
fleet.example.internal {
    encode zstd gzip

    reverse_proxy 192.0.2.20:8080 192.0.2.21:8080 {
        lb_policy round_robin
        lb_try_duration 5s
        lb_try_interval 250ms
        fail_duration 30s
        max_fails 2
        health_uri /api/v1/healthz
        health_interval 10s
        health_timeout 2s
    }

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        -Server
    }
}
```

使用 `sqlite` 时，会话在进程内存中，Caddy 必须把每个浏览器固定到一个实例。用 `lb_policy cookie`，且不要在代理后面运行第二个实例：

```caddyfile
fleet.example.internal {
    encode zstd gzip

    reverse_proxy 192.0.2.20:8080 {
        lb_policy cookie incus_fleet_lb
        fail_duration 30s
        max_fails 2
        health_uri /api/v1/healthz
        health_interval 10s
        health_timeout 2s
    }

    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        -Server
    }
}
```

使用真实 DNS 名称，或对仅内网部署使用 Caddy 的内部 CA。在 Caddy 处终止 TLS；应用实例在内网以纯 HTTP 提供服务。Caddy 会设置 `X-Forwarded-Proto`，因此即便上游连接是 HTTP，应用也会把会话 cookie 标记为 `Secure`。直接以纯 HTTP 访问应用实例也可用于最初的内网测试，但此时 cookie 不会被标记为 `Secure`。

使用 `sqlite` 时，不带粘性的轮询会让登录间歇失败：会话 cookie 只在创建它的实例上有效。这正是迁移到共享数据库的原因。

Caddy 会透明代理 WebSocket：终端、VGA、SPICE 和文件端点（`/api/v1/.../console|exec|vga|spice|files`）无需额外配置。使用 `sqlite` 时，粘性 cookie 会把它们保持在同一实例。共享数据库下任意实例都能处理，但 SPICE 客户端每个会话会打开多个 websocket 连接，重连可能落到不同实例；如果依赖长连接的 SPICE，请保留粘性会话。

## 6. 防火墙与 SPICE

Incus Fleet 会为需要图形控制台的虚拟机配置一个 SPICE TCP 监听。该监听没有 ticket 认证，绑定在集群成员的所有网卡上，端口按实例名在 **5910-5989** 范围内分配。不要向不受信任的网络暴露该范围。仅允许为浏览器 websocket 做桥接的应用主机访问，例如：

```bash
sudo nft add rule inet filter input tcp dport 5910-5989 \
  ip saddr @fleet_app_hosts accept
```

容器 exec 与串行控制台不需要任何额外的集群侧端口；它们通过 Incus API 传输。

## 7. 部署升级

1. 构建新的二进制并复制到 `/home/incus/bin/incus-fleet`。
2. 重新安装更新后的 unit 文件，让加固修复生效，然后 reload 并重启：

   ```bash
   sudo install -m 644 deploy/systemd/incus-fleet.service /etc/systemd/system/incus-fleet.service
   sudo install -m 0440 -o root -g root deploy/nodesudoers /etc/sudoers.d/incus-fleet-nodesudoers
   sudo visudo -cf /etc/sudoers.d/incus-fleet-nodesudoers
   sudo systemctl daemon-reload
   sudo systemctl restart incus-fleet
   ```

   首次启动会应用 schema v11，新增 `answer_profiles.packages` 列。
3. 在该主机上验证健康状态，然后在另一台主机上重复安装二进制/unit 并重启。
4. 未完成的迁移会在启动时被标记为失败。确认目标端没有残留的部分副本后，重新提交这些迁移。

回滚时恢复之前的二进制并重启。数据库 schema 是增量的；目前不需要降级步骤。

## 8. 备份

在每台应用主机上备份：

- `/home/incus/config.yaml`（若 token 位于环境变量文件中，则不包含机密）；
- `/home/incus/incus-fleet.env`（包含管理员 token —— 请妥善保护）；
- `/home/incus/certs/`（私钥）；
- `/home/incus/data/fleet.db`（SQLite）或 PostgreSQL 数据库；
- `/home/incus/data/mesh`（`mesh.data_dir`，Raft 状态）和
  `/home/incus/data/remote-write`（`metrics.remote_write.spool_dir`，磁盘持久化的重放队列）。

数据库现在保存迁移历史以及共享的 SSH 密钥与脚本库。数据库丢失不会丢失实例，但会丢失这些可复用资产，因此应把它纳入定期备份。

复制数据库文件前，请停止服务或使用 SQLite 的在线备份。对于 PostgreSQL，请对 fleet 数据库使用 `pg_dump`。

## 9. 故障排查

| 现象 | 检查项 |
| --- | --- |
| API 返回 `401 unauthorized` | `Authorization: Bearer` token 正确，或会话 cookie 有效 |
| 登录时返回 `401 invalid_token` | 环境变量文件中的 token 与客户端发送的一致 |
| 集群被标记为离线 | 端点可达、`tls_server_cert` 正确、客户端证书已被信任 |
| `503 cluster_unavailable` | 从 UI/API 重新连接，并检查应用日志 |
| 在线迁移被拒绝 | 虚拟机运行中、`migration.stateful=true`、`mode=move` |
| 在线迁移后虚拟机不可达 | 客户机保留了源端 DHCP 租约；在客户机内续约 DHCP，或使用相同的集群子网 |
| 目标存储池不存在 | 在目标集群上创建同名的存储池 |
| 登录可用，刷新后失败 | Caddy 未使用粘性会话 |
| 重启后迁移卡住/失败 | 未完成的任务会被标记为失败；检查目标端是否有部分副本 |
| ISO 上传或启动失败 | ISO 卷存在于存储池中、虚拟机名未被占用、存储池有可用空间 |
| overlay DHCP 启动失败 | 已安装 `dnsmasq`；服务对 `/etc/dnsmasq.d` 有写权限（`ReadWritePaths`）；sudoers 白名单允许 `tee`/`systemctl`/`rm` |
| 一键安装 agent 失败 | `asset_url` 指向 UI 源站（不是 `gateway_url`）；已安装 `sha256sum`；已配置目标架构对应的 amd64/arm64 二进制 |
| 远程桌面无法连接 | 虚拟机运行中、Incus agent 就绪、防火墙允许应用主机访问 5910-5989 |
| 镜像上传返回 413 | 镜像超过 8 GiB；压缩根文件系统，或修改代码中的限制 |
| 批量操作报告部分失败 | 检查 207 响应中各实例的 `error`/`stderr`；重试失败的目标 |

在 Incus 侧检查：

```bash
incus config trust list
incus list
incus info --show-log <instance>
```

## 安全检查清单

- [ ] 示例文件除替换占位符外未作修改
- [ ] 管理员 token 使用至少 32 个随机字节生成
- [ ] 环境变量文件与私钥属主为服务账号，权限 `0600`
- [ ] git 中没有 token、密钥或仅本地保留的文档
- [ ] 终端 websocket 保持粘性（重连必须命中同一个应用实例）
- [ ] Caddy 提供 TLS；仅在使用 `sqlite` 驱动时需要粘性（共享数据库可轮询）
- [ ] 应用 API 端口 8080 只能从 Caddy 主机访问（防火墙）
- [ ] Incus API 8443 只能从应用主机访问（防火墙）
- [ ] SPICE 端口 5910-5989 只能从应用主机访问（防火墙）
- [ ] 插件 bearer token 放在仅 root 可读的文件中，插件 API token 仅含批准过的 scope
- [ ] `INCUS_FLEET_MESH_SECRET` 在所有 voter 上一致且未提交到仓库
- [ ] SD 与 remote write/metrics token 存放在文件或环境变量中，而非 YAML 中
- [ ] 数据库备份包含共享密钥与脚本库
- [ ] 推送前运行了机密扫描
