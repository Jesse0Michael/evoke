# evoke image

Compose `.evoke` files by selector, local path, or registry reference, merge them, and submit the result to a generation pipeline (currently ComfyUI).

```console
$ evoke image <input>...
```

## Input types

Each positional argument is classified as one of six input types:

| Type | Pattern | Example |
|:-----|:--------|:--------|
| **Selector** | Tag expression | `character`, `nurse+modern` |
| **Local path** | Starts with `./`, `../`, is absolute, or ends in `.evoke` | `./sumi.evoke` |
| **Registry reference** | Starts with `@` | `@jesse/sumi` |
| **Literal prompt** | Contains spaces | `"a scientist in a lab"` |
| **Batch count** | `x` or `X` followed by a positive integer | `x25` |
| **Enumeration marker** | `xall`, case-insensitive | `xall` |

### Selectors

A selector matches files from the local index by tag. Tags are declared in the `TAGS` block of each `.evoke` file, and the index adds each file's base name as an implicit tag — `sumi.evoke` is reachable as `sumi` whether or not it declares any tags of its own.

Simple tag selectors:
```console
$ evoke image character winter
```

Multiple tags joined with `+` require all tags to be present:
```console
$ evoke image nurse+modern forest
```

### Local paths

Direct file references bypass the index:
```console
$ evoke image ./sumi.evoke ./winter-coat.evoke
```

### Registry references

Pull files from the hosted registry:
```console
$ evoke image @jesse/sumi @jesse/winter-coat
```

Registry references are cached locally in `~/.evoke/library/` and tracked in a manifest file.

### Literal prompts

Any argument containing spaces is treated as a literal prompt string and added directly to the PROMPT declaration in the composition. Use shell quoting to pass multi-word strings:

```console
$ evoke image sumi.evoke "a female scientist in a science lab"
```

This merges the `sumi.evoke` file with the literal text appended to the positive prompt. Literal prompts compose with file-based PROMPT declarations — they accumulate just like any other PROMPT contribution.

## What ends up in the prompt

Only the declarations a diffusion model can render are sent. The merged composition becomes two prompt strings, assembled in this order:

```text
positive:  IMAGE text → APPEARANCE → PROMPT → APPAREL → ENVIRONMENT
negative:  !IMAGE text → !APPEARANCE → !PROMPT → !APPAREL → !ENVIRONMENT
```

`CHARACTER`, `PERSONALITY`, `BACKSTORY`, and `SCENARIO` are **not** included — they carry identity, disposition, history, and narrative situation, which a diffusion model cannot render. They are consumed by [`evoke chat`](chat.md) instead. `VOICE` is not included either, and no command reads it yet — it describes how a subject sounds. `NAME` is used for the output directory, not the prompt — see [Grouping output](../file-format/declarations.md#grouping-output) to shelve several characters under a shared directory. Everything drawable about a subject belongs in `APPEARANCE`.

Order is significant. CLIP processes roughly 75 tokens per chunk and dilutes what comes later, so material near the front of the positive prompt carries more weight than material near the end. `IMAGE` text leads, which makes it the right place for quality tags and the wrong place for character detail.

Use `-v` to print the merged composition and the exact payload submitted.

## Selector resolution

Before selectors can be used, source paths must be configured:

```console
$ evoke settings set path ~/my-evoke-files
```

The file index is refreshed automatically before selector resolution. It is a SQLite database at `~/.evoke/index.db` that stores tags and declarations for fast selector matching.

## Flags

| Flag | Default | Description |
|:-----|:--------|:------------|
| `-b` | `1` | Number of images to generate. Each iteration re-resolves selectors independently, so when multiple files match a tag, each generation randomly picks one for variety. Overridden by an `xN` argument. With `xall` it becomes a per-combination multiplier. |
| `-v`, `--verbose` | `false` | Print the merged composition and ComfyUI request payload. |

## Bases

A **base** is the model architecture a composition targets. Each one is a ComfyUI node graph embedded in the binary, and the composition selects it with the `base` setting on its unnamed [`IMAGE`](../file-format/declarations.md#image) declaration:

```text
IMAGE
    base = anima
    unet = anima-base-v1.0.safetensors
```

| Base | Architecture | Notes |
|:-----|:-------------|:------|
| `sdxl` | SDXL / Illustrious, one checkpoint via `CheckpointLoaderSimple` | The default when no `base` is set. Reads `checkpoint`. `euler_ancestral`/`karras`, cfg 4, 40 steps, 1216x832. |
| `anima` | [Anima](https://huggingface.co/circlestone-labs/Anima), a Qwen-Image derivative loaded as three files | Comfy-Org's `image_anima_base_v1` reference graph. Reads `unet`, `clip`, `clip_type`, `vae`, `weight_dtype`, and `shift` instead of `checkpoint`. `euler`/`simple`, cfg 4, 30 steps. |

There is no flag for this. The pipeline file that supplies the checkpoint or the unet is what makes a composition SDXL or Anima, so switching architecture means composing a different pipeline file — and a flag could only ever contradict what that file already says:

```console
$ evoke image sumi anima
```

Each base carries its own defaults for anything the composition leaves unset. **Bases read different model settings and ignore each other's**: a `checkpoint` means nothing to `anima`, and `unet` means nothing to `sdxl`. Keep the settings for an architecture in the pipeline file that names it, as [`examples/anima.evoke`](https://github.com/jesse0michael/evoke/blob/main/examples/anima.evoke) does.

A base no template implements is an error that lists what is available:

```console
$ evoke image sumi flux-pipeline
evoke image: unknown IMAGE base "flux" (available: anima, sdxl)
```

Base names are matched without regard to case or surrounding whitespace.

The LoRA chain, the upscale pass, the detailers, and the `Image Saver` metadata node behave identically under both.

### LoRAs and bases

LoRA weights are trained against one base model and cannot load into another. A [`LORA`](../file-format/declarations.md#lora) may declare the base it was built for, and one whose base does not match the active one is **skipped** rather than treated as an error:

```text
LORA sumi-illustrious
    model = sumi_illustrious_v3.safetensors
    strength = 0.8

LORA sumi-anima
    base = anima
    model = sumi_anima_v1.safetensors
    strength = 0.7
```

That lets one file carry a variant per architecture and stay composable with either pipeline. `base` defaults the same way here as on `IMAGE`, so the untagged block above is the SDXL variant and is skipped under `anima` — write `base` only for an architecture that is not the default. Skipped LoRAs are reported alongside the ComfyUI response.

### Shift

Anima's model config already samples at **shift 3.0**, so the `shift` setting is an *override*, not an enable. The `anima` base leaves it unset and takes the built-in value.

### Negative guidance at low CFG

`anima` can insert a `NAGuidance` node between the model chain and the sampler. It is off until `nag_scale` is set, so the default graph matches the reference exactly:

```text
IMAGE
    base = anima
    unet = anima-turbo-v1.0.safetensors
    cfg = 1
    steps = 10
    nag_scale = 5.0
```

This matters only on the turbo path. At cfg 1 there is no classifier-free guidance, so the negative prompt is inert — NAG restores it through cross-attention. Above cfg ~2 it is redundant. `nag_alpha` and `nag_tau` default to 0.5 and 1.5.

`anima` requires three model files that are not part of a stock ComfyUI install — `anima-base-v1.0.safetensors` in `models/diffusion_models`, `qwen_3_06b_base.safetensors` in `models/text_encoders`, and `qwen_image_vae.safetensors` in `models/vae`.

## Batch mode

Multiple generations from the same set of inputs, written either as the `-b` flag or as an `xN` argument in the input list. These two are equivalent:

```console
$ evoke image -b 5 anime character formal
$ evoke image anime character formal x5
```

Each of the 5 generations independently resolves selector inputs. When a selector matches multiple files, a different random pick is made each time — so you get variety across the batch rather than 5 identical images.

Static inputs (local paths, registry references, and literal prompts) are resolved once and shared across all iterations.

### The `xN` shorthand

`xN` may appear anywhere among the inputs — it is removed from the list before composing, so `x5 character` and `character x5` behave identically. The `x` is case-insensitive, and the count is **capped at 100**; a larger number is clamped to 100 rather than rejected.

When both forms are given, `xN` wins over `-b`. When several `xN` arguments are given, the last one wins.

`N` must be a positive integer. Anything else after the `x` — `x0`, `x-1`, `xfoo` — is not a batch count and falls through to selector classification, where it is looked up as an ordinary tag. A tag literally named `x5` is therefore unreachable as a bare argument; reference that file by path instead.

## Enumerating every combination

Batch mode *samples* the selectors — each generation makes a fresh random pick. The `xall` argument *enumerates* them instead, generating one image for every combination of matching files:

```console
$ evoke image ill character pose xall
```

If `character` matches 4 files and `pose` matches 3, that is 12 generations covering every pairing. A selector matching a single file contributes one option, so pinning part of the composition costs nothing — `ill` above resolves to one pipeline file and simply appears in all 12.

The order is deterministic: files are sorted by path within each selector, and the leftmost selector varies slowest. The same command over an unchanged corpus produces the same sequence every time.

Selectors are enumerated independently, so a file matching two of them can fill both slots in the same combination.

### `xall` with a batch count

`xall` and `xN` are separate axes and may be combined, in which case `xN` becomes a per-combination multiplier:

```console
$ evoke image ill character pose xall x3
```

That is 3 images of each of the 12 combinations — 36 total.

### The combination limit

The number of combinations is **capped at 100**. Unlike `xN`, which clamps a larger count, exceeding the limit is an error:

```console
$ evoke image character apparel xall
evoke image: xall would generate 480 combinations (character 12 x apparel 40), more than the limit of 100; narrow the selection or drop xall
```

Silently generating a subset would contradict the request, so the per-selector match counts are reported instead — add tags to narrow a slot, or pin one by path.

`xall` is case-insensitive and may appear anywhere among the inputs. It is not a selector: a tag literally named `xall` is unreachable as a bare argument, same as `x5`; reference that file by path instead.

## Environment variables

| Variable | Default | Description |
|:---------|:--------|:------------|
| `COMFY_URL` | `http://127.0.0.1:8188` | ComfyUI server URL |

## Output

The command prints which files were selected for each input, then submits the merged composition to ComfyUI:

```console
$ evoke image character winter
character
  selected: /path/to/sumi.evoke (selector)
winter
  selected: /path/to/winter-coat.evoke (selector)

prompt queued: <prompt-id>
```

## Exit codes

| Code | Meaning |
|:----:|:--------|
| `0` | Composition submitted successfully. |
| `1` | Resolution, parse, validation, or generation error. |
| `2` | Usage error (no inputs given). |
