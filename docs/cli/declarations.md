---
title: evoke declarations
parent: CLI
nav_order: 2
---

# evoke declarations

List the built-in declarations and their capabilities.

```console
$ evoke declarations
```

This is the authoritative, always-current view of which declarations exist and which prefixes each supports — the same schema the [validate](validate) command checks against. For prose descriptions of each declaration, see [Declarations](../file-format/declarations).

## Output

For each declaration, in canonical render order, it prints the merge mode and whether the `!` negative and `?` default prefixes are supported:

```console
$ evoke declarations
NAME
  merge:    singular
  negative: no
  default:  no

IDENTITY
  merge:    accumulating
  negative: no
  default:  no

PERSONALITY
  merge:    accumulating
  negative: yes
  default:  yes

...
```

- **merge** — `singular` or `accumulating`. See [Merge Modes](../file-format/merge-modes).
- **negative** — whether the `!` [negative channel](../file-format/prefixes#--the-negative-channel) is supported.
- **default** — whether the `?` [default prefix](../file-format/prefixes#--defaults) is supported.

## Exit code

Always returns `0`.
