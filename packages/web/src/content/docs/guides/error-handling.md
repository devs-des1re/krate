---
title: Error Handling
order: 7
description: Handle build-time, render-time, and client-side errors in a Krate app.
---

# Error Handling

Krate fails loudly at build time and degrades gracefully at runtime. This guide
covers where errors surface and how to handle each kind.

## Build-time errors

Compile errors (lexer/parser/bundler/renderer) are reported with
`file:line:col`, a source line, a caret, and an actionable hint:

```
src/pages/about.tsx:20:0: expected ), got EOF
  <p>...</p>
       ^
  hint: check for an unclosed element or expression
```

`krate build` collects every error and exits non-zero. Fix them before
deploying — a failed build never writes a partial `dist/`.

To surface errors early in CI, run `krate check` (quality gates) and
`krate types && tsc --noEmit` (type errors) after `krate build`.

## Render-time errors (SSR/ISR/streaming)

Request-time pages are rendered by the Node SSR sidecar. If a page throws:

- The sidecar returns a `500`-status region frame; the Go server keeps serving
  the **baked shell** as a fallback and sets `X-Krate-Error: render`.
- The failure is logged to stderr with the route.

A page can declare a custom error surface by exporting an error page at
`src/pages/500.tsx` (and `404.tsx` for not-found). `krate build` bakes these.

## Client-side errors

Hydration errors are caught and reported:

- **Dev:** an overlay shows the error message and stack (`error-overlay.ts`).
- **Prod:** set a global hook to forward errors to your telemetry:

```ts
// runs on the client after hydration
window.__krate_onHydrationError = (err) => {
  navigator.sendBeacon("/telemetry", JSON.stringify({ message: String(err) }));
};
```

If hydration throws, the router falls back to a full page load so users still
reach the page.

## Error pages

Create `src/pages/404.tsx` and `src/pages/500.tsx`. They are ordinary pages and
are emitted as `dist/404.html` / `dist/500.html`:

```tsx
export default function NotFound() {
  return (
    <section class="not-found">
      <Head><title>404 — Not found</title></Head>
      <h1>Page not found</h1>
      <p>That page doesn't exist.</p>
      <a href="/">Go home</a>
    </section>
  );
}
```

## API route errors

A handler that throws returns `500` with a JSON body (embedded QuickJS runtime)
or the error surfaced by the sidecar. Validate input and return explicit
statuses:

```ts
export async function POST(req: Request) {
  const body = await req.json().catch(() => null);
  if (!body) return new Response(JSON.stringify({ error: "bad json" }), { status: 400 });
  return new Response(JSON.stringify({ ok: true }), { status: 200 });
}
```

## See also

- [Troubleshooting](/docs/guides/troubleshooting/)
- [Rendering](/docs/core-concepts/rendering/)
- [Deployment](/docs/guides/deployment/)
