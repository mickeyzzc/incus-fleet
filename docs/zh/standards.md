# 工程标准

[English](../en/standards.md) | **简体中文**

这些规则适用于本仓库的每一处改动。在可行的情况下，它们由评审、CI 和 Makefile 强制执行。

## 测试驱动开发

1. 先写一个描述该行为的失败测试。
2. 实现能让它通过的最小改动。
3. 在测试套件全绿的前提下重构。
4. 为每个缺陷修复补充回归测试。

覆盖率门槛：

- **Go：**`internal` 各包的语句覆盖率至少 **90%**，不包括 `internal/testutil`。`main()` 会调用 `os.Exit`，而 `web` 仅内嵌资源，因此 `cmd` 和 `web` 会被构建/测试，但不计入分母。低于门槛时 `make coverage-go` 和 CI 会失败。
- **前端：**行、语句和函数覆盖率至少 **90%**，分支覆盖率至少 **85%**。阈值位于 `web/vite.config.ts`；低于阈值时 `bun run coverage` 会失败。

覆盖率是底线，不是目标。应断言行为和错误路径，而不是实现细节。

## Go 标准

### 目录结构与边界

- `cmd/` 只包含进程装配代码。
- `internal/*` 各包暴露小型接口，并让依赖指向内部：`httpapi → instances/migrations/clusters → store/config`。
- 把共享类型放在拥有该行为的包中。
- 不要为单个辅助函数新增包；优先在现有的归属包中添加一个函数。

### 代码风格

- `gofmt` 是强制要求；运行 `make fmt-check`。CI 通过 `golangci-lint` 强制格式。
- 导出的标识符要有以标识符名称开头的文档注释。
- 用上下文包装错误：`fmt.Errorf("migrations: copy instance: %w", err)`。
- 当 HTTP 层需要映射行为时，使用哨兵错误（`ErrInvalidRequest`、`ErrConflict`、`ErrNotFound`）。
- I/O 操作把 `context.Context` 作为第一个参数传入；绝不要把它存入结构体。
- 使用 `log/slog` 与结构化键值对。不要记录机密、token、证书内容或完整请求体。
- 网络与数据库操作应显式设置超时。
- 用互斥锁保护共享状态；临界区内不要有 I/O。
- 避免包级可变状态。`version` 变量是唯一的构建期例外。

### 配置

- 每个新选项都需要默认值、校验和测试。
- 环境变量覆盖使用 `INCUS_FLEET_` 前缀，并优先于 YAML。
- 校验错误必须指出有问题的字段和集群。
- 绝不要在示例配置中放入真实的 token、密钥或密码。

### 测试

- 映射/校验逻辑使用表驱动测试。
- API 行为使用 `httptest`；断言状态码、内容类型和 JSON 结构。
- 使用 `internal/testutil/incusfake`，而不是真实集群。
- 迁移测试必须覆盖成功、复制失败回滚、移动清理、在线迁移校验以及中断任务恢复。
- 涉及并发时使用竞态检测器（`go test -race ./internal/migrations`）。
- 测试不得依赖挂钟睡眠；应注入时钟或使用短超时。

## 前端标准

### 技术栈约定

- Vue 3 `<script setup lang="ts">`；新组件不使用选项式 API。
- TypeScript strict mode，启用 `noUncheckedIndexedAccess` 和 `noUnusedLocals`。应用代码中不要使用 `any`；测试文件仅可为范围很窄的测试替身使用它。
- Pinia setup store；每个领域一个 store（`auth`、`clusters`、`instances`、`migrations`）。
- 所有 HTTP 调用都经过 `src/api/client.ts`。组件不直接调用 `fetch`。
- 共享的格式化逻辑放在 `src/utils/format.ts`。
- 组件库使用 Element Plus；样式放在 `src/styles/main.css` 或 scoped 块中。

### 组件

- Props 和 emits 显式声明类型。
- 优先使用计算状态，而不是重复的 ref。
- 破坏性或不可逆的操作必须在 UI 中显式确认。
- 在受影响的视图附近展示 API 错误；瞬时提示使用 `ElMessage`。
- 绝不要把管理员 token 存入 `localStorage` 或 `sessionStorage`。

### 测试

- Vitest + `@vue/test-utils` + jsdom。
- `src/test/setup.ts` 全局 mock `@/api` 和 `ElMessage`，stub Teleport/transition，并替换 `ElSelect`，因为 Element Plus 的 select 在 jsdom 下测量尺寸时会递归。不要在组件测试中重新引入真实的 select。
- 每个 store action 都需要成功与错误测试。
- 视图需要覆盖加载、空、错误和交互状态。
- 路由测试使用 `createFleetRouter(createMemoryHistory())`。
- 在测试之间重置 mock。当用户可见的断言已经足够时，避免把测试耦合到 Element Plus 的内部实现。

### 格式化与依赖

- 包管理器是 Bun；提交 `web/bun.lock`。CI 中使用 `bun install --frozen-lockfile`。
- 把 TypeScript 固定在 5.9.x，直到 `vue-tsc` 支持 TypeScript 7。
- ESLint 必须以零错误通过。警告应修复或有明确理由。
- 不要为几行可充分测试的代码就能实现的行为新增依赖。

## 安全标准

- git 中不得有机密：token、私钥、密码、WiFi 凭据、`.env` 文件或真实生产配置。
- 仓库忽略 `/AGENTS.md`、`/docs/local/`、`*.key`、`*.pem`、`config*.yaml`、`/data/` 和 `/coverage*`。不要削弱这些规则。
- 根目录的 `AGENTS.md` 和 `docs/local/` 仅本地保留，绝不能被推送。
- 对提交的文档、示例、测试和截图做脱敏：只使用占位符，不得出现真实主机名、IP 地址、用户名、集群名、PSK 或 token。
- 使用文档专用地址（`192.0.2.0/24`、`198.51.100.0/24`、`2001:db8::/32`）、`example.com` 域名，以及 `c1`/`c2` 这类通用集群名。
- 截图必须针对一次性环境拍摄，或在提交前打码。检查每个可见字段：地址、主机名、实例名、用户名、MAC 地址和组织标识。
- 每次推送前以及发布仓库前运行机密扫描。
- 集群端点必须使用 HTTPS，并使用固定的客户端证书。
- 凭据比较必须使用恒定时间算法，会话标识符必须随机且高熵。
- 不要记录机密。绝不要把 token、密钥、密码或其他凭据写入错误字符串。API 目前会对 5xx 响应返回底层错误文本并记录完整错误，因此错误文本必须保持可安全披露。
- 保持依赖为最新；关注 Go 和 npm 包的安全公告。

## 文档标准

- 英语是默认语言。根目录 `README.md` 和 `docs/en/*.md` 是权威版本。
- 文档树按语言拆分：`docs/en/`（英文）和 `docs/zh/`（简体中文），各自带独立的 `images/` 目录。根目录的双语文案也遵循同样拆分——英文文件保留在根目录，中文镜像放在 `docs/zh/`，文件名为 `*.zh-CN.md`。
- 每个文档都有另一种语言的对应版本。每个文件都以指向对应文件的语言切换链接开头。
- 在同一次改动中更新两种语言。过期的翻译属于文档缺陷。
- 同一语言目录内使用相对链接（如 `deployment.md`、`images/...`）；跨语言链接需跨目录（如从 `docs/zh/` 到 `../../en/api.md`）。
- API 变更必须更新 `docs/en/api.md` 和 `docs/zh/api.md`，包括新增路由、请求结构、状态码和限制。
- 仅本地使用的资料（真实地址、凭据、运维笔记、未脱敏的 QA 记录）放在 `docs/local/`，该目录被 git 忽略且永不推送。绝不能把其中内容复制到 `docs/en/` 或 `docs/zh/`。
- 图表使用标记为 `mermaid` 的 Mermaid 代码块。仅当 Mermaid 无法表达布局时，才在该语言的 `images/` 目录中放入 SVG；不要提交 ASCII 架构图或流程图，代码块中的缩进目录树不受此限。
- 截图按 UI 语言放在 `docs/en/images/` 或 `docs/zh/images/`，并引用在 README 或它所说明的用户指南步骤中。添加描述性替代文本，必要时说明 UI 语言。
- QA 记录（`qa-*.md`）提交前必须脱敏；真实 IP 和主机名替换为 RFC 5737 文档地址和虚构名称。
- 不要记录代码未实现的行为；对照 handler、常量和测试核实说法。

## Git 与评审

- 提交信息使用祈使语气，必要时加作用域前缀：`migrations: restart source when copy fails`、`web: add migration dialog tests`。
- 保持提交聚焦；不要把重构与行为变更混在一起。
- 每个 PR 都要描述行为、运行的测试以及任何运维影响。
- 除 `.gitkeep` 外，不要提交生成的 `web/dist` 资源。
- 评审后未经协调，不要改写共享历史。

## 完成定义

当满足以下条件时，改动才算完成：

- 测试先编写并通过；
- 覆盖率门槛通过；
- 本地 `make check` 通过；
- 面向用户的文档和 API 文档在**英文和中文**两种语言下都已更新；
- 截图和示例已脱敏；
- 没有机密或仅本地文件被暂存；
- 已说明运维影响（迁移行为、配置、部署）。
