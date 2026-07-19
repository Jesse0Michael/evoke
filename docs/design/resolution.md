---
title: Resolution Model
parent: Design
nav_order: 2
---

# Resolution Model
{: .no_toc }

Planned
{: .label .label-yellow }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

Resolution is the stage between validated files and rendered output. It takes the declarations from every selected file and produces one **neutral, target-independent resolved document**. This is the compiler's center of gravity, and `--target resolved-json` is intended to be one of the first things implemented, because it makes parser and merge behavior observable.

For the merge semantics themselves — singular conflicts, accumulation, dedup, defaults — see [Merge Modes](../file-format/merge-modes).

## The neutral resolved representation

The resolver produces a document that retains provenance, so you can inspect where every value came from:

```text
violet skin
    from characters/sumi.evoke:18

green winter coat
    from apparel/winter-coat.evoke:4

pine forest
    from locations/pine-forest.evoke:3
```

Provenance matters for debugging unexpected prompts. The conceptual Go model:

```go
type SourceLocation struct {
    File   string
    Line   int
    Column int
}

type Channel string

const (
    ChannelPositive Channel = "positive"
    ChannelNegative Channel = "negative"
)

type Contribution struct {
    Declaration string
    Channel     Channel
    Default     bool
    Value       Value
    Source      SourceLocation
}

type ResolvedDeclaration struct {
    Name          string
    Positive      []ResolvedValue
    Negative      []ResolvedValue
    Contributions []Contribution
}

type ResolvedDocument struct {
    Declarations map[string]ResolvedDeclaration
    Diagnostics  []Diagnostic
}
```

{: .note }
> The `SourceLocation` / provenance model shown here is the *design target*, not the MVP. The current AST tracks only a bare line number for diagnostics — see [Principles → No provenance in the MVP](principles#no-provenance-in-the-mvp).

## The resolution algorithm

For each declaration and each channel:

1. Collect explicit contributions.
2. Collect default contributions.
3. Validate whether the declaration supports the channel and default behavior.
4. If explicit contributions exist, ignore the defaults.
5. If none exist, resolve the defaults.
6. Apply the declaration's [merge mode](../file-format/merge-modes).
7. Deduplicate exact normalized values where appropriate.
8. Preserve source provenance.
9. Report singular or structured conflicts.
10. Store the result in the neutral representation.

As pseudocode:

```go
func ResolveDeclaration(def DeclarationDefinition, contributions []Contribution) (ResolvedDeclaration, error) {
    positive, err := resolveChannel(def, ChannelPositive, contributions)
    if err != nil {
        return ResolvedDeclaration{}, err
    }
    negative, err := resolveChannel(def, ChannelNegative, contributions)
    if err != nil {
        return ResolvedDeclaration{}, err
    }
    return ResolvedDeclaration{
        Name:          def.Name,
        Positive:      positive,
        Negative:      negative,
        Contributions: contributions,
    }, nil
}

func resolveSingular(values []Contribution) ([]ResolvedValue, error) {
    if len(values) == 0 {
        return nil, nil
    }
    if len(values) > 1 {
        return nil, ErrConflict
    }
    return []ResolvedValue{resolveValue(values[0])}, nil
}

func resolveAccumulating(values []Contribution) []ResolvedValue {
    return deduplicatePreservingOrder(values)
}
```

## Validation layers

Validation happens in layers, and where each layer runs matters:

| Layer | Checks | Where |
|:------|:-------|:------|
| **Syntax** | Malformed names, missing values, bad indentation, invalid prefixes, invalid UTF-8. | Parser — ✅ today |
| **Declaration** | Declaration exists; prefix is supported; value matches its shape. | Validate — ✅ today |
| **File** | A single file may be incomplete; only reject *illegal* declarations, never *partial* files. | Validate — ✅ today |
| **Composition** | Multiple active singular values, conflicting structured fields, duplicate keys, multiple greetings. | Resolve — planned |
| **Target** | Requirements of a specific output (an image prompt needs a visual contribution; a card export needs a name). | Render — planned |

A source file must never be rejected merely because it can't independently satisfy a target. Target requirements apply to the *composed* result, not to each file.
