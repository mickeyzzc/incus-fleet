# 部署

[English](../deployment.md) | **简体中文**

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

每个应用实例都是独立的：独立的二进制、配置、证书、数据库和内存会话。不要把两个应用实例指向同一个 SQLite 文件。

## 前置条件

在每台应用主机上：

- 带 systemd 的 Linux；如果部署发布版二进制，则**不需要** Go。
- 能访问每个 Incus API 端点的 8443 端口。
- 一个专用服务账号和目录（示例使用 `/home/incus`）。
- SQLite（默认）或 PostgreSQL 服务器。

在每个 Incus 集群上：

- Incus 已初始化，且存储池与网络与你将要迁移的实例相匹配。
- 有权添加受信任的客户端证书（`incus-admin` 组或 root）。

## 1. 构建发布版二进制

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
# Optional overrides:
# INCUS_FLEET_LISTEN=0.0.0.0:8080
# INCUS_FLEET_DATABASE_DRIVER=postgres
# INCUS_FLEET_DATABASE_DSN=postgres://...
EOF
sudo chmod 600 /home/incus/incus-fleet.env
sudo chown incus-fleet:incus-fleet /home/incus/incus-fleet.env
```

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

## 5. 配置 Caddy

由于会话保存在内存中，Caddy 必须把每个浏览器固定到同一个应用实例。示例 `deploy/caddy/Caddyfile` 使用 `lb_policy cookie` 加上被动/主动健康检查：

```caddyfile
fleet.example.internal {
    encode zstd gzip

    reverse_proxy 192.0.2.20:8080 192.0.2.21:8080 {
        lb_policy cookie incus_fleet_lb
        lb_try_duration 5s
        lb_try_interval 250ms
        fail_duration 30s
        max_fails 2
        health_uri /api/v1/healthz
        health_interval 10s
        health_timeout 2s
    }
}
```

使用真实 DNS 名称，或对仅内网部署使用 Caddy 的内部 CA。在 Caddy 处终止 TLS；应用实例在内网以纯 HTTP 提供服务。Caddy 会设置 `X-Forwarded-Proto`，因此即便上游连接是 HTTP，应用也会把会话 cookie 标记为 `Secure`。直接以纯 HTTP 访问应用实例也可用于最初的内网测试，但此时 cookie 不会被标记为 `Secure`。

如果采用轮询而没有粘性，浏览器登录会间歇性失败，因为会话 cookie 只在创建它的实例上有效。

Caddy 会透明代理 WebSocket：终端、VGA、SPICE 和文件端点（`/api/v1/.../console|exec|vga|spice|files`）与 UI 使用同一个粘性 cookie，因此无需额外配置。SPICE 客户端每个会话会打开多个 websocket 连接，所有这些连接都必须到达同一个应用实例；当浏览器支持时，粘性 cookie 可以满足这一点。

## 6. 防火墙与 SPICE

Incus Fleet 会为需要图形控制台的虚拟机配置一个 SPICE TCP 监听。该监听没有 ticket 认证，绑定在集群成员的所有网卡上，端口按实例名在 **5910-5989** 范围内分配。不要向不受信任的网络暴露该范围。仅允许为浏览器 websocket 做桥接的应用主机访问，例如：

```bash
sudo nft add rule inet filter input tcp dport 5910-5989 \
  ip saddr @fleet_app_hosts accept
```

容器 exec 与串行控制台不需要任何额外的集群侧端口；它们通过 Incus API 传输。

## 7. 部署升级

1. 构建新的二进制并复制到 `/home/incus/bin/incus-fleet`。
2. 在一台主机上执行 `sudo systemctl restart incus-fleet`；验证健康状态。
3. 在另一台主机上重复。
4. 未完成的迁移会在启动时被标记为失败。确认目标端没有残留的部分副本后，重新提交这些迁移。

回滚时恢复之前的二进制并重启。数据库 schema 是增量的；目前不需要降级步骤。

## 8. 备份

在每台应用主机上备份：

- `/home/incus/config.yaml`（若 token 位于环境变量文件中，则不包含机密）；
- `/home/incus/incus-fleet.env`（包含管理员 token —— 请妥善保护）；
- `/home/incus/certs/`（私钥）；
- `/home/incus/data/fleet.db`（SQLite）或 PostgreSQL 数据库。

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
- [ ] Caddy 提供 TLS 并使用粘性会话
- [ ] 应用 API 端口 8080 只能从 Caddy 主机访问（防火墙）
- [ ] Incus API 8443 只能从应用主机访问（防火墙）
- [ ] SPICE 端口 5910-5989 只能从应用主机访问（防火墙）
- [ ] 数据库备份包含共享密钥与脚本库
- [ ] 推送前运行了机密扫描
