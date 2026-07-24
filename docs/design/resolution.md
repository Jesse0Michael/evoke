---
title: Resolution Model
parent: Design
nav_order: 2
---

# Resolution Model
{: .no_toc }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

Resolution is the merge stage. It takes declarations from every selected file and produces a single `Composition` — the merged result that a generator consumes. The implementation lives in `pkg/evoke/merge.go`.

For the merge semantics themselves — singular conflicts, accumulation, dedup, defaults — see [Merge Modes](../file-format/merge-modes).

## The Composition

The resolver produces a `Composition` with typed fields for each declaration. Declarations that support a negative channel produce a `Prompt` struct with separate `Positive` and `Negative` slices:

```go
type Prompt struct {
    Positive []string
    Negative []string
}

type Composition struct {
    Name        string
    Character   []string
    Personality Prompt
    Backstory   []string
    Appearance  Prompt
    Apparel     Prompt
    Environment Prompt
    Scenario    string
    Prompt      Prompt
}
```

## The resolution algorithm

For each declaration and each channel (positive / negative):

1. Collect all contributions, separating explicit from default.
2. If any explicit contributions exist, ignore all defaults.
3. If no explicit contributions exist, use the defaults.
4. Apply the declaration's [merge mode](../file-format/merge-modes):
   - **Singular** — at most one value. Multiple explicit values are a conflict (warns, uses first).
   - **Accumulating** — combine all values in order, deduplicating exact normalized matches.
5. Positive and negative channels are resolved independently.

## Validation

Validation happens per-file before merging. Each file is checked against the built-in schema:

| Check | Description |
|:------|:------------|
| Unknown declaration | The declaration name isn't one of the nine built-ins (after migration alias resolution). |
| Unsupported `!` prefix | A `!` prefix on a declaration that doesn't support a negative channel. |
| Unsupported `?` prefix | A `?` prefix on a declaration that doesn't support defaults. |

A single file is allowed to be incomplete — it's only rejected for containing *illegal* declarations, never for being partial.
