# File Format

The mechanics the parser and merger enforce. For what to put *in* the blocks, see [Style Guide](style-guide.md).

## Syntax

```text
?APPAREL
    green shirt
    blue jeans
```

- **`?`** — an optional prefix (`!`, `?`, or `?!`), immediately before the name.
- **`APPAREL`** — the declaration name, at **column 1**, no indentation. A new header ends the previous block.
- indented lines — the values belonging to the header above them.

Rules:

- **Names** are letters, digits, underscores, and hyphens. Case-insensitive, normalized to uppercase; uppercase is canonical. Dotted/namespaced names (`COMFYUI.FACE_DETAILER`) are reserved and rejected today.
- **Indent with spaces only.** A tab in the indentation is an error. The amount is not significant, only its presence.
- **Blank lines do not end a block.** A block runs until the next column-1 header.
- **Comments** are lines whose first non-whitespace character is `#`, at any indentation. There are no inline comments.
- **UTF-8 only.** Invalid UTF-8 is rejected.
- **Empty blocks are invalid.** Every header needs at least one value line.
- **A newline ends a value.** Never wrap one sentence across two lines; only trailing whitespace is trimmed, internal punctuation and spacing are preserved.
- **Structured declarations** (`IMAGE`, `LORA`, `DETAILER`, `CHAT`, `KNOWLEDGE`) take an optional or required argument on the header line and `key = value` settings as values. Every other declaration rejects text after the name — `NAME Sumi` is an error; the value belongs on an indented line.

### Syntax errors

The parser accumulates all of them with line numbers rather than stopping at the first:

| Message                                                                | Cause                                            |
| :--------------------------------------------------------------------- | :----------------------------------------------- |
| `file is not valid UTF-8`                                              | invalid UTF-8                                    |
| `tabs are not allowed for indentation; use spaces`                     | a value line indented with a tab                 |
| `declaration is missing a name`                                        | a prefix with no name after it                   |
| `invalid prefix; only "!", "?", and "?!" are allowed…`                 | an unexpected leading operator, e.g. `!?NAME`    |
| `unexpected text "..." after declaration name; values belong on…`      | extra text on the header line, e.g. `NAME Sumi`  |
| `invalid declaration name "..."`                                       | characters outside `A–Z a–z 0–9 _ -`             |
| `indented value has no preceding declaration`                          | a value line before any header                   |
| `declaration "..." has no values`                                      | an empty block                                   |

*Unknown declaration* and *unsupported prefix* are **validation** errors, raised after a file parses cleanly.

## TAGS

`TAGS` is a metadata block, not a declaration: the tags used by the selector system to find the file. It accepts no prefixes and contributes nothing to any prompt. **Write them as one comma-separated line** — the parser splits on commas and newlines both, and a two-tag block spread over two lines is three lines of file for nothing.

```text
TAGS
    character
```

A selector is tags joined with `+` — `evoke image nurse+modern` matches files carrying both. When several files match, one is chosen at random.

The index adds each file's base name as an **implicit tag**, so `sumi.evoke` is always reachable as `evoke image sumi` whether or not it declares any tags. A file *named* for a single-tag selector outranks files that merely carry the tag: with `sumi.evoke`, `sumi-winter.evoke`, and `sumi-beach.evoke` all tagged `sumi`, `evoke image sumi` always resolves `sumi.evoke`. Give the outfit files a shared tag that isn't a file name (`sumi-wardrobe`) when you want a roll among them. A path and an `@namespace/name` registry reference also work as inputs.

## Prefixes & channels

A prefix selects a **channel** or marks a **default**. It is never an operation.

| Prefix   | Name             | Meaning                                                                     |
| :------- | :--------------- | :-------------------------------------------------------------------------- |
| *(none)* | positive         | an explicit positive contribution                                           |
| `!`      | negative         | contributes to the negative / exclusion channel                             |
| `?`      | default          | used only when an explicit contribution doesn't supply the same thing (see below) |
| `?!`     | default negative | a default contribution into the negative channel — yields to any explicit `!` |

Only declarations whose definition supports a prefix may use it — `!NAME` is a validation error.

**What `!` is not.** It does not delete, remove, override, disable, or negate a positive value. It selects the negative channel, which each target interprets its own way (image → negative prompt; chat → "traits to avoid"). Positive and negative channels resolve independently, so one declaration can carry both.

**What `?` means.** A stable property of the content: *use this when nothing more specific was selected.* `?APPAREL green shirt` in a character file plus an explicit `APPAREL heavy winter coat` in an outfit file resolves to the coat; select the character alone and it wears the shirt. `?` means "only if nothing else contributed" — not "optional."

A default yields to an explicit contribution of **the same thing**, and the unit depends on what was contributed:

| The default contributes                                                                    | An explicit contribution replaces                                                       |
| :------------------------------------------------------------------------------------------ | :--------------------------------------------------------------------------------------- |
| **values in a channel** — `?APPAREL`, `?APPEARANCE`, `?PERSONALITY`, prompt text on `?IMAGE`/`?DETAILER`/`?CHAT` | **the whole channel** — one explicit line replaces every default line; you cannot add to an inherited list |
| **a `key = value` setting** on `?IMAGE`, `?LORA`, `?DETAILER`, `?CHAT`, `?KNOWLEDGE`       | **that one key** — settings the explicit block never named still apply                   |

One rule at two granularities: a setting is addressable by name, a prompt line isn't. So an explicit `IMAGE` block containing only `steps = 20` inherits the default's `checkpoint`, `cfg`, and text; add a prompt line to it and the default's text drops out entirely.

**`?!` is a default in the negative channel.** Declaration *and* channel together are the unit of "the same thing," so a `?!APPEARANCE` block yields to an explicit `!APPEARANCE` anywhere in the composition and is untouched by an explicit positive `APPEARANCE`. It is the negative side of the same rule, with no extra semantics — use it where a file's exclusions are its answer until a caller supplies different ones.

**There is deliberately no `=` / force / override operator.** Canonical values use `?`, which any explicit declaration already suppresses. Two *conflicting explicit* singular values are a conflict, never resolved by file order.

## Merge modes

Each channel resolves independently.

**Singular** — at most one active explicit value.

- one explicit → use it
- zero explicit → use the default, if supported
- more than one explicit → **conflict** (warns, uses the first; never silently ordered)

**Accumulating** — combines every contribution in source order, then removes exact duplicates after trimming surrounding whitespace. Dedup is purely textual: `violet skin` and `  violet skin  ` collapse; `violet skin` and `purple skin` stay as two values. No semantic dedup is ever attempted.

Resolution, per declaration and channel: collect explicit → collect defaults → if any explicit exists, ignore all defaults → apply the merge mode → dedup exact normalized values → report singular conflicts.

## The fifteen declarations

`IDENTITY` is a migration alias for `CHARACTER`. Any other name is an *unknown declaration* validation error.

| Declaration   | Merge        | `!` | `?` | Argument | Order |
| :------------ | :----------- | :-: | :-: | :------: | :---: |
| `NAME`        | singular     |  —  |  —  |    —     |  10   |
| `CHARACTER`   | accumulating |  —  |  —  |    —     |  20   |
| `PERSONALITY` | accumulating |  ✓  |  ✓  |    —     |  30   |
| `BACKSTORY`   | accumulating |  —  |  —  |    —     |  40   |
| `VOICE`       | accumulating |  ✓  |  ✓  |    —     |  45   |
| `APPEARANCE`  | accumulating |  ✓  |  ✓  |    —     |  50   |
| `APPAREL`     | accumulating |  ✓  |  ✓  |    —     |  60   |
| `ENVIRONMENT` | accumulating |  ✓  |  ✓  |    —     |  70   |
| `SCENARIO`    | singular     |  —  |  ✓  |    —     |  80   |
| `PROMPT`      | accumulating |  ✓  |  ✓  |    —     |  90   |
| `IMAGE`       | singular     |  ✓  |  ✓  | optional |  100  |
| `LORA`        | singular     |  —  |  ✓  | required |  110  |
| `DETAILER`    | singular     |  ✓  |  ✓  | required |  120  |
| `CHAT`        | singular     |  —  |  ✓  |    —     |  130  |
| `KNOWLEDGE`   | singular     |  —  |  ✓  | required |  140  |

**Order** is the canonical ascending order — it makes output depend on the declaration rather than on file order, and it is what `evoke inspect` prints. It is **not** the image prompt order: that leads with `IMAGE` text, then `PROMPT`, then `APPEARANCE`, and position there is weight. For both, see [Style Guide](style-guide.md) §1.

Notes on the non-obvious ones:

- `CHARACTER` is **positive only**. An identity has no meaningful "not this"; contradictions go in `!PERSONALITY`. Drawable detail goes in `APPEARANCE`.
- `VOICE` is read by **no target yet** — see [Style Guide](style-guide.md) §1. It parses, merges, and inspects like any other block; nothing renders it. Write it only when asked for it, and never to carry material that belongs in `PERSONALITY`.
- `SCENARIO` is **singular** — only one file in a composition may supply it.
- `PROMPT` is the **shot composition** channel — subject count, framing, and what the subject is doing. A character file carries it as a `?` default so it renders coherently alone; a shot file states it explicitly and replaces that default whole. See [Style Guide](style-guide.md) §3.10.
- `APPAREL` is deliberately broad (no `OUTFIT`/`FOOTWEAR`); `ENVIRONMENT` carries the whole scene/setting role (no `LOCATION`).

## Structured declarations

`IMAGE`, `LORA`, `DETAILER`, `CHAT`, `KNOWLEDGE`. Singular **per argument**, values a mix of `key = value` settings and free prompt text where the declaration accepts it. They do **not** follow the all-or-nothing singular rule — every block contributing to the same declaration and argument merges **setting by setting**:

The character file carries the canonical configuration:

```text
DETAILER face
    clear visible irises, defined iris ring
    detector = bbox/face_yolov8m.pt
    guide_size = 1024
    denoise = 0.3
    max_detection = 1
```

The shot file changes one setting and inherits the rest:

```text
DETAILER face
    max_detection = 2
```

→ `evoke image character shot` gives `max_detection = 2` with `detector`, `guide_size`, `denoise`, and the text inherited. **A block only names what it changes.**

Settings resolve in two passes, last writer winning within each:

1. every `?` **default** block, in argument order;
2. every **explicit** block, in argument order, overriding the defaults.

An explicit setting outranks a default regardless of file order; between two blocks of the same kind, the later argument wins. Two files setting the same key is layering, not a conflict — it does not warn. **This is the only place argument order deliberately decides an outcome**; singular declarations still conflict order-independently.

This is the ordinary `?` rule at setting granularity, not an exception to it — a key the explicit block never named was never contested, so the default still applies.

Not per-setting:

- **Prompt text is a channel, not a key.** Any explicit text suppresses *all* default text, and text accumulates (exact-dedup) across the blocks in the winning group. Two explicit blocks that both add text both contribute; an explicit block that writes only settings contests no text, so the default's text survives.
- **`lora =` accumulates** across every block instead of overriding.
- **The negative channel accumulates from every contribution**, defaults included, and an explicit positive block never suppresses it.
- **`disabled = true`** switches a resolved stage off — see below.

`?` marks a structured block as the canonical-but-yielding configuration: the one a more specific file is expected to tune, and whose prompt text steps aside when another file writes its own. It is not *required* for overridability — a later explicit block tunes an earlier explicit one just as well. If an override isn't taking effect, check argument order first: the file you expect to win must come later.

### IMAGE

The argument names the pipeline stage; an unnamed `IMAGE` is base generation. Prompt text on an `IMAGE` block lands at the **front** of the prompt — right for quality anchors, wrong for eye color.

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

Settings: `base`, `checkpoint`, `group`, `steps`, `cfg`, `sampler_name`, `scheduler`, `width`, `height`, `denoise`, `disabled`. Split-architecture bases load the diffusion model, text encoder, and VAE as three files instead of a checkpoint: `unet`, `clip`, `clip_type`, `vae`, `weight_dtype`, `shift`, and — where the base supports it — `nag_scale`, `nag_alpha`, `nag_tau`. For `IMAGE upscale`: `upscale_model`, `factor`, `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `tile_width`, `tile_height`, `disabled`. For `IMAGE edit`: `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `disabled` — no `width` or `height`, since the source image dictates the size. For `IMAGE paint`: `unet`, `clip`, `clip_type`, `vae`, `weight_dtype`, `shift`, `cfg_norm`, `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `disabled`.

`base` on the unnamed stage names the model architecture to render — `sdxl` (the default) or `anima`. It selects which node graph the composition compiles into and which defaults it inherits, and each architecture reads only its own model settings, so a `checkpoint` means nothing under `anima`. Set it in the pipeline file that supplies the model files. A `base` on `IMAGE upscale`, `IMAGE edit`, or `IMAGE paint` is ignored. `paint` renders one architecture and nothing selects it: an instruction-edit model has nothing to do with the one that generated the image, so the composition's `base` would be answering a different question.

`group` on the base `IMAGE` stage nests the output directory under a shared one: images land in `<group>/<NAME>/` instead of `<NAME>/`. It layers like any other setting, so a collection file holding only a `group` shelves every render it takes part in, and leaving that file out of the command restores the normal path.

### LORA

Argument is the reference name. Settings: `model` (required, the filename), `base`, `strength` (default 1.0), `clip` (default 1.0).

```text
LORA rimix
    model = rimix_v2.safetensors
    strength = 0.8
    clip = 0.8
```

`base` declares the architecture the weights were trained against, and means exactly what it does on `IMAGE` — including its default. LoRA weights cannot load into a different base model, so a `LORA` whose `base` does not match the composition's is **skipped**, not an error: one file can carry a variant per architecture and stay composable with either pipeline. Omitting it selects `sdxl`, so an untagged `LORA` is an SDXL LoRA and is skipped under any other base. Set `base` only to name an architecture that is *not* the default.

### DETAILER

Argument is the body region: `face`, `eye`, `upper_body`, `lower_body`, `hand`. Accepts prompt text alongside settings, and supports `!`.

```text
?DETAILER face
    clear visible irises, defined iris ring
    detector = bbox/face_yolov8m.pt
    guide_size = 1024
    steps = 15
    denoise = 0.3

!DETAILER face
    empty eyes, missing iris
```

Settings: `detector`, `guide_size`, `max_size`, `steps`, `cfg`, `sampler_name`, `scheduler`, `denoise`, `feather`, `bbox_threshold`, `bbox_dilation`, `bbox_crop_factor`, `noise_mask_feather`, `drop_size`, `max_detection`, `disabled`.

### CHAT

Chat-only, no argument. Free-text lines become extra system instructions — write them in second person (see [Style Guide](style-guide.md) §4.7).

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

Settings: `backend` (`llama.cpp` or `mlx`), `model`, `context_window`, `gpu_layers`, `max_output_tokens`, `safety_margin`, `min_recent_turns`, `temperature`, `top_p`, `repeat_penalty`, `seed`, `stop`, `thinking`.

`model` is a GGUF **file name**, like a `checkpoint` — resolved against the model directories in trusted local settings, so the file names the model rather than a machine path and stays portable.

### KNOWLEDGE

Chat-only. Argument names the retrieval source. Settings: `db` (required), `top_k`, `embed_model`.

```text
KNOWLEDGE lore
    db = lore.db
    top_k = 5
```

`db` is a SQLite file **name**, resolved the same way as `CHAT` `model`. Omit `embed_model` normally — the database records the model it was built with and chat adopts it; setting a conflicting one is an error rather than a silent drop in retrieval quality.

### Disabling a stage

`IMAGE`, `LORA`, and `DETAILER` accept `disabled`, so a composition can switch off a pass another file supplied:

A `no-upscale.evoke` holding only this is enough:

```text
IMAGE upscale
    disabled = true
```

| Declaration            | Effect                                                                                                          |
| :--------------------- | :-------------------------------------------------------------------------------------------------------------- |
| `IMAGE edit`           | `evoke edit` falls back to the architecture's built-in edit defaults                                             |
| `IMAGE paint`          | `evoke paint` falls back to its architecture's built-in defaults                                                 |
| `IMAGE upscale`        | upscale pass skipped                                                                                            |
| `DETAILER <region>`    | that region's inpaint pass skipped                                                                              |
| `LORA <name>`          | dropped from the chain; references resolve to nothing                                                            |
| `IMAGE` (unnamed base) | **not** a way to skip generation — only ignores the base stage's own settings and text. Rarely what you want.    |

- **Exactly `true` disables.** `TRUE`, `1`, `yes` leave the stage on, with no warning.
- **It layers like any setting**, so a later argument can set `disabled = false` to switch a stage back on; last file naming the key wins.
- **`evoke inspect` prints a disabled block with a leading `!`** (`!DETAILER face`) — the fastest way to confirm what a composition turned off.
- `CHAT` and `KNOWLEDGE` don't support it; `evoke chat` reports it as an ignored unknown setting.

Two other ways a stage ends up off, both worth checking when a detailer never ran:

- **An explicit negative block with no positive contribution anywhere** disables the stage — `!DETAILER hand` alone means "no hand detailer exists." A `?DETAILER hand` default plus `!DETAILER hand` means "the default config with this negative prompt," not "disable."
- **The generator disables what it cannot configure**: a detailer with no resolved `detector`, or an upscale with no `upscale_model`. Settings never switch a stage *on* — some file must supply the detector or model, so setting only `max_detection` for a region nothing has configured changes nothing.
