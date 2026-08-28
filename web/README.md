# tickraft-web

Frontend of the Tickraft open-source edition — a pnpm monorepo consisting of `packages/core` (kernel) and `packages/features` (business features), plus an `app/` demo application.

## Tech Stack

- **Vue 3.5+** — Composition API with `<script setup>`
- **TypeScript 6** — strict mode
- **Vite 8** — build tooling
- **Vue Router 5** — config-based routing
- **Pinia 4** — state management (Composition API style)
- **Element Plus** — UI component library
- **UnoCSS** — atomic CSS
- **vee-validate 4 + zod 4** — form validation
- **pnpm** — package manager (workspace monorepo)

## Directory Structure

```
web/
├── packages/
│   ├── core/        # @tickraft/core — kernel (components, composables, router, stores, i18n, styles)
│   ├── features/    # @tickraft/features — business features (routes, menus, views, api, i18n)
│   └── ui/          # shared UI utilities
├── app/             # demo application (main.ts + Vite/UnoCSS config)
├── package.json
├── pnpm-workspace.yaml
└── tsconfig.base.json
```

## Prerequisites

- **Node.js** 22+
- **pnpm** 9+

## Development

All commands should be run from the `web/` directory.

```bash
# Install dependencies
pnpm install

# Start dev server
pnpm run dev

# Build for production
pnpm run build

# Preview production build
pnpm run preview
```

### Additional Commands

```bash
# Lint
pnpm run lint

# Format
pnpm run format

# Stylelint
pnpm run stylelint

# Type check
pnpm run type-check

# Run tests
pnpm run test
```

## Related

- [Root README](../README.md) — Tickraft project overview and deployment guide
- [Contributing Guide](../CONTRIBUTING.md) — development conventions and contribution workflow
