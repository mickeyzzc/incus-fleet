[English](../en/qa-ux-improvements.md) | **简体中文**

# Web 端体验问题（UX）优化清单

> 记录方式：真实浏览器操作发现的问题。每项包含现状、问题、建议、影响范围、优先级、状态。

## UX-01 内存/磁盘大小输入应提供单位下拉框并限制为纯数字

- **发现位置**：实例创建/编辑弹窗（`web/src/components/InstanceFormDialog.vue:523` 内存、root disk）
- **现状**：`内存`、`根磁盘大小`、`附加磁盘大小` 均为自由文本框，placeholder 提示用户自行输入 `1GiB` 这样的混合字符串；校验只在提交时或失焦后按正则解析。
- **问题**：用户需同时理解数值和单位语法（MiB/GiB/百分比），认知负担高；手滑输入 `1024GB`、`1 GiB`、中文全角字符等容易出错或产生非预期结果。
- **建议**：采用「数字输入框 + 单位下拉框（MiB / GiB / %）」组合，文本框只允许数字，前端按所选单位拼装为 Incus 所需字符串；默认值与当前值自动拆分回填。
- **影响范围**：内存、root disk、附加数据盘、ISO 创建 VM 根盘、存储卷容量、网络 MTU 等所有带单位的输入。
- **优先级**：P1
- **状态**：待修复

## UX-02 危险操作缺少分级确认

- **发现位置**：集群卡片「删除」、节点移除、私有互联「禁用」、实例「删除」
- **现状**：多数为单一确认弹窗，仅点击「确定」即执行。
- **问题**：删除集群/节点/互联属于不可逆操作，误点风险高。
- **建议**：输入资源名称（如节点名 `node-c`）后才能启用确认按钮；文案明确列出影响（哪些实例/隧道会被删除）。
- **优先级**：P1
- **状态**：待修复

## UX-03 数值范围缺少即时提示

- **发现位置**：CPU / 内存 / 磁盘 / MTU / VNI 输入
- **现状**：无上限、可用范围、当前值的提示。
- **问题**：用户不知道填多少合适，容易超过节点容量。
- **建议**：字段下方展示允许范围与当前值/节点剩余容量，失焦即校验并提示。
- **优先级**：P2
- **状态**：待修复

## UX-04 长任务缺少阶段化进度

- **发现位置**：镜像导入/分发、ISO 上传、无人值守安装、跨集群迁移
- **现状**：主要为 loading/进度条或仅状态文字。
- **问题**：用户无法判断当前处于哪个阶段、是否卡住、预计还要多久。
- **建议**：展示分阶段步骤（上传→校验→创建→完成）与预计时间，失败时提供「复制日志/重试」。
- **优先级**：P2
- **状态**：待修复

## UX-05 空状态缺少直达操作

- **发现位置**：应答配置页（当前显示「暂无已存应答配置」）、镜像为空时
- **现状**：仅有文字提示。
- **问题**：新用户不知道下一步做什么。
- **建议**：空状态中提供「新增配置 / 上传 ISO / 导入镜像」直达按钮。
- **优先级**：P2
- **状态**：待修复

## UX-06 错误提示可操作性不足

- **现状**：操作失败通常为 `ElMessage.error` 瞬时提示。
- **建议**：对失败操作保留「重试 / 查看日志 / 定位资源」动作，错误文案说明具体原因与下一步。
- **优先级**：P2
- **状态**：待修复

## UX-07 互联网络启用后 UI 不自动刷新，且「启用私有网络」按钮仍显示

- **发现位置**：c63 → 网络 → 私有互联网络
- **现状**：启用成功后 toast 提示成功，但列表仍显示「该集群尚未配置私有网络」，必须手动刷新页面才出现；已启用状态下顶部仍显示「启用私有网络」按钮。
- **建议**：操作成功后自动重新拉取列表；已存在互联时隐藏或禁用「启用」按钮，避免误以为可创建多个。
- **优先级**：P2
- **状态**：待修复

## UX-08 资源容量提示缺少节点剩余容量参照

- **发现位置**：创建实例的内存/根磁盘输入
- **现状**：只说「只填数字按 MiB 处理」，不显示所选集群/节点的可用内存与存储余量。
- **建议**：在字段下方显示「节点可用：X GiB / 总 Y GiB」，并对超出给出即时警告。
- **优先级**：P2
- **状态**：待修复

## UX-09 设备配置摘要被截断且无法直接看全 / 复制

- **发现位置**：实例详情 → 设备 → 配置列（`web/src/components/InstanceDevices.vue:334,523`）
- **现状**：`Object.entries(device).map(k=v).join(' · ')` 生成的摘要用 CSS `text-overflow: ellipsis` 截断（如 `boot.priority=1 · pool=default · source=archlinux-x86_64` 看不全），单元格无 `title`、无复制按钮；虽然存在 `type="expand"` 展开行，但入口是表格最左侧的小箭头，不显眼，用户第一眼看不到完整信息。
- **建议**：摘要加 `title` 原生提示 + 一键复制按钮；展开行改为显式「查看完整配置」链接；长值（source/path）单独换行显示。
- **优先级**：P2
- **状态**：待修复

## UX-10 镜像指纹被硬截断，无法查看或复制完整值

- **发现位置**：镜像与自动化 → 镜像列表（`web/src/views/AssetsView.vue:255-256,442`）
- **现状**：`shortFingerprint()` 把超过 16 字符的指纹截成 `202a2b54e5a1…`，无 tooltip、无复制入口；用户无法拿到完整 fingerprint 去 CLI 操作或核对。
- **建议**：保留短显示但 hover 显示完整值，点击即复制完整指纹；或提供「详情」抽屉列出完整 fingerprint、别名、类型、大小、缓存状态。
- **优先级**：P1
- **状态**：待修复

## UX-11 应答配置的语言区域 / 键盘布局应为下拉选择

- **发现位置**：应答配置弹窗（`web/src/views/AnswerProfilesView.vue:215-220`）
- **现状**：`语言区域`（placeholder `en_US`）、`键盘布局`（placeholder `us`）都是自由文本输入，用户需记住区域码/键盘布局码，容易填错格式。
- **建议**：改为可搜索下拉框（`el-select filterable allow-create`），内置常用 locale（`en_US`/`zh_CN`/`de_DE`/`fr_FR`…）与键盘布局（`us`/`cn`/`de`/`fr`…），同时允许自定义。
- **优先级**：P2
- **状态**：待修复

## UX-12 应答配置可编辑字段过少，复杂需求被迫写原始应答文件

- **发现位置**：应答配置弹窗（`web/src/views/AnswerProfilesView.vue:190-225`）
- **现状**：模板方式只有 主机名、用户账户、密码、时区、目标磁盘、语言区域、键盘布局 7 个字段；想要配置软件包、NTP、root 密码、分区方案、网络、GRUB 等只能切到「使用自定义应答文件」粘贴整段 preseed/user-data，认知负担高且无校验。
- **建议**：按安装器类型扩展结构化字段（软件包列表、root 密码、NTP/时区服务器、分区方式、网络 DHCP/静态、附加 kernel 参数、是否安装 SSH 等），并提供「高级 → 原始应答文件」作为叠加而非二选一；后端 `AnswerSpec` 已支持的字段应全部暴露。
- **优先级**：P1
- **状态**：待修复

## UX-13 节点详情页排版需要优化

- **发现位置**：`/clusters/:cluster/members/:member`（`web/src/views/NodeDetailView.vue`）
- **现状**：概览信息、主机指标、节点网络、Wi-Fi 等区块纵向堆叠，内容密集、留白与层级不一致；长内容（路由表、Wi-Fi 列表、指标图）把页面拉得很长。
- **建议**：改为卡片 + 标签页分区（概览 / 网络 / 路由 / 防火墙 / Wi-Fi / 指标 / 历史），关键状态置顶，次要内容折叠，统一间距与标题层级。
- **优先级**：P2
- **状态**：待修复

## UX-14 Wi-Fi 网络列表缺少面板级刷新按钮

- **发现位置**：节点详情页 → Wi-Fi 网络（`web/src/components/NodeNetworkPanel.vue:519-555`）
- **现状**：只有页面顶部的全局「刷新」（`NodeNetworkPanel.vue:362`），Wi-Fi 扫描结果会随时间过期，用户想重新扫描必须滚动到顶部点击全局刷新或刷新整页。
- **建议**：Wi-Fi 面板标题栏增加独立「重新扫描」按钮（带 loading），并可显示上次扫描时间；扫描属于轻量读操作，不应依赖全局刷新。
- **优先级**：P2
- **状态**：待修复

---

## 全站系统性复查补充（按「截断 / 控件 / 配置项 / 排版 / 刷新」标准）

> 以下为用户抽查后触发的全站审计结果，与上面条目合并规划；编号 UX-15 起。

### UX-15 枚举字段大量误用自由文本框

- **位置**：`AnswerProfilesView.vue:208-220`、`IsoLaunchDialog.vue:349-358`（时区、语言区域、键盘布局、目标磁盘）；`AssetsView.vue:753`（ISO 上传存储池）；`MemberDialog.vue:97-103`（故障域、组）；`AddClusterMemberDialog.vue:348`（成员名）；`NodeNetworkPanel.vue:596`（路由设备）
- **现状**：这些都是封闭枚举或已加载数据，却让用户手输（如 `UTC`、`en_US`、`us`、`/dev/vda`、`root`）。
- **建议**：时区→`Intl.supportedValuesOf('timeZone')` 可搜索下拉；语言区域/键盘布局→内置常用值 + `allow-create`；目标磁盘/存储池/成员名/路由设备→从已加载数据下拉；故障域/组→多选或 datalist。
- **优先级**：P1
- **状态**：待修复

### UX-16 容量/数值字段误用自由文本框（UX-01 的全站扩展）

- **位置**：`InstanceFormDialog.vue:519,524,532,555`、`InstanceDevices.vue:389,495`、`StorageVolumesDialog.vue:151`、`IsoLaunchDialog.vue:276`、`OverlayDialog.vue:136,159-161`、`NodeNetworkPanel.vue:590`
- **现状**：CPU/内存/磁盘/卷容量/子网/DHCP 起止 IP/网关均为自由文本；Overlay 的子网只校验非空、DHCP IP 无校验。
- **建议**：数字 + 单位下拉；CIDR/IP 用带校验的专用输入（ Element Plus 无内置则用正则即时校验）；网关 IP 必须校验（当前 `submitRoute` 完全不校验）。
- **优先级**：P1
- **状态**：待修复

### UX-17 下拉框 `allow-create` 用在封闭枚举上

- **位置**：`NetworkDialog.vue:118-129`（网络类型）、`StoragePoolDialog.vue:118-129`（存储驱动）、`InstanceFormDialog.vue:497-516`（profiles）
- **现状**：允许用户输入任意值，产生后端无法处理的类型/不存在的 profile。
- **建议**：移除 `allow-create`；profiles 只允许选择已存在项，新建 profile 走独立入口。
- **优先级**：P2
- **状态**：待修复

### UX-18 已有资产未做选择器，重复手输

- **位置**：`InstanceOpsDialog.vue:99-105,134-140`（运行脚本 / 注入公钥用裸 textarea）、`InstanceDevices.vue:381,487`（磁盘来源）、`ClusterRegisterDialog.vue:137`（project）、`AddClusterMemberDialog.vue:443`（agent 成员名）
- **现状**：脚本库/公钥库/存储卷/agent 列表都已加载，却要求用户手动粘贴或手输，且必须完全一致才能匹配。
- **建议**：提供下拉选择器 + 「高级 → 手动输入」切换。
- **优先级**：P2
- **状态**：待修复

### UX-19 应答配置表单缺少即时校验与密码确认

- **位置**：`AnswerProfilesView.vue:63-71,205`、`IsoLaunchDialog.vue:142-154,346`、`UsersView.vue:90-97,273-278`
- **现状**：只在提交时校验，字段级 `rules` 缺失；密码无确认框、无强度提示；后端要求模板 profile 必须有 username、hostname/username 有正则约束，前端未同步。
- **建议**：`el-form rules` + `prop` 即时校验；密码确认 + 强度；与后端 `ValidateAnswerSpec` 对齐；提交前禁用按钮并给出错误定位。
- **优先级**：P1
- **状态**：待修复

### UX-20 「自定义应答文件」路径丢失 hostname 且无预览

- **位置**：`AnswerProfilesView.vue:190-195`、`IsoLaunchDialog.vue:199-206,322-327`
- **现状**：勾选「使用自定义应答文件」后 hostname 输入框被隐藏，且请求不发送 `spec`；但后端 `RenderSeedFiles` 始终用 `spec.Hostname` 生成 meta-data，导致 seed 身份固定为 `fleet-unattended`。应答配置页也没有预览入口（ISO 启动弹窗有）。
- **建议**：自定义模式下仍保留 hostname；提供「预览应答文件」；模板/已存配置/自定义改为单选组而非两个独立控件。
- **优先级**：P1
- **状态**：待修复

### UX-21 安装器类型无关的字段仍然显示

- **位置**：`AnswerProfilesView.vue:196-221`、`IsoLaunchDialog.vue:340-360`
- **现状**：`Locale/Keyboard/Disk` 对 Ubuntu autoinstall 和 Generic NoCloud 实际不生效（`unattended.go:384-429` 硬编码 locale、忽略这些字段），但表单对所有安装器都显示。
- **建议**：按安装器类型动态显示/禁用字段；若后端应支持，则修复 `renderAutoinstall`/`renderCloudConfig` 使用这些值（见 BUG-012）。
- **优先级**：P2
- **状态**：待修复

### UX-22 `KeyValueEditor` 让结构化配置变成纯文本

- **位置**：`ProfileDialog.vue:146-183`、`NetworkDialog.vue:135-142`、`StoragePoolDialog.vue:135-142`
- **现状**：profile 配置、网络/存储池 config 全部是自由 key/value 文本；设备 `type` 也是自由文本；MTU、IP、容量等无类型校验。
- **建议**：常见 key（limits.cpu/memory、disk size、mtu、ipv4.address、source）提供结构化字段，保留「高级 KV」作为补充。
- **优先级**：P2
- **状态**：待修复

### UX-23 节点网络四个区块纵向堆叠且横幅重复（布局）

- **位置**：`NodeNetworkPanel.vue:346-558`、`NodeDetailView.vue:68-101`
- **现状**：网卡/路由/防火墙/Wi-Fi 四个独立表格无 Tab/折叠，整页超过 1500px；常驻提示横幅、只读提示、错误提示叠加，把关键信息推到首屏之外（实测 c62/node-b 页面需长滚动才能看到 Wi-Fi）。
- **建议**：四个区块改为 `el-tabs`（网卡/路由/防火墙/Wi-Fi）或可折叠面板；常驻说明改为标题旁 help tooltip；只读与警告合并为一条；not-found 用替换式空状态。
- **优先级**：P1
- **状态**：待修复

### UX-24 节点网络各表格缺少 loading 与空状态引导

- **位置**：`NodeNetworkPanel.vue:400,465,502,528`
- **现状**：只有网卡表格有 `v-loading`；路由/防火墙/Wi-Fi 在刷新时静默换数据；空表只有默认「无数据」，用户分不清「还没加载」和「确实为空」。
- **建议**：四表统一 `v-loading` + 每节「更新时间」；`#empty` 用 `el-empty` 并给出「配置网卡/添加路由/添加规则/扫描网络」直达按钮。
- **优先级**：P2
- **状态**：待修复

### UX-25 多处列表缺少局部刷新，且变更后不回拉

- **位置**：`AssetsView.vue:483-547`（ISO 无刷新，镜像有）、`:549-637`（SSH 公钥/脚本无刷新）；`NodeNetworkPanel.vue:644-675`（操作历史对话框无刷新/重试）；`UsersView.vue:302-333`（会话对话框无刷新）；`InstanceMetricsPanel.vue:107-114,242`（自动轮询但无手动刷新/暂停）；`AssetsView.vue:269-289`（镜像分发成功后不刷新镜像列表）
- **建议**：为各面板/对话框增加局部刷新按钮与错误重试；镜像分发成功后 `await fetchImages()`；实例监控增加手动刷新/暂停，与主机指标面板保持一致。
- **优先级**：P2
- **状态**：待修复

### UX-26 加载中同时显示空状态，节点页无骨架屏

- **位置**：`HostMetricsPanel.vue:137-138`、`NodeDetailView.vue:103-128`
- **现状**：首次加载时「加载中…」和空状态同时渲染；节点详情在数据返回前只有横幅、内容区大面积空白（实测截图确认）。
- **建议**：空状态条件改为 `v-else-if` + `!loading`；节点页使用 `el-skeleton` 占位。
- **优先级**：P2
- **状态**：待修复

### UX-27 表格操作列过宽且危险操作视觉区分不足

- **位置**：`InstancesView.vue:394-408`、`ClusterView.vue:733`、`InstanceActions.vue:37-94`、`NodeNetworkPanel.vue:437-445,479,510`
- **现状**：实例操作列宽 420px、6–7 个按钮平铺，挤压数据列；危险操作仅为 danger 色文字链接，与「配置/连接」紧邻，易误点。
- **建议**：收拢为「操作」下拉菜单（编辑/迁移/克隆 | 停止/重启 | 删除），列宽降到 ~120px；危险项分组并用 `danger plain` 按钮 + 图标；批量栏中 danger 样式留给停止/删除，「清空选择」改中性。
- **优先级**：P2
- **状态**：待修复

### UX-28 配置类错误被静默吞掉，失败无持久提示

- **位置**：`AssetsView.vue` 只渲染 `clustersError/imagesError/isosError`，`stores/assets.ts:35,92` 的 `assets.error`（SSH 公钥/脚本加载失败）从未展示；`NodeNetworkPanel.vue:118-120`、`UsersView.vue:135-137` 仅用瞬时 toast 报加载失败。
- **建议**：相关 tab 增加错误 alert + 重试按钮；对话框失败改为持久化 `loadError` + Retry。
- **优先级**：P2
- **状态**：待修复

### UX-29 窄屏下节点网络表格未适配

- **位置**：`NodeNetworkPanel.vue:400-556`（组件内无 `@media`；全局断点仅在 `styles/main.css:652-703`）
- **建议**：≤768px 改为卡片式列表，隐藏次要列，操作收进下拉菜单。
- **优先级**：P3
- **状态**：待修复

### UX-30 批量操作输出被截到 80 字符且无标记

- **位置**：`BatchOpsDialog.vue:150`
- **现状**：`row.stdout.trim().slice(0, 80)` 静默截断，无 `…`、无 tooltip、无复制、无展开行；用户无法判断是否被截断，也拿不到完整诊断输出。
- **建议**：列加 `show-overflow-tooltip` 并渲染完整 stdout；增加展开行 `<pre>` + 复制按钮。
- **优先级**：P1
- **状态**：待修复

### UX-31 脚本内容只显示第一行，SSH 公钥被省略号截断

- **位置**：`AssetsView.vue:618`（脚本 `row.body.split('\n')[0]`）、`:573` + CSS `:816-821`（公钥 `.key-text` nowrap ellipsis）
- **现状**：脚本只能靠「编辑」弹窗（写操作）读全文；公钥完全无法 hover 查看或复制。
- **建议**：展开行显示完整脚本/公钥 + 复制按钮；`title` 属性兜底。
- **优先级**：P1
- **状态**：待修复

### UX-32 集群镜像页指纹截断，删除确认只显示 12 位

- **位置**：`ClusterView.vue:844,850,407`
- **现状**：64 位 SHA256 只显示 12 位；删除确认弹窗也只用 12 位指纹核对目标，不可逆操作核对信息不足。
- **建议**：加 `title` + 复制按钮；删除确认展示完整指纹；与 UX-10 统一实现「指纹详情」。
- **优先级**：P1
- **状态**：待修复

### UX-33 远程桌面错误信息被截断

- **位置**：`RemoteDesktopDialog.vue:381` + CSS `:444-450`
- **现状**：`.desktop-error` nowrap ellipsis，长错误（连接/流/电源失败原因）看不全。
- **建议**：`title` 兜底，或改为自动换行 + 全宽 `el-alert`。
- **优先级**：P2
- **状态**：待修复

### UX-34 节点任务/审计历史只显示少量列，关键字段被丢弃

- **位置**：`NodeNetworkPanel.vue:647-657`（任务）、`:660-673`（审计）
- **现状**：任务只显示 type/state/time，`error`/`progress`/`result`/`plan` 全部未展示；审计只显示 action/backend/result/time，`detail`/`actor`/`source_ip`/`task_id` 全部未展示。数据已加载但被丢弃，排障时看不到失败原因与变更详情。
- **建议**：增加展开行展示全部字段，错误与详情用 `<pre white-space:pre-wrap>` 并支持复制；长列加 `show-overflow-tooltip`。
- **优先级**：P1
- **状态**：待修复

### UX-35 Profile 配置预览与迁移 ID 截断无复制

- **位置**：`ClusterView.vue:1096-1100` + CSS `:1257-1262`（配置键 ellipsis）；`MigrationsView.vue:253-257` + `utils/format.ts:82-89`（ID 截 8 位）
- **现状**：profile 配置键看不全；迁移 ID 只能看前 8 位，虽有详情抽屉但无复制入口。
- **建议**：`title` + 复制按钮；profile 配置改为可展开键值列表。
- **优先级**：P2
- **状态**：待修复

### UX-36 全站缺少 `show-overflow-tooltip` 的系统性约定

- **现状**：`web/src` 中 `show-overflow-tooltip` 出现 0 次；长值列（指纹、公钥、脚本、审计 detail、防火墙规则、stdout、上传文件名）只能靠 CSS 换行或自定义 nowrap 截断。
- **建议**：制定统一约定：所有可能超长的表格列默认 `show-overflow-tooltip`，关键信息（指纹/公钥/命令/ID）额外提供复制按钮，形成 `CopyableText` 组件复用。
- **优先级**：P2
- **状态**：待修复

### UX-37 仪表盘网络列表与上传文件名信息不全

- **位置**：`DashboardView.vue:367-372`（只列 3 个网络 + `+N` 无提示）、`AssetsView.vue:771`（上传卡片文件名 ellipsis）
- **建议**：`+N` 加 tooltip 列出全部网络；上传卡片 `title` 显示完整文件名。
- **优先级**：P2
- **状态**：待修复

---

## 视觉设计维度复查（用户第三批抽查触发）

### UX-38 侧栏菜单上下间距过大、层级不清

- **位置**：`styles/main.css:114-120,152-186`
- **实测**：侧栏宽 236px；菜单项高 44px + 上下 margin 3px = **每行约 50px**；分组标题 `padding:16px 12px 4px`，品牌区 `padding-bottom:18px`，首个菜单前约 34px 空白；子菜单缩进 `44px !important`。
- **问题**：用户明确反馈「导航条按钮上下间距太大，层级、字体、间距都要优化」；四个分组标题累计 64px 纯空白，节点/集群多时侧栏迅速变成长滚动。
- **建议**：`--el-menu-item-height:38px`、菜单项 `height:38px; line-height:38px; margin:2px 0`；分组标题 `padding:10px 12px 4px; font-size:10.5px; font-weight:600`，首个 `:first-child { padding-top:4px }`；品牌底距 18→10px；子项缩进 44→32px 并去掉 `!important`。
- **优先级**：P2
- **状态**：待修复

### UX-39 侧栏不支持桌面端图标折叠

- **位置**：`App.vue:32,156-166,248-254`、`styles/main.css:259-261,669-675`
- **现状**：只有 `sidebarOpen`，窄屏时 `mobile-menu-toggle` 才显示，且 `.sidebar-collapsed .sidebar { display:none }` 是**整栏隐藏**；桌面端 `el-menu` 没有 `:collapse`，`el-aside width="236px"` 写死，无过渡。用户要求「缩到左侧只显示图标」目前完全做不到。
- **建议**：新增独立的 `isCollapsed`（可持久化到 localStorage）；`el-aside :width="isCollapsed ? '64px' : '236px'"` + `transition`；`el-menu :collapse="isCollapsed"`；桌面端折叠按钮（Fold/Expand）+ i18n 文案；折叠时隐藏分组标题/计数/品牌文字，集群子菜单补图标；补 `App.test.ts` 折叠断言。
- **优先级**：P2
- **状态**：待修复

### UX-40 页面标题重复、字号体系混乱

- **位置**：`App.vue:262`（顶栏 h1）+ 各视图 `.page-header` h2（DashboardView.vue:251、InstancesView.vue:258、ClusterView.vue:480 等 9 处）；`styles/main.css` 大量 11/11.5/12/12.5/13/13.5/14/15/16/17/18/20/21/22/28px
- **实测截图**：仪表盘顶部同时出现面包屑「总览」、顶栏 h1「总览」、内容区 h2「总览」，三层重复。
- **建议**：只保留一处页面主标题，顶栏 h1 或内容 h2 二选一；建立字号 token `--fs-xs:11 / sm:12 / md:13 / lg:14 / xl:16 / 2xl:20 / 3xl:28`，把散落的 11.5/12.5/13.5/15/17/21/22 归并；统一行高 body 1.5、标题 1.25。
- **优先级**：P2
- **状态**：待修复

### UX-41 设计 token 不完整、硬编码颜色影响深浅主题

- **位置**：`styles/main.css:22-24,106,128,351,405-410,483-486,567`、`InstanceMetricsPanel.vue:304`、`InstanceOpsDialog.vue:192`、`MigrateDialog.vue:262-263,301`、`DashboardView.vue:506,510`
- **现状**：`--fleet-mono` 被引用但未定义；`--fleet-bg-soft` 未定义导致深色模式浅底浅字；渐变终点 `#111c33` 在深色模式下不跟随；字重 750/800 无可变字体支撑；CJK 字体未声明；mono 字体栈重复 4 份。
- **建议**：定义 `--fleet-mono`、`--fleet-sidebar-end`、`--fleet-panel-muted`、`--fleet-line` 等 token；mono 统一用变量；字重改 600/700；字体栈补 `PingFang SC / Microsoft YaHei / Noto Sans CJK SC`；深色模式引入 Element Plus 官方 `dark/css-vars.css` 而非手写子集。
- **优先级**：P2
- **状态**：待修复

### UX-42 整体布局比例与间距需要统一

- **位置**：`styles/main.css:227-285,313-378,495-506`、`DashboardView.vue:457-461`、各弹窗 `width`、各视图内联 `style="width:280px"` 等
- **现状**：顶栏 `min-height:82px`、内容 `padding:28px 32px 48px`、gap 20px；卡片圆角 16/18/22px 混用；stat-card 28px/750 与 resource-value 22px/600 两套 KPI 样式；弹窗宽度 15 种（420～960px）、抽屉 520/560px 混用；筛选框宽度内联 120～300px；表格操作列固定 420px；负 margin 散落。
- **建议**：统一间距刻度 4/8/12/16/20；顶栏 64px、内容 `20px 24px 32px`、gap 16px；圆角 8/12/16 三档；KPI 统一 26–28px/700 + `tabular-nums`；弹窗四档 `sm480/md600/lg760/xl960` 且用 `min(…,92vw)`；抽屉统一 560px；筛选用 `.filter-input { width:clamp(160px,22vw,280px) }`；操作列收进下拉（见 UX-27）。
- **优先级**：P2
- **状态**：待修复

### UX-43 响应式布局缺口：移动端侧栏遮挡内容、断点不统一

- **位置**：`styles/main.css:652-703`、`InstanceFormDialog.vue:676-686`
- **现状**：≤960px 侧栏变 `position:sticky; height:auto; max-height:55vh; width:100%!important`，与同样 sticky 的顶栏争抢顶部、内容被推到首屏之下；弹窗 `max-width` 只在 ≤900px 生效，901–1000px 的 960px 远程桌面弹窗仍溢出；≤620px 未覆盖 detail-grid、cluster-metrics、topbar actions、HelpDialog 双栏、field-grid/volume-grid 多列；全局断点 960/900/620 与组件 720 不统一。
- **建议**：改为真正的抽屉式侧栏（fixed + translateX + 遮罩 + `sidebarOpen` 驱动）；`max-width:calc(100vw-32px)` 全局生效；建立 960/720/620/480 断点规范并补齐小屏规则。
- **优先级**：P2
- **状态**：待修复
