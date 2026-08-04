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

## Backend: managed llama.cpp

Evoke **owns** the backend for the session. When a chat starts, it launches a llama.cpp `llama-server` child process with the resolved model and runtime settings, waits for it to report healthy, talks to it, and **shuts it down when the session ends** — on `/exit`, EOF, interrupt, or error. It never leaves the process running.

You do not start `llama-server` yourself. Evoke binds it to loopback on a fixed port (default `127.0.0.1:8080`) and, to avoid running two backends at once, **refuses to start if that port is already in use** — stop the other process (or point Evoke at a different port) and try again.

Only one backend driver is supported: `llama.cpp`. `llama-server` must be installed and on your `PATH` (or set its path in settings). Managing multiple concurrent backends is out of scope.

## Prompt compilation

The compiler renders resolved declarations into a deterministic prompt, and separates permanent facts from the opening scene:

- **System prompt (persistent):** `NAME`, `CHARACTER`, `PERSONALITY` (positive traits, plus a "traits to avoid" line from the `!` channel), `BACKSTORY`, and any free-text lines on the `CHAT` block. This carries only what is true for the whole conversation, so re-sending it every turn never re-asserts a scene.
- **Opening turn (seeded once):** `SCENARIO` is framed as the first user turn — the character responds to it — instead of living in the always-resent system prompt. The scene sets the stage, then ages out of the context window naturally rather than being repeated on every request.
- **Excluded:** `APPEARANCE`, `APPAREL`, and `ENVIRONMENT` are image-generation concerns and do not enter the chat prompt at all; likewise image-only content (`PROMPT`, `IMAGE`, `LORA`, `DETAILER` and sampler settings). `VOICE` is also excluded — it describes how the character sounds, which no target reads yet; speech habits a text model can act on belong in `PERSONALITY` or the `CHAT` instructions.

When a `SCENARIO` is present the character speaks first (its response to the seeded scene); otherwise the user sends the first message.

## Configuration

The `model` in a `CHAT` declaration is a **GGUF file name**, exactly like a `checkpoint` in an `IMAGE` declaration. Evoke locates that file in the model directories configured in **trusted local settings** (`~/.evoke/settings.json`), so a portable `.evoke` file names the model, never a machine path:

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

- **`model`** (in the `.evoke` file) → a GGUF file name. Evoke searches each `model_paths` directory — first as a direct (or relative) path, then **recursively by file name**, so a nested layout like `…/MN-Violet-Lotus-12B.Q4_K_M/MN-Violet-Lotus-12B.Q4_K_M.gguf` resolves from just `MN-Violet-Lotus-12B.Q4_K_M.gguf`. The `.gguf` extension is optional. Not found → a clear error at startup listing what was searched.
- **`model_paths`** → directories that hold your GGUF files (like ComfyUI's models directory). `~` is expanded.
- **`executable`** → the `llama-server` binary. Optional; defaults to `llama-server` on your `PATH`.
- **`host` / `port`** → the loopback endpoint Evoke binds the backend to. Optional; default `127.0.0.1:8080`.

Runtime settings that shape the launch (`context_window`, `gpu_layers`) live in the portable `CHAT` declaration, since they describe the character's needs, not the machine.

## Context management

The conversation history is bounded by a deterministic token-budgeted sliding window:

```text
input budget = context_window − max_output_tokens − safety_margin
```

The system prompt is always pinned and the newest user message is always kept. When a request would exceed the budget, the oldest **complete** user/assistant pairs are dropped first (never half a pair). Every request begins `system → user → …`, so it satisfies strict-alternation chat templates. If the pinned prompt plus the newest message still cannot fit, the command reports a clear error rather than silently truncating.

Token counts are conservatively estimated (Evoke does not query the backend tokenizer), so `safety_margin` absorbs the slack.

## Interface

On an interactive terminal, chat runs as a full-screen UI: a scrolling transcript above a pinned `>` input line. While a reply is generating, a spinner marks where it will appear in the log and the input line stays put — you can compose your next message, but **Enter is ignored until the reply resolves**. This is the default (non-streaming) experience.

Scroll the transcript with **PageUp/PageDown/↑/↓** (and the **mouse wheel** on terminals that translate it to arrow keys for full-screen apps); it follows new replies only when you're already at the bottom, so scrolling up to re-read isn't interrupted. The mouse is not captured, so **click-drag to select and copy** works exactly as in a normal terminal.

The plain line-based interface is used automatically when output is piped or non-interactive, when `--stream` is set (streaming shows tokens as they arrive, which the full-screen UI does not), or when `--no-tui` is passed.

## Interactive commands

| Command | Effect |
|:--------|:-------|
| `/exit`, `/quit` | End the session. |
| `/reset` | Clear the conversation and replay the opening scene (the compiled character is kept; the character re-opens from the `SCENARIO`). |
| `/context` | Show retained turns, estimated input tokens, and the budget. |

Both interfaces exit cleanly on interrupt (Ctrl-C); the line interface also exits on EOF (Ctrl-D). Slash commands are handled locally and never sent to the model.

## Flags

| Flag | Default | Description |
|:-----|:--------|:------------|
| `--stream` | `false` | Stream the reply token-by-token as it generates, instead of showing it once complete. Uses the line interface (not the full-screen UI). Overrides the `chat.stream` setting for the session. |
| `--no-tui` | `false` | Use the plain line-based interface instead of the full-screen UI. |
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
Type /exit to quit, /reset to clear history, /context for budget info.

You> What's the capital of France?

assistant> The capital of France is Paris.

You> /exit
Session ended.
```

## Exit codes

| Code | Meaning |
|:----:|:--------|
| `0` | Session ended cleanly (`/exit`, EOF, or interrupt). |
| `1` | Resolution, compilation, backend startup, or backend crash error. |
| `2` | Usage error (no inputs given). |
