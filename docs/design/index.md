---
title: Design
nav_order: 5
has_children: true
---

# Design

This section documents the reasoning, vocabulary, and architecture behind Evoke.

## Overview

Evoke is an experimental declarative file format, CLI, and registry for defining and composing AI characters and related generative assets.

The core idea: **prompts should be treated as compiled output rather than primary source material.**

Instead of maintaining one large image prompt or character card, you write small reusable `.evoke` files containing structured declarations — character traits, appearance, personality, backstory, apparel, environment, scenario, and prompt fragments. You declare `TAGS` blocks for discovery.

The CLI selects files by tag-based selectors, local paths, or registry references, merges their declarations according to declaration-specific rules (singular vs accumulating, defaults vs explicit, positive vs negative channels), and submits the resolved composition to a generation backend (currently ComfyUI for image generation).

## The pipeline

The format logic lives in `pkg/evoke/`:

```text
parse  →  schema lookup  →  validate  →  merge  →  generate
```

| Stage | Package | Description |
|:------|:--------|:------------|
| Parse `.evoke` into a Document | `pkg/evoke/parse.go` | Lexer + parser for `.evoke` syntax |
| Look up declaration definitions | `pkg/evoke/schema.go` | Built-in declaration registry, aliases |
| Per-file semantic validation | `pkg/evoke/validate.go` | Unknown declarations, unsupported prefix checks |
| Merge/resolve documents | `pkg/evoke/merge.go` | Channels, conflicts, defaults, accumulation, dedup → `Composition` |
| Select by tag | `pkg/evoke/selector.go` | Facet/tag matching, random selection |
| Generate output | `internal/generate/comfyui` | Convert composition to ComfyUI workflow |

## In this section

- **[Principles](principles)** — the non-obvious invariants: typeless files, external composition, no early concatenation.
- **[Resolution Model](resolution)** — the merge algorithm: singular vs accumulating, default suppression, conflict handling.
- **[Registry](registry)** — the hosted registry API for publishing and pulling `.evoke` artifacts.

## Framing

> A declarative, composable source format and CLI for AI characters, scenes, prompts, and generative assets.

Or, more simply:

> Evoke treats prompts as compiled artifacts.

The broader question it explores:

> Can generative AI assets be managed like source code — modular, composable, versioned, inspectable, shareable, and compiled into target-specific outputs?
