# Contributing to Tickraft

> 版本：V1.1 | 更新：2026-08-05 | 状态：active

> ⚠️ **免责声明**：本中文文档仅供参考翻译，不构成法律或技术承诺。如中英文存在任何不一致，请以 [英文权威文档 CONTRIBUTING.md](./CONTRIBUTING.md) 为准。
>
> 本中文文档与英文权威文档保持版本同步，版本号见文档头部。

感谢你对 Tickraft 开源项目的关注！本文档描述如何搭建开发环境、贡献流程、需遵守的代码规范，以及适用于全部贡献的法律框架（CLA + 双协议授权）。

## 开发环境

- Go 1.26+
- Node.js 18+
- pnpm 8+
- 推荐：`gofmt`、`go vet`、`golangci-lint`、ESLint、Prettier 均已纳入工具链

## 开发流程

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'feat: add amazing feature'`)
4. 推送分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

## 代码规范

- Go 代码需通过 `gofmt`、`go vet`、`golangci-lint`
- 前端代码需通过 ESLint 和 TypeScript 类型检查
- 公共 API 需有文档注释
- 提交信息使用约定式提交（Conventional Commits）

### 提交信息规范（Conventional Commits）

提交信息需遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范，格式为 `<type>[optional scope]: <description>`，常用类型：

- `feat`：新功能
- `fix`：缺陷修复
- `docs`：文档变更
- `style`：代码格式（不影响功能）
- `refactor`：重构（既非新增功能也非修复缺陷）
- `perf`：性能优化
- `test`：测试相关
- `chore`：构建/工具链/杂项

示例：`feat(scheduler): support event-driven scheduling`

## 开源红线

- 严禁提交任何商业插件代码、付费逻辑、License 校验代码
- 严禁内置遥测、用户数据上报、付费推广埋点
- 严禁在 UI 内植入付费广告、订阅套餐页面

## 许可证

本仓库采用 AGPLv3 + 商业授权双协议模式。详见 [LICENSE](LICENSE)。

提交的代码将由版权持有人以双协议模式分发（AGPLv3 或商业授权）。

## 版权声明规则

每个源文件必须在文件最顶部携带标准 3 行版权声明，声明与文件内容间保留一行空行。所有文件的声明内容完全一致，仅注释语法因文件类型而异。

**标准声明内容：**

```
Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
SPDX-License-Identifier: AGPL-3.0-or-later
Dual-licensed — see LICENSE for details.
```

**各文件类型注释语法：** `.go` / `.ts` / `.js` / `.vue` / `.scss` 使用 `//`；`.yaml` / `.yml` / `.toml` 使用 `#`；`.html` 使用 `<!-- -->`。

### 修改现有文件

- 保持原有版权声明不变 —— 不得添加个人版权行。
- 不得修改年份（固定 `2026` 代表首次发布年份，而非最后修改年份）。
- 即使重写整个文件，也不得删除声明。

### 新增文件

- 使用标准 3 行声明，版权持有人为 `Beijing Ruishuo Technology Co., Ltd.`（非贡献者个人名称）。
- 新文件使用创建当年的年份（如 2027 年创建的文件使用 `2027`）。
- 运行 `make license-header-fix` 自动添加声明，或从现有文件复制。

### 第三方代码

如引入其他开源项目的代码，须保留原始版权声明，并在标准声明下方添加来源说明：

```go
// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.
//
// This file incorporates work from <project-name>:
//   Copyright (c) <year> <original-author>
//   Licensed under <license>
```

- 原始许可证须与 AGPLv3 兼容（MIT、Apache-2.0、BSD 兼容；GPL-2.0 不兼容）。
- 禁止引入 GPL/AGPL/SSPL 许可证代码 —— `license-scan` CI 检查会阻止此类依赖。

### 贡献者署名

根据 CLA 协议，贡献的版权转让给版权持有人。贡献者通过 Git 提交历史（`git log --author`）获得署名，而非通过文件头版权行。不得在任何文件中添加 `Copyright (c) <year> <contributor-name>`。

### CI 强制检查

`license-headers` CI 作业在每次 push 和 pull request 时运行 `make license-header`。任何缺失标准声明的文件将导致检查失败并阻止合并。提交 PR 前请在本地运行 `make license-header` 验证合规性。

## CLA 贡献者协议

所有贡献者在首次 Pull Request 合并前需签署 CLA（Contributor License Agreement）。CLA 全文详见 [CLA.md](CLA.md)，核心条款如下：

- **版权转让**：贡献者将其贡献的版权转让给版权持有人（北京睿朔科技有限公司 (Beijing Ruishuo Technology Co., Ltd.)），确保版权统一归属，支持双协议分发。
- **专利授权**：贡献者授予版权持有人非独占、全球范围、免版税的专利授权，用于其贡献中包含的专利权利。
- **双协议分发授权**：版权持有人有权以 AGPLv3 或商业授权任一协议分发贡献者的贡献，无需另行支付费用。
- **保留权利**：除明示转让的版权与授予的专利权外，贡献者保留其对贡献的其他全部权利。

**签署方式**：在你的任一 Pull Request 下评论以下短语（机器人会自动记录签名，无需维护者介入）：

```
I agree to the terms of the Tickraft Contributor License Agreement.
```

> 也可使用中文签署：`我已阅读并同意《Tickraft 贡献者许可协议》的条款。`

该签署对后续全部贡献有效，无需就每次 PR 重复签署。签名记录后，`CLA` 工作流会在每个 Pull Request 上自动校验并添加 `cla-signed` 标签；未签署贡献者的 Pull Request 将被 `CLA Gate` 必选检查拦截，直至全部贡献者完成签署。

## 文档翻译与同步

Tickraft 文档采用双语维护：英文为权威基线，中文为参考翻译。本章节规范两种语言版本的同步机制。

### 权威语言

- 英文为全部文档的权威基线。
- 中文仅为参考翻译，**例外**：`README.md` 为强制双语等效文档（`README.md` 与 `README.zh-CN.md` 具同等地位）。
- 任何不一致情况下，以英文版本为准。

### 文件命名

- 英文权威文档保留原始文件名（如 `CONTRIBUTING.md`、`CLA.md`）。
- 中文参考文档使用 `*.zh-CN.md` / `*.zh-CN.txt` 后缀（如 `CONTRIBUTING.zh-CN.md`、`CLA.zh-CN.md`）。
- `docs/` 目录采用子目录布局：英文文档位于 `docs/*.md`，中文翻译位于 `docs/zh-CN/*.md`（文件名相同、目录不同）。截图等共享资源统一放在 `docs/screenshots/` 下，两种语言版本共同引用。

### 中文免责声明

全部中文参考文档（README 除外）必须在首个 H1 标题之后紧接免责声明，声明本中文文档仅供参考、英文版本为权威。

### 更新触发

英文权威文档发生以下任一变更时，触发中文翻译更新：

- 新增章节
- 修改既有条款
- 版本号升级
- 事实性修正

纯格式变更（空白、换行、不影响语义的拼写修正）**不**触发翻译更新。

### 翻译评审流程

1. **初译** — 由译者完成初始翻译。
2. **评审** — 由评审人检查语义准确性与术语一致性。
3. **合并** — 由维护者合并翻译 PR。

译者与评审人必须**不是同一人**（双人原则）。翻译 PR 必须链接触发其更新的英文变更 PR 或 commit。法律文档（`CLA.md`、`LICENSES/COMMERCIAL.txt`）翻译需由维护者主导最终评审。

### 版本管理

英文权威文档与中文参考文档须保持版本号一致。格式：

```
> 版本：Vx.y | 更新：YYYY-MM-DD | 状态：active
```

中文版本落后于英文版本视为缺陷，必须在下一次发布前同步。

### 冲突解决

- 以英文权威文本为准；中文版本须在 7 个自然日内对齐。
- 法律文本冲突一律以英文版本为准；中文翻译不具独立法律效力。
- 冲突解决须记录于 PR 描述与提交信息中。
