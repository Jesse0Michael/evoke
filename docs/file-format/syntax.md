---
title: Syntax
parent: File Format
nav_order: 1
---

# Syntax
{: .no_toc }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

Evoke syntax is line-oriented and block-structured. The parser scans the source line by line — there is no separate token stream — and enforces only syntax. Whether a declaration actually *exists* and whether its prefix is *allowed* are separate, declaration-level checks handled during validation.

## Anatomy of a block

```text
?APPAREL
    green shirt
    blue jeans
```

- **`?`** — an optional [prefix](prefixes) (`!`, `?`, or `?!`).
- **`APPAREL`** — the declaration name, at column 1 (no indentation).
- **`green shirt` / `blue jeans`** — indented value lines belonging to the header above them.

## The rules

### Declaration headers start at column 1

A header is an optional prefix immediately followed by a name, with no leading whitespace. A new header ends the previous block.

Names are made of letters, digits, underscores, and hyphens. Names are **case-insensitive** and normalized to uppercase internally, but the canonical style is uppercase. Dotted / namespaced names (`COMFYUI.FACE_DETAILER`) are reserved for future extensions and are rejected today.

```text
NAME          # canonical
name          # accepted, same as NAME
Name          # accepted, same as NAME
```

### Value lines are indented, with spaces

Any indented, non-blank line attaches to the most recent header. **Indent with spaces only** — a tab in the indentation is an error. The amount of indentation is not significant, only its presence.

Values preserve their internal punctuation and spacing; only trailing whitespace is trimmed.

### Blank lines separate blocks visually but don't terminate them

A value block continues until the next header. Blank lines inside a block are ignored — they're for readability and don't end the block.

### Comments start with `#`

Any line whose first non-whitespace character is `#` is a comment, at any indentation. There are no inline comments — `#` only starts a comment at the beginning of the (trimmed) line.

```text
# a top-level comment
PERSONALITY
    warm
    # this line is a comment, not a value
    playful
```

### Files must be UTF-8

Invalid UTF-8 is rejected outright.

### Empty blocks are invalid

A header with no value lines is an error — every declaration must contribute at least one value.

## Grammar sketch

An informal grammar for the current syntax:

```text
document      = { blank | comment | declaration } ;
declaration   = header newline value_line { value_line | blank } ;
header        = [ prefix ] name ;
prefix        = "!" | "?" | "?!" ;
name          = ( letter | digit | "_" | "-" )+ ;
value_line    = indentation text newline ;
comment       = { space } "#" text newline ;
```

## Syntax errors

The parser accumulates every error it finds rather than stopping at the first, and reports each with a line number. The errors it can raise:

| Message | Cause |
|:--------|:------|
| `file is not valid UTF-8` | The file contains invalid UTF-8. |
| `tabs are not allowed for indentation; use spaces` | A value line is indented with a tab. |
| `declaration is missing a name` | A prefix with no name after it. |
| `invalid prefix; only "!", "?", and "?!" are allowed before a declaration name` | An unexpected leading operator, e.g. `!?NAME`. |
| `unexpected text "..." after declaration name; values belong on indented lines` | Extra text on the header line, e.g. `NAME Sumi`. |
| `invalid declaration name "..."` | A name with characters outside `A–Z a–z 0–9 _ -`. |
| `indented value has no preceding declaration` | A value line before any header. |
| `declaration "..." has no values` | A header with no indented values (an empty block). |

{: .note }
> These are **syntax** errors. Errors like *unknown declaration* or *unsupported prefix* come from the validation stage, which runs after a file parses cleanly.
