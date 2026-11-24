**English** | [简体中文](../zh/qa-fix-plan.md)

> **Translation note:** this QA record is currently maintained in Chinese only; the Chinese text below is redacted for public release. An English translation is pending. See the [Chinese version](../zh/qa-fix-plan.md) for the full record.

# Web 端问题统一修复计划

> 来源：2026-11-23 全站 E2E 实测 + 用户抽查后按「信息展示完整性 / 控件适配 / 配置项丰富度 / 排版 / 局部刷新」标准的全站复查。
> 详见 `issues.md`（BUG-001～012）、`ux-improvements.md`（UX-01～37）、`test-report.md`。

## 一、已完成的代码修复（待发版验证）

| 项 | 文件 | 说明 |
| --- | --- | --- |
| BUG-001 | `internal/httpapi/iso.go` | 默认应答配置 hostname 为空时回填 VM 名称 |
| BUG-002 | `internal/instances/management.go` + `CreateRequest.Member` + 前端 | 物理 NIC 创建按所属成员定向，失败返回明确错误 |
| BUG-003 | `internal/instances/instance_admin.go` | 空卷磁盘在实例所属成员创建并回填 source |
| BUG-004 | `internal/httpapi/handlers_cluster.go` + `resources_test.go` | 物理 NIC 候选过滤 loopback `lo` |
| BUG-005 | `InstanceFormDialog.vue` + i18n | 网络提示随受管/物理/未选切换 |
| BUG-006 | 后端 `DELETE /members/{member}` + 前端「移除节点」 | 节点删除闭环；残留实例时返回 400 并列出实例 |
| BUG-007 | `deploy/systemd/incus-fleet.service` | `ReadWritePaths` 增加 `/etc/dnsmasq.d` 与 overlay unit 路径 |
| BUG-008 | `internal/overlay` Update + `PATCH /overlay/{name}` + 前端编辑 | 互联可修改并重新应用 |
| BUG-009 | `internal/instances.Rename` + `POST .../rename` + 改名弹窗 | 实例改名；运行中给出明确提示 |
| BUG-010 | `internal/instances/unattended.go` | remaster 类安装器使用 `security.csm=true`，BIOS 配置的 10s 自动引导生效 |
| BUG-011 | `internal/nodenet/nodenet.go` + `odenet_test.go` | 解码 iw 的 `\xHH` SSID 转义，修复中文网络名乱码 |
| BUG-012 | `internal/unattended/unattended.go` | Ubuntu/NoCloud 使用 locale/keyboard；UI 隐藏磁盘字段 |
| BUG-013 | `internal/unattended/unattended.go` | preseed 增加自动分区触发、显式 expert_recipe（biosgrub + ext4 根）与 LVM/MD 清理 |
| BUG-014 | `internal/instances/unattended_tool.go` | 派生 ISO 名称按 VM 唯一化 |

验证：`go test ./cmd/... ./internal/... ./web` 全通过；前端 `651 passed`。

现场额外做了（非代码）：dnsmasq 安装、sudoers 白名单升级、 systemd drop-in，使私有互联可在 Web 上完成启用/对账/关闭。

## 已实现的 UX 修复（本次）

- UX-01/16（实例表单部分）：新增 `SizeInput`（数字 + 单位下拉），用于内存、根盘、附加盘。
- UX-09：设备配置摘要改为 `CopyableText`（全文 tooltip + 复制）。
- UX-10/32/35/36：镜像/集群媒体指纹、SSH 公钥、脚本正文、profile 配置使用 `CopyableText`。
- UX-11/15（无人值守部分）：应答配置与 ISO 弹窗的时区/语言/键盘改为可搜索下拉。
- UX-12：应答配置新增「附加软件包」字段，数据库迁移 v11 与四种安装器渲染均已接通。
- UX-14：Wi-Fi 面板独立「重新扫描」。
- UX-17：网络类型/存储驱动移除 `allow-create`。
- UX-21：Ubuntu/NoCloud 隐藏不适用的目标磁盘字段。
- UX-24：节点网络路由/防火墙/Wi-Fi 表增加 loading。
- UX-25：ISO/SSH 公钥/脚本/节点历史/实例监控增加局部刷新；镜像分发后自动回拉。
- UX-26：修复加载中与空状态同时显示；节点页增加骨架屏。
- UX-30/31/34：批量 stdout 展开、脚本/公钥完整值、任务/审计展开全部字段。
- UX-33：远程桌面错误加 title。
- UX-37：仪表盘 `+N` 与上传文件名加 title。
- UX-38/39：侧栏行高 50→42px、分组间距收紧；新增桌面图标折叠（236→64px，持久化）。
- UX-40/41/42/43（部分）：顶栏 82→64px、内容间距收紧、CJK/mono token、字号变量、移动端断点与单列布局。

## 二、第一波（P0，先修功能缺失与严重缺陷）

| 编号 | 问题 | 建议方案 |
| --- | --- | --- |
| BUG-006 | Web 无法删除集群节点 | 前端加 `deleteMember()` + 节点行「移除节点」；后端补 `DELETE /api/v1/clusters/{cluster}/members/{member}`，含驱逐前检查（实例/角色）、二次确认文案；无能力时至少在 UI 明示需 CLI |
| BUG-010 | Debian 无人值守卡 GRUB，UEFI 未自动引导 | 检查 remaster 是否只改 BIOS 配置；补写 `EFI/BOOT/grub.cfg` / `boot/grub/grub.cfg` 的 `set timeout=10` + 默认项；安装完成检测超时后在 UI 明确提示失败 |
| BUG-002 | 物理 NIC 创建未绑定节点 + 部分成功报 500 | 创建请求携带目标 member 并校验该 NIC 存在；启动失败时返回结构化「已创建但启动失败」结果，前端给出「编辑网络后启动」入口 |
| BUG-007 | 同上，需正式发版并更新部署文档 | 同步 `docs/en/deployment.md` 的 sudoers/unit 说明 |

## 三、第二波（P1，信息完整性与配置能力）

### 功能缺陷

| 编号 | 问题 | 建议方案 |
| --- | --- | --- |
| BUG-003 | 挂载新空卷缺 source，500 | 后端先创建卷并把卷名作为 source；或前端强制选择已有卷并说明 |
| BUG-008 | 互联无法修改 | 后端补 `PATCH /overlay/{name}`（MTU/DHCP/网关等），前端加编辑弹窗；不支持则隐藏修改期望并说明「关闭后重建」 |
| BUG-009 | 实例无法改名 | 后端补 rename 路由，前端在行操作与抽屉加入口，含名称校验与依赖提示 |
| BUG-012 | Ubuntu/NoCloud 忽略 locale/keyboard/disk | 让渲染函数使用这些字段；否则按安装器类型隐藏字段（与 UX-21 一并处理） |

### 体验（信息拿不到 / 配置不了）

- **UX-01 / UX-16**：内存、磁盘、卷容量、CPU 改为「数字输入 + 单位下拉」；CIDR/IP/MTU/VNI 用带即时校验的输入。
- **UX-10 / UX-32 / UX-35 / UX-36**：指纹、任务 ID、公钥、脚本、审计详情统一做 `CopyableText` 组件（短显示 + hover 全文 + 点击复制）；表格长列默认 `show-overflow-tooltip`；删除确认展示完整指纹。
- **UX-11 / UX-15**：时区、语言区域、键盘布局、目标磁盘、存储池、成员名、路由设备、project、故障域/组改为可搜索下拉或数据选择器（允许 `allow-create` 仅在确有新值合理时）。
- **UX-12**：应答配置扩展结构化字段（软件包、root 密码、NTP、分区、网络、附加参数），原始应答文件作为「高级」叠加而非二选一。
- **UX-19 / UX-20**：表单级 `rules` 即时校验、密码确认与强度；自定义应答模式下保留 hostname 并提供预览；模板/已存配置/自定义改为单选。
- **UX-30 / UX-31 / UX-34**：批量 stdout、脚本正文、SSH 公钥、任务/审计历史用展开行 `<pre>` 展示完整值并支持复制。
- **UX-24 / UX-25**：节点网络各表加 loading 与空状态 CTA；ISO/密钥/脚本/历史/会话面板加局部刷新；镜像分发成功后自动回拉列表。

## 三·补充 视觉设计专项（用户第三批抽查后新增，P2）

| 编号 | 问题 | 建议方案 |
| --- | --- | --- |
| UX-38 | 侧栏行高 44+3+3≈50px、分组标题 16px 上边距、子项缩进 44px | 行高 38px、margin 2px、分组标题 10px、子项缩进 32px，统一层级字号 |
| UX-39 | 桌面端无法把侧栏缩成图标栏（现有 `sidebar-collapsed` 只是窄屏整栏隐藏） | 新增 `isCollapsed` + `:width` + `:collapse` + 桌面切换按钮 + i18n + 单测，折叠时隐藏分组文字并补子菜单图标 |
| UX-40 | 顶栏 h1 与各页 h2 标题重复；字号 11～28px 无体系 | 只保留一处主标题；建立 `--fs-*` token 与统一行高 |
| UX-41 | `--fleet-mono`/`--fleet-bg-soft` 未定义、硬编码浅色、深色模式 token 不全、缺 CJK 字体 | 补齐 token、统一 mono、字重 600/700、引入 Element Plus 官方暗色变量 |
| UX-42 | 顶栏 82px、内容 48px 底距、圆角 16/18/22 混用、15 种弹窗宽度、420px 操作列、内联宽度 | 统一间距/圆角/字号刻度，弹窗四档，抽屉 560px，筛选框 `clamp()`，操作列收进下拉 |
| UX-43 | ≤960px 侧栏 55vh sticky 与顶栏重叠；小屏断点缺规则 | 改抽屉式侧栏 + 遮罩；`max-width` 全局生效；建立 960/720/620/480 断点规范 |

> 实现建议：视觉专项先做 token 与组件基类（UX-40/41/42），再做侧栏折叠（UX-39）和响应式（UX-43），避免逐页改样式产生回归。

## 四、第三波（P2，排版与一致性）

- **UX-02**：删除节点/互联/实例改为输入资源名确认，文案列出影响。
- **UX-03 / UX-08**：数值字段显示允许范围与节点剩余容量；失焦即校验。
- **UX-04 / UX-05 / UX-06**：长任务阶段化进度；空状态直达按钮；错误提示附重试/日志动作。
- **UX-07 / UX-26**：操作成功后自动刷新列表；已启用时隐藏「启用」按钮；加载中不要同时显示空状态，节点页加骨架屏。
- **UX-13 / UX-23**：✅ 节点网络已改为「网卡 / 路由 / 防火墙 / Wi-Fi」标签页；UX-29 窄屏卡片化仍待做。
- **UX-14**：✅ Wi-Fi 面板独立「重新扫描」按钮。
- **UX-17**：✅ 网络类型/存储驱动移除 `allow-create`；UX-18/22 的结构化 KV/资产选择器部分完成（脚本/公钥选择器已加）。
- **UX-27**：✅ 实例操作收拢为「电源按钮 + 操作菜单」，列宽 420→240。
- **UX-28**：静默错误持久提示 + 重试仍待做。
- **UX-33 / UX-35**：远程桌面错误与 profile 配置预览改为可换行 + tooltip。

## 五、第四波（P3，打磨）

- UX-37：仪表盘 `+N` 网络 tooltip；上传卡片完整文件名 tooltip。
- UX-09：设备配置摘要加 `title` + 复制，展开入口显式化。
- 统一术语（macvlan/VXLAN/VNI/preseed 等）加 tooltip 说明。

## 六、建议沉淀的可复用组件

| 组件 | 用途 | 覆盖 |
| --- | --- | --- |
| `CopyableText` | 短显示 + 全文 tooltip + 点击复制 | UX-10/32/35/36/09/31 |
| `SizeInput` | 数字 + 单位下拉 + 即时校验 + 范围提示 | UX-01/16/03/08 |
| `EnumSelect` | 可搜索枚举 + 可选 allow-create | UX-11/15/17 |
| `DetailRowsDrawer` | 展开显示任务/审计/设备的全部字段 | UX-30/31/34/09/35 |
| `PanelHeader` | 标题 + 局部刷新 + loading + 更新时间 | UX-14/23/24/25 |

## 七、验证方式

1. Go：`go test ./cmd/... ./internal/... ./web`；新增路由需补 handler 测试。
2. 前端：`bun run test`、`bun run coverage`、`bun run lint`、`bun run typecheck`。
3. E2E 回归：按 `web-e2e-test-plan.md` 重跑对应用例，重点验证：
   - Debian/Ubuntu/Arch 无人值守安装到关机并自动卸载介质；
   - 节点删除→重新加入→修改→配置的完整闭环；
   - 互联启用→修改→对账→删除的完整闭环；
   - 资源扩容/删除/修改后 UI 与后端配置一致；
   - 所有长值（指纹、公钥、脚本、stdout、审计 detail）可查看完整并复制。
