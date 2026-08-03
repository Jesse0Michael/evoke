# File Format

The `.evoke` format is intentionally small and block-oriented. A file is a sequence of **declaration blocks**: a declaration name at the start of a line, followed by indented value lines. Files also include a `TAGS` metadata block for discovery by the selector system.

```text
TAGS
    character
    mascot

NAME
    Sumi

CHARACTER
    an octopus humanoid, and the mascot of this project

APPEARANCE
    octopus humanoid
    small round body
    smooth violet skin
    large luminous eyes
```

There is no file header, no type, and no import mechanism. A file's meaning emerges entirely from the declarations it contains — a file with character and appearance blocks happens to describe a character; a file with only apparel blocks happens to describe an outfit. The CLI only interprets declarations.

The pages in this section cover:

- **[Syntax](syntax.md)** — the exact line-oriented rules the parser enforces, and every syntax error it can raise.
- **[Declarations](declarations.md)** — the fourteen built-in declarations, with their merge mode, which prefixes each supports, and which command consumes each.
- **[Prefixes & Channels](prefixes.md)** — what the `!` (negative) and `?` (default) prefixes select, and what they deliberately do *not* mean.
- **[Merge Modes](merge-modes.md)** — how repeated contributions to the same declaration combine.
