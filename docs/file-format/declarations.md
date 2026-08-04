# Declarations

Every declaration has a registered definition that fixes its **merge mode**, whether it supports the **`!` negative channel**, whether it supports the **`?` default**, and its canonical **render order**. The fifteen declarations below are the implemented set.

## The built-in declarations

| Declaration | Merge | `!` negative | `?` default | Argument | Order |
|:------------|:------|:------------:|:-----------:|:--------:|:-----:|
| `NAME`        | singular      | — | — | — | 10 |
| `CHARACTER`   | accumulating  | — | — | — | 20 |
| `PERSONALITY` | accumulating  | ✓ | ✓ | — | 30 |
| `BACKSTORY`   | accumulating  | — | — | — | 40 |
| `VOICE`       | accumulating  | ✓ | ✓ | — | 45 |
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

- **Merge** — how repeated contributions combine. See [Merge Modes](merge-modes.md). The structured declarations (`IMAGE`, `LORA`, `DETAILER`, `CHAT`, `KNOWLEDGE`) are singular **per argument** but resolve by [field-level layering](merge-modes.md#structured-field-level-overlay), not all-or-nothing: a block only needs to name the settings it changes and inherits the rest, and for a key two files both set, the later argument wins.
- **`!` negative** — whether values may be routed to the exclusion channel. See [Prefixes & Channels](prefixes.md).
- **`?` default** — whether values may be marked as defaults used only when nothing more specific exists.
- **Order** — the canonical, ascending order renderers use so output is deterministic rather than dependent on file order.

## Which command consumes which declaration

A declaration is only meaningful to the commands that read it. `image` and `chat` consume overlapping but distinct subsets, and a declaration ignored by a command costs nothing there — it is simply not rendered.

| Declaration | `evoke image` | `evoke chat` |
|:------------|:--------------|:-------------|
| `NAME`        | output directory name (not part of the prompt) | the character's name |
| `CHARACTER`   | — | identity |
| `PERSONALITY` | — | personality, plus "traits to avoid" from the `!` channel |
| `BACKSTORY`   | — | backstory |
| `VOICE`       | — | — |
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

`CHARACTER`, `PERSONALITY`, `BACKSTORY`, and `SCENARIO` are **chat-only**. They describe identity, disposition, history, and narrative situation — none of which a diffusion model can render — so the image pipeline does not put them in the prompt. This means a character file can carry a full identity and backstory without it competing for the image prompt's limited attention. Everything drawable about a subject belongs in `APPEARANCE`.

`VOICE` is read by **no command yet** — see [VOICE](#voice). It parses, validates, merges, and shows up in `evoke inspect` like any other declaration, so a character file can carry its voice today; nothing renders it until an audio target exists. A declaration no command reads costs its composition nothing, which is exactly what makes adding a target cheap.

For guidance on writing the content of these blocks well, see the style guide at `skills/evoke-authoring/references/style-guide.md`.

## Reference

### NAME

The human-facing character or entity name. **Singular** — a single explicit value. No negative channel (a name has no meaningful "negative prompt"), no default.

```text
NAME
    Ashley
```

### CHARACTER

A stable factual description of who the character is. **Accumulating**; positive only — there is no negative channel, because an identity has no meaningful "not this" (contradictions belong in `!PERSONALITY`). **Chat-only** — not rendered into image prompts; drawable detail goes in `APPEARANCE`. `IDENTITY` is accepted as a migration alias for `CHARACTER`.

```text
CHARACTER
    Ashley is an emergency-room nurse and the most senior person on the night shift.
    She trains every new hire on the ward, which is why she is the one paged when a case goes wrong.
```

### PERSONALITY

Behavioral tendencies and traits. **Accumulating**; supports the negative channel and defaults. **Chat-only** — not rendered into image prompts.

```text
PERSONALITY
    explains each procedure while she performs it, so nobody has to ask what is happening
    goes brisk and formal the moment a case turns serious, and stays that way until it is over
    deflects direct flirting by handing the patient a task

!PERSONALITY
    cruel, emotionally detached
```

### BACKSTORY

Canonical background information. **Accumulating**, positive only. **Chat-only** — not rendered into image prompts, so it can be as long as the character needs.

```text
BACKSTORY
    Trained in Milwaukee, moved to Chicago for her first hospital job.
```

### VOICE

What the subject **sounds** like — timbre, pitch, pace, accent, and vocal texture. **Accumulating**, with `?` default support.

```text
VOICE
    low alto, slight rasp, unhurried cadence
```

**No command consumes `VOICE` yet.** It is a place to record a character's voice alongside the rest of the character, so the information is captured, versioned, and pushed to the registry with the file rather than living in someone's notes until an audio target arrives. Until then it is inert: absent from image prompts, absent from the chat system prompt, and absent from `knowledge.db`.

Two boundaries keep it that way when a target does appear:

- **`VOICE` describes the voice, not the words.** How the character sounds is `VOICE`; what they say and how they phrase it is `PERSONALITY` and the `CHAT` instructions. "Rarely uses contractions" is a language trait that a text model can act on today — it belongs there, not here.
- **`VOICE` is content, not engine configuration.** A synthesis engine, model, and its sampling settings are to `VOICE` what `IMAGE` is to `APPEARANCE`: a separate structured declaration, added when there is a backend to configure. `VOICE` will not grow `key = value` settings.

Write it as short comma-separated phrases, the way `APPEARANCE` is written. The plausible first consumer is a synthesis target conditioned on a voice description, which cannot read prose; a language target reads phrases perfectly well, so phrases are the form that survives either outcome.

### APPEARANCE

General visible physical traits. **Accumulating**; supports the negative channel and defaults.

```text
APPEARANCE
    (violet skin:1.25)
    small round body, glowing speckles, large luminous eyes

!APPEARANCE
    scary, slimy, monstrous
```

### APPAREL

Clothing and accessories. **Accumulating**; supports the negative channel and defaults. This is deliberately broad for the MVP — narrower declarations like `OUTFIT` or `FOOTWEAR` may come later.

```text
?APPAREL
    green shirt, blue jeans

APPAREL
    heavy green winter coat, black boots
```

The `?` default apparel is used only when no explicit `APPAREL` appears in the composition.

### ENVIRONMENT

Scene and setting details. **Accumulating**; supports the negative channel and defaults. In the MVP, `ENVIRONMENT` carries the entire scene/setting role — there is no separate `LOCATION` declaration.

```text
ENVIRONMENT
    pine forest, tall evergreen trees, soft morning mist
```

### SCENARIO

The current narrative or conversational situation. **Singular** (one active value); supports defaults, positive only. **Chat-only** — it seeds the opening turn and is not rendered into image prompts.

```text
SCENARIO
    Ashley is checking the user's temperature after they arrived with a fever.
```

### PROMPT

Direct prompt material for when no more specific declaration fits — an escape hatch, not the preferred representation. **Accumulating**; supports the negative channel and defaults.

```text
PROMPT
    cinematic portrait composition

!PROMPT
    blurry, deformed hands
```

### IMAGE

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

**Settings:** `checkpoint`, `steps`, `cfg`, `sampler_name`, `scheduler`, `width`, `height`, `denoise`, `disabled`. For `IMAGE upscale`: `upscale_model`, `factor`, `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `tile_width`, `tile_height`, `disabled`.

`disabled = true` switches a stage off — see [Disabling a stage](#disabling-a-stage).

### LORA

LoRA model definition. **Singular** (per argument); supports defaults. The argument is the reference name. Values are `key = value` settings.

```text
LORA rimix
    model = rimix_v2.safetensors
    strength = 0.8
    clip = 0.8
```

**Settings:** `model` (required — the filename), `strength` (default 1.0), `clip` (default 1.0), `disabled`.

### DETAILER

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

**Settings:** `detector`, `guide_size`, `max_size`, `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `feather`, `bbox_threshold`, `bbox_dilation`, `bbox_crop_factor`, `noise_mask_feather`, `drop_size`, `max_detection`, `disabled`.

A later file can raise `max_detection` — or set `disabled = true` — with a `DETAILER face` block naming only that setting; the detector, sizes, and prompt text are inherited. See [field-level layering](merge-modes.md#structured-field-level-overlay).

### CHAT

Interactive-chat configuration consumed by [`evoke chat`](../cli/chat.md). **Singular** with field-level default overlay (like `IMAGE`/`LORA`): a general file may supply `?CHAT` defaults that a more specific file overrides field by field. Values are a mix of `key = value` settings and free-text lines; the free text becomes extra chat-specific system instructions. It does not affect image generation.

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

`model` is a GGUF **file name** — like a `checkpoint` in `IMAGE` — resolved against the model directories in trusted local settings, so a `.evoke` file names the model, not a machine path, and stays portable. Evoke launches and manages the `llama-server` backend for the session. See [`evoke chat`](../cli/chat.md) for how it resolves and how the backend is managed.

### KNOWLEDGE

A retrieval source for retrieval-augmented generation during chat. **Singular per argument** (the argument names the source); supports defaults, positive only. **Chat-only.**

```text
KNOWLEDGE lore
    db = lore.db
    top_k = 5
    embed_model = nomic-embed-text
```

**Settings:** `db` (required), `top_k`, `embed_model`.

`db` is a SQLite file **name**, resolved against the same `chat.model_paths` directories as the `CHAT` `model` setting, so the file stays portable. The database holds pre-embedded text chunks; at each turn the user's message is embedded via an ollama-compatible endpoint and the closest `top_k` chunks are injected as reference material.

Build the database from a directory of markdown and `.evoke` files with [`evoke knowledge`](../cli/knowledge.md). `embed_model` is optional and normally omitted: the database records the model it was built with, and chat adopts it. Set it only to override that — a conflict between the two is an error rather than a silent drop in retrieval quality.

## Disabling a stage

`IMAGE`, `LORA`, and `DETAILER` accept a `disabled` setting, so a composition can switch off a pass that another file supplied. A `no-upscale.evoke` holding just this is enough for a quick draft pass:

```text
IMAGE upscale
    disabled = true
```

```bash
evoke image character pipeline no-upscale
```

- **Exactly `true` disables.** `TRUE`, `1`, and `yes` are not recognized and leave the stage on. There is no validation warning for a misspelled value.
- **It layers like any other setting**, so a later argument can set `disabled = false` to turn a stage back on, and the last file to name the key wins. See [field-level layering](merge-modes.md#structured-field-level-overlay).
- **`evoke inspect` renders a disabled block with a leading `!`** — `!DETAILER face` — which is the quickest way to confirm what a composition actually turned off.

What each one disables:

| Declaration            | Effect                                                                    |
| :--------------------- | :------------------------------------------------------------------------ |
| `IMAGE upscale`        | the upscale pass is skipped                                               |
| `DETAILER <region>`    | that region's inpaint pass is skipped                                     |
| `LORA <name>`          | dropped from the LoRA chain; references to it resolve to nothing          |
| `IMAGE` (unnamed base) | **not** a way to skip generation — only makes the base stage's settings and prompt text be ignored, falling back to built-in defaults. Rarely what you want. |

`CHAT` and `KNOWLEDGE` do not support `disabled`; `evoke chat` reports it as an ignored unknown setting.

### The other two ways a stage ends up off

Both come up when hunting down a detailer that never ran:

- **An explicit negative block with no positive contribution anywhere** disables the stage. `!DETAILER hand` on its own means "there is no hand detailer, and here is the negative prompt for it" — nothing positive ever configured it. A `?DETAILER hand` default *plus* an explicit `!DETAILER hand` means "use the default config with this negative prompt," not "disable."
- **The generator disables anything it cannot configure**: a detailer whose resolved settings include no `detector`, or an upscale stage with no `upscale_model`. Settings alone never switch a stage *on* — some file in the composition has to supply the detector or model, so a shot file that sets only `max_detection` for a region no file has configured changes nothing.

## What isn't here

The fifteen declarations above are the complete set. Using any other name is an *unknown declaration* validation error. Namespaced/dotted extension names (`FOO.BAR`) are also rejected.
