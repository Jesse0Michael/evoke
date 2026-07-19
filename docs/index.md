---
title: Home
layout: home
nav_order: 1
---

# Evoke
{: .fs-9 }

A declarative, composable source format and compiler for AI characters, scenes, prompts, and agent configurations.
{: .fs-6 .fw-300 }

[Get started](getting-started){: .btn .btn-primary .fs-5 .mb-4 .mb-md-0 .mr-2 }
[View on GitHub](https://github.com/jesse0michael/evoke){: .btn .fs-5 .mb-4 .mb-md-0 }

---

## Prompts are compiled artifacts, not source material

Instead of maintaining one large image prompt, system prompt, or character card, you author small reusable `.evoke` files of structured declarations. A compiler merges any selection of them into a neutral resolved representation, then renders that into a target format — an image prompt, an LLM system prompt, an agent package, a character card.

```text
# sumi.evoke
NAME
    Sumi

APPEARANCE
    small
    round
    violet skin
    glowing speckles

?APPAREL
    green shirt
    blue jeans
```

```text
# winter-coat.evoke
APPAREL
    heavy green winter coat
    black boots
```

```console
$ evoke validate sumi.evoke
sumi.evoke: valid
```

Compose the two on the command line and the default `green shirt / blue jeans` from the character steps aside for the explicit winter coat — because the `?` default is only used when nothing more specific is selected.

## What makes it different

- **Files are typeless.** A `.evoke` file never declares that it's a "character" or "location" file. Its meaning emerges from the declarations it contains.
- **Composition is external.** Files don't import each other — the caller picks which files compose together.
- **Nothing is concatenated early.** Merging works on structured declarations; flattening to a prompt string happens last, per target.

Read the [Design](design) section for the full reasoning.

## Status
{: .d-inline-block }

Experimental
{: .label .label-yellow }

Milestones 1–2 are done: the parser, the declaration schema, and per-file validation are implemented and wired into `evoke validate` and `evoke declarations`. The resolver, the rendering targets, and the registry described in the [Design](design) section are planned but not yet built.

Where a page documents something that exists today versus something planned, it says so.

## Where to go next

| Section | What's there |
|:--------|:-------------|
| [Getting Started](getting-started) | Build the CLI and write your first file |
| [File Format](file-format) | Syntax, declarations, prefixes, merge modes |
| [CLI](cli) | Command reference for `validate` and `declarations` |
| [Design](design) | The project brief: principles, resolution model, targets, roadmap |
