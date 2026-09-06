# evoke chat

Compose `.evoke` files into a character and start an interactive conversation with a local LLM backend. Chat is a second output target built from the same resolved Evoke data as [`image`](image.md): the parser, selectors, and merge semantics are identical — only the compilation target differs.

```console
$ evoke chat <input>...
```

Inputs are classified and resolved exactly as in [`evoke image`](image.md#input-types) (selectors, local paths, `@namespace/name` registry references, literal prompts). The merged composition's persistent character declarations become the system prompt; the `CHAT` declaration selects the model, sampling, and conversation policy.

The shipped [`examples/chat.evoke`](https://github.com/jesse0michael/evoke/blob/main/examples/chat.evoke) is a self-contained basic assistant — start there:

```console
$ evoke chat chat
```

Because Evoke composes files rather than referencing them, a `CHAT` pipeline can be reused across characters and scenes. A pipeline you author (say `roleplay`) supplies only the model and sampling, while character and scene files supply the rest:

```console
$ evoke chat roleplay yasmin beach
```

- `roleplay` carries the `CHAT` declaration (model, context size, sampling).
- `yasmin` supplies identity, personality, and backstory.
- `beach` supplies the starting `SCENARIO` (its `ENVIRONMENT`/`APPAREL` are for image generation and are ignored by chat).

## Backends

Two runtimes are supported, selected by the `backend` setting on the `CHAT` declaration:

| `backend` | binary | model reference |
| --- | --- | --- |
| `llama.cpp` (default) | `llama-server` | a GGUF file name |
| `mlx` | `mlx_lm.server` | a model directory or a Hugging Face repo id |

The binary must be installed and on your `PATH`, or named by `chat.executable` in settings.

### Evoke starts the backend, or connects to a matching one

When the configured endpoint is free, Evoke **starts** the backend for the session: it launches the binary as a child process with the resolved model and runtime settings, waits for it to report healthy, talks to it, and **shuts it down when the session ends** — on interrupt, EOF, or error. It never leaves a process it started running.

When something is already listening, Evoke asks it what it loaded rather than refusing outright. Both runtimes serve several conversations at once — `llama-server` runs 4 slots by default over a unified KV cache, and `mlx_lm.server` batches concurrent requests — so a second terminal against the same model needs no second copy of the weights in memory.

**A backend serving the same model is used as is**, and Evoke does not shut it down on exit, because it did not start it:

```console
$ evoke chat roleplay yasmin
Using the llama.cpp backend already running at 127.0.0.1:8080 (mistral-nemo)
Character: Yasmin
Backend:   llama.cpp (already running)
```

**Anything else is your decision.** Evoke prints what does not match and asks:

```console
A llama.cpp backend is already running at 127.0.0.1:8080 but does not match this chat:
  - it has /models/other.gguf loaded
llama-server serves the model it was launched with and ignores the model named in a request, so the replies would come from that model rather than this character's.
Connect anyway? [y/N]:
```

The default is no. **A non-interactive run is never asked** — it gets the same refusal with the reason included, so a script can neither hang on a prompt nor silently talk to the wrong model.

Two things are checked, and what a mismatch costs differs by runtime:

| check | how it is known | what connecting anyway does |
|:--|:--|:--|
| model | llama.cpp: `model_path` from `/props`. mlx: its `--model`, which `/v1/models` appends only when that was a local path — a server started from a Hugging Face repo id reports nothing, and Evoke says so rather than guessing. | **llama.cpp:** it serves the model it launched with and *ignores* the model named in a request, so you would silently be talking to that one. **mlx:** it loads the model named in each request, so yours would load and evict the resident one. |
| context | llama.cpp: `default_generation_settings.n_ctx` from `/props`. mlx: nothing to check — MLX takes no context cap. | Evoke's history budget would promise a window the backend does not have, and the backend would discard turns the sliding window still counts. |

If what holds the port is not that backend at all, it is still a plain error — the mlx probe rejects a `llama-server` even though both answer `/v1/models`.

Starting the process is still what makes a composition reproducible when Evoke does start it: every setting the `CHAT` declaration names is true of the running backend by construction, including the launch-time ones a request cannot carry (`context_window` → `--ctx-size`, `gpu_layers` → `-ngl` on llama.cpp). Connecting to a server it did not start trades that guarantee for what the backend will report about itself — which is why the model and context are verified, and why anything unverifiable is a question rather than an assumption.

### MLX notes

`mlx_lm.server` has no context-size or GPU-offload flag — MLX uses unified memory and takes no context cap. So `gpu_layers` is reported as having no effect, and `context_window` remains a purely Evoke-side history budget. This is also why mlx is the runtime with the least at stake in connecting to a running server: its launch arguments are only `--model`, `--host` and `--port`, and every sampling setting (`temperature`, `top_p`, `max_output_tokens`, `repeat_penalty`, `thinking`) travels in each request. Two chats with entirely different sampling profiles share one server correctly.

Its `/health` also answers `200` immediately, before any model is loaded. An MLX backend is therefore "ready" as soon as the port serves, and the model load lands on your first turn.

`mlx_lm.server` defaults `temperature` to **0.0** where `llama-server` defaults to 0.8, so a `CHAT` declaration that sets no `temperature` samples greedily on MLX and not on llama.cpp. Evoke sends nothing when the setting is absent — each backend applies its own default — so set `temperature` explicitly on any character meant to read the same on both.

### Knowledge retrieval needs an embedding server

A `KNOWLEDGE` declaration embeds your message on every turn, so an ollama-compatible endpoint has to be up for the whole session. Evoke checks `chat.embed_url` when it opens the knowledge bases and starts `ollama serve` if nothing answers a loopback address, keeping it alive until the chat ends and stopping it only if it started it. The embedding model must already be pulled; the error names the command when it is not. See [`evoke knowledge`](knowledge.md#the-embedding-service).

### Reasoning models

A reasoning model emits its chain of thought on a separate channel and only then answers. With a small `max_output_tokens` it can spend the entire budget thinking and return **no content at all**, so set `thinking` on the `CHAT` declaration:

```text
CHAT
    backend = mlx
    model = mlx-community/Qwen3.5-9B-4bit
    thinking = off
```

`thinking` travels in each request rather than on the launch command, so it is a per-character setting. Left unset, the backend's own default applies. When a reply arrives with no content, Evoke reports why rather than printing a blank turn.

## Prompt compilation

The compiler renders resolved declarations into a deterministic prompt, and separates permanent facts from the opening scene:

- **System prompt (persistent):** `NAME`, `CHARACTER`, `PERSONALITY` (positive traits, plus a "traits to avoid" line from the `!` channel), `BACKSTORY`, and any free-text lines on the `CHAT` block. This carries only what is true for the whole conversation, so re-sending it every turn never re-asserts a scene.
- **Opening turn (seeded once):** `SCENARIO` is framed as the first user turn — the character responds to it — instead of living in the always-resent system prompt. The scene sets the stage, then ages out of the context window naturally rather than being repeated on every request.
- **Excluded:** `APPEARANCE`, `APPAREL`, and `ENVIRONMENT` are image-generation concerns and do not enter the chat prompt at all; likewise image-only content (`PROMPT`, `IMAGE`, `LORA`, `DETAILER` and sampler settings). `VOICE` is also excluded — it describes how the character sounds, which no target reads yet; speech habits a text model can act on belong in `PERSONALITY` or the `CHAT` instructions.

When a `SCENARIO` is present the character speaks first (its response to the seeded scene); otherwise the user sends the first message.

## Configuration

The `model` in a `CHAT` declaration names a model the way a `checkpoint` names one in an `IMAGE` declaration. Evoke resolves it against the model directories configured in **trusted local settings** (`~/.evoke/settings.json`), so a portable `.evoke` file names the model, never a machine path:

```json
{
  "chat": {
    "executable": "llama-server",
    "host": "127.0.0.1",
    "port": 8080,
    "model_paths": [
      "~/.localai/models"
    ]
  }
}
```

- **`model`** (in the `.evoke` file) → what counts as a match depends on the backend:
    - **llama.cpp** → a GGUF file name. Evoke searches each `model_paths` directory — first as a direct (or relative) path, then **recursively by file name**, so a nested layout like `…/MN-Violet-Lotus-12B.Q4_K_M/MN-Violet-Lotus-12B.Q4_K_M.gguf` resolves from just `MN-Violet-Lotus-12B.Q4_K_M.gguf`. The `.gguf` extension is optional.
    - **mlx** → a model *directory* under `model_paths`, or a Hugging Face repo id such as `mlx-community/Qwen3-8B-4bit`, which `mlx_lm` downloads on demand. A local directory wins when both would match.
    - Not found → a clear error at startup listing what was searched.
- **`model_paths`** → directories that hold your models (like ComfyUI's models directory). `~` is expanded.
- **`executable`** → the backend binary. Optional; defaults to `llama-server` or `mlx_lm.server` for the selected backend. It is a single setting, so set it only when overriding the default for the backend you use.
- **`host` / `port`** → the loopback endpoint Evoke binds the backend to. Optional; default `127.0.0.1:8080`.

Runtime settings that shape the launch (`context_window`, `gpu_layers`) live in the portable `CHAT` declaration, since they describe the character's needs, not the machine.

## Context management

The conversation history is bounded by a deterministic token-budgeted sliding window:

```text
input budget = context_window − max_output_tokens − safety_margin
```

The system prompt is always pinned and the newest user message is always kept. When a request would exceed the budget, the oldest **complete** user/assistant pairs are dropped first (never half a pair). Every request begins `system → user → …`, so it satisfies strict-alternation chat templates. If the pinned prompt plus the newest message still cannot fit, the command reports a clear error rather than silently truncating.

Token counts are conservatively estimated (Evoke does not query the backend tokenizer), so `safety_margin` absorbs the slack.

## Conversation memory

A conversation is remembered between runs. Repeat a command and it picks up where it left off:

```console
$ evoke chat girlfriend haley
...
You: see you tomorrow
Haley: can't wait!
^C

$ evoke chat girlfriend haley
Character: Haley
Backend:   llama.cpp (managed)
Model:     mistral-nemo
Context:   8192 tokens
Resumed:   14 stored turns
```

**The inputs alone are the key.** Editing a character file, adding a trait, swapping the model, or resolving a selector to a different file all continue the same conversation — what you typed is what you expect to resume. Only a different set of inputs starts a different conversation, and inputs are sorted before hashing, so `girlfriend haley` and `haley girlfriend` are the same thread.

Transcripts are stored one JSON file per conversation in `~/.evoke/sessions/`, named for the inputs that produced them:

```console
$ ls ~/.evoke/sessions
girlfriend+haley-4f2c81ae.json
roleplay+yasmin+beach-9b0d1c73.json
```

Memory lives here rather than in a `.evoke` file on purpose: a `.evoke` file is shareable and may be published to a registry, while your conversation with a character belongs to your invocation of it, not to the character.

The dialogue is stored **verbatim**, not summarized. Nothing is lost to a summarization pass, and a transcript far longer than the context window is safe to restore — the [sliding window](#context-management) already decides how much of it a request carries, dropping the oldest complete pairs. What ages out of the window is still on disk.

The transcript is written after **every completed turn**, not at exit, so an interrupt has nothing to finish before the backend shuts down and an abrupt kill loses at most the turn in flight. Each write is renamed into place, so a transcript is never half-written. A failed write is reported as a warning and the conversation continues — durable memory is a convenience, not a precondition for talking.

Two ways to start over:

- **`/reset`** during a session clears the conversation and discards what was stored, then replays the opening scene. Quitting straight after a reset does not bring the old conversation back.
- **`--new`** starts a fresh conversation instead of resuming. The stored one is left intact until the new session has a turn of its own to replace it with, so `--new` followed by an immediate Ctrl-C changes nothing.

A resumed session does **not** replay the `SCENARIO` opening — the scene was set the first time, and the character's response to it is part of the restored transcript.

## Interface

On an interactive terminal, chat runs as a full-screen UI: a pinned header, a scrolling transcript, and a pinned `>` input line. The header names who you are talking to and lists the commands, and is chrome rather than log content — it stays put as you scroll and survives `/reset`:

```text
Yasmin · qwen3-8b-q4.gguf (launched) · 8192 ctx
/reset clear history · /context token budget
────────────────────────────────────────────────────────
```

A resumed conversation opens on its own history, so you can scroll back through it. While a reply is generating, a spinner marks where it will appear in the log and the input line stays put — you can compose your next message, but **Enter is ignored until the reply resolves**. This is the default (non-streaming) experience.

Scroll the transcript with **PageUp/PageDown/↑/↓** (and the **mouse wheel** on terminals that translate it to arrow keys for full-screen apps); it follows new replies only when you're already at the bottom, so scrolling up to re-read isn't interrupted. The mouse is not captured, so **click-drag to select and copy** works exactly as in a normal terminal.

The plain line-based interface is used automatically when output is piped or non-interactive, when `--stream` is set (streaming shows tokens as they arrive, which the full-screen UI does not), or when `--no-tui` is passed.

## Interactive commands

| Command | Effect |
|:--------|:-------|
| `/reset` | Clear the conversation, discard the [stored transcript](#conversation-memory), and replay the opening scene (the compiled character is kept; the character re-opens from the `SCENARIO`). |
| `/context` | Show retained turns, estimated input tokens, and the budget. |

There is deliberately no quit command. Both interfaces end on interrupt (**Ctrl-C**), and the line interface also on EOF (**Ctrl-D**); either shuts the backend down cleanly and loses at most a reply still generating, since the [transcript is stored](#conversation-memory) after every completed turn. Slash commands are handled locally and never sent to the model.

## Flags

| Flag | Default | Description |
|:-----|:--------|:------------|
| `--stream` | `false` | Stream the reply token-by-token as it generates, instead of showing it once complete. Uses the line interface (not the full-screen UI). Overrides the `chat.stream` setting for the session. |
| `--no-tui` | `false` | Use the plain line-based interface instead of the full-screen UI. |
| `--new` | `false` | Start a new conversation instead of resuming the [stored one](#conversation-memory) for these inputs. |
| `-v`, `--verbose` | `false` | Print the merged composition before compiling. |

## Environment variables

| Variable | Default | Description |
|:---------|:--------|:------------|
| `EVOKE_CHAT_HOST` | `127.0.0.1` | Loopback host the managed backend binds to. |
| `EVOKE_CHAT_PORT` | `8080` | Port the managed backend binds to. |
| `EVOKE_LLAMA_SERVER` | `llama-server` | The `llama-server` executable (name on `PATH` or absolute path). |
| `EVOKE_CHAT_MODEL_PATH` | — | Extra model directories to search (OS path-list separated). Combined with `chat.model_paths`. |
| `EVOKE_CHAT_STARTUP_TIMEOUT` | `180s` | How long to wait for the backend to become healthy before giving up. |

Settings in `~/.evoke/settings.json` (`chat.executable`, `chat.host`, `chat.port`) override these.

Two presentation settings control how replies are shown:

- `chat.color` (`on` / `off` / `auto`, default `auto`) — ANSI styling of speaker labels and reply emphasis. `auto` enables it only when writing to a terminal (and honors `NO_COLOR`). Set with `evoke settings set chat.color <on|off|auto>`.
- `chat.stream` (`on` / `off` / `auto`, default off) — stream the reply as it generates, versus rendering it once complete. The `--stream` flag overrides this per session. Set with `evoke settings set chat.stream <on|off|auto>`.

## Example session

```console
$ evoke chat chat

Starting llama.cpp backend (mistral-nemo) ...
Character: assistant
Backend:   llama.cpp (managed)
Model:     mistral-nemo
Context:   8192 tokens
Ctrl-C or Ctrl-D to quit, /reset to clear history, /context for budget info.

You> What's the capital of France?

assistant> The capital of France is Paris.

You> ^D
Session ended.
```

## Exit codes

| Code | Meaning |
|:----:|:--------|
| `0` | Session ended cleanly (interrupt or EOF). |
| `1` | Resolution, compilation, backend startup, or backend crash error. |
| `2` | Usage error (no inputs given). |
