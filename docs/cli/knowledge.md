---
title: evoke knowledge
parent: CLI
nav_order: 3
---

# evoke knowledge

Build the vector database that a [`KNOWLEDGE`](../file-format/declarations#knowledge) declaration points at. It walks a directory of markdown, splits every file into heading-scoped chunks, embeds each chunk through an ollama-compatible endpoint, and writes a SQLite database.

```console
$ evoke knowledge <dir>
```

The database is a build artifact, not source: rebuild it whenever the corpus changes.

## Where the database goes

The database is written to the working directory as `knowledge.db`, and `--output` is an ordinary path — relative or absolute, exactly like any other CLI:

```console
$ evoke knowledge ./lore
Wrote /Users/you/lore/knowledge.db
  42 files, 318 chunks, nomic-embed-text (768 dimensions)

Reference it from a .evoke file:

    KNOWLEDGE knowledge db=knowledge.db

To make it resolvable by chat:

    evoke settings set chat.model_path /Users/you/lore
```

That last line appears only when the database is not already under a `chat.model_paths` directory. `chat` looks a `db=` up **by file name** across those directories (recursively), the same way it resolves a `CHAT` model — but `chat.model_paths` is a *search* path, frequently a directory some other application owns, so `knowledge` never writes into it. Keep the database next to the corpus it was built from and add that directory to the search path once.

The build writes to a temporary file and renames it into place only on success, so a rebuild that fails partway — an unreachable embedding endpoint, an interrupt — leaves the existing database intact.

## Flags

| Flag | Default | Description |
|:-----|:--------|:------------|
| `--output`, `-o` | `./knowledge.db` | Database to write, as an ordinary relative or absolute path. |
| `--model` | `nomic-embed-text` | Embedding model. Recorded in the database. |
| `--url` | `chat.embed_url`, else `http://localhost:11434` | Ollama-compatible API base URL. |
| `--max-tokens` | `1000` | Target maximum tokens per chunk. |
| `--overlap` | `100` | Token overlap carried across a split section. |
| `--exclude` | — | Glob of files to skip, matched on relative path or base name. Repeatable. |
| `--dry-run` | `false` | Chunk and report counts without embedding or writing. |
| `--verbose`, `-v` | `false` | Print per-file chunk counts. |

## Chunking

Each markdown file is split at heading boundaries, and every chunk carries its full heading path (`Flora > Ashroot`) so the vector captures where the text sits in the document. A section longer than `--max-tokens` is split further on paragraph boundaries, with `--overlap` tokens of the previous piece prepended to the next so meaning is not lost at the seam. Base64 image data and image markup are stripped before embedding.

Hidden files and directories and `node_modules` are always skipped. Everything else needs `--exclude`:

```console
$ evoke knowledge ./docs --exclude index.md --exclude 'Drafts/*'
```

Use `--dry-run` to check what the chunking produces before spending time on embeddings:

```console
$ evoke knowledge ./docs --dry-run -v
```

## The embedding model must match

The database records the model that produced its vectors. Querying it with a different model does not fail at the storage layer — it silently returns meaningless similarity scores — so `chat` refuses to open a database whose recorded model conflicts with an explicit `embed_model` setting on the declaration.

Leave `embed_model` off the declaration and chat adopts whatever the database was built with, which is almost always what you want:

```text
KNOWLEDGE lore
    db = lore.db
    top_k = 5
```

Rebuild with a different `--model` and the declaration keeps working with no edit.
