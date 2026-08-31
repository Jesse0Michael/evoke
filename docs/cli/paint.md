# evoke paint

Alter an existing image by instruction. Input resolution is identical to [`evoke image`](image.md) — the same selectors, local paths, registry references, literal prompts, and batching — but the source image goes into the model's *text encoder* as vision tokens plus a reference latent, and the prompt is an instruction rather than a description.

```console
$ evoke paint -i <image> <input>...
```

```console
$ evoke paint -i ~/output/sumi/ill_sumi_01.png "change her shirt to blue"
```

## paint versus edit

[`evoke edit`](edit.md) is a **redraw**: the source becomes the starting latent and the whole frame is resampled at partial denoise. It cannot make a small change — turn the denoise low enough to preserve the face and the change does not take; turn it high enough for the change to take and the face goes with it.

`paint` is an **instruction edit**. Minimal difference from the source is the model's training objective, not a side effect of partial denoise, so a targeted change is what it is built to do. Sampling runs from `denoise = 1.0` and the source is preserved by the reference latent instead.

They read different declarations, use different models, and neither is a special case of the other. Which one you want depends on whether you are changing a thing or reworking a frame.

## The prompt is an instruction

`paint` reads **`PROMPT`** and the text on its own `IMAGE paint` stage. It does **not** read `APPEARANCE`, `APPAREL`, or `ENVIRONMENT` — describing the whole subject to a model whose job is minimal change is what makes it change more than you asked. Write what should be different, not what the picture contains:

```console
$ evoke paint -i shot.png "give her a leather jacket"
$ evoke paint -i shot.png "make it night, keep everything else the same"
```

Since a literal prompt lands in `PROMPT`, a character file adds nothing to a paint run. The command needs no `.evoke` file at all beyond the instruction.

## The paint stage

`IMAGE paint` configures the model. It shares nothing with the unnamed `IMAGE` stage and nothing with `IMAGE edit`: the architecture that alters an image has no relationship to the one that generated it, so it names its own `unet`, `clip`, and `vae`.

```text
?IMAGE paint
    unet = qwen_image_edit_2509_fp8_e4m3fn.safetensors
    clip = qwen_2.5_vl_7b_fp8_scaled.safetensors
    vae = qwen_image_vae.safetensors
    steps = 20
    cfg = 4.0
```

Every one of those is also the built-in default, so the block is only needed to change something. See [`IMAGE`](../file-format/declarations.md#image) for the full setting list.

`IMAGE paint` may reference LoRAs, which load model-only. The step-distilled Lightning LoRAs are the ones worth having — they take the sampler from 20 steps at cfg 4 down to 4 steps at cfg 1:

```text
LORA qwen-lightning
    base = qwen
    model = Qwen-Image-Edit-2509-Lightning-4steps-V1.0-bf16.safetensors

?IMAGE paint
    lora = qwen-lightning
    steps = 4
    cfg = 1.0
```

`base = qwen` is required on that block. A LoRA trained against SDXL or Anima cannot load into this architecture, so anything not built for it is skipped and reported.

## Required models

Three files, none of which ship with ComfyUI:

| File | Directory |
|:-----|:----------|
| `qwen_image_edit_2509_fp8_e4m3fn.safetensors` | `models/diffusion_models` |
| `qwen_2.5_vl_7b_fp8_scaled.safetensors` | `models/text_encoders` |
| `qwen_image_vae.safetensors` | `models/vae` |

The VAE is the same one Anima uses. The text encoder is **not** — Anima's `qwen_3_06b_base` is a much smaller distilled encoder, and the instruction path needs the full vision-language model for the image tokens.

## What paint does not run

No upscale pass and no detailers. Feed the result back through [`evoke edit`](edit.md) at a low denoise if you want the generating pipeline's native texture back — the instruction model preserves the source but renders it in its own hand.

## From the viewer

[`evoke view`](index.md) binds `p` to this command and `e` to `evoke edit`, so both are reachable from the frame you are looking at. Type the inputs you would have passed after `-i`; for paint that is usually just the instruction:

```text
paint ▸ change her shirt to blue
```

## Flags

| Flag | Default | Description |
|:-----|:--------|:------------|
| `-i`, `--input` | — | **Required.** Path to the source image. |
| `-b` | `1` | Number of images to generate. Behaves exactly as in [`evoke image`](image.md#batch-mode), including the `xN` and `xall` forms. |
| `-v`, `--verbose` | `false` | Print the merged composition and the ComfyUI request payload. |

## Environment variables

| Variable | Default | Description |
|:---------|:--------|:------------|
| `COMFY_URL` | `http://127.0.0.1:8188` | ComfyUI server URL |

## Exit codes

| Code | Meaning |
|:----:|:--------|
| `0` | Composition submitted successfully. |
| `1` | Upload, resolution, parse, validation, or generation error. |
| `2` | Usage error (no `-i`, or no inputs given). |
