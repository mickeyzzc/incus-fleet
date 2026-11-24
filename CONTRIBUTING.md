# Contributing to Incus Fleet

**English** | [简体中文](docs/zh/CONTRIBUTING.zh-CN.md)

Thanks for helping improve Incus Fleet. This project manages real infrastructure, so changes
are reviewed for correctness, security, and operational safety before merge.

By participating you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- Search existing issues and pull requests to avoid duplicate work.
- For large features or behavior changes, open an issue first and describe the use case.
- Report security vulnerabilities privately as described in [SECURITY.md](SECURITY.md), never
  in a public issue.
- Do not include real IP addresses, hostnames, usernames, cluster names, tokens, keys, or
  screenshots of production systems in issues, commits, tests, or documentation.

## Development setup

```bash
make deps
make test
make coverage
make lint
```

Requirements are Go 1.27+, Bun 1.4+, and optionally live Incus clusters for manual testing.
Unit tests use `internal/testutil/incusfake` and never require a cluster.

Frontend development:

```bash
cd web
bun run dev
```

## Workflow

1. Fork the repository and create a topic branch.
2. Write a failing test that describes the change (TDD is required).
3. Implement the smallest change that makes the test pass.
4. Update English and Chinese documentation in the same change when behavior or instructions
   change.
5. Run the full gate: `make check && make coverage`.
6. Open a pull request using the template. Describe behavior, tests, and operational impact.

## Standards

Read [docs/en/standards.md](docs/en/standards.md) for the complete rules. Highlights:

- Go coverage must stay at or above 90% (`make coverage-go`).
- Frontend coverage must meet 90% statements/lines/functions and 85% branches.
- Run `gofmt`, `go vet`, `golangci-lint`, ESLint, and `vue-tsc` before pushing.
- Keep dependencies directed inward and follow existing package boundaries.
- Use structured logging and never log secrets.
- Every document needs an English canonical version and a Chinese mirror.
- Sanitize examples and screenshots; use documentation addresses such as `192.0.2.10`,
  `198.51.100.10`, and `example.com`.

## Commit messages

Use the imperative mood with a scoped prefix when useful:

```
migrations: restart source when copy fails
web: add batch toolbar tests
docs: document ISO installer workflow
```

Keep commits focused. Do not mix refactors with behavior changes.

## Pull request checklist

- [ ] Tests were written first and pass.
- [ ] Coverage gates pass.
- [ ] `make check` passes.
- [ ] English and Chinese docs are updated.
- [ ] New API routes and fields are documented in `docs/en/api.md` and `docs/zh/api.md`.
- [ ] Examples and screenshots are sanitized.
- [ ] No secrets, local-only files, or build artifacts are included.

## License

By contributing you agree that your contributions are licensed under the
[GNU Affero General Public License v3.0](LICENSE).
