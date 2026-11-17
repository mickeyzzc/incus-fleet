# 变更日志

[English](CHANGELOG.md) | **简体中文**

本文件记录项目的所有重要变更。格式遵循
[Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循
[语义化版本](https://semver.org/lang/zh-CN/)。

## 未发布

### 新增

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

### 修复

- 集群 TLS 证书字段同时接受内联 PEM 正文和文件路径。
- `.gitignore` 排除 `*.crt` 证书文件。

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
