---
title: Typed Routes & Content
description: End-to-end TypeScript safety for links, route params, and content collections.
order: 12
---

# Typed Routes & Content

Krate knows every route and content entry at build time, so it generates
TypeScript declarations for them. `<Link href>`, dynamic route params, and
content collections become fully type-checked — with no configuration and no
runtime cost.

## Generated files

Every `krate build` (and the standalone `krate types` command) writes:

| File | Purpose |
|------|---------|
| `.krate/types/routes.d.ts` | `Route`, `StaticRoute`, `DynamicRoute`, `RouteParams`, and a `routes` manifest |
| `.krate/types/content.d.ts` | One `*Data` / `*Entry` interface per collection plus `ContentTypes` (only when collections are configured) |
| `src/krate-env.d.ts` | Bridge that augments `@krate/runtime` with the above |

All three are generated — add them to `.gitignore` and regenerate in CI. The
`.krate/` directory is ignored by default; add `krate-env.d.ts` too.

## Typed links

The generated `Route` is a union of your static routes plus template-literal
types for dynamic ones:

```ts
export type StaticRoute = "/" | "/about" | "/docs/getting-started";
export type DynamicRoute = `/video/${string}`;
export type Route = StaticRoute | DynamicRoute;
```

Because `src/krate-env.d.ts` augments `@krate/runtime`, `<Link href>` and
`<a href>` get autocomplete for every route. Arbitrary URLs (external links,
hashes, query strings) are always accepted, so nothing that works today breaks.

```tsx
<Link href="/docs/getting-started">Get started</Link>
<Link href="/video/abc123">Watch</Link>
<Link href="https://example.com">External</Link>
```

## Typed route params

`RouteParams` maps each dynamic pattern to its params object:

```ts
export interface RouteParams {
  "/video/[id]": { id: string };
}
```

## Content collections

Declare collections in `krate.config.ts` under `content`. Krate validates every
entry's frontmatter against the schema and generates types for it:

```ts
// krate.config.ts
import { defineConfig, defineContent } from "@krate/core";

export default defineConfig({
  content: defineContent({
    blog: {
      dir: "src/content/blog",
      schema: {
        title: "string",
        description: "string",
        date: "date",              // authored as an ISO string
        tags: "string[]",
        draft: "boolean",
        order: { type: "number", required: true },
      },
    },
  }),
});
```

`defineContent` is an optional identity helper — a plain object works too:

```ts
export default defineConfig({
  content: {
    blog: { dir: "src/content/blog", schema: { title: "string" } },
  },
});
```

Field specs are either a shorthand type string (`"string"`, `"number"`,
`"boolean"`, `"string[]"`, `"number[]"`, `"date"`) or an object with `type` and
an optional `required` flag. Unknown fields are ignored; omitted optional fields
are fine.

A **required** field that is missing — or a value of the wrong type — fails the
build (and `krate types` exits non-zero):

```text
Content error: blog: src/content/blog/post.md: missing required field "order"
```

The generated declarations type each entry:

```ts
export interface BlogData {
  author?: string;
  date?: string;
  description?: string;
  draft?: boolean;
  order: number;      // required
  tags?: string[];
  title?: string;
}
export interface BlogEntry {
  slug: string;
  path: string;
  data: BlogData;
}
```

## The `krate types` command

Generate declarations without running a full build — useful in CI and editors:

```sh
krate types          # writes .krate/types + src/krate-env.d.ts
krate types ./site   # explicit project directory
```

A typical CI type-check:

```sh
krate types && tsc --noEmit
```

> `krate types` discovers routes from `src/pages` and validates content
> collections, but it does not run plugins. For sites whose pages are generated
> by a plugin (for example the [docs plugin](/docs/features/plugins/)), run
> `krate build` to include those routes in the generated declarations.

## How it works

- Routes come from the page tree (`src/pages/**`), including plugin-generated
  pages and dynamic `[param]` segments. Error pages (`404`/`500`) are excluded.
- Collection entries are discovered under each collection's `dir`; frontmatter
  is parsed with the same mini-YAML parser used by the docs plugin.
- Route/params types are advisory: `krate-env.d.ts` imports them and augments
  the runtime. If generation fails, the build warns rather than stopping.
- Content **schema violations are build errors**, because they indicate a
  content bug.
