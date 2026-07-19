---
title: evoke validate
parent: CLI
nav_order: 1
---

# evoke validate

Check one or more `.evoke` files for syntax and declaration errors.

```console
$ evoke validate <file.evoke>...
```

Each file is checked in two stages:

1. **Syntax** — the [parser](../file-format/syntax) turns the file into a document, reporting malformed headers, tab indentation, empty blocks, invalid UTF-8, and so on.
2. **Declaration semantics** — for a file that parses cleanly, each declaration is checked against the [built-in schema](../file-format/declarations): the declaration must exist, and any `!` or `?` prefix it carries must be supported.

A file with syntax errors is reported without running the semantic stage, since semantic checks assume a well-formed document.

This is **per-file** validation. A single file is allowed to be incomplete — it's only rejected for containing *illegal* declarations, never for being partial. Cross-file conflicts (two singular values, competing defaults) belong to the composition stage, which is part of the planned `compile` command.

## Output

A valid file prints one line to standard output:

```console
$ evoke validate sumi.evoke
sumi.evoke: valid
```

Every error is printed to standard error, prefixed with the file and carrying a line number. The parser accumulates all errors rather than stopping at the first, so a single run reports everything wrong with the file:

```console
$ evoke validate broken.evoke
broken.evoke: line 3: tabs are not allowed for indentation; use spaces
broken.evoke: line 8: unknown declaration "LOCATION"
broken.evoke: line 12: NAME does not support the ! (negative) prefix
```

## Semantic errors

Beyond the [syntax errors](../file-format/syntax#syntax-errors) the parser raises, validation adds:

| Message | Cause |
|:--------|:------|
| `unknown declaration "..."` | The declaration isn't one of the nine built-ins. |
| `NAME does not support the ! (negative) prefix` | A `!` prefix on a declaration without a negative channel. |
| `SCENARIO does not support the ? (default) prefix` | A `?` prefix on a declaration that doesn't allow defaults. |

## Exit code

Returns `0` when every file is valid. Returns `1` if any file is invalid or unreadable, and `2` if no files were given.
