---
title: Home
layout: home
nav_order: 1
---

# Evoke
{: .fs-9 }

A declarative, composable source format and CLI for AI characters, scenes, prompts, and generative assets.
{: .fs-6 .fw-300 }

[Get started](getting-started){: .btn .btn-primary .fs-5 .mb-4 .mb-md-0 .mr-2 }
[View on GitHub](https://github.com/jesse0michael/evoke){: .btn .fs-5 .mb-4 .mb-md-0 }

---

## Prompts are compiled artifacts, not source material

Instead of maintaining one large image prompt or character card, you author small reusable `.evoke` files of structured declarations. The CLI merges any selection of them — resolved by tag-based selectors, local paths, or registry references — and sends the composition through a generation pipeline.

```text
# sumi.evoke
TAGS
    character
    mascot

NAME
    Sumi

CHARACTER
    octopus humanoid mascot

APPEARANCE
    small round body
    smooth violet skin
    large luminous eyes

?APPAREL
    little teal explorer's vest
```

```text
# winter-coat.evoke
TAGS
    apparel
    winter

APPAREL
    heavy green winter coat
    black boots
```

```console
$ evoke generate character winter
```

The `generate` command resolves the selectors `character` and `winter` against your indexed `.evoke` files by tag, merges the matched documents, and submits the composition to ComfyUI. The default `?APPAREL` from the character steps aside for the explicit winter coat — because the `?` default is only used when nothing more specific is selected.

## What makes it different

- **Files are typeless.** A `.evoke` file never declares that it's a "character" or "location" file. Its meaning emerges from the declarations it contains.
- **Composition is external.** Files don't import each other — the caller picks which files compose together via selectors, paths, or registry references.
- **Nothing is concatenated early.** Merging works on structured declarations; flattening to a prompt string happens last.

Read the [Design](design) section for the full reasoning.

## Status
{: .d-inline-block }

Experimental
{: .label .label-yellow }

The parser, declaration schema, merge/resolver, tag-based selector system, local SQLite file index, registry client, and the `generate` pipeline are all implemented. The hosted registry API is functional.

## Where to go next

| Section | What's there |
|:--------|:-------------|
| [Getting Started](getting-started) | Build the CLI, write your first files, and run `generate` |
| [File Format](file-format) | Syntax, declarations, prefixes, merge modes |
| [CLI](cli) | Command reference for `generate`, `login`, `settings`, and `index` |
| [Design](design) | The project brief: principles, resolution model, and architecture |
