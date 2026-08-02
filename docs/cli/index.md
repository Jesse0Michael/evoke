---
title: CLI
nav_order: 4
has_children: true
---

# CLI

`evoke` is a single Go binary. Build it from a clone of the repository:

```console
$ go install ./cmd/evoke
```

## Commands

| Command | Description |
|:--------|:------------|
| [`evoke image`](image) | Compose files by selector, path, or registry reference and submit to a generation pipeline. |
| [`evoke chat`](chat) | Compose files into a character and start an interactive chat with a local LLM backend. |
| [`evoke knowledge`](knowledge) | Build a RAG vector database from a directory of markdown for `KNOWLEDGE` to reference. |
| [`evoke login`](login) | Sign in to the registry via Google OAuth. |
| [`evoke settings`](settings) | Manage user settings (source paths, registry URL). |
| `evoke view` | Interactive terminal image viewer with metadata display. |
| `evoke queue` | Display the ComfyUI generation queue. |
| `evoke clear` | Clear the ComfyUI generation queue. |
| `evoke completion` | Output shell completion script. |
| `evoke help` | Print usage. Also `-h`, `--help`. |

Running `evoke` with no command prints usage and exits non-zero.

## Exit codes

| Code | Meaning |
|:----:|:--------|
| `0` | Success. |
| `1` | A runtime error (file not found, parse error, generation failure). |
| `2` | Usage error — no command, unknown command, or missing arguments. |
