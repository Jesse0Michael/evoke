# Merge Modes

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
    violet skin, glowing speckles
```

```text
APPEARANCE
    green eyes
```

→ resolves to:

```text
APPEARANCE
    violet skin, glowing speckles, green eyes
```

Exact duplicates are removed after trimming surrounding whitespace. Deduplication is purely textual — `violet skin` and `  violet skin  ` collapse to one, but no *semantic* deduplication is attempted. `violet skin` and `purple skin` are kept as two distinct values; the compiler never tries to decide that two different phrasings mean the same thing.

Most declarations are accumulating: `CHARACTER`, `PERSONALITY`, `BACKSTORY`, `VOICE`, `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, and `PROMPT`.

## Structured: field-level overlay

`IMAGE`, `LORA`, `DETAILER`, `CHAT`, and `KNOWLEDGE` are singular **per argument**, but they do not follow the all-or-nothing rule above. Their values are `key = value` settings, and every block contributing to the same declaration and argument merges **setting by setting**:

A character file carries the canonical face detailer:

```text
?DETAILER face
    clear visible irises, defined iris ring
    detector = bbox/face_yolov8m.pt
    guide_size = 1024
    denoise = 0.3
    max_detection = 1
```

A two-subject shot file changes one setting:

```text
DETAILER face
    max_detection = 2
```

→ resolves to `max_detection = 2` with `detector`, `guide_size`, `denoise`, and the prompt text all inherited. **An explicit block only has to name the settings it changes.** This is what lets a shot or pipeline file tune a character's canonical configuration without restating it.

Settings resolve in two passes, and within each pass the **last file in composition order wins**:

1. every **default** block, in order;
2. every **explicit** block, in order, overriding what the defaults set.

So an explicit setting always outranks a default no matter which file came first, and between two blocks of the same kind the later one wins. Two files setting the same key is ordinary layering, not a conflict — it does not warn.

This is the ordinary `?` rule at setting granularity, not an exception to it: a default yields to an explicit contribution of [the same thing](prefixes.md#what-counts-as-the-same-thing), and for a `key = value` line "the same thing" is that key. A default the explicit block never mentioned was never contested, so it still applies.

This is the one place where the order of `evoke` arguments deliberately decides an outcome. It is safe here because the caller's order *is* the intent — `evoke image character shot+ff` means "this character, then this shot's adjustments" — and because a setting is a single field with no ambiguity, unlike two different `NAME`s. Singular and accumulating declarations keep their order-independent conflict semantics.

Three things do not follow the per-setting rule:

- **Prompt text is a channel, not a key.** Any explicit text suppresses *all* default text, and text accumulates across the blocks in the winning group, deduplicated exactly. Two explicit blocks that both add text both contribute. An explicit block that writes only settings contests no text, so the default's text survives.
- **`lora = ` keys accumulate** across every block instead of overriding.
- **The negative channel accumulates from everything.** `!DETAILER face` / `!IMAGE` text from every contribution — defaults included — is concatenated, and an explicit positive block never suppresses it.

`disabled` layers like any other setting, so a shot file can switch off a detailer or upscale pass a pipeline file supplied and a later argument can switch it back on. See [Disabling a stage](declarations.md#disabling-a-stage).

**Authoring consequence:** `?` marks a structured block as the canonical-but-yielding configuration — the one a more specific file is expected to tune, and whose prompt text steps aside entirely when another file writes its own. It is not *required* for a block to be overridable: a later explicit file can tune an earlier explicit one just as well. When two files disagree about a key, the answer is the argument order the caller typed.

## The resolution order, in one place

For each declaration and channel, resolution:

1. collects explicit contributions,
2. collects default contributions,
3. if any explicit contribution exists, ignores all defaults,
4. otherwise uses the defaults,
5. applies the merge mode above,
6. deduplicates exact normalized values where appropriate, and
7. reports singular conflicts (warns, uses first).

Steps 3 and 4 are the singular/accumulating path. Structured declarations take the layering path instead: their settings are not resolved as one atomic value, so defaults are merged *underneath* an explicit block rather than discarded, and two explicit blocks layer instead of conflicting.

The result is the `Composition` described in the [Design](../design/resolution.md) section.
