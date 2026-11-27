# Fleet 服务插件规范与打包

[English](../en/plugin-spec.md) | **中文**

插件是“由 Fleet 创建并接管的服务实例”。安装插件时 Fleet 会：

1. 在选定集群中按资源占用自动挑选节点（内存/存储压力取较大值，最小者胜出）；
2. 用插件声明的镜像创建容器，套用 CPU/内存/根盘/网卡默认值；
3. 等待实例拿到 IPv4；
4. 在实例内执行安装脚本；
5. 记录上游地址（`http://<ip>:<port>`）、签发插件作用域令牌，并把界面挂到
   `/plugin/<id>`；
6. 任何一步失败都会删除刚创建的实例，避免留下半成品。

> `manifest.yaml`（`schema: fleet-plugin/v1`）现为规范格式；下述 `plugin.json`
> 格式对仅接入的目录项仍然有效。配置签名密钥后，`scripts/pack-plugin.sh` 会生成
> `manifest.yaml.sig` HMAC 旁车文件。完整 schema、任务接口与当前限制见
> [插件系统](plugins.md)。

## 目录结构

```text
my-plugin/
├── plugin.json      # 必填，插件清单
└── install.sh       # 部署型插件必填，在实例内以 root 执行
```

## plugin.json

```json
{
  "id": "hello",
  "name": "Hello service",
  "description": "最小示例：部署一个 8080 端口的 HTTP 服务。",
  "ui_path": "/plugin/hello",
  "wiring": "example.hello_url",
  "image": "debian/13",
  "default_cpu": "1",
  "default_memory": "512MiB",
  "default_disk": "10GiB",
  "default_network": "incusbr0",
  "port": 8080,
  "health_path": "/",
  "install_script_file": "install.sh"
}
```

### 字段

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `id` | 是 | 小写 DNS 标签，全局唯一；决定 `/plugin/<id>` 与 `/plugin-ui/<id>/` |
| `name` | 是 | 展示名 |
| `description` | 否 | 商店卡片说明 |
| `ui_path` | 是 | 必须以 `/plugin/` 开头；Fleet 会用它生成侧边栏菜单 |
| `wiring` | 否 | 接线键，如 `metrics.query_url`、`logs.query_url`；供 Fleet 内部消费 |
| `image` | 部署型必填 | 集群中已存在的镜像别名，如 `debian/13` |
| `default_cpu` | 部署型必填 | 例如 `2` |
| `default_memory` | 部署型必填 | 例如 `2GiB` |
| `default_disk` | 部署型必填 | 根盘，例如 `20GiB` |
| `default_network` | 部署型必填 | 通常 `incusbr0`；也可用私有网络网桥 |
| `port` | 部署型必填 | 服务监听端口，用于生成上游地址 |
| `health_path` | 否 | 健康检查路径，供安装脚本/运维使用 |
| `install_script_file` | 部署型必填 | 相对清单的脚本路径，禁止 `..` |
| `upstream_hint` | 接入型必填 | 不部署实例、只接入已有服务时使用 |

> 部署型插件若用户显式提供 `upstream`，Fleet 会跳过部署，按“接入已有服务”处理。

## 安装脚本约定

- 以 `root` 在实例内通过 `sh -s` 执行，工作目录为 `/`；
- 必须自行保证服务在实例重启后仍然运行（推荐 systemd；容器内不可用时再退化为
  `nohup`）；
- 脚本退出码非 0 视为安装失败，Fleet 会删除实例；
- 不要在脚本里写入 Fleet 令牌或密码明文；需要凭据时由操作员在实例内配置；
- 安装脚本有 10 分钟超时。

## 资源默认值与调度

安装对话框会预填清单里的默认值，操作员可以改集群、节点、实例名、CPU、内存、存储和
网卡。节点留空时使用内置轻量调度：

```text
score(member) = max(memory.used/memory.total, storage.used/storage.total)
选择 score 最小且状态为 Online 的成员
```

这足以在少量节点间分散负载，不引入 Kubernetes 的复杂调度器。

## 打包

```bash
scripts/pack-plugin.sh plugins/examples/hello
# -> dist/hello.fleetplugin
```

脚本会校验必填字段、id 格式、端口、安装脚本是否存在，然后打包为 tar.gz。
把生成的 `.fleetplugin` 解压到 Fleet 配置的 `plugins.dir`（默认 `data/plugins`），
重启（或等待下次目录扫描）后即可在插件商店看到。

## 接线与安全

- 每个插件安装会签发一个只含 `read:plugins`、绑定插件 ID 的令牌；
- 卸载会删除实例、撤销令牌并移除菜单入口；
- 插件 UI 通过同源 `/plugin-ui/<id>/` 代理，代理不会转发 Fleet 会话 Cookie；
- 自定义插件默认按社区插件对待：不要假设它能获得宿主权限；Fleet 只通过实例内
  `root` 脚本安装，不向宿主写入文件。

## 参考示例

- `plugins/examples/hello/`：最小部署型插件；
- 内置 `vm`（VictoriaMetrics）与 `vmlogs`（VictoriaLogs）：官方定制服务，安装后
  自动接管指标/日志接线。
