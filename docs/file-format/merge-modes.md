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

When several files (or several blocks) contribute to the same declaration, the declaration's **merge mode** decides how those contributions combine. Each channel — positive and negative — is resolved independently. Two modes are implemented today; a third, structured merging, is part of the design.

{: .note }
> Merging happens during composition — the planned `evoke compile` command. Today's `evoke validate` checks one file at a time and does not merge. This page describes the resolution semantics the compiler is being built to.

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

Most declarations are accumulating: `IDENTITY`, `PERSONALITY`, `BACKSTORY`, `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, and `PROMPT`.

## Structured
{: .d-inline-block }

Planned
{: .label .label-yellow }

Structured declarations would merge at the **field** level: values with different fields merge together, while conflicting values for the same singular field produce a conflict — the same conflict rule as singular declarations, applied per field.

```text
IDENTITY
    name "Sumi"
    occupation "student"
```

The MVP represents such data as accumulating lists or text blocks rather than typed fields, but the architecture deliberately avoids assuming every declaration is a flat string, so structured merging can be added without reworking the pipeline.

## The resolution order, in one place

For each declaration and channel, resolution:

1. collects explicit contributions,
2. collects default contributions,
3. checks the declaration actually supports that channel / default,
4. if any explicit contribution exists, ignores the defaults,
5. otherwise resolves the defaults,
6. applies the merge mode above,
7. deduplicates exact normalized values where appropriate, and
8. reports singular or structured conflicts.

The neutral result of all this is the *resolved document* described in the [Design](../design/resolution) section.
