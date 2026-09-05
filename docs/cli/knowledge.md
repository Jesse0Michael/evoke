# evoke knowledge

Build the vector database that a [`KNOWLEDGE`](../file-format/declarations.md#knowledge) declaration points at. It walks a directory of markdown and `.evoke` files, splits every file into heading-scoped chunks, embeds each chunk through an ollama-compatible endpoint, and writes a SQLite database.

```console
$ evoke knowledge <dir>
```

The database is a build artifact, not source: rebuild it whenever the corpus changes.

## The embedding service

Embedding runs through an ollama-compatible `/api/embed` endpoint — `chat.embed_url` in [settings](settings.md), default `http://localhost:11434`. Evoke checks that endpoint before the build and, when nothing answers a **loopback** address, launches `ollama serve` itself, waits for the API, and stops it again when the build finishes. A server that was already running is used as it is and never stopped: an ollama request names its model, so an adopted server is indistinguishable from a started one. (This is why the rule differs from [`chat`](chat.md), where a busy port is an error — a language backend carries launch-time settings a request cannot.)

A non-loopback `chat.embed_url` is never launched. A remote endpoint that is down is an error, since starting a local server in its place would embed against something other than what was configured.

The model itself is not downloaded for you. When it is not present, the build stops and prints the command:

```console
$ evoke knowledge ./lore
evoke knowledge: embedding model "nomic-embed-text" not available at http://localhost:11434; pull it with:

    ollama pull nomic-embed-text
```

`evoke chat` does the same for retrieval, since every turn embeds the user's message — the endpoint has to stay up for the whole session, so a server Evoke starts lives as long as the chat does.

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
| `--verbose`, `-v` | `false` | Print per-file chunk counts. |

## Chunking

Each markdown file is split at heading boundaries, and every chunk carries its full heading path (`Flora > Ashroot`) so the vector captures where the text sits in the document. A section longer than `--max-tokens` is split further on paragraph boundaries, with `--overlap` tokens of the previous piece prepended to the next so meaning is not lost at the seam. Base64 image data and image markup are stripped before embedding.

## `.evoke` files

A `.evoke` file in the corpus is parsed and re-rendered as markdown before chunking, so what gets indexed is the file's *world facts* rather than its raw text. `NAME` becomes the `#` heading and each remaining declaration a `##` section, giving heading paths like `Sumi > Personality` — one chunk per aspect, so "how does she behave" retrieves separately from "who is she."

| Indexed | Dropped |
|:--------|:--------|
| `NAME` — the heading anchor | every `!` negative channel |
| `CHARACTER` | `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT` |
| `PERSONALITY` | `IMAGE`, `LORA`, `DETAILER`, `VOICE` |
| `BACKSTORY` | `CHAT`, `KNOWLEDGE`, `TAGS`, `SCENARIO` |

The dropped set is everything that is *input to a generator* rather than a statement about the world — tag soup, prompt weights, sampler settings, and runtime config. `VOICE` falls on the generator side of that line for the same reason `APPEARANCE` does: it describes a voice to be rendered, not something the character should recite as fact. `SCENARIO` is excluded because it is a transient situation rather than canon; embedding it would make a momentary scene setup permanently retrievable as fact.

Negatives are dropped rather than relabeled. `!PERSONALITY cruel` means "not this," and a retrieved chunk carries no frame that preserves the inversion — stored as-is it would feed the model the opposite of canon. (`evoke chat` can keep them because it relabels them "Traits to avoid:" for a live model.)

A `?` default is indexed when it is the effective value, which for a single file it always is.

Files that render to nothing contribute no chunks. That covers two cases: a file with no `NAME` (evoke files compose, but the builder walks them one at a time, so a nameless `winter-coat.evoke` fragment would be prose about nobody), and a file carrying only generator input. A file that fails to parse fails the build rather than being silently skipped.

Hidden files and directories and `node_modules` are always skipped. Everything else needs `--exclude`:

```console
$ evoke knowledge ./docs --exclude index.md --exclude 'Drafts/*'
```

Use `-v` to see what the chunking produced, per file:

```console
$ evoke knowledge ./docs -v
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
