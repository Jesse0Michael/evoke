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

Every declaration has a registered definition that fixes its **merge mode**, whether it supports the **`!` negative channel**, whether it supports the **`?` default**, and its canonical **render order**. The twelve declarations below are the implemented set.

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

- **Merge** — how repeated contributions combine. See [Merge Modes](merge-modes).
- **`!` negative** — whether values may be routed to the exclusion channel. See [Prefixes & Channels](prefixes).
- **`?` default** — whether values may be marked as defaults used only when nothing more specific exists.
- **Order** — the canonical, ascending order renderers use so output is deterministic rather than dependent on file order.

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

Behavioral tendencies and traits. **Accumulating**; supports the negative channel and defaults.

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

Canonical background information. **Accumulating**, positive only.

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

The current narrative or conversational situation. **Singular** (one active value); supports defaults, positive only.

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

## What isn't here

The twelve declarations above are the complete set. Using any other name is an *unknown declaration* validation error. Namespaced/dotted extension names (`FOO.BAR`) are also rejected.
