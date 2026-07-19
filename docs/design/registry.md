---
title: Registry & Roadmap
parent: Design
nav_order: 5
---

# Registry & Roadmap
{: .no_toc }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

## The registry
{: .d-inline-block }

Planned
{: .label .label-yellow }

The registry is a future distribution system for Evoke files. Like the format itself, it does not require files to declare a category or type. The registry may attach optional **metadata for discovery** — title, description, tags, author, license, version, content hash, maturity rating, preview image, dates — but that is registry metadata, not part of the core language. A registry entry that humans call "a character" or "an outfit" is, to the compiler, just an Evoke document.

### References

```text
namespace/name@version
```

```text
characters/sumi@3
apparel/winter-coat@1
locations/pine-forest@2
scenarios/job-interview@4
```

The path hierarchy is organizational only — it does not define file semantics. A registry-aware compile would look like:

```console
$ evoke compile characters/sumi@3 apparel/winter-coat@1 locations/pine-forest@2 --target prompt
```

The client downloads immutable versions, verifies hashes, parses the files, and hands them to the same local compiler.

### Versioning and reproducibility

Registry versions are immutable — `characters/sumi@3` always resolves to the same content. Floating references (`characters/sumi`) are allowed, but resolved builds should record the exact version and content hash so a generation can be reproduced later:

```json
{
  "sources": [
    { "reference": "characters/sumi@3", "sha256": "..." },
    { "reference": "apparel/winter-coat@1",   "sha256": "..." }
  ]
}
```

### Licensing

The registry supports license metadata (`license`, `author`, `source`, `attribution`, `redistribution_allowed`, `commercial_use_allowed`) but does not try to solve IP law inside the file format. The goals: original assets are easy to publish, authors can state terms, uploads have an identified publisher, immutable versions retain attribution, and the service can respond to takedowns. The core `.evoke` format stays content-neutral.

### The interface

The first implementation defines interfaces and a **local filesystem registry**, not a hosted service:

```go
type Registry interface {
    Resolve(ctx context.Context, ref Reference) (Artifact, error)
    Search(ctx context.Context, query string) ([]SearchResult, error)
}
```

```text
~/.evoke/registry/
    characters/
    apparel/
    locations/
    scenarios/
```

The directory names are organizational only.

## Explicitly out of scope

Two areas are interesting but deliberately **not** part of the initial project:

- **Workflow topology.** Checkpoints, samplers, detailer passes, upscaling, ControlNet, IP-Adapter — this is a graph-and-execution problem, not a character-data merge problem. Evoke should not try to recreate ComfyUI. Workflow support stays an extension point (registry presets, backend-specific extension declarations, adapters that inject resolved prompts into existing workflows), not a prerequisite.
- **Runtime agent memory.** Canonical background may be authored via `BACKSTORY` / `MEMORY` declarations, but session history, automatic memory extraction, vector retrieval, expiration, and per-user long-term memory belong to an external runtime. Evoke focuses on authored, versioned source data — not a memory database.

## Roadmap

The build order, milestone by milestone:

| # | Milestone | Delivers | Status |
|:-:|:----------|:---------|:-------|
| 1 | **Parser** | Parse `.evoke` — comments, blocks, `!`/`?` prefixes, source lines, good errors → `evoke validate`. | ✅ Done |
| 2 | **Declaration registry + validation** | The nine-declaration schema and per-file checks → `evoke declarations`, `evoke validate`. | ✅ Done |
| 3 | **Resolver** | Channels, conflicts, accumulation, dedup, diagnostics. | Next |
| 4 | **Debug output** | `evoke compile --target resolved-json`, `evoke explain`. | Planned |
| 5 | **`prompt` renderer** | Deterministic positive/negative image prompt. | Planned |
| 6 | **`system-prompt` renderer** | Coherent LLM system prompt from agent declarations. | Planned |
| 7 | **Registry interfaces** | Local filesystem registry behind the `Registry` interface. | Planned |

`compile` (produces output only) stays distinct from any future `generate` / `chat` / `run` commands that call external backends. Workflow topology and runtime memory are out of scope for the initial project.
