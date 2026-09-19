# AGENTS.md

Guidance for AI agents and human contributors working on **Krate** itself (the
monorepo). For the AGENTS.md that ships inside scaffolded apps, see
`packages/create-krate-app/templates/app/AGENTS.md`.

Krate is a Go-native web framework: a Go compiler lexes/parses/bundles TSX into
static HTML plus a tiny signal-based hydration runtime, with optional
SSR/ISR/streaming via Node sidecars.

## Repository layout

```
packages/
  compiler/            Go compiler + CLI (`krate`)
    cmd/krate/         CLI entry point
    internal/          lexer, parser, bundler, renderer, build, config, check, plugin, ...
    ast/               AST types (public, used by pluginsdk)
    pluginsdk/         Go plugin SDK
  runtime/             @krate/runtime — signals, DOM, router, server renderer (TS)
  components/          @krate/components — component library (TSX)
  core/                @krate/core — npm CLI wrapper + platform binaries
  plugin/              @krate/plugin — plugin authoring types (TS)
  base-docs-theme/     @krate/base-docs-theme — default docs theme
  web/                 the krate.js.org docs site
  create-krate-app/    app scaffolder + templates
  create-krate-docs/   docs-site scaffolder + templates
  benchmark/           cross-framework benchmark harness
examples/              example app used by integration tests
```

## Commands

```sh
# From repo root:
pnpm dev               # run the compiler in dev against examples/
pnpm test              # sync docs + go test ./...
pnpm vet               # sync docs + go vet ./...
pnpm sync:docs         # regenerate embedded framework docs
pnpm check:docs        # fail if embedded docs are stale

# From packages/compiler:
go build ./...
go test ./...
go vet ./...
```

CI runs `go build`, `go vet`, `go test`, and 30s fuzz targets (`ci.yml`).

## Workflow

- New compiler/parser/bundler/renderer behavior ships with **Go unit tests**.
- New runtime behavior is covered by a docs example under `examples/`.
- User-facing behavior changes update the docs under
  `packages/web/src/content/docs/`.
- Run `pnpm check:docs` after touching docs.
- Do not commit `dist/`, `.krate/`, or generated embedded docs.

## Conventions

- **Errors:** use `internal/diag` (`file:line:col` + hint). Prefer
  `errWithMsg` with an actionable hint over a bare error.
- **Escaping:** all HTML/JS interpolation goes through `internal/escape`. Never
  build HTML/JS strings with raw user content.
- **Paths:** validate anything derived from content against the project root
  (`pluginapi.WithinRoot`).
- **Config:** new keys go in `internal/config` and are added to
  `knownTopLevelKeys` in `validate.go` so unknown-key warnings stay accurate.
- **No comments** unless they explain non-obvious intent.

## Roadmap / planning

The `TODO*.md` files at the repo root are the working plan:

- `TODO.md` / `TODO2.md` — feature catalog and rounds (server actions, API
  client, i18n, deploy adapters, …).
- `TODO3.md`–`TODO6.md` — CSS signals, Tailwind correctness.
- `TODO7.md` — Go API packages, build caching, CSS/JS DCE.
- `TODO8.md` — docs-theme polish (implemented).
- `TODO9.md` — lexer/parser correctness audit (implemented).
- `TODO10.md` — production-readiness roadmap (server hardening, testing,
  observability, docs).

## See also

- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Krate docs](https://krate.js.org/docs/)
