[English](../en/qa-issues.md) | **简体中文**

# Web 端测试问题记录（BUG）

> 全部问题均通过浏览器 UI 真实操作发现。状态：待修复 / 已修复 / 已验证。

## BUG-018 创建实例表单的「额外存储」缺少 source，导致创建失败

- **环境**：c63 / 虚拟机 `debian-lab-oldpc`
- **现象**：在「创建实例 → 额外存储」填写 `vardata / /var / 100GiB` 后提交，返回 500；
  日志为 `instances: create debian-lab-oldpc: ... Device validation failed for "vardata":
  Disk entry is missing the required "source" or "path" property`。
- **根因**：`internal/instances/management.go` 把 `disks[]` 转成设备时只写入
  `type/path/pool/size`，没有写入 `source`；而 `AttachDevice` 路径已经通过
  `ensureVMDiskVolume` 正确创建了卷并回填 `source`。
- **修复**：`Create` 在生成 `disks[]` 设备前用 `ensureVMDiskVolume` 创建卷并回填 `source`，
  与「添加磁盘」路径一致。
- **验证**：Web 创建 `qa-spice-check` 时填写 `/data / 5GiB`，实例设备的 `data0` 带
  `source=dev-qa-spice-check-data0`，卷已创建。
- **状态**：已修复、已验证。

## BUG-019 非受管网桥被当作 macvlan 父接口，第二网卡无链路

- **环境**：c63 / `fleetbr0`（带宿主地址的宿主网桥）
- **现象**：通过「添加网卡」选择 `fleetbr0` 后设备为 `nictype=macvlan parent=fleetbr0`，
  客机接口始终 DOWN，无法获取 10.99 网段地址。同样的问题也出现在把带宿主 IP 的
  物理网卡 `enp4s0` 用作 macvlan 父接口时。
- **根因**：Fleet 的 `InstanceDevices` / `InstanceFormDialog` 对所有非受管网络统一生成
  `macvlan`，没有依据网络类型（`bridge`）选择 `bridged`。
- **修复**：`applyNICNetwork` 和创建路径按 `api.Network.Type == "bridge"` 选择
  `nictype=bridged`，仅物理网卡使用 `macvlan`。
- **验证**：以 `fleetbr0`（type=bridge）创建/添加网卡，设备为
  `nictype=bridged parent=fleetbr0`，客机接口正常 UP。
- **状态**：已修复、已验证。

## BUG-020 新建网络对话框无法设置成员级 parent

- **现象**：「集群 → 网络 → 新建网络」选择 `physical` 并填写 `parent=enp4s0` 后提交，
  后端报 `Config key "parent" is cluster member specific`；留空则网络无法被实例使用
  （状态 `Pending`，报 `Specified network is not fully created`）。
- **根因**：`NetworkDialog` 没有成员选择控件，而 Incus 集群网络需要先按成员定义，再实例化。
- **修复**：`NetworkRequest` 增加 `member`，`NetworkDialog` 增加成员下拉（含操作提示）；
  选择成员时后端用 `client.UseTarget(member)` 定义网络，留空时按原有流程实例化。
- **验证**：Web 打开「新建网络」可见「成员」下拉及两步操作说明。
- **状态**：已修复、已验证。

## BUG-021 终端里直接打字没有任何反应

- **现象**：实例详情 →「终端」打开后，在 xterm 黑色区域直接输入命令，屏幕只回显字符，
  不会执行；用户误以为终端坏了。
- **根因**：`TerminalDialog` 只把下方「输入命令并回车」输入框绑定到 WebSocket，
  没有注册 xterm 的 `onData`，xterm 表面是只读的。
- **修复**：注册 `term.onData`，把键盘输入直接发到会话；同时抽出 `ensureExecStarted`，
  首次按键也会正确启动 exec 的 bash shell。
- **验证**：浏览器中在终端区域输入 `hostname -f` 并回车，返回 `debian-lab-oldpc`。
- **状态**：已修复、已验证。

## BUG-022 打开远程桌面会把正在运行的 VM 关机再开机

- **现象**：实例列表显示「运行中」，但点「远程桌面」后客户机重新走一遍启动流程。
- **根因**：两个配置都是在首次打开桌面时才写入，而 Incus 不允许 VM 运行时修改
  `raw.qemu` / `raw.qemu.conf`，因此实现里先 `stop --force`、写配置、再 `start`。
  - SPICE 监听（`-spice ...`）由 `PrepareSPICE` 写入。
  - QMP 监视器（`raw.qemu.conf` + `raw.apparmor`）由 `console.Monitor.EnsureMonitor` 写入。
- **修复**：
  1. VM 创建时就把 `-spice` 参数、`raw.qemu.conf` 和 `raw.apparmor` 写进实例配置；
  2. `PrepareSPICE` 检测已有同端口监听时不再停机/写配置；
  3. `EnsureMonitor` 先用 QMP 握手探测现有监视器，可响应时直接返回，不改写配置。
- **验证**：Web 创建 `qa-spice-check2`（自带配置），启动后记录 `started_at` 与客机 uptime，
  再打开远程桌面：桌面正常连接，`started_at` 不变、uptime 持续增长。
- **状态**：已修复、已验证。

## BUG-016 Ubuntu 24.04 无人值守安装未写入根盘、apt 阻塞、SSH 密码不可用

- **环境**：c63 / Ubuntu 24.04.3 live-server ISO / Subiquity 24.04
- **现象**：安装器能自动进入 Subiquity，且 `lsblk` 能看到 `/dev/vda`（20G），但 `root.img` 始终 0 写入；部分运行停在 apt 配置；装完后 SSH 密码登录失败。
- **根因与修复**：
  1. 显式 storage 配置缺少 `ptable: gpt`。curtin 报 `ptable: 'None'`、`parent partition has invalid partition table`，分区阶段直接失败。已加 `ptable: gpt`。
  2. 安装器默认访问 `archive.ubuntu.com`/`security.ubuntu.com`，慢或不可达时 apt 配置长期阻塞。已改为空源列表，完全离线使用 ISO 的 fsimage/本地包。
  3. Subiquity 把 `identity.password` 原样写入 `/etc/shadow`，明文不是合法密码哈希，SSH 密码验证失败。渲染 user-data 时改用 bcrypt 哈希；已是 `$` 开头哈希的值保持不变。
  4. ISO 的 `boot.priority=1` 高于根盘，安装完成重启后再次进入安装器。根盘改为 `boot.priority=2`：空盘时固件仍回落到 ISO，装好后优先从磁盘启动。
- **验证**：`qa-ubuntu-iso-vm13` 通过 Web 创建；安装后 `root.img=2.8G`、实例 STOPPED、`iso0/iso1` 自动卸载；启动后 `vda1` bios_grub + `vda2` 挂载 `/`，`ubuuser` 可登录，hostname 为 `qa-ubuntu-iso-vm13`。

## BUG-017 SPICE 端口名称哈希碰撞导致 VM 启动失败

- **现象**：创建 `qa-ubuntu-iso-vm8` 时 QEMU 报 `binding socket to 127.0.0.1:5974 failed`，实例创建失败。
- **根因**：`SPICEPortFor` 只在 80 个端口内按名称哈希；与运行中的 `debian-installer` 撞到同一端口。
- **修复**：启动时用 `net.Listen("127.0.0.1:0")` 选取空闲端口写入 `raw.qemu`；`PrepareSPICE`/`SPICEDial` 从 `raw.qemu` 解析实际端口，不再用确定性哈希覆盖。
- **验证**：`qa-ubuntu-iso-vm13` 的 `raw.qemu` 端口为 5920，`POST .../spice/prepare {"port":0}` 返回 `{"port":5920,...}`。

## BUG-001 无人值守安装默认应答配置未使用虚拟机名称作为主机名

- **用例编号**：C1 / C5（默认应答配置 → 预览应答文件）
- **环境**：`http://192.0.2.21:8080`，集群 c63，ISO `debian-13.7.0-amd64-netinst`
- **严重级别**：Major
- **复现步骤**：
  1. 在「镜像与自动化 → 应答配置」新建配置 `qa-debian-preseed`，安装器 Debian/d-i preseed，**主机名留空**（字段提示明确写着「留空则使用虚拟机名称」），其余字段填写后设为默认。
  2. 集群 c63 → 镜像与安装介质 → ISO `debian-13.7.0-amd64-netinst` → 创建虚拟机。
  3. 填写虚拟机名称 `qa-debian-iso-vm`，开启无人值守安装，选择默认应答配置 `qa-debian-preseed (default)`。
  4. 点击「预览应答文件」。
- **实际结果**：生成的 preseed 中
  ```
  d-i netcfg/get_hostname string fleet-unattended
  d-i netcfg/hostname string fleet-unattended
  ```
  主机名为硬编码 `fleet-unattended`，与虚拟机名称 `qa-debian-iso-vm` 不符。
- **期望结果**：主机名应为 `qa-debian-iso-vm`（按字段提示的语义）。
- **定位**：`internal/httpapi/iso.go` 的 `applyAnswerProfile` 应用默认 profile 后未把 VM 名称回填到空 hostname。
- **修复**：在 `applyAnswerProfile` 末尾，当应答文件为空且 hostname 为空时用 `req.Name` 回填；`go test ./internal/httpapi ./internal/unattended` 通过。
- **状态**：代码已修复（待发版验证）

## BUG-002 选择物理网卡创建实例时未绑定到拥有该网卡的节点，且部分成功后报 500

- **用例编号**：D1 / E7 / L2
- **环境**：集群 c63（成员 node-a、node-c），Web `192.0.2.21:8080`
- **严重级别**：Critical
- **复现步骤**：
  1. 实例页 → 创建实例 → 集群 c63、类型容器、镜像 debian/13、名称 `qa-nginx-01`。
  2. 网络保持默认项 `bond0（物理网卡，由局域网 DHCP 分配）`，CPU=1、内存=512MiB、根盘=5GiB、创建后启动。
  3. 点击创建。
- **实际结果**：
  - UI 提示「保存实例失败：internal server error」，`POST /api/v1/clusters/c63/instances` 返回 500 `{"error":{"code":"internal_error","message":"internal server error"}}`。
  - 但实例 `qa-nginx-01` 实际已被创建（位于 `node-c`，状态「已停止」），属于部分成功。
  - 服务端日志：`instances: create qa-nginx-01: Failed to start device "eth0": Parent device 'bond0' doesn't exist`。
  - 原因：物理 NIC 是节点本地资源，UI 允许在集群级表单选择 `bond0`，但创建时未把实例调度到拥有该网卡的成员。
- **期望结果**：
  1. 选择物理 NIC 时应只允许在该 NIC 所属成员上创建，或自动把实例固定到该成员；否则应禁止选择并说明「该网卡仅在节点 X 可用」。
  2. 若启动阶段失败但实例已创建，应返回明确的状态码与错误（例如 207/部分成功），UI 应展示「实例已创建但启动失败，原因：…」并提供「编辑网络后启动」的入口，而不是笼统的 internal server error。
- **定位**：后端创建实例流程（启动失败时未回滚/未返回结构化错误）；前端 `InstanceFormDialog` 网络选项未携带/展示成员归属。
- **状态**：代码已修复并部署（待完整回归）

## BUG-003 挂载「新建空卷」时缺少 source，返回 500

- **用例编号**：E4
- **环境**：c63 / qa-nginx-01 / 设备页 → 挂载设备
- **严重级别**：Major
- **复现步骤**：
  1. 实例详情 → 设备 → 挂载设备 → 类型「磁盘」，设备名 `data0`，来源留空（提示为「留空则新建空卷」），挂载路径 `/data`，容量 `2GiB`。
  2. 点击创建。
- **实际结果**：`POST /api/v1/clusters/c63/instances/qa-nginx-01/devices` 返回 500 `internal server error`。服务端日志：
  `attach qa-nginx-01/data0: Invalid devices: Device validation failed for "data0": Disk entry is missing the required "source" or "path" property`
  请求体为 `{"name":"data0","type":"disk","pool":"default","path":"/data","size":"2GiB"}`，未创建卷、未设置 source。
- **期望结果**：按 UI 提示自动在所选存储池创建空卷并把卷名作为 source 传入；若不能自动创建，应在 UI 明确告知必须填写已有卷名。
- **状态**：代码已修复并部署（待完整回归）

## BUG-004 物理网卡下拉框把回环接口 `lo` 当作可选项

- **用例编号**：E7 / J2
- **环境**：c63 创建/编辑实例的网络下拉框
- **严重级别**：Minor
- **现象**：分组「物理网卡 — node-a, node-c」中包含 `lo`（loopback）。loopback 不能作为 macvlan 父设备，选中后必然启动失败。
- **期望结果**：过滤掉 `lo`、虚拟回环、down 状态接口以及 bond 从属 NIC（用户指南已说明 bond 从属应隐藏）。
- **修复**：`internal/httpapi/handlers_cluster.go` 的 `enrichNetworks` 过滤 `lo`，并补充测试（`resources_test.go`）。
- **状态**：代码已修复（待发版验证）

## BUG-005 编辑实例选择受管网络后仍显示「物理网卡」说明文字

- **用例编号**：E8
- **环境**：c63 / qa-nginx-01 编辑弹窗
- **现象**：网络字段已选中 `incusbr0 · bridge`（受管网络），下方提示仍为「物理网卡：实例直接接入局域网，由路由器 DHCP 分配同网段地址。」
- **期望结果**：提示应随所选网络类型切换为受管桥接网络的说明。
- **状态**：代码已修复并部署（待完整回归）

## BUG-006 Web 端无法删除集群节点（node-c）

- **用例编号**：H4
- **环境**：c63 集群「概览与节点」标签
- **严重级别**：Blocker
- **现象**：节点列表每行的操作只有「管理节点 / 编辑 / 升级 Incus」，没有任何移除/删除节点的入口；打开节点详情页也没有删除操作。
- **代码依据**：
  - 前端 `web/src/api/client.ts` 只有 `updateMember()`（PATCH），没有 `deleteMember()`。
  - 后端 `internal/httpapi/httpapi.go:206,293` 只注册了 `POST /api/v1/clusters/{cluster}/members`（添加）和 `PATCH /api/v1/clusters/{cluster}/members/{member}`（修改），**没有 DELETE 成员路由**。
- **期望结果**：提供节点移除流程（含成员驱逐/实例迁移影响提示、二次确认），或在 UI 明确说明「节点移除需通过 Incus CLI 完成」。
- **备注**：用户明确要求删除后重新加回该节点；当前仅「添加节点」可用，删除链路缺失。
- **状态**：代码已修复并部署（待完整回归）

## BUG-007 systemd 单元未放开 overlay DHCP 所需目录，私有互联无法启用

- **用例编号**：I1
- **环境**：c63 / node-a，Web 启用私有互联网络
- **严重级别**：Critical
- **现象**：Web 点击「启用」返回 503：
  `overlay: node-a: write dhcp: nodenet: host operations unavailable: write dnsmasq config: exit status 1`
- **根因**：`deploy/systemd/incus-fleet.service:31` 的 `ReadWritePaths` 只有
  `/home/incus/data /etc/network/interfaces.d /etc/wpa_supplicant`；在 `ProtectSystem=strict` 下，
  `internal/nodenet/overlay_dhcp.go` 需要写入 `/etc/dnsmasq.d/fleet-overlay-*.conf` 和
  `/etc/systemd/system/fleet-overlay-dhcp-*.service`，目录为只读，sudo tee 也失败（exit status 1）。
  另外部署现场的 sudoers 白名单也是旧版，缺少 dnsmasq/systemd/rm 条目。
- **期望结果**：示例单元与部署文档一并加入
  `ReadWritePaths=/etc/dnsmasq.d /etc/systemd/system/fleet-overlay-dhcp-*.service`，并提示升级 sudoers 白名单。
- **状态**：代码已修复并部署（待完整回归）（现场已通过 systemd drop-in + 更新 sudoers 临时修复，使测试继续）

## BUG-008 私有互联网络无法修改（仅能启用/删除/对账）

- **用例编号**：I3
- **环境**：c63 → 网络 → 私有互联网络 `qa-overlay`
- **严重级别**：Major
- **现象**：已启用互联的行只有「重新对账 / 关闭」两个操作，点击名称也无编辑弹窗；无修改子网、VNI、MTU、网关成员、DHCP 池的入口。
- **代码依据**：后端只注册了
  `GET/POST /clusters/{cluster}/overlay`、`GET/DELETE /clusters/{cluster}/overlay/{overlay}`、
  `POST .../reconcile`；**没有 PATCH/PUT 更新路由**。前端也没有编辑对话框。
- **期望结果**：提供「编辑互联」功能（至少支持 MTU、DHCP 池、网关成员等非破坏性字段），或明确说明只能关闭后重新启用。
- **状态**：代码已修复并部署（待完整回归）

## BUG-009 Web 端无法给实例改名

- **用例编号**：F2
- **环境**：实例列表 / 实例详情抽屉
- **严重级别**：Major
- **现象**：实例行只有「停止/重启/编辑/迁移/删除/克隆」，详情抽屉也没有重命名入口；编辑弹窗中名称字段为只读。
- **代码依据**：`web/src` 全文无 instance rename 相关实现；`internal/httpapi/httpapi.go` 无重命名路由。
- **期望结果**：增加「重命名」操作（含名称校验、依赖提示），或说明需通过 Incus CLI 改名。
- **状态**：代码已修复并部署（待完整回归）

## BUG-010 Debian 无人值守安装未自动引导，VM 卡在 GRUB 安装菜单

- **用例编号**：C1
- **环境**：c63 / qa-debian-iso-vm / ISO `debian-13.7.0-amd64-netinst` + profile `qa-debian-preseed`
- **严重级别**：Critical
- **复现步骤**：
  1. 按 C1 流程启动 Debian 无人值守安装（API 返回 201，派生卷 `debian-13.7.0-amd64-netinst-debian-preseed-auto` 已创建并挂载）。
  2. 等待 30 分钟后通过「远程桌面」查看该 VM。
- **实际结果**：VM 仍显示 `Debian GNU/Linux 13.7.0 UEFI Installer menu`，光标停在 `Graphical install`，未自动选择默认项、未进入安装器；实例一直「运行中」，没有自动关机与卸载介质。设备页确认挂载的是派生 ISO（`boot.priority=1 · source=...-debian-preseed-auto`）。
- **期望结果**：派生 ISO 的 GRUB/UEFI 配置应让默认安装项在约 10 秒后自动引导，安装结束后自动关机并卸载介质；若超过合理时间未检测到完成信号，应在 UI 提示安装可能失败。
- **可能原因**：remaster 只改了 BIOS 的 `isolinux/grub` 配置，UEFI 启动路径（`boot/grub/grub.cfg` / `EFI/BOOT/grub.cfg`）未写入自动引导参数与 timeout。
- **状态**：代码已修复并部署（待完整回归）

## BUG-011 Wi-Fi 扫描结果中的中文 SSID 显示为字面转义序列

- **用例编号**：H7 / UX-14
- **环境**：节点详情页 → Wi-Fi 网络（c62 / node-b）
- **严重级别**：Minor
- **现象**：列表中多个网络名显示为 `\xe5\x8a\xa7\xe7\xbd\x91...` 之类的字面转义字符串，而非可读的中文名称；用户无法识别该连哪个网络，也无法据此输入 SSID 连接。
- **根因**：`iw dev <dev> scan` 对非 ASCII SSID 输出 `\xHH` 转义，`internal/nodenet/odenet.go` 的 `parseWiFiScan()` 直接把该字符串作为 SSID 返回，前端照原样显示。
- **修复**：新增 `decodeIWEscapedSSID()`，把 `\xHH` 还原成 UTF-8 字节；`parseWiFiScan()` 解析 SSID 时调用；新增单测 `TestScanWiFiDecodesEscapedSSID`。`go test ./internal/nodenet` 通过。
- **状态**：代码已修复（待发版验证）

## BUG-012 Ubuntu autoinstall / NoCloud 忽略用户填写的语言、键盘与磁盘

- **用例编号**：C4 / UX-21
- **环境**：应答配置 / ISO 无人值守弹窗（Ubuntu autoinstall、Generic cloud-init）
- **严重级别**：Major
- **现象**：表单允许填写「语言区域」「键盘布局」「目标磁盘」，但选 Ubuntu/Generic 时
  `renderAutoinstall` 硬编码 `locale: en_US` 且完全忽略 `spec.Keyboard`、`spec.Disk`；
  `renderCloudConfig` 同样忽略这三个字段。用户以为设置生效，实际被丢弃。
- **定位**：`internal/unattended/unattended.go:384-429`。
- **期望结果**：要么让这些字段对相应安装器真正生效，要么按安装器类型隐藏/禁用不适用字段（与 UX-21 一起修）。
- **状态**：代码已修复（renderAutoinstall/renderCloudConfig 使用 locale/keyboard；UI 对 Ubuntu/NoCloud 隐藏磁盘字段）

## BUG-013 Debian 无人值守分区失败：No root file system is defined

- **用例编号**：C1
- **环境**：c63 / Debian 13 netinst / preseed 默认配置
- **严重级别**：Critical
- **现象**：自动引导成功后，安装器运行到「Partition disks」并停止：
  `!! ERROR: No root file system  No root file system is defined.`
- **根因**：preseed 设置了 `partman-auto/method regular` 和 `choose_recipe atomic`，但缺少触发自动分区的
  `partman-auto/init_automatically_partition`，分区器启动后没有执行任何 recipe。
- **修复**：在 `renderPreseed` 中加入
  `d-i partman-auto/init_automatically_partition select Guided - use entire disk`，
  并补充 LVM/MD 清理、显式 `expert_recipe`（biosgrub + ext4 根）与确认项。
- **复测与最终根因**：通过在安装器 shell 中检查 `partman-auto` udeb 源码确认：
  - 自动分区问题的直接原因是 `partman-auto/automatically_partition` 与
    `partman-auto/init_automatically_partition` 都必须使用选项标签 `auto`（此前填的是菜单文案/`regular`），
    且旧版 `partman-auto` 的一次性自动分区检查在 virtio 磁盘出现前就执行了。
  - 修复为：预置两个键为 `auto`、`partman/early_command` 等待 8 秒、根盘固定 `io.bus=virtio-blk`、
    显式 expert_recipe（biosgrub + swap + ext4 根）。
  - 继续复测发现的语言/国家/镜像/介质扫描/时区/GRUB 设备问题也逐一预置：
    `debian-installer/language=en`、`debian-installer/country=US`、`time/zone=Etc/UTC`、
    `mirror/*`、`apt-setup/cdrom/set-next=false`、`grub-installer/bootdev=/dev/vda`。
- **验证**：
  - `qa-debian-csm11`、`qa-debian-csm12` 完成安装、关机并自动卸载 ISO；启动后进入
    `Debian GNU/Linux 13 qa-debian-csm11 tty1 login:`。
  - `qa-debian-csm17` 使用最终配置完成 **全自动零输入** 安装：状态 STOPPED、仅保留 root 盘（ISO 已自动卸载）；
    磁盘检查为 `debian=13.8`、`host=qa-debian-csm17`、`grub=yes`、`ssh=yes`。
- **状态**：已修复并完成零输入验证

## BUG-015 Ubuntu autoinstall user-data 未嵌套在 autoinstall: 下，安装中止

- **用例编号**：C2
- **环境**：c63 / ubuntu-24.04 ISO / Subiquity 24.04
- **严重级别**：Critical
- **现象**：Ubuntu 安装器停在 tty1：
  `cloud-init schema validation failure for 'shutdown','identity','storage','late-commands'`
  `'shutdown' is valid autoinstall but not found under 'autoinstall'`
  `Misplaced autoinstall directives resulted in a cloud-init schema validation failure`
- **根因**：`renderAutoinstall` 生成的 user-data 以 `#cloud-config` 开头，但 `version/locale/identity/ssh/storage/shutdown/late-commands` 都放在顶层；Subiquity 要求全部嵌套在 `autoinstall:` 映射下。
- **修复**：
  1. 重写 `renderAutoinstall`，所有指令缩进到 `autoinstall:` 下，软件包列表缩进为 `packages:` 子项；补充单测断言 `autoinstall:` 存在。
  2. Subiquity 还要求内核命令行带 `autoinstall`，否则会停在确认界面。新增 `NeedsBootPatch`（Ubuntu 仍保留 NoCloud seed）与 `BootArgs(ProfileUbuntuAutoinstall)="autoinstall"`，并让 `PatchBootConfig` 识别 `/casper/vmlinuz`；`bootConfigPaths` 增加 Ubuntu 的 `/boot/grub/grub.cfg`、`/boot/grub/loopback.cfg`；创建流程改用 `NeedsBootPatch` 触发重制与 `security.csm`。
- **状态**：已修复并部署。`qa-ubuntu-iso-vm3` Web 无人值守安装已验证自动进入 Subiquity 安装阶段（无任何输入），等待完成关机/卸载介质。

## BUG-014 重复发起无人值守安装时派生 ISO 名称冲突，返回 500

- **用例编号**：C1
- **环境**：c63，同一 ISO 第二次启动无人值守安装
- **严重级别**：Major
- **现象**：`POST .../launch-unattended` 返回 500 `internal server error`，日志：
  `import remastered iso ... Failed creating custom volume from ISO: Cannot create volume, already exists on target storage`。
- **根因**：`derivedISOName(isoName, profile)` 只由 ISO 与 profile 决定，第二次安装复用同名卷；
  上一次安装失败后残留卷不会被清理。
- **修复**：派生卷名加入 VM 名称（`<iso>-<profile>-auto-<vm>`），超长时裁剪并追加哈希，保证 63 字符内唯一。
- **状态**：代码已修复（待复测完成）
