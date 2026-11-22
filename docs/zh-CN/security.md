# 敏感信息与运维数据管理

本仓库是公开的。两条规则保证这一点：**敏感内容永不入库**，**由工具自动强制**，
而不是依赖人记得检查。

## 绝不能提交的内容

- 私钥、证书、CSR 材料，以及任何 `certs/` 路径下的文件。
- 密码、API token、集群加入 token、数据库凭据。
- 真实内网网段、主机名和运维记录。请使用 RFC 5737 文档段：
  `192.0.2.0/24`、`198.51.100.0/24`、`203.0.113.0/24`；主机名以 `.example` 结尾。
- 运维笔记与审计记录。这些放在仓库之外的目录（见下），不要放进 `docs/`。

## 运维材料放在哪里

证书和运行时状态放在工作树之外：

```
~/.config/incus-fleet/certs/<cluster>/<name>.crt
~/.config/incus-fleet/certs/<cluster>/<name>.key
```

配置里指向这些路径：

```yaml
clusters:
  - name: c62
    endpoint: https://incus.example:8443
    tls_cert: /home/incus/.config/incus-fleet/certs/c62/app.crt
    tls_key:  /home/incus/.config/incus-fleet/certs/c62/app.key
```

环境变量覆盖同理，例如用 `INCUS_FLEET_AUTH_TOKEN` 提供引导管理员 token。token 应当
运行时生成，不要粘贴字面量：

```bash
INCUS_FLEET_AUTH_TOKEN="$(openssl rand -hex 32)" ./bin/incus-fleet -config config.local.yaml -check
```

本地笔记与审计文档放在忽略规则已覆盖的路径下——`docs/local/`、`local-notes/`、
`notes/` 或 `scratch/`——或干脆放在仓库之外。这四个目录都按整目录忽略。

## 三层强制

防护是分层的，单点失误不会导致凭据泄漏。

**第一层 —— `.gitignore`。** 按位置（整目录忽略本地材料）和凭据形态
（`*.key`、`id_ed25519`、`*.kubeconfig`、`*.env`、`*.tfstate`、`secrets/` 等）双重拒绝。
规则在任何层级生效：一份笔记从 `docs/local/` 复制到新目录，即使文件名看着无害，
也会被内容扫描拦住。

**第二层 —— pre-commit hook。** `scripts/secret-scan.sh --staged` 检查即将进入历史的
真实字节：私钥块、凭据赋值、Bearer token、运维内网网段与主机名；同时拒绝把应留在本地的
路径加入暂存区。

每个克隆安装一次：

```bash
make hooks          # 将 core.hooksPath 指向 .githooks
```

**第三层 —— CI。** `.github/workflows/ci.yml` 的 `secrets` job 用同一个扫描器遍历
所有已跟踪文件和所有可达提交，能抓住 `git commit --no-verify` 绕过以及从未安装 hook 的
fork。

## 手动检查

```bash
make secret-scan           # 暂存区
make secret-scan-all       # 所有已跟踪文件
make secret-scan-history   # 所有提交，专查私钥
```

干净时输出 `secret-scan: clean`。有命中时会打印文件、行号和命中的规则；应当修正而不是
加例外。若确实是测试占位符，改成文档网段或 `.example` 主机名——这样才能让允许列表保持
小且可审计。

## 万一泄漏

1. **先轮换凭据。** 已推送的密钥一律视为已泄漏，清理历史并不能撤回公开。
2. 若仍保留在某个分支，再从历史中移除。
3. 记录事件与漏掉的规则，并补强扫描器。
