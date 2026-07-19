---
title: File Format
nav_order: 3
has_children: true
---

# File Format

The `.evoke` format is intentionally small and block-oriented. A file is a sequence of **declaration blocks**: a declaration name at the start of a line, followed by indented value lines.

```text
NAME
    Sumi

APPEARANCE
    small
    round
    violet skin
    glowing speckles
```

There is no file header, no type, and no import mechanism. A file's meaning emerges entirely from the declarations it contains — a file with identity and appearance blocks happens to describe a character; a file with only apparel blocks happens to describe an outfit. The compiler only interprets declarations.

The pages in this section cover:

- **[Syntax](syntax)** — the exact line-oriented rules the parser enforces, and every syntax error it can raise.
- **[Declarations](declarations)** — the nine built-in declarations, with their merge mode and which prefixes each supports.
- **[Prefixes & Channels](prefixes)** — what the `!` (negative) and `?` (default) prefixes select, and what they deliberately do *not* mean.
- **[Merge Modes](merge-modes)** — how repeated contributions to the same declaration combine.

{: .note }
> The MVP implements nine declarations, not the full vocabulary sketched in the [Design](../design) brief. Namespaced extension names like `FOO.BAR` are also out of scope for now — the parser rejects them.
