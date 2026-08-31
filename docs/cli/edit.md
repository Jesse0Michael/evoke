# evoke edit

Redraw an existing image through a composition. Everything about input resolution is identical to [`evoke image`](image.md) — the same selectors, local paths, registry references, literal prompts, and batching — but instead of sampling from noise, the workflow encodes a source image and resamples it at partial strength.

```console
$ evoke edit -i <image> <input>...
```

```console
$ evoke edit -i ~/output/sumi/ill_sumi_01.png sumi ill "leather jacket, rain"
```

## What an edit is

This is a redraw, not an instruction edit — see [`evoke paint`](paint.md) for the command that makes targeted changes. An edit is a **redraw**: the source image becomes the starting latent, and the prompt is still the whole composition rather than a sentence describing a change. To put the character in a leather jacket you compose a file (or a literal prompt) that says `leather jacket` — the same thing you would compose for `evoke image` — and the source image constrains the pose, framing, and colour blocking that the model draws it onto.

How much survives is the `denoise` setting on the [`IMAGE edit`](../file-format/declarations.md#image) stage. That is the whole dial, and it belongs in the pipeline file next to the sampler it is a variant of:

```text
?IMAGE edit
    steps = 30
    cfg = 4
    sampler_name = euler_ancestral
    scheduler = karras
    denoise = 0.45
```

Low values touch up, high values reinterpret. At `denoise = 1.0` nothing of the source survives and the command is a slower `evoke image`.

There is deliberately no flag for it. A pipeline file is what knows how its model behaves at partial denoise, the same way it knows its sampler and its cfg.

## The edit stage

`IMAGE edit` is a complete sampler spec, exactly like `IMAGE upscale` — it does not adjust the base stage's settings, it replaces them. The base stage samples from noise at `denoise = 1.0` and has to keep doing so for the same pipeline file to still serve `evoke image`, so an edit that inherited it would discard the source image.

What the edit stage does **not** carry is `width` and `height`. The source image dictates the output size, whatever it is.

Everything else about the composition still applies. The unnamed `IMAGE` stage supplies the model files, the quality text, and the negative prompt; `LORA` blocks resolve into the chain under the same base guard; `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, and `PROMPT` render in the usual order.

## What an edit does not run

No upscale pass and no detailers, even when the pipeline file configures them. An edit runs the one pass it was asked for, and the result can be fed straight back in for another. Composing an upscale into an edit would silently double the cost of every invocation, and a detailer pass over a region the edit just changed is a second opinion, not a refinement.

## Bases

The composition picks the architecture the same way it does for `evoke image` — the `base` setting on the unnamed `IMAGE` stage — and the edit workflow for that architecture is what renders. `sdxl` and `anima` both have one. A base with no edit workflow is an error that lists what is available.

## The source image

`-i` takes an ordinary path to an image file. It is uploaded to the backend once per command and reused across every generation in the batch, under a name derived from its content, so editing the same source repeatedly does not accumulate copies.

## From the viewer

[`evoke view`](index.md) binds `e` to this command and `p` to [`evoke paint`](paint.md). Either opens an input line on the controls row; type the inputs you would have passed after `-i` and press Enter, and the displayed image becomes the source. Escape cancels, and an empty line does nothing.

The line is split the way a shell would split it, so quote a prompt to keep it as one argument:

```text
edit ▸ sumi ill "leather jacket, rain"
```

Whatever the command reports lands in the status bar — the queue confirmation in green, a failure in red.

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
