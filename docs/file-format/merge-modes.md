---
title: Merge Modes
parent: File Format
nav_order: 4
---

# Merge Modes
{: .no_toc }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

When several files (or several blocks) contribute to the same declaration, the declaration's **merge mode** decides how those contributions combine. Each channel — positive and negative — is resolved independently.

## Singular

A singular declaration has at most one active explicit value.

- **One** explicit value → use it.
- **Zero** explicit values → use the default, if the declaration supports one.
- **More than one** explicit value → a **conflict**.

`NAME` and `SCENARIO` are singular. A default plus an explicit value resolves cleanly:

```text
?SCENARIO
    an ordinary afternoon

SCENARIO
    Ashley is checking the user's temperature after a fever.
```

→ resolves to *"Ashley is checking the user's temperature after a fever."*

But two conflicting explicit values are a conflict, not a race:

```text
SCENARIO
    a quiet morning at home

SCENARIO
    a tense hospital emergency
```

→ **conflict.** The compiler will not silently pick the first or the last. Conflicts like this are the reason file order is not allowed to silently change behavior.

## Accumulating

An accumulating declaration combines every contribution, in source order.

```text
APPEARANCE
    violet skin
    glowing speckles
```

```text
APPEARANCE
    green eyes
```

→ resolves to:

```text
violet skin
glowing speckles
green eyes
```

Exact duplicates are removed after trimming surrounding whitespace. Deduplication is purely textual — `violet skin` and `  violet skin  ` collapse to one, but no *semantic* deduplication is attempted. `violet skin` and `purple skin` are kept as two distinct values; the compiler never tries to decide that two different phrasings mean the same thing.

Most declarations are accumulating: `CHARACTER`, `PERSONALITY`, `BACKSTORY`, `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, and `PROMPT`.

## The resolution order, in one place

For each declaration and channel, resolution:

1. collects explicit contributions,
2. collects default contributions,
3. if any explicit contribution exists, ignores all defaults,
4. otherwise uses the defaults,
5. applies the merge mode above,
6. deduplicates exact normalized values where appropriate, and
7. reports singular conflicts (warns, uses first).

The result is the `Composition` described in the [Design](../design/resolution) section.
