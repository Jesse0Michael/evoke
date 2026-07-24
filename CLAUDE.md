# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What Evoke is

Evoke is an experimental declarative source format (`.evoke`), CLI, and registry for defining and composing AI characters and generative assets. The governing idea: **prompts are compiled artifacts, not source material.** Users author small reusable `.evoke` files of structured declarations; the CLI merges any selection of them — resolved by tag-based selectors, local paths, or registry references — and sends the composition through a generation pipeline (currently ComfyUI for image generation).

The `docs/` directory is a Just the Docs / GitHub Pages site; product docs live under `docs/file-format/` and `docs/cli/`, and design principles under `docs/design/`.

**Status:** The parser, declaration schema, validation, merge/resolver, tag-based selector system, SQLite file index, registry client, and the `generate` command are all implemented. The hosted registry API is functional.

**Declaration set (12).** `NAME`, `CHARACTER`, `PERSONALITY`, `BACKSTORY`, `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `SCENARIO`, `PROMPT`, `IMAGE`, `LORA`, `DETAILER`. `IDENTITY` is a migration alias for `CHARACTER`. `ENVIRONMENT` carries the whole scene/setting role. `IMAGE`, `LORA`, and `DETAILER` are structured declarations that accept arguments and key=value settings. `TAGS` is a metadata block (not a declaration) used for selector matching. Namespaced/dotted extension names (`FOO.BAR`) are out of scope; the parser rejects them.

## Core mental model (the non-obvious parts)

These invariants shape almost every file and are easy to violate:

- **Files are typeless.** A `.evoke` file never declares that it is a "character," "apparel," or "location" file. Its meaning *emerges* from the declarations it contains. Never add a `TYPE`/`FROM`/`IMPORT` mechanism.
- **Composition is external.** Files do not reference each other. The *caller* selects which files compose together (`evoke generate character shot`). File order must not silently change behavior unless the spec explicitly says so.
- **Never concatenate early.** Parsing and merging operate on structured declarations. Flattening to a prompt string is a *rendering* concern that happens last.
- **Tag-based discovery.** Files declare `TAGS` blocks; the selector system (`pkg/evoke/selector.go`) matches files by tag and/or facet. The SQLite index (`internal/cli/indexdb.go`) stores parsed metadata for fast selector resolution.

## Declaration prefixes (channels & default)

A declaration line may carry a prefix that selects a *channel* or *default*, not an operation:

- (no prefix) — explicit **positive** contribution.
- `!` — **negative/exclusion** channel (e.g. image negative prompt). It does **not** mean delete/override/disable. Only declarations that support a negative channel may use it (`!NAME` is a validation error).
- `?` — **default**: used only when no explicit contribution exists for the same declaration+channel. `?APPAREL` defaults are suppressed the moment any explicit `APPAREL` appears.
- `?!` — default into the negative channel. Postponed until a real use case appears; not required for the first parser.
- There is deliberately **no** `=`/force/replace operator in v1. Two conflicting explicit singular values are a *conflict*, never silently resolved by order.

## Merge modes

Each declaration has a registered `Definition` (see `pkg/evoke/schema.go`) fixing its merge mode, channel/default support, render order, and selector aliases:

- **singular** — at most one explicit value. Zero → use default; more than one explicit → **conflict** (warns, uses first).
- **accumulating** — combine values in order; deduplicate only *exact* normalized (trim-whitespace) matches. Never attempt semantic dedup (`violet skin` ≠ `purple skin`).

## Pipeline & package layout

All core format logic lives in `pkg/evoke/`:

- `pkg/evoke/parse.go` — lexer + parser for `.evoke` syntax. Produces a `Document`.
- `pkg/evoke/document.go` — `Document`, `Declaration`, `Metadata` types.
- `pkg/evoke/schema.go` — `Definition` type, the built-in declaration registry, selector aliases, and migration aliases.
- `pkg/evoke/validate.go` — per-file semantic validation (unknown declaration, unsupported `!`/`?` prefix).
- `pkg/evoke/merge.go` — the resolver/merger: explicit-vs-default selection per declaration+channel, singular conflicts, accumulation + exact dedup. Produces a `Composition`.
- `pkg/evoke/selector.go` — tag-based selector parsing, matching, and random selection.

CLI and generation:

- `cmd/evoke` — thin `evoke` CLI entrypoint: owns command dispatch + usage only, delegates to `internal/cli`. Builds to `bin/evoke`. Commands: `login`, `generate`, `settings`, `index`, `queue`, `clear`, `history`, `view`, `completion`.
- `internal/cli` — command implementations. `generate` resolves inputs (selectors via the SQLite index, local file paths, `@namespace/name` registry references), merges documents, and submits to ComfyUI. `login` runs the Google loopback + PKCE flow, exchanges the ID token at the registry, and stores tokens in `~/.evoke/credentials.json`. `settings` manages `~/.evoke/settings.json` (source paths). `index` refreshes the local SQLite index. `view` launches an interactive terminal image viewer with metadata display. `history` shows recent generations with output resolution. `queue` displays the ComfyUI generation queue. `clear` clears the queue.
- `internal/cli/indexdb.go` — SQLite-backed file index storing tags and declarations for fast selector resolution.
- `internal/cli/sources.go` — source root discovery (EVOKE_PATH, configured paths, library directory), file walking, input classification.
- `internal/generate` — `Generator` interface.
- `internal/generate/comfyui` — ComfyUI generator: converts a `Composition` into template data, renders a workflow template, submits to ComfyUI's `/prompt` endpoint.
- `internal/client` — the **generated** Go client (`client.gen.go`) for `api/openapi.yaml`; consumed by `internal/cli` for registry operations.

## Backend registry API (`evoke-registry`)

The hosted registry is the distribution backend — think **"docker of our files."** It stores immutable `.evoke` artifact versions and the accounts that publish them, and serves push / pull / list over **REST/HTTP**. It lives in this same module, sharing the parser/schema/registry-reference code with the CLI.

**Deliberate choices (do not silently change these):**

- **REST/HTTP, not gRPC.** A registry is push/pull/list of small content-addressed text documents — the Docker/npm/OCI shape. Routes use stdlib `net/http` with Go 1.22+ method+path patterns (`mux.HandleFunc("PUT /v1/{namespace}/{name}", ...)`). No proto toolchain, no entproto annotations on the schemas.
- **Spec-first: `api/openapi.yaml` is the source of truth, clients are generated.** The REST surface is described in OpenAPI 3.0 at [api/openapi.yaml](api/openapi.yaml). Go clients are **generated, never hand-written**, by **oapi-codegen** — a **go.mod `tool` dependency** run via `go tool oapi-codegen` (so the only build dependency is Go itself, same pattern as ent). `make generate` (`go generate ./...`) regenerates both. The generated client lives in `internal/client/client.gen.go` (`DO NOT EDIT`; change the spec + regenerate). When you add/change an endpoint: update the handler **and** `api/openapi.yaml`, then `make generate`. (Server handlers are still hand-written for now; generating server stubs from the same spec is a possible later step to keep them in lockstep.)
- **ent + Postgres** for entities and edges (`entgo.io/ent`). Schemas live in `internal/ent/schema/`; regenerate with `make generate` (`go generate ./...` → `go tool ent generate ./schema`). **Generated ent code is committed** — never gitignore `internal/ent/`. Schema changes require re-running `make generate`.
- **Content lives in a Postgres column** (`Version.content` bytes) for the MVP — `.evoke` files are tiny. Everything goes through the `store.Store` interface so an object-store (S3/MinIO) backend can slot in later without touching HTTP handlers. Versions are addressed by `sha256`, so that swap needs no schema change.
- **Auth is federated (Google/OIDC), the identity is ours.** There is **no password storage**. The client (CLI/web) obtains a Google ID token through its own browser/PKCE flow and `POST`s it to the backend; the backend *verifies it once* and issues **our own** `pkg/auth` JWT pair. Every other endpoint authenticates with our JWT — never a Google token. Google is a *login method*, not the identity: a `User` is canonical and an `Identity(provider, subject)` row links a login method to it, so adding providers (GitHub, email/pass, SSO) later is additive with no migration. Do **not** verify Google tokens on every request, and do **not** collapse "Google user" into "the identity."
- **No API tokens yet — but leave the seam.** Only human, browser-interactive login exists for now. A machine (server-side bot/generator that isn't on the user's laptop) would need a revocable API token; that's a documented later feature. Keep the "who is this request?" resolution behind the auth middleware seam so a token type can be bolted on without reworking handlers. Don't build API tokens until a non-human publisher actually appears.
- **Content lives in a Postgres column** (`Version.content` bytes) for the MVP — `.evoke` files are tiny. Everything goes through the `store.Store` interface so an object-store (S3/MinIO) backend can slot in later without touching HTTP handlers. Versions are addressed by `sha256`, so that swap needs no schema change.
- **Reuse the `github.com/jesse0michael/pkg/*` helpers** rather than hand-rolling infra. The server's lifecycle is owned by **`boot.App[Config]` + a `boot.Runner`** — `boot` absorbs signal-aware context, config loading (env + files + flags), logger setup, the `:9999` health/metrics endpoint, shutdown ordering, and (when `config.OpenTelemetryConfig` is present in `Config`) telemetry. The runner's `Run` builds deps + starts serving without blocking; `Close` does graceful shutdown. Don't reintroduce hand-rolled `signal.NotifyContext` / manual shutdown in `cmd/app`. Also in use: `config` (`PostgresConfig`, `NewPostgresClient`, `AppConfig`), `auth` (`JWTAuth`, access/refresh tokens, `auth.Subject(ctx)`, `WithSubject`), `http/handlers` (`HandleHealth`, `HandleWithMiddleware`), `http/middleware` (`Auth`, `Default`), `http/errors` (`WriteError`, `NewError`). Each `pkg/<name>` is an independently versioned module — pin the versions kithwind uses. **Note:** `boot.App` targets long-running services; the `cmd/evoke` tool is a fire-and-exit subcommand dispatcher and deliberately does **not** use `boot.App` (it would pull in the `:9999` server + block-until-signal loop). When the CLI grows networked subcommands, reach for `pkg/config`/`pkg/logger` directly, not `boot.App`.
- **`internal/auth/oidc` is written for later `pkg` extraction.** It's provider-agnostic (generic OIDC via `github.com/coreos/go-oidc/v3`) except the `NewGoogle` constructor; the registry only consumes the `Verifier` interface, and storage sits behind the `store` interface. When it's solid, lift the provider-agnostic core to `pkg/auth/oidc` and leave the ent-specific `FindOrCreateUserByIdentity` behind.

**Package layout (server side):**

- `internal/ent/schema` — ent schemas: `User` (account/publisher, **no password**; username derived from the provider email on first login, editable), `Identity` (provider + subject → user, the pluggable login-method seam), `Artifact` (namespace/name, owned by a user), `Version` (immutable content + sha256, monotonic per artifact). No entproto annotations.
- `internal/auth/oidc` — provider-agnostic OIDC `Verifier` + `Claims` (+ `NewGoogle`). Verifies ID tokens against the provider's JWKS and checks the audience against an **accepted-client list** (`NewGoogle` takes `[]string`) so the Desktop client (CLI) and Web client (site) are both honored; fails closed on an empty list. A verify failure maps to 401.
- `internal/store` — the `Store` persistence interface + ent-backed implementation. `FindOrCreateUserByIdentity` resolves/links/creates accounts in an `ent.Tx`; maps ent constraint/not-found errors to `store.ErrConflict` / `store.ErrNotFound`. Account deletion removes children explicitly in a tx (ent doesn't apply FK cascade uniformly across dialects).
- `internal/api` — the HTTP server: `Server`, `Routes()`, and handlers (`accounts.go` Google login + account CRUD, `artifacts.go` push/list/pull). `api.Server` satisfies `pkg/http/server`'s `Router` (via `Routes()`), so the `*http.Server` is built by that pkg, not by hand. Handlers map `store` errors to HTTP status via `http/errors.WriteError`; auth-protected routes wrap with `middleware.Auth`.
- `internal/client` — the **generated** Go client (`client.gen.go`) for `api/openapi.yaml`; `generate.go` holds the `go:generate` directive and `cfg.yaml` the oapi-codegen config. Consumed by `internal/cli`.
- `cmd/app` — the registry server entrypoint (builds to `bin/evoke-registry`): load config → Postgres/ent client → `Schema.Create` → `store.New` → `auth.NewJWTAuth` → `oidc.NewGoogle` (OIDC discovery, needs network at startup) → serve with graceful shutdown.

**MVP endpoints** (auth + accounts + upload are the certain surface; the rest will grow):

```text
GET    /health
POST   /v1/tokens/google                 exchange a Google ID token -> {user, access_token, refresh_token}
GET    /v1/account            [auth]     the authenticated account ("me")
PATCH  /v1/account            [auth]     update username
DELETE /v1/account            [auth]     delete account (cascades to identities + artifacts)
PUT    /v1/{namespace}/{name} [auth]     push raw .evoke body -> new immutable version
GET    /v1/{namespace}/{name}            list versions
GET    /v1/{namespace}/{name}/{version}  pull raw .evoke bytes (X-Evoke-SHA256 header)
```

**Conventions:** IDs are `uuid` strings. Immutable versions are never mutated; a new push is always a new row. `Schema.Create` auto-migration is fine for the MVP — move to versioned migrations before anything durable. Tests: table-driven; store/handler tests run against an in-memory sqlite ent client (`enttest`, `CGO_ENABLED=1` — imports `github.com/mattn/go-sqlite3`), and handler tests inject a fake `oidc.Verifier` so no real Google round-trip is needed. The store and handlers are the correctness core here.

**Local dev:** `make up` (Postgres + registry via docker-compose) or `make run` (registry against the compose Postgres). `make run` supplies sane dev defaults and sources a gitignored `.env` if present; `docker compose` auto-reads `.env` too. `AUTH_SECRET_KEY` has a dev default in `make run`; `GOOGLE_CLIENT_ID` is the accepted-audience list (comma-separate the Desktop + Web client IDs; login rejects tokens whose audience isn't listed). There is no committed env template — secrets live only in the local `.env`. Never bake real secrets into committed files.

## Build, test, lint

Module path is `github.com/jesse0michael/evoke`. Build binaries into the repo-root `bin/` directory.

```bash
make build                                   # build both binaries into bin/ (evoke, evoke-registry)
go install ./cmd/evoke                       # install the CLI to GOPATH/bin
go build -o bin/evoke ./cmd/evoke            # build the CLI only
go build -o bin/evoke-registry ./cmd/app     # build the registry API only
make generate                                # regenerate ent code after schema changes
make up / make down                          # local stack (Postgres + registry) via docker-compose
make run                                      # run the registry against the compose Postgres
go test ./...                                # all tests
go test ./internal/store/ -run TestStore   # a single package / test
golangci-lint run                            # lint
```

After editing `.go` files, run `goimports -w` on them and `golangci-lint run --fix` on the changed files (per global Go guidelines).

## Testing conventions specific to Evoke

- The resolver/merger and parser are the correctness core — cover **every merge behavior** (singular one/zero/conflict, accumulating dedup, default suppression, positive vs negative channels, unsupported-prefix errors) with table-driven tests.
- Assert against the parsed `Document` / the `Composition`. Parser and validate tests use inline `src` strings; larger end-to-end fixtures can live under `testdata/`.

## Build order (milestones)

1. ✅ **Parser** — parse `.evoke`, comments, blocks, `!`/`?` prefixes, source lines, good errors.
2. ✅ **Declaration registry + semantic validation** — the 12-declaration schema and per-file checks (unknown declaration, unsupported `!`/`?` prefix).
3. ✅ **Resolver** — channels, conflicts, accumulation, dedup, diagnostics → `Composition`.
4. ✅ **Tag-based selectors + SQLite index** — facet/tag matching, source root discovery, persistent file index.
5. ✅ **`generate` command** — compose files by selector/path/registry-ref and submit to ComfyUI.
6. ✅ **Registry API** — hosted push/pull/list with Google OIDC auth.
7. ✅ **Registry client integration** — `@namespace/name` references, local library cache, manifest tracking.
