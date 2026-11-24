**English** | [简体中文](../zh/qa-test-report.md)

> **Translation note:** this QA record is currently maintained in Chinese only; the Chinese text below is redacted for public release. An English translation is pending. See the [Chinese version](../zh/qa-test-report.md) for the full record.

# Incus Fleet Web 端全面测试报告（2026-11-23）

## 1. 测试环境

| 项 | 值 |
| --- | --- |
| Web（两实例共享 rqlite 状态） | `http://192.0.2.21:8080`（n305）、`http://198.51.100.21:8080`（n100） |
| 登录 | bootstrap admin / 密码登录，会话正常 |
| 集群 | `c62 on n100`（1 成员）、`c63 on n305`（2 成员：node-a、node-c） |
| 语言 | 中文（同时检查英文选项） |
| 浏览器 | 应用内 Chromium（通过 browser 工具全程 UI 操作） |

> 所有操作均通过浏览器 UI 完成；仅环境排障（查日志、装 dnsmasq、更新 sudoers/systemd drop-in）使用 SSH，且仅为让 Web 流程可继续执行。

## 2. 已执行用例与结果

| 用例组 | 结果 | 说明 |
| --- | --- | --- |
| A 登录/导航/语言 | 通过 | 登录、会话、集群侧栏、语言切换正常 |
| B 镜像与自动化 | 通过 | 新建 3 个应答配置（Debian/Ubuntu/Generic cloud-init），默认配置生效；镜像/ISO 列表、ISO 上传入口可访问 |
| C 无人值守安装 | **部分失败** | Debian、Arch 两条 ISO 无人值守流程均成功启动（API 201、派生卷生成）；但 Debian VM 30 分钟后仍停在 GRUB 安装菜单（BUG-010）；默认 profile 主机名错误（BUG-001，已修复代码）；Ubuntu 无 ISO/镜像，未能安装 |
| C+ Ubuntu 24.04 ISO 复测 | **通过** | 2026-11-24 Web 创建 `qa-ubuntu-iso-vm13`：`root.img=2.8G`、安装器自动关机、`iso0/iso1` 自动卸载；启动后 `vda2` 挂载 `/`、SSH `ubuuser` 登录成功。修复见 BUG-016（GPT ptable、离线 apt、bcrypt 密码、根盘 boot.priority）。 |
| D 容器服务 | **通过（3 个服务）** | `pgsql-01` PostgreSQL（原有）、`qa-nginx-01` Nginx 1.26.3（curl 返回 200）、`qa-redis-01` Redis（服务运行）；均经 Web 终端安装 |
| E 资源变更 | 部分通过 | CPU 1→2、内存 512→768MiB、根盘 5→8GiB、NIC bond0→incusbr0 修改成功；新增数据盘失败（BUG-003）；物理 NIC 选项含 lo（BUG-004）；提示文案错误（BUG-005） |
| F 运维 | 部分通过 | 迁移（复制模式 c63→c62）成功，迁移记录可见进度与完成状态；**改名不可用**（BUG-009）；快照/克隆/重建/文件管理入口存在（未逐一深度执行） |
| H 集群与节点 | 部分通过 | 节点信息编辑（描述/故障域/组）成功；节点网络页可读 NIC/路由/防火墙并支持配置；**节点删除不可用**（BUG-006），因此无法完成「删除后加回」闭环；Add node 对话框（agent/SSH）可用 |
| I 私有互联 | 通过（含环境修复） | 启用 `qa-overlay`（10.99.0.0/24、VNI 99001、1 条 VXLAN 隧道）成功；重新对账、关闭删除成功；**修改不可用**（BUG-008） |
| J 基础设施 | 部分通过 | 集群/受管网络/存储池/配置文件列表正常；物理 NIC 相关缺陷见 BUG-002/004 |
| K 管理面 | 部分通过 | 插件、用户管理、任务/审计入口存在；遥测页显示 `telemetry is not enabled`（n305 未启用 metrics 配置） |
| L 异常边界 | 记录缺陷 | 发现多个 500/503 返回与部分成功问题（BUG-002/003/007） |

## 3. 缺陷汇总（详见 issues.md）

| 编号 | 级别 | 标题 | 状态 |
| --- | --- | --- | --- |
| BUG-001 | Major | 默认应答配置主机名未使用 VM 名称 | **代码已修复**（`internal/httpapi/iso.go`，测试通过） |
| BUG-002 | Critical | 物理 NIC 创建实例未绑定节点 + 部分成功报 500 | 待修复 |
| BUG-003 | Major | 挂载「新建空卷」缺 source，返回 500 | 待修复 |
| BUG-004 | Minor | 物理网卡下拉框包含 `lo` | **代码已修复**（`internal/httpapi/handlers_cluster.go`）|
| BUG-005 | Minor | 选受管网络仍显示物理网卡说明 | 待修复 |
| BUG-006 | Blocker | Web 无法删除集群节点（无后端路由） | 待修复 |
| BUG-007 | Critical | systemd 单元缺 overlay 写目录 | **代码已修复**（`deploy/systemd/incus-fleet.service`；现场用 drop-in 修复后互联测试通过） |
| BUG-008 | Major | 私有互联无法修改（无 PATCH） | 待修复 |
| BUG-009 | Major | 实例无法改名（无前端/后端实现） | 待修复 |
| BUG-010 | Critical | Debian 无人值守未自动引导，卡在 GRUB | 待修复 |

## 4. 体验优化汇总（详见 ux-improvements.md）

- UX-01 内存/磁盘大小：单位下拉框 + 纯数字输入（用户示例）
- UX-02 危险操作分级确认（输入资源名）
- UX-03 数值范围与剩余容量提示
- UX-04 长任务阶段化进度
- UX-05 空状态直达按钮
- UX-06 错误提示可操作化
- UX-07 操作后列表不自动刷新、已启用仍显示「启用」按钮
- UX-08 资源输入显示节点剩余容量

## 5. 未能完成的测试与原因

1. **Ubuntu ISO / 镜像安装**：集群无 Ubuntu ISO，亦无 Ubuntu/Arch 云镜像；浏览器上传工具单文件上限 5 MiB，无法上传数百 MB~GB 级介质（人工通过浏览器文件选择器不受此限制）。
2. **Arch 真实验证**：Arch ISO 使用 Generic cloud-init profile 启动，但 Arch 官方 ISO 不消费 NoCloud seed，未形成真实系统安装；已记录该路径的可用性限制。
3. **删除并重新加回 node-c**：后端/前端均无节点删除能力（BUG-006），闭环无法完成。
4. **互联修改**：无 PATCH 接口与编辑 UI（BUG-008）。

## 6. 已落地修复

- `internal/httpapi/iso.go`：应用默认应答配置后，若主机名为空则回填 VM 名称。
- `internal/httpapi/handlers_cluster.go`（+ `resources_test.go`）：从物理 NIC 候选中过滤 loopback `lo`。
- `deploy/systemd/incus-fleet.service`：`ReadWritePaths` 增加 `/etc/dnsmasq.d` 与 `/etc/systemd/system/fleet-overlay-dhcp-*.service`。
- 以上 Go 修改均通过 `go test ./internal/httpapi ./internal/unattended`。

## 7. 建议优先级

P0：BUG-006（节点删除）、BUG-010（无人值守不自动引导）、BUG-002（部分成功 500）
P1：BUG-007 已修需发版、BUG-008、BUG-009、BUG-003、BUG-012、UX-01/10/11/12/15/16/19/20/30/31/32/34
P2：BUG-001/004/011 已修需发版、BUG-005、UX-02~09、UX-13/14、UX-17/18/21~29、UX-33/35/36

## 8. 复查补充（用户抽查后按同一标准全站扫描）

用户抽查发现 7 类问题（信息截断、该用下拉、可配置项太少、排版、局部刷新等），据此对全站做了系统性复查，新增：

- **BUG-011**：Wi-Fi 中文 SSID 显示为 `\xHH` 字面转义（已修复）
- **BUG-012**：Ubuntu/NoCloud 忽略 locale/keyboard/disk
- **UX-09～UX-14**：用户直接指出的 7 项
- **UX-15～UX-37**：全站复查发现（枚举误用文本框、容量字段、allow-create 滥用、已有资产未做选择器、校验不足、自定义应答丢 hostname、布局堆叠、局部刷新缺失、加载/空状态冲突、危险操作视觉区分、任务/审计字段被丢弃、`show-overflow-tooltip` 全站缺失等）

## 9. 视觉设计维度复查（第三批抽查后）

用户指出侧栏间距、层级字体、图标折叠与整体排版比例问题后，新增 **UX-38～43**：

- 侧栏行高 44+3+3≈50px、分组标题 16px、子项缩进 44px（实测计算样式确认）
- 桌面端**没有**真正的图标折叠：`sidebar-collapsed` 只在 ≤960px 把侧栏整栏 `display:none`，`el-menu` 无 `:collapse`
- 顶栏 h1 与各页 h2 标题重复（仪表盘出现三层「总览」）
- 字号 11～28px 无 scale；`--fleet-mono` / `--fleet-bg-soft` 未定义；硬编码浅色破坏深色模式
- 顶栏 82px、内容 48px 底距、15 种弹窗宽度、420px 操作列、内联筛选框宽度
- ≤960px 侧栏 55vh sticky 与顶栏争抢顶部；≤620px 断点覆盖不足

统一修复计划见 `docs/qa/fix-plan.md`（含「视觉设计专项」一节）。

## 10. 修复后部署与验证（2026-11-24）

构建：`make build`（Vue 生产包 + Linux 二进制）。
测试：Go `./cmd/... ./internal/... ./web` 全通过；前端 `651 passed (62 files)`。
部署：替换 node-a、node-b 的 `/home/incus/bin/incus-fleet` 并重启，`systemctl is-active` = active，`/api/v1/healthz` = 200。

已在 Web 上复测：

| 项 | 证据 |
| --- | --- |
| BUG-001 主机名 | 预览 preseed 含 `netcfg/get_hostname string qa-preview-vm` |
| BUG-002 物理网卡 | 新建 `qa-bond-test2` 落在 `node-a`（bond0 所属成员）并运行 |
| BUG-003 空卷挂载 | `qa-redis-01` 成功挂载 `data1`，`source=dev-qa-redis-01-data1` |
| BUG-006 节点删除 | `DELETE /members/node-c` 返回 400，明确提示需先迁移/删除 `qa-nginx-01, qa-redis-01` |
| BUG-008 互联修改 | Web 编辑 qa-overlay，MTU 1450→1400，列表即时更新 |
| BUG-009 实例改名 | 停止后 Web 改名为 `qa-nginx-c62`，列表刷新后生效 |
| BUG-010 UEFI/CSM | 新 VM `qa-debian-csm2` 配置 `security.csm=true`，截图为 Debian 安装器运行而非菜单卡死 |
| BUG-013 分区 | 根因是 partman 自动分区选项标签必须为 `auto`，且一次性检查早于 virtio 磁盘出现。已预置两键为 `auto`、延迟 partman 8s、根盘 virtio-blk、biosgrub+swap+ext4 recipe，并补齐 language/country/mirror/CD 扫描/Etc-UTC/GRUB 设备。**`qa-debian-csm17` 全自动零输入完成**：STOPPED、ISO 自动卸载，磁盘校验 `debian=13.8 / host=qa-debian-csm17 / grub=yes / ssh=yes`。csm11/csm12 也完成并可启动到登录界面 |
| BUG-014 ISO 命名 | 派生卷名为 `...-auto-qa-debian-csm4`，同一 ISO 连续安装不再冲突 |
| UX-10/32 指纹 | 镜像/集群媒体指纹显示复制按钮，title 为完整 SHA256 |
| UX-14 Wi-Fi | 节点页 Wi-Fi 面板出现「重新扫描」 |
| UX-25 刷新 | ISO/密钥/脚本/历史/实例监控出现局部刷新 |
| UX-38/39 侧栏 | 菜单行高降到 38px；点击侧栏按钮 `aside` 宽度 236→64px，分组标题隐藏 |
| UX-11/15 下拉 | 应答配置弹窗出现 4 个选择器（安装器/时区/语言/键盘） |
| UX-12 配置项 | 应答配置新增「附加软件包」；API 创建返回 `packages="curl vim htop"`，数据库迁移 v11 生效 |
| UX-02 危险确认 | 删除实例弹窗要求输入实例名，未输入时删除按钮 `disabled`；移除节点弹窗要求输入节点名 |
| UX-03/08 容量提示 | 创建实例选择 c63 后显示「集群内存：已用 … / 共 …」「集群存储：…」 |
| UX-13/23 布局 | 节点网络出现 4 个标签页（网卡/路由/防火墙/Wi-Fi） |
| UX-18 选择器 | 节点路由设备改为下拉（来自 NIC 列表）；磁盘来源改为存储卷下拉 + 自定义 |
| UX-27 操作列 | 实例行显示「停止/重启/操作」，操作菜单内含编辑/改名/迁移/删除；列宽 420→240 |
| UX-28 持久错误 | SSH 公钥/脚本标签页与节点历史弹窗增加错误横幅 + 重试 |
| UX-29/42/43 | 全局弹窗/抽屉视口上限、768px 表格与单列布局、筛选宽度自适应 |
| BUG-013 零输入 | `qa-debian-csm17` STOPPED、ISO 自动卸载；磁盘校验 `debian=13.8 / host=qa-debian-csm17 / grub=yes / ssh=yes` |
| UX-30/31/34 | 批量输出展开、公钥/脚本复制、任务/审计展开详情 |

未完全覆盖：

- 媒体补测（2026-11-24）：已下载并导入 Ubuntu 24.04 ISO、Ubuntu 24.04 cloud、Arch cloud。
  - `qa-ubuntu-cloud-vm` 通过 Web 创建，RUNNING，IP `10.63.0.136`。
  - `qa-arch-cloud-vm` 通过 Web 创建，RUNNING。
  - `qa-ubuntu-iso-vm2` 使用修复后的 autoinstall user-data（`autoinstall:` 嵌套）进行无人值守安装复测。
- 视觉重构其余项（UX-29 窄屏卡片、UX-42 全量弹窗规格、UX-43 抽屉式移动侧栏）仍在 `fix-plan.md` 的后续批次。
- UX-12 已实现「附加软件包」结构化字段（schema v11）；更多安装器字段可按同一模式扩展。

### UX 收尾批次（2026-11-24）

| 项 | 结果 |
| --- | --- |
| UX-19/20 表单校验与预览 | 应答配置表单增加名称/主机名即时校验、密码确认与强度条；自定义应答模式保留主机名并写入 NoCloud meta-data；新增 `POST /api/v1/answer-profiles/preview`，配置页可直接预览。浏览器验证：密码不一致被拦截、强度显示、预览返回 `preseed.cfg` 内容。 |
| UX-22 结构化配置 | ProfileDialog 增加 `limits.cpu` / `limits.memory` / `limits.disk` / `boot.autostart` 与设备类型下拉；NetworkDialog 增加 IPv4/IPv6 地址与 NAT、`bridge.mtu`、`dns.domain`；StoragePoolDialog 增加 `source`。浏览器验证：IPv4 回填 `10.63.0.1/24`、存储池「源」字段与提示正常显示。 |
| UX-04 长任务进度 | 迁移列表展示后端 stage 标签；ISO 上传卡片显示百分比/速度/ETA；无人值守弹窗增加「准备介质 → 自动安装 → 关机 → 卸载介质」阶段条。 |
| UX-06 错误可操作 | 应答配置列表增加持久错误横幅 + 重试；迁移列表保留重试入口。 |
| UX-29/42/43 响应式 | ≤960px 侧栏改为抽屉式 + 遮罩，导航后自动关闭；≤768px 全部表格通过表头 `data-label` 转为卡片布局；弹窗/抽屉宽度上限与单列网格保留。App 测试覆盖窄屏初始折叠与遮罩。 |

验证：前端 63 文件 / 668 测试全部通过，coverage 语句 91.67%、函数 90.68%、分支 86.30%、行 92.03%；`vue-tsc`、Go 全包与 Go 覆盖率门槛通过；n305/n100 已部署本轮构建。
