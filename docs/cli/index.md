# CLI

`evoke` is a single Go binary. Build it from a clone of the repository:

```console
$ go install ./cmd/evoke
```

## Commands

| Command | Description |
|:--------|:------------|
| [`evoke image`](image.md) | Compose files by selector, path, or registry reference and submit to a generation pipeline. |
| [`evoke edit`](edit.md) | Redraw an existing image through a composition, resolving inputs exactly as `evoke image` does. |
| [`evoke paint`](paint.md) | Alter an existing image by instruction, through an instruction-edit model. |
| [`evoke chat`](chat.md) | Compose files into a character and start an interactive chat with a local LLM backend. |
| [`evoke knowledge`](knowledge.md) | Build a RAG vector database from a directory of markdown and `.evoke` files for `KNOWLEDGE` to reference. |
| `evoke login` | Sign in to the registry via Google OAuth. |
| [`evoke settings`](settings.md) | Manage user settings (source paths, registry URL). |
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
