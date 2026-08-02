---
title: Declarations
parent: File Format
nav_order: 2
---

# Declarations
{: .no_toc }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

Every declaration has a registered definition that fixes its **merge mode**, whether it supports the **`!` negative channel**, whether it supports the **`?` default**, and its canonical **render order**. The thirteen declarations below are the implemented set.

## The built-in declarations

| Declaration | Merge | `!` negative | `?` default | Argument | Order |
|:------------|:------|:------------:|:-----------:|:--------:|:-----:|
| `NAME`        | singular      | — | — | — | 10 |
| `CHARACTER`   | accumulating  | — | — | — | 20 |
| `PERSONALITY` | accumulating  | ✓ | ✓ | — | 30 |
| `BACKSTORY`   | accumulating  | — | — | — | 40 |
| `APPEARANCE`  | accumulating  | ✓ | ✓ | — | 50 |
| `APPAREL`     | accumulating  | ✓ | ✓ | — | 60 |
| `ENVIRONMENT` | accumulating  | ✓ | ✓ | — | 70 |
| `SCENARIO`    | singular      | — | ✓ | — | 80 |
| `PROMPT`      | accumulating  | ✓ | ✓ | — | 90 |
| `IMAGE`       | singular      | ✓ | ✓ | optional | 100 |
| `LORA`        | singular      | — | ✓ | required | 110 |
| `DETAILER`    | singular      | ✓ | ✓ | required | 120 |
| `CHAT`        | singular      | — | ✓ | — | 130 |
| `KNOWLEDGE`   | singular      | — | ✓ | required | 140 |

- **Merge** — how repeated contributions combine. See [Merge Modes](merge-modes).
- **`!` negative** — whether values may be routed to the exclusion channel. See [Prefixes & Channels](prefixes).
- **`?` default** — whether values may be marked as defaults used only when nothing more specific exists.
- **Order** — the canonical, ascending order renderers use so output is deterministic rather than dependent on file order.

## Which command consumes which declaration

A declaration is only meaningful to the commands that read it. `image` and `chat` consume overlapping but distinct subsets, and a declaration ignored by a command costs nothing there — it is simply not rendered.

| Declaration | `evoke image` | `evoke chat` |
|:------------|:--------------|:-------------|
| `NAME`        | output directory name (not part of the prompt) | the character's name |
| `CHARACTER`   | positive prompt | identity |
| `PERSONALITY` | — | personality, plus "traits to avoid" from the `!` channel |
| `BACKSTORY`   | — | backstory |
| `APPEARANCE`  | positive / negative prompt | — |
| `APPAREL`     | apparel conditioning | — |
| `ENVIRONMENT` | environment conditioning | — |
| `SCENARIO`    | — | the opening turn |
| `PROMPT`      | positive / negative prompt | — |
| `IMAGE`       | sampler settings and prompt text | — |
| `LORA`        | LoRA chain | — |
| `DETAILER`    | per-region inpaint prompts | — |
| `CHAT`        | — | runtime, sampling, and system instructions |
| `KNOWLEDGE`   | — | retrieval sources |

`PERSONALITY`, `BACKSTORY`, and `SCENARIO` are **chat-only**. They describe disposition, history, and narrative situation — none of which a diffusion model can render — so the image pipeline does not put them in the prompt. This means a character file can carry a full backstory without it competing for the image prompt's limited attention.

For guidance on writing the content of these blocks well, see the [Style Guide](https://github.com/jesse0michael/evoke/blob/main/STYLE.md).

## Reference

### NAME
{: .no_toc }

The human-facing character or entity name. **Singular** — a single explicit value. No negative channel (a name has no meaningful "negative prompt"), no default.

```text
NAME
    Ashley
```

### CHARACTER
{: .no_toc }

A stable factual description of who the character is. **Accumulating**, positive only. `IDENTITY` is accepted as a migration alias for `CHARACTER`.

```text
CHARACTER
    adult emergency-room nurse
    grew up in Wisconsin
```

### PERSONALITY
{: .no_toc }

Behavioral tendencies and traits. **Accumulating**; supports the negative channel and defaults. **Chat-only** — not rendered into image prompts.

```text
PERSONALITY
    warm
    competent
    easily flustered by direct flirting

!PERSONALITY
    cruel
    emotionally detached
```

### BACKSTORY
{: .no_toc }

Canonical background information. **Accumulating**, positive only. **Chat-only** — not rendered into image prompts, so it can be as long as the character needs.

```text
BACKSTORY
    Trained in Milwaukee, moved to Chicago for her first hospital job.
```

### APPEARANCE
{: .no_toc }

General visible physical traits. **Accumulating**; supports the negative channel and defaults.

```text
APPEARANCE
    small
    round
    violet skin
    glowing speckles

!APPEARANCE
    scary
    slimy
    monstrous
```

### APPAREL
{: .no_toc }

Clothing and accessories. **Accumulating**; supports the negative channel and defaults. This is deliberately broad for the MVP — narrower declarations like `OUTFIT` or `FOOTWEAR` may come later.

```text
?APPAREL
    green shirt
    blue jeans

APPAREL
    heavy green winter coat
    black boots
```

The `?` default apparel is used only when no explicit `APPAREL` appears in the composition.

### ENVIRONMENT
{: .no_toc }

Scene and setting details. **Accumulating**; supports the negative channel and defaults. In the MVP, `ENVIRONMENT` carries the entire scene/setting role — there is no separate `LOCATION` declaration.

```text
ENVIRONMENT
    pine forest
    tall evergreen trees
    soft morning mist
```

### SCENARIO
{: .no_toc }

The current narrative or conversational situation. **Singular** (one active value); supports defaults, positive only. **Chat-only** — it seeds the opening turn and is not rendered into image prompts.

```text
SCENARIO
    Ashley is checking the user's temperature after they arrived with a fever.
```

### PROMPT
{: .no_toc }

Direct prompt material for when no more specific declaration fits — an escape hatch, not the preferred representation. **Accumulating**; supports the negative channel and defaults.

```text
PROMPT
    cinematic portrait composition

!PROMPT
    blurry
    deformed hands
```

### IMAGE
{: .no_toc }

Pipeline stage configuration for image generation. **Singular** (per argument); supports the negative channel and defaults. The argument names the stage (e.g., `upscale`); an unnamed IMAGE is the base generation stage. Values are a mix of `key = value` settings and prompt text lines.

```text
IMAGE
    checkpoint = riMixIllustriousAnima_riMixV2.safetensors
    steps = 40
    cfg = 4
    width = 1216
    height = 832

IMAGE upscale
    upscale_model = 4x-UltraSharp.pth
    factor = 2
    steps = 12
    denoise = 0.3
```

**Settings:** `checkpoint`, `steps`, `cfg`, `sampler_name`, `scheduler`, `width`, `height`, `denoise`. For `IMAGE upscale`: `upscale_model`, `factor`, `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `tile_width`, `tile_height`.

### LORA
{: .no_toc }

LoRA model definition. **Singular** (per argument); supports defaults. The argument is the reference name. Values are `key = value` settings.

```text
LORA rimix
    model = rimix_v2.safetensors
    strength = 0.8
    clip = 0.8
```

**Settings:** `model` (required — the filename), `strength` (default 1.0), `clip` (default 1.0).

### DETAILER
{: .no_toc }

Inpainting detailer configuration. **Singular** (per argument); supports the negative channel and defaults. The argument names the body region: `face`, `eye`, `upper_body`, `lower_body`, `hand`. Values are a mix of `key = value` settings and prompt text lines.

```text
?DETAILER face
    clear visible irises, defined iris ring
    detector = bbox/face_yolov8m.pt
    guide_size = 1024
    max_size = 1536
    steps = 15
    cfg = 3
    denoise = 0.3
    max_detection = 2

!DETAILER face
    empty eyes, missing iris
```

**Settings:** `detector`, `guide_size`, `max_size`, `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `feather`, `bbox_threshold`, `bbox_dilation`, `bbox_crop_factor`, `noise_mask_feather`, `drop_size`, `max_detection`.

### CHAT
{: .no_toc }

Interactive-chat configuration consumed by [`evoke chat`](../cli/chat). **Singular** with field-level default overlay (like `IMAGE`/`LORA`): a general file may supply `?CHAT` defaults that a more specific file overrides field by field. Values are a mix of `key = value` settings and free-text lines; the free text becomes extra chat-specific system instructions. It does not affect image generation.

```text
?CHAT
    backend = llama.cpp
    model = MN-Violet-Lotus-12B.Q4_K_M.gguf
    context_window = 8192
    gpu_layers = 99
    temperature = 0.85
    max_output_tokens = 512
    Stay in character at all times.
```

**Settings:** `backend` (`llama.cpp`), `model`, `context_window`, `gpu_layers`, `max_output_tokens`, `safety_margin`, `min_recent_turns`, `temperature`, `top_p`, `repeat_penalty`, `seed`, `stop`.

`model` is a GGUF **file name** — like a `checkpoint` in `IMAGE` — resolved against the model directories in trusted local settings, so a `.evoke` file names the model, not a machine path, and stays portable. Evoke launches and manages the `llama-server` backend for the session. See [`evoke chat`](../cli/chat) for how it resolves and how the backend is managed.

### KNOWLEDGE
{: .no_toc }

A retrieval source for retrieval-augmented generation during chat. **Singular per argument** (the argument names the source); supports defaults, positive only. **Chat-only.**

```text
KNOWLEDGE lore
    db = lore.db
    top_k = 5
    embed_model = nomic-embed-text
```

**Settings:** `db` (required), `top_k`, `embed_model`.

`db` is a SQLite file **name**, resolved against the same `chat.model_paths` directories as the `CHAT` `model` setting, so the file stays portable. The database holds pre-embedded text chunks; at each turn the user's message is embedded via an ollama-compatible endpoint and the closest `top_k` chunks are injected as reference material.

Build the database from a directory of markdown with [`evoke knowledge`](../cli/knowledge). `embed_model` is optional and normally omitted: the database records the model it was built with, and chat adopts it. Set it only to override that — a conflict between the two is an error rather than a silent drop in retrieval quality.

## What isn't here

The fourteen declarations above are the complete set. Using any other name is an *unknown declaration* validation error. Namespaced/dotted extension names (`FOO.BAR`) are also rejected.
