---
title: Styling
order: 6
---

# Styling

Krate ships a full CSS pipeline: CSS Modules, Go-native Tailwind, rule-level
deduplication, minification, and `@import` inlining. There is no PostCSS and no
external CSS tooling.

## CSS Modules

Files named `*.module.css` are scoped automatically:

```tsx
// Card.module.css
.card {
  padding: 1rem;
  border-radius: 8px;
}
```

```tsx
import styles from './Card.module.css';

export default function Card() {
  return <div class={styles.card}>…</div>;
}
```

- Class names are hashed: `className` → `className_<fnv32a_hash>`.
- The hash is **FNV-32a of the absolute file path** (6-char base36), so it's
  deterministic per file and stable across builds.

## Tailwind CSS

Krate's Tailwind is **Go-native** — no PostCSS, no Node at build time.

```typescript
tailwind: {
  enabled: true,
  scanDirs: ["src"],          // or: content: ["./src/**/*.{tsx,mdx}"]
  preflight: false,           // opt-in base reset
  strict: false,              // warn on classes that produce no rule
  darkMode: "media",          // "media" | "class" | "selector"
}
```

- A **candidate scanner** extracts Tailwind tokens from every string and
  template literal in the scanned files, so classes inside
  `class={cond ? "a" : "b"}`, `clsx()/cn()/cva()` arguments, and arrays are
  all found.
- The CSS generator maps classes to rules from a built-in rule set. Output is
  **deterministic** — identical inputs always produce the same stylesheet.
- Theme configuration lives in `tailwind.config.ts`. It is **statically
  parsed** by default (no Node required); set `tailwind.executeConfig: true` to
  execute it via `npx tsx` instead. `theme.extend` merges onto the defaults;
  top-level `theme` keys replace them.

```tsx
<div class="p-4 hover:bg-zinc-100 dark:bg-zinc-900 w-[100px] -mt-2 bg-blue-500/50">
  Responsive, dark-mode aware, arbitrary values, negatives, color opacity.
</div>
```

Supported features:

- **Variants:** state (`hover:`, `focus:`, `active:`, `disabled:`, …),
  `group-*`/`peer-*` (incl. named), `data-*`/`aria-*`/`has-*`, responsive
  breakpoints from `theme.screens` (`sm:`, `md:`, …, plus `min-*`/`max-*`),
  `dark:`, `motion-safe:`/`motion-reduce:`, `print:`, capability queries
  (`pointer-coarse:`, `noscript:`, `forced-colors:`, …), positional
  (`nth-*`, `first-line:`), `not-*`, child (`*:`, `**:`), container queries
  (`@sm:`, `@[400px]:`), and arbitrary variants (`[&:nth-child(3)]:`).
- **Arbitrary values** for nearly every property, with type disambiguation
  (`w-[100px]`, `bg-[url(/a.png)]`, `bg-[length:8px]`, `text-[14px]` →
  font-size vs `text-[#fff]` → color, `grid-cols-[repeat(3,_1fr)]`), including
  typed hints (`text-[color:var(--fg)]`).
- Negatives (`-mt-4`), color opacity modifiers (`bg-blue-500/50`,
  `from-indigo-400/50`), and arbitrary spacing multiples (`p-13`, `p-13.5`).
- **Filters** (`blur-*`, `brightness-*`, `grayscale`, `hue-rotate-*`, `invert`,
  `saturate-*`, `sepia`, `drop-shadow-*`, and `backdrop-*`), **animation**
  (`animate-*`), **blend modes**, plus layout, typography, interactivity,
  3D-transform, border, and background families.
- Preflight: opt in with `tailwind.preflight: true`.

### Differences from Tailwind

Krate implements a **documented subset**; it is not byte-identical to the
Tailwind CLI. Notably: JS plugins, the `@tailwindcss/*` plugin ecosystem, and
`@apply` are not supported. Unrecognized classes produce no rule (enable
`tailwind.strict` to surface them).

## Global CSS

Plain CSS imported or referenced in pages is collected, deduplicated at the
rule level, and written as a single hashed `styles.<hash>.css`.

## The CSS processing pipeline

1. **Collect** CSS from all pages.
2. **Merge** with deduplication (rule-level).
3. **Inline `@import`** recursively (circular-safe, depth limit 10).
4. **Minify** — comment stripping, whitespace collapsing, `rgba()`/`rgb()`
   (both comma and space syntax) → hex, hex shortening, zero-unit removal,
   `calc()` simplification, and duplicate-declaration removal. The minifier
   preserves vendor-prefixed fallbacks (e.g. `display:-webkit-box` before
   `display:flex`) and case-sensitive custom properties.
5. **Hash-based filename** — `styles.<hash>.css`.

## Custom properties & theming

There's nothing special needed to use CSS custom properties — they pass through
the pipeline unchanged. The docs site uses a `:root[data-theme="dark"]` scheme
toggled at runtime for light/dark theming.
