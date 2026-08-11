// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

export default {
  extends: ['stylelint-config-standard-scss', 'stylelint-config-recess-order'],
  // Build output and generated bundles must never be linted (mirrors the
  // `**/dist/**` ignore in eslint.config.js).
  ignoreFiles: ['**/dist/**', '**/node_modules/**'],
  rules: {
    // tk- is the project namespace; el- is Element Plus' public class/variable
    // namespace (used for :deep(.el-*) overrides and the --el-* bridge in
    // styles/element.scss — see its header comment). Element Plus also emits
    // state classes (is-*) and a few legacy utility classes (cell, hover-row)
    // from its own markup that we target inside :deep() overrides.
    'selector-class-pattern': [
      '^(tk-|el-|is-|cell|hover-row)',
      { message: 'Class names must start with tk- (or el-/is- for Element Plus overrides) prefix' },
    ],
    // The codebase intentionally writes compact single-line declaration blocks
    // (e.g. @keyframes steps, color swatch maps). stylelint-config-standard-scss
    // v17 enables declaration-block-single-line-max-declarations by default;
    // keep the pre-v17 behaviour of allowing them.
    'declaration-block-single-line-max-declarations': null,
    // SCSS comment blocks use bare `//` lines to separate paragraphs; the
    // upgraded config flags them as empty comments, so relax the rule.
    'scss/comment-no-empty': null,
    // Prettier formats long calc() expressions with trailing operators at
    // end-of-line; the scss config rejects a newline after `-`, so relax it.
    'scss/operator-no-newline-after': null,
    // stylelint matches custom-property names without the leading `--`
    // (the rule tests `property.slice(2)`), so the pattern must omit it.
    'custom-property-pattern': [
      '^(tk-|el-)',
      { message: 'CSS custom properties must start with --tk- (or --el- for Element Plus bridge) prefix' },
    ],
    // Vue SFC scoped-style pseudo-classes are unknown to stylelint by default.
    'selector-pseudo-class-no-unknown': [
      true,
      { ignorePseudoClasses: ['deep', 'slotted', 'global'] },
    ],
  },
  overrides: [
    {
      files: ['**/*.vue'],
      customSyntax: 'postcss-html',
    },
  ],
}
