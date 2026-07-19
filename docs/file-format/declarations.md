---
title: Declarations
parent: File Format
nav_order: 2
---

# Declarations
{: .no_toc }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

Every declaration has a registered definition that fixes its **merge mode**, whether it supports the **`!` negative channel**, whether it supports the **`?` default**, and its canonical **render order**. The nine declarations below are the MVP set. You can list them from the CLI at any time with [`evoke declarations`](../cli/declarations).

## The built-in declarations

| Declaration | Merge | `!` negative | `?` default | Order |
|:------------|:------|:------------:|:-----------:|:-----:|
| `NAME`        | singular      | — | — | 10 |
| `IDENTITY`    | accumulating  | — | — | 20 |
| `PERSONALITY` | accumulating  | ✓ | ✓ | 30 |
| `BACKSTORY`   | accumulating  | — | — | 40 |
| `APPEARANCE`  | accumulating  | ✓ | ✓ | 50 |
| `APPAREL`     | accumulating  | ✓ | ✓ | 60 |
| `ENVIRONMENT` | accumulating  | ✓ | ✓ | 70 |
| `SCENARIO`    | singular      | — | ✓ | 80 |
| `PROMPT`      | accumulating  | ✓ | ✓ | 90 |

- **Merge** — how repeated contributions combine. See [Merge Modes](merge-modes).
- **`!` negative** — whether values may be routed to the exclusion channel. See [Prefixes & Channels](prefixes).
- **`?` default** — whether values may be marked as defaults used only when nothing more specific exists.
- **Order** — the canonical, ascending order renderers use so output is deterministic rather than dependent on file order.

## Reference

### NAME
{: .no_toc }

The human-facing character or entity name. **Singular** — a single explicit value. No negative channel (a name has no meaningful "negative prompt"), no default.

```text
NAME
    Ashley
```

### IDENTITY
{: .no_toc }

A stable factual description of who the character is. **Accumulating**, positive only.

```text
IDENTITY
    adult emergency-room nurse
    grew up in Wisconsin
```

### PERSONALITY
{: .no_toc }

Behavioral tendencies and traits. **Accumulating**; supports the negative channel and defaults.

```text
PERSONALITY
    warm
    competent
    easily flustered by direct flirting

!PERSONALITY
    cruel
    emotionally detached
```

### BACKSTORY
{: .no_toc }

Canonical background information. **Accumulating**, positive only.

```text
BACKSTORY
    Trained in Milwaukee, moved to Chicago for her first hospital job.
```

### APPEARANCE
{: .no_toc }

General visible physical traits. **Accumulating**; supports the negative channel and defaults.

```text
APPEARANCE
    small
    round
    violet skin
    glowing speckles

!APPEARANCE
    scary
    slimy
    monstrous
```

### APPAREL
{: .no_toc }

Clothing and accessories. **Accumulating**; supports the negative channel and defaults. This is deliberately broad for the MVP — narrower declarations like `OUTFIT` or `FOOTWEAR` may come later.

```text
?APPAREL
    green shirt
    blue jeans

APPAREL
    heavy green winter coat
    black boots
```

The `?` default apparel is used only when no explicit `APPAREL` appears in the composition.

### ENVIRONMENT
{: .no_toc }

Scene and setting details. **Accumulating**; supports the negative channel and defaults. In the MVP, `ENVIRONMENT` carries the entire scene/setting role — there is no separate `LOCATION` declaration yet.

```text
ENVIRONMENT
    pine forest
    tall evergreen trees
    soft morning mist
```

### SCENARIO
{: .no_toc }

The current narrative or conversational situation. **Singular** (one active value); supports defaults, positive only.

```text
SCENARIO
    Ashley is checking the user's temperature after they arrived with a fever.
```

### PROMPT
{: .no_toc }

Direct prompt material for when no more specific declaration fits — an escape hatch, not the preferred representation. **Accumulating**; supports the negative channel and defaults.

```text
PROMPT
    cinematic portrait composition

!PROMPT
    blurry
    deformed hands
```

## What isn't here yet

The [Design](../design) brief sketches a larger vocabulary — `SPEECH`, `POSE`, `STYLE`, `LOCATION`, `LIGHTING`, `OBJECTIVE`, `RULE`, `BOUNDARY`, `GREETING`, `EXAMPLE-DIALOGUE`, and more. Those are intentionally **not** implemented in the MVP. Using any of them today is an *unknown declaration* error.
