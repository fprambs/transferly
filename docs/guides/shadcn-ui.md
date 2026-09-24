# shadcn/ui and Tailwind v4

Sources: [Vite installation](https://ui.shadcn.com/docs/installation/vite), [components.json](https://ui.shadcn.com/docs/components-json).

## Setup

1. Vite React TypeScript project.
2. `pnpm add tailwindcss @tailwindcss/vite`; `@import "tailwindcss";` in `src/index.css`; `tailwindcss()` plugin in `vite.config.ts`. No `tailwind.config.js` in v4.
3. `@/*` alias in `tsconfig.json`, `tsconfig.app.json` and `vite.config.ts`; `pnpm add -D @types/node`.
4. `pnpm dlx shadcn@latest init -b base -p nova --rtl --css-variables -y`. Without `-p` the CLI asks for a preset and blocks in a non-interactive container; run it with `docker compose run -T`.
5. `pnpm dlx shadcn@latest add <component>`.

## Rules

- Components are copied to `src/components/ui/` and owned by the project; edit them there.
- `cssVariables: true`; it cannot be changed after init.
- Style `new-york` (`default` is deprecated).
- Business components live in `features/`; primitives in `components/ui/`.
- Variants with `cva`; class merging with `cn()`.
- Icons from `lucide-react`.

## Base Library

Resolved: `shadcn` 4.21 offers `--base base|radix|aria`. Sentinel uses Base UI (`style: base-nova`, generated `components.json`) with RTL enabled.

## Dependencies

`init` adds `cn`, the shadcn maintainers' replacement for `clsx` plus `tailwind-merge`. It was published days before adoption; it is tracked in `docs/security/`.

## Tests

Testing Library by role and label; visual and axe checks in Playwright.
