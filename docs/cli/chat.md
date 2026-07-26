---
title: evoke chat
parent: CLI
nav_order: 2
---

# evoke chat

Compose `.evoke` files into a character and start an interactive conversation with a local LLM backend. Chat is a second output target built from the same resolved Evoke data as [`generate`](generate): the parser, selectors, and merge semantics are identical — only the compilation target differs.

```console
$ evoke chat <input>...
```

Inputs are classified and resolved exactly as in [`evoke generate`](generate#input-types) (selectors, local paths, `@namespace/name` registry references, literal prompts). The merged composition's persistent character declarations become the system prompt; the `CHAT` declaration selects the model, sampling, and conversation policy.

The shipped [`examples/chat.evoke`](https://github.com/jesse0michael/evoke/blob/main/examples/chat.evoke) is a self-contained basic assistant — start there:

```console
$ evoke chat chat
```

Because Evoke composes files rather than referencing them, a `CHAT` pipeline can be reused across characters and scenes. A pipeline you author (say `roleplay`) supplies only the model and sampling, while character and scene files supply the rest:

```console
$ evoke chat roleplay yasmin beach
```

- `roleplay` carries the `CHAT` declaration (model, context size, sampling).
- `yasmin` supplies identity, personality, appearance, and backstory.
- `beach` supplies the starting scenario and environment.

## Backend: managed llama.cpp

Evoke **owns** the backend for the session. When a chat starts, it launches a llama.cpp `llama-server` child process with the resolved model and runtime settings, waits for it to report healthy, talks to it, and **shuts it down when the session ends** — on `/exit`, EOF, interrupt, or error. It never leaves the process running.

You do not start `llama-server` yourself. Evoke binds it to loopback on a fixed port (default `127.0.0.1:8080`) and, to avoid running two backends at once, **refuses to start if that port is already in use** — stop the other process (or point Evoke at a different port) and try again.

Only one backend driver is supported: `llama.cpp`. `llama-server` must be installed and on your `PATH` (or set its path in settings). Managing multiple concurrent backends is out of scope.

## Prompt compilation

The compiler renders resolved declarations into a deterministic prompt, and separates permanent facts from the opening scene:

- **System prompt (persistent):** `NAME`, `CHARACTER`, `PERSONALITY` (positive traits, plus a "traits to avoid" line from the `!` channel), `BACKSTORY`, `APPEARANCE`, and any free-text lines on the `CHAT` block.
- **Starting situation (evolving):** `APPAREL`, `ENVIRONMENT`, and `SCENARIO`, labeled so the model can let the scene change during the conversation.
- **Excluded:** image-only content (`PROMPT`, `IMAGE`, `LORA`, `DETAILER` and sampler settings) never enters the chat prompt.

The user sends the first message; there is no assistant greeting.

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

## Interactive commands

| Command | Effect |
|:--------|:-------|
| `/exit`, `/quit` | End the session. |
| `/reset` | Clear the conversation while keeping the compiled character and scenario. |
| `/context` | Show retained turns, estimated input tokens, and the budget. |

The session also exits cleanly on EOF (Ctrl-D) and on interrupt (Ctrl-C). Slash commands are handled locally and never sent to the model.

## Flags

| Flag | Default | Description |
|:-----|:--------|:------------|
| `--explain` | `false` | Compile and print the plan — model, the exact launch command, sampling, and the full system prompt — **without** starting a backend. |
| `--no-stream` | `false` | Request a single non-streamed response instead of streaming tokens. |
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
| `0` | Session ended cleanly (`/exit`, EOF, or interrupt), or `--explain` succeeded. |
| `1` | Resolution, compilation, backend startup, or backend crash error. |
| `2` | Usage error (no inputs given). |
