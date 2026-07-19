---
title: CLI
nav_order: 4
has_children: true
---

# CLI

`evoke` is a single Go binary. Build it from a clone of the repository:

```console
$ go build -o bin/evoke ./cmd/evoke
```

## Commands available today

| Command | Description |
|:--------|:------------|
| [`evoke validate`](validate) | Check one or more files for syntax and declaration errors. |
| [`evoke declarations`](declarations) | List the built-in declarations and their capabilities. |
| `evoke help` | Print usage. Also `-h`, `--help`. |

Running `evoke` with no command prints usage and exits non-zero.

## Exit codes

| Code | Meaning |
|:----:|:--------|
| `0` | Success. |
| `1` | A file was invalid or could not be read. |
| `2` | Usage error — no command, unknown command, or no files given. |

## Planned commands
{: .d-inline-block }

Planned
{: .label .label-yellow }

The [Design](../design/cli-vision) section describes the commands the compiler is being built toward — `compile` (merge and render to a target), `explain` (show how values resolved and where they came from), and `fmt` (canonical formatting) — as well as the future registry commands. They are not implemented yet.
