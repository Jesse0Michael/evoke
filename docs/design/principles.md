---
title: Principles
parent: Design
nav_order: 1
---

# Core Principles
{: .no_toc }

These invariants shape almost every design decision. They're easy to violate by accident, so they're stated explicitly.

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

## Files do not declare a type

An Evoke file must never be required to identify itself as a character, apparel, location, scenario, or workflow file. There is no `TYPE CHARACTER`, no header, no schema selector.

A file's purpose **emerges** from the declarations it contains:

- a file with character, personality, and appearance declarations happens to represent a character;
- a file with only apparel declarations happens to represent an outfit;
- a file with environment declarations happens to represent a setting.

A file may mix any supported declarations. The compiler doesn't care what category a file "is" — it only interprets declarations.

{: .warning }
> Don't add a `TYPE`, `FROM`, or `IMPORT` mechanism. It would break the typeless model.

## Composition is controlled externally

Evoke files do not import or reference one another. There is no `FROM`, `IMPORT`, or `USE` declaration. The **caller** selects the files to compose:

```console
$ evoke generate character winter forest
```

File selection order must not silently determine behavior unless the specification explicitly defines a reason for order to matter. Two conflicting singular values are a [conflict](../file-format/merge-modes#singular), not a last-one-wins race.

## Declarations, not directives

The syntax may resemble a Dockerfile, but entries are **declarations**, not executable instructions.

```text
PERSONALITY
    warm
    mischievous
```

This declares traits; it does not execute an operation. Internally the code may call these nodes or statements, but project-facing language prefers **declaration**.

## Prompts are compiled artifacts

The compiler must not immediately concatenate every value into one string. It first produces a **neutral resolved representation**, which can then be rendered differently per target. The same appearance data renders as:

**Image prompt**

```text
small, round, violet skin, glowing speckles
```

**LLM system context**

```text
Sumi is small and round, with violet skin and glowing speckles.
```

**Character-card JSON**

```json
{ "appearance": ["small", "round", "violet skin", "glowing speckles"] }
```

So the merger operates on structured declarations, never on final prompt strings. Flattening is a rendering concern that happens last.
