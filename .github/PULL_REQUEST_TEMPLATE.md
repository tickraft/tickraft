# Pull Request

## Description

<!-- Clearly describe WHAT this PR changes and WHY it is needed.
Link any design docs, issues, or discussions that provide context. -->

## Type of change

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Refactor (no functional change, no API change)
- [ ] Documentation update
- [ ] Test improvement
- [ ] Chore (build, CI, deps, tooling)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)

## Related Issue

<!-- Use "closes #123" / "fixes #123" / "resolves #123" to auto-close the linked issue.
If there is no related issue, explain why this change is needed. -->

closes #

## Checklist

- [ ] Code passes `gofmt`, `go vet`, and `golangci-lint`
- [ ] Frontend changes pass ESLint / Prettier / Stylelint (if applicable)
- [ ] Tests added or updated to cover the change
- [ ] Public API, types, and exported symbols are documented (Godoc / JSDoc)
- [ ] Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (e.g. `feat(scheduler): add retry backoff`)
- [ ] No secrets, credentials, or PII committed

## Contributor License Agreement

> **⚠ Before this PR can be merged, every contributor must sign the CLA.**
> The check runs automatically — signing is done by posting a comment below, **not** by ticking a checkbox here.

**How to sign** — reply to this pull request with:

```
I agree to the terms of the Tickraft Contributor License Agreement.
```

<details>
<summary>中文签署方式（点击展开）</summary>

在本 Pull Request 下方评论：

```
我已阅读并同意《Tickraft 贡献者许可协议》的条款。
```

</details>

- 📄 Full text: [CLA.md](https://github.com/tickraft/tickraft/blob/main/CLA.md) · [CLA.zh-CN.md](https://github.com/tickraft/tickraft/blob/main/CLA.zh-CN.md)
- ✅ One signature covers all your future contributions — no need to re-sign for each PR.
- 🔒 The `CLA Gate` check blocks CI until every contributor has signed. Once you sign, CI re-runs automatically.

## Open-source red line confirmation

Tickraft is dual-licensed under AGPLv3 + Commercial. The open-source repository must stay clean of any commercial-only logic. By submitting this PR I confirm:

- [ ] This PR contains no commercial plugin code, payment logic, license-check code, telemetry, or user data reporting.
