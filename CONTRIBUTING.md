# Contributing to Tickraft

> Version: V1.1 | Updated: 2026-08-05 | Status: active

Thank you for your interest in contributing to the Tickraft open-source project! This document describes how to set up your development environment, the workflow we follow, the standards we enforce, and the legal framework (CLA + dual license) that applies to every contribution.

## Development Environment

- Go 1.26+
- Node.js 18+
- pnpm 8+
- Recommended: `gofmt`, `go vet`, `golangci-lint`, ESLint, and Prettier are wired into the toolchain.

## Development Workflow

1. Fork this repository.
2. Create a feature branch (`git checkout -b feature/amazing-feature`).
3. Commit your changes (`git commit -m 'feat: add amazing feature'`).
4. Push the branch (`git push origin feature/amazing-feature`).
5. Open a Pull Request.

## Code Standards

- Go code must pass `gofmt`, `go vet`, and `golangci-lint`.
- Frontend code must pass ESLint and TypeScript type checking.
- Public APIs must have doc comments.
- Commit messages must follow [Conventional Commits](https://www.conventionalcommits.org/).

### Commit Message Convention (Conventional Commits)

Commit messages follow the [Conventional Commits](https://www.conventionalcommits.org/) specification in the form `<type>[optional scope]: <description>`. Common types:

- `feat`: a new feature
- `fix`: a bug fix
- `docs`: documentation changes
- `style`: code formatting (no functional impact)
- `refactor`: a code change that neither adds a feature nor fixes a bug
- `perf`: a performance improvement
- `test`: test-related changes
- `chore`: build / toolchain / miscellaneous

Example: `feat(scheduler): support event-driven scheduling`

## Open-Source Red Line

- Never commit any commercial plugin code, payment logic, or license-check code.
- Never embed telemetry, user-data reporting, or paid-promotion tracking.
- Never inject paid advertisements or subscription/plan pages into the UI.

## License

This repository is distributed under the AGPLv3 + Commercial dual-license model. See [LICENSE](LICENSE) for the full statement.

Contributions are distributed by the copyright holder under the dual-license model (AGPLv3 or Commercial License).

## Copyright Header Rules

Every source file must carry a standardized 3-line copyright header at the very top, followed by one blank line before the file's content. The header format is identical across all files; only the comment syntax differs by file type.

**Standard header content:**

```
Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
SPDX-License-Identifier: AGPL-3.0-or-later
Dual-licensed — see LICENSE for details.
```

**Comment syntax by file type:** `.go` / `.ts` / `.js` / `.vue` / `.scss` use `//`; `.yaml` / `.yml` / `.toml` use `#`; `.html` uses `<!-- -->`.

### Modifying existing files

- Keep the existing copyright header unchanged — do not add your own copyright line.
- Do not modify the year (the fixed `2026` represents first publication, not last modification).
- Do not remove the header even if you rewrite the entire file.

### Adding new files

- Use the standard 3-line header with copyright holder `Beijing Ruishuo Technology Co., Ltd.` (not your own name).
- Use the current year for new files (e.g., a file created in 2027 uses `2027`).
- Run `make license-header-fix` to add the header automatically, or copy it from an existing file.

### Third-party code

If you incorporate code from another open-source project, keep the original copyright notice and add it below the standard header:

```go
// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.
//
// This file incorporates work from <project-name>:
//   Copyright (c) <year> <original-author>
//   Licensed under <license>
```

- The original license must be AGPLv3-compatible (MIT, Apache-2.0, BSD are compatible; GPL-2.0 is not).
- GPL/AGPL/SSPL-licensed code is prohibited — the `license-scan` CI check blocks such dependencies.

### Contributor attribution

Per the CLA, copyright in contributions is assigned to the copyright holder. Contributors are credited through Git commit history (`git log --author`), not through per-file copyright lines. Do not add `Copyright (c) <year> <contributor-name>` to any file.

### CI enforcement

The `license-headers` CI job runs `make license-header` on every push and pull request. Any file missing the standard header will fail the check and block the merge. Run `make license-header` locally before submitting a PR to verify compliance.

## CLA (Contributor License Agreement)

All contributors must sign the CLA before their first Pull Request is merged. The full CLA text is in [CLA.md](CLA.md). Core terms:

- **Copyright assignment**: the contributor assigns the copyright of the contribution to the copyright holder (Beijing Ruishuo Technology Co., Ltd.) to ensure unified ownership and enable dual-license distribution.
- **Patent grant**: the contributor grants the copyright holder a non-exclusive, worldwide, royalty-free patent license for any patent claims embodied in the contribution.
- **Dual-license distribution authorization**: the copyright holder is authorized to distribute the contribution under AGPLv3 or the Commercial License, at its discretion, without additional payment.
- **Reserved rights**: except for the copyright assigned and the patent rights granted above, the contributor retains all other rights in the contribution.

**How to sign**: reply to any of your pull requests with the following phrase (a bot records your signature automatically — no maintainer action needed):

```
I agree to the terms of the Tickraft Contributor License Agreement.
```

> 中文签署方式：在你的任一 Pull Request 下评论：`我已阅读并同意《Tickraft 贡献者许可协议》的条款。`

This signature covers all subsequent contributions; you do not need to re-sign for each PR. Once recorded, the `CLA` workflow verifies your signature on every pull request and applies the `cla-signed` label. Pull requests with unsigned contributors are blocked by the required `CLA Gate` job until every contributor signs.

## Documentation Translation & Sync

Tickraft maintains documentation in two languages: English as the authoritative baseline and Chinese as a reference translation. This section codifies how the two language versions are kept in sync.

### Authoritative Language

- English is the authoritative baseline for all documentation.
- Chinese is a reference translation only, **except** for `README.md`, which is a mandatory bilingual equivalent (both `README.md` and `README.zh-CN.md` carry equal standing).
- In the event of any inconsistency, the English version prevails.

### File Naming

- English authoritative docs keep the original filename (e.g. `CONTRIBUTING.md`, `CLA.md`).
- Chinese reference docs use the `*.zh-CN.md` / `*.zh-CN.txt` suffix (e.g. `CONTRIBUTING.zh-CN.md`, `CLA.zh-CN.md`).
- The `docs/` directory uses a subdirectory layout instead: English docs live at `docs/*.md` and Chinese translations live at `docs/zh-CN/*.md` (same filename, different directory). Screenshots and other shared assets stay under `docs/screenshots/` and are referenced by both language versions.

### Chinese Disclaimer

All Chinese reference docs (except `README`) must carry a disclaimer header immediately after the first H1 title, stating that the document is for reference only and that the English version is authoritative.

### Update Trigger

A Chinese translation update is triggered whenever the English authoritative document changes in any of the following ways:

- A new section is added.
- An existing clause is modified.
- A version bump occurs.
- A factual fix is applied.

Pure formatting changes (whitespace, line wrapping, typo fixes that do not alter meaning) do **not** trigger a translation update.

### Translation Review Flow

1. **Initial translation** — performed by a translator.
2. **Review** — a reviewer checks semantic accuracy and terminology consistency.
3. **Merge** — a maintainer merges the translation PR.

The translator and the reviewer must **not** be the same person (two-person principle). The translation PR must link the English change PR or commit that triggered it. Translations of legal documents (`CLA.md`, `LICENSES/COMMERCIAL.txt`) require a maintainer-led final review.

### Version Management

The English authoritative document and its Chinese reference must carry matching version headers. Format:

```
> 版本：Vx.y | 更新：YYYY-MM-DD | 状态：active
```

A Chinese version that lags behind its English counterpart is a defect and must be synced before the next release.

### Conflict Resolution

- The English authoritative text prevails; the Chinese version must align within 7 calendar days.
- For legal text conflicts, the English version is always authoritative; the Chinese translation has no independent legal effect.
- The conflict resolution must be recorded in the PR description and the commit message.
