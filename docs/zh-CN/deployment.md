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

白名单覆盖 `ip`、`nft`、`iw`、`dhcpcd`、`wpa_supplicant`、`hostname`，以及仅限
`/etc/network/interfaces.d/*` 和 `/etc/wpa_supplicant/fleet.conf` 的 `tee`（接口名在使用前
会校验）。不同发行版的二进制路径可能不同；如果主机把它们放在别处，请调整该文件（runner 按
相同的 PATH 顺序解析二进制，并把绝对路径传给 sudo）。

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
二进制，且 `ip`/`iw`/`nft` 需要真实的设备视图；`ReadWritePaths` 授予上面两个 `/etc` 目录。
请保留其余加固项。Wi-Fi 扫描/连接还需要在每个节点安装 `wpasupplicant`
（`apt-get install -y wpasupplicant`）。

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

从 UI/API 创建一次性 token（只显示一次），然后构建并注册 agent：

```bash
cargo build --manifest-path agent/Cargo.toml --release
go build -o bin/incus-fleet-helper ./cmd/incus-fleet-helper
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

agent 上报 facts/心跳、只读快照，并可经 helper 执行允许列表内的网络变更。eBPF、Incus
bootstrap 和指标持久化仍在后续阶段。示例单元使用 `NoNewPrivileges=false`（sudo 需要 setuid
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
Prometheus text 或 CSV。Prometheus/VictoriaMetrics remote write 和 HTTP SD 在后续阶段。

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

remote write 队列是磁盘持久化的。每个批次先写入 `spool_dir` 再进入内存队列；发送成功后删除文件，
崩溃/重启遗留的文件会在启动时重放。`queue_size` 限制内存队列，`max_bytes` 限制 spool（默认
64 MiB），任一上限超出时删除最旧文件。429/5xx 后退重试；4xx 批次作为永久拒绝丢弃。
`spool_dir` 默认是文件型 SQLite 数据库旁的 `data/remote-write`。Prometheus 需要
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
有增量说明主机 tracepoint 已挂载；容器内 block I/O 应增加 `instance=<name> ...
block_latency_count=...` 行和 bucket 计数。

per-cgroup eBPF 指标通过读取每个 cgroup 的内核 file handle（包括嵌套的 systemd cgroup）归因，
容器 block I/O 会变成 `fleet_instance_block_latency_seconds_*`，以及重传/OOM 计数器。block 延迟
bucket 使用 2 的幂纳秒。VM 只做宿主侧 cgroup 指标。TCP 重传归因是尽力而为，因为定时器可能在
softirq 上下文运行；主机计数器是精确的。`fleet_agent_telemetry_mode` 报告 `ebpf` 或 `procfs`。

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
2. 点击 **创建注册 token**。对话框显示一次性 token 和 `incus-fleet-agent enroll` 命令；在节点上
   执行后，对话框会轮询 agent 上线状态。
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
