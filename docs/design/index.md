---
title: Design
nav_order: 5
has_children: true
---

# Design

This section is the Evoke project brief — the reasoning, vocabulary, and long-range plan behind the format. It's the design source of truth. Where the [File Format](../file-format) and [CLI](../cli) sections document what exists today, this section documents the whole idea, including the parts not yet built.

{: .note }
> Much of what follows describes intended behavior and future milestones. The MVP implements the [parser](../file-format/syntax), the [nine-declaration schema](../file-format/declarations), and [per-file validation](../cli/validate). The resolver, the rendering targets, and the registry are designed here but not yet implemented.

## Overview

Evoke is an experimental declarative file format, CLI, and registry for defining and composing AI characters and related generative assets.

The core idea: **prompts should be treated as compiled output rather than primary source material.**

Instead of maintaining one large image prompt, system prompt, or character card, you write small reusable `.evoke` files containing structured declarations — character identity, appearance, personality, backstory, apparel, environment, scenario, behavioral instructions, positive and negative prompt fragments, example dialogue, greetings.

A tool selects any number of Evoke files, merges their declarations according to declaration-specific rules, produces a neutral resolved representation, and renders that representation into a target format. Possible targets include:

- image-generation positive and negative prompts
- LLM system prompts
- complete LLM request payloads
- character-card JSON (including Character Card V2 / V3)
- application-specific configuration
- human-readable resolved documents

Evoke isn't trying to replace YAML, JSON, Character Cards, or workflow formats. Its value should come from **defined composition semantics, reusable components, validation, provenance, and target-specific compilation.**

## The pipeline

The compiler is a staged pipeline. Each stage is one package under `internal/`:

```text
parse (ast)  →  schema lookup  →  validate  →  resolve  →  render(target)
```

| Stage | Package | Status |
|:------|:--------|:-------|
| Parse `.evoke` into an AST | `internal/parser`, `internal/ast` | ✅ Implemented |
| Look up declaration definitions | `internal/schema` | ✅ Implemented |
| Per-file semantic validation | `internal/validate` | ✅ Implemented |
| Resolve channels, conflicts, defaults | `internal/resolve` | Planned |
| Render to a target | `internal/render` | Planned |
| Registry reference resolution | `internal/registry` | Planned |

## In this section

- **[Principles](principles)** — the non-obvious invariants: typeless files, external composition, no early concatenation, no provenance in the MVP.
- **[Resolution Model](resolution)** — the neutral resolved representation and the resolution algorithm.
- **[Targets](targets)** — the rendering targets the resolver feeds: `prompt`, `system-prompt`, `agent-json`, `resolved-json`, `explain`.
- **[CLI Vision](cli-vision)** — the full command surface, including `compile`, `explain`, and `fmt`.
- **[Registry & Roadmap](registry)** — the future distribution system, out-of-scope areas, and the milestone plan.

## Framing

> A declarative, composable source format and compiler for AI characters, scenes, prompts, and agent configurations.

Or, more simply:

> Evoke treats prompts as compiled artifacts.

The broader question it explores:

> Can generative AI assets be managed like source code — modular, composable, versioned, inspectable, shareable, and compiled into target-specific outputs?

The initial goal is not to prove a custom syntax beats YAML or JSON. It's to explore whether a domain-specific composition model is useful enough to justify a dedicated source format and toolchain.
