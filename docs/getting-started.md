---
title: Getting Started
nav_order: 2
---

# Getting Started
{: .no_toc }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

## Build the CLI

Evoke is a single Go binary. From a clone of the repository:

```console
$ go build -o bin/evoke ./cmd/evoke
```

That produces `bin/evoke`. Everything below assumes it's on your path or that you call it as `./bin/evoke`.

Two commands are available today:

- `evoke validate <file.evoke>...` — check files for syntax and declaration errors
- `evoke declarations` — list the built-in declarations and their capabilities

See the [CLI reference](cli) for details.

## Write your first file

A `.evoke` file is a list of **declaration blocks**. A declaration name sits at the start of a line; its values are indented beneath it.

Create `sumi.evoke`:

```text
# Sumi — a small character file

NAME
    Sumi

IDENTITY
    adult student

PERSONALITY
    warm
    playful
    easily embarrassed

APPEARANCE
    small
    round
    violet skin
    glowing speckles
```

Validate it:

```console
$ evoke validate sumi.evoke
sumi.evoke: valid
```

A file does not have to be complete. A file with only an `APPAREL` block is just as valid as a full character — validation only rejects declarations that are *illegal*, never files that are *partial*.

## Compose files

The whole point of Evoke is that small files combine. You never reference one file from another; instead the caller lists the files to compose. Add an apparel file:

```text
# winter-coat.evoke
APPAREL
    heavy green winter coat
    black boots

!APPAREL
    sandals
    short sleeves
```

The `!` prefix routes those values to the **negative** channel (things to exclude — for an image target, a negative prompt). See [Prefixes & Channels](file-format/prefixes).

Composition — merging several files, resolving conflicts and defaults, and rendering a target — is the `evoke compile` command described in the [Design](design) section. It is planned but not yet implemented; `validate` and `declarations` are what run today.

## Learn the format

- [Syntax](file-format/syntax) — the exact line-oriented rules the parser enforces
- [Declarations](file-format/declarations) — the nine built-in declarations
- [Prefixes & Channels](file-format/prefixes) — what `!` and `?` mean
- [Merge Modes](file-format/merge-modes) — how repeated values combine
