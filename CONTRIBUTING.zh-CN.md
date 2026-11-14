# 为 Incus Fleet 贡献代码

[English](CONTRIBUTING.md) | **简体中文**

感谢你帮助改进 Incus Fleet。本项目管理真实基础设施，因此所有变更在合并前都会经过
正确性、安全性和运维安全性审查。

参与即表示你同意遵守[行为准则](CODE_OF_CONDUCT.zh-CN.md)。

## 开始之前

- 先搜索已有 Issue 和 Pull Request，避免重复工作。
- 大型功能或行为变更请先开 Issue 描述使用场景。
- 安全漏洞请按 [SECURITY.zh-CN.md](SECURITY.zh-CN.md) 私下报告，不要公开 Issue。
- 不要在 Issue、提交、测试或文档中出现真实 IP、主机名、用户名、集群名、token、
  密钥或生产环境截图。

## 开发环境

```bash
make deps
make test
make coverage
make lint
```

环境要求：Go 1.27+、Bun 1.4+，手动测试时可选真实 Incus 集群。单元测试使用
`internal/testutil/incusfake`，不依赖集群。

前端开发：

```bash
cd web
bun run dev
```

## 工作流程

1. Fork 仓库并创建主题分支。
2. 先写一个描述该变更的失败测试（必须遵循 TDD）。
3. 用最小改动让测试通过。
4. 行为或操作说明变化时，在同一次变更中更新英文和中文文档。
5. 运行完整门禁：`make check && make coverage`。
6. 使用模板提交 Pull Request，说明行为、测试与运维影响。

## 规范

完整规则见 [docs/zh-CN/standards.md](docs/zh-CN/standards.md)。要点：

- Go 覆盖率不得低于 90%（`make coverage-go`）。
- 前端覆盖率需达到语句/行/函数 90%、分支 85%。
- 推送前运行 `gofmt`、`go vet`、`golangci-lint`、ESLint 与 `vue-tsc`。
- 保持依赖方向向内，遵循现有包边界。
- 使用结构化日志，禁止记录机密信息。
- 每篇文档都需要英文基准版和中文镜像版。
- 示例与截图必须脱敏；使用 `192.0.2.10`、`198.51.100.10`、`example.com` 等
  文档专用地址。

## 提交信息

使用祈使语气，必要时加作用域前缀：

```
migrations: restart source when copy fails
web: add batch toolbar tests
docs: document ISO installer workflow
```

保持提交聚焦，不要混合重构与行为变更。

## Pull Request 检查清单

- [ ] 测试先写并通过。
- [ ] 覆盖率门禁通过。
- [ ] `make check` 通过。
- [ ] 英文和中文文档均已更新。
- [ ] 新增 API 路由与字段已写入 `docs/api.md` 和 `docs/zh-CN/api.md`。
- [ ] 示例和截图已脱敏。
- [ ] 不包含机密信息、本地专用文件或构建产物。

## 许可证

提交贡献即表示你同意以
[GNU Affero 通用公共许可证第 3 版](LICENSE) 授权你的贡献。
