---
title: Prefixes & Channels
parent: File Format
nav_order: 3
---

# Prefixes & Channels
{: .no_toc }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

A declaration name may carry a prefix that selects a **channel** or marks a value as a **default**. A prefix is not an operation — it never means delete, override, or disable.

| Prefix | Name | Meaning |
|:-------|:-----|:--------|
| *(none)* | positive | An explicit positive contribution. |
| `!` | negative | Contributes to the negative / exclusion channel. |
| `?` | default | Used only when no explicit contribution exists for the same declaration and channel. |
| `?!` | default negative | A default contribution into the negative channel. Reserved; deferred until a real use case appears. |

Only declarations whose [definition](declarations) supports a given prefix may use it. Using an unsupported prefix is a validation error.

## No prefix — positive contribution

The ordinary case: an explicit, positive value.

```text
APPAREL
    black dress
```

How multiple positive values combine depends on the declaration's [merge mode](merge-modes).

## `!` — the negative channel

The `!` prefix routes values into a separate **negative** channel — the "avoid this" side of a declaration.

```text
!APPAREL
    sneakers
    jeans
```

Different targets interpret the negative channel differently: an image target turns it into a negative prompt; a language target might turn it into natural-language avoidance instructions.

What `!` is **not**:

- It does **not** delete or remove a positive value.
- It does **not** override or disable anything.
- It does **not** negate a boolean setting.

It simply selects the negative channel, and only for declarations that support one. `!NAME` is a validation error — a name has no meaningful negative channel.

The positive and negative channels are resolved independently. A declaration can carry both:

```text
APPEARANCE
    small
    violet skin

!APPEARANCE
    scary
    monstrous
```

## `?` — defaults

The `?` prefix marks a **default**: a value used only when no explicit contribution exists for the same declaration and channel. This gives source content canonical fallbacks without needing a replacement operator.

```text
# sumi.evoke — the character's canonical outfit
?APPAREL
    green shirt
    blue jeans
```

```text
# winter-coat.evoke — an explicit choice
APPAREL
    heavy green winter coat
    black boots
```

Compose both and the result uses the winter coat. The moment *any* explicit `APPAREL` contribution appears, every `?APPAREL` default is suppressed. With no explicit apparel anywhere, the default green-shirt outfit is used instead.

Think of a default as a stable property of the content: *"use this when nothing more specific has been selected."*

## `?!` — default negative

The combination — a default contribution into the negative channel — is part of the design but deliberately postponed until a concrete use case justifies it. It is not needed for the current milestones.

## Why there is no `=` / force operator

Version one has no replace, force, or override operator. If canonical values use `?`, an ordinary explicit declaration already suppresses them — so a force operator isn't needed to "win" over a default. And two *conflicting* explicit singular values are treated as a [conflict](merge-modes#singular), never silently resolved by file order. A force operator may be reconsidered later only if a real use case demands it.
