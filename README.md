# Evoke

A declarative, composable source format and CLI for AI characters, scenes, prompts, and generative assets.

The core idea: **prompts are compiled artifacts, not source material.** Instead of maintaining one large image prompt or character card, you author small reusable `.evoke` files of structured declarations. The CLI merges any selection of them — by tag-based selectors, local paths, or registry references — resolves channels and defaults, and sends the result through a generation pipeline (currently ComfyUI).

## Documentation

📖 **[jesse0michael.github.io/evoke](https://jesse0michael.github.io/evoke)** — file format, CLI reference, and design principles.

The docs live in [`docs/`](docs/) and are published with GitHub Pages (Just the Docs theme, native Jekyll build — no build step).

## Quick start

```console
$ go install ./cmd/evoke
$ evoke generate character shot
```

A `.evoke` file is a list of declaration blocks:

```text
TAGS
    character
    mascot
    octopus

NAME
    Sumi

CHARACTER
    octopus humanoid mascot

APPEARANCE
    small round body
    smooth violet skin
    eight tapering tentacles
    large luminous eyes

?APPAREL
    little teal explorer's vest
```

The `generate` command resolves inputs — tag-based selectors like `character`, local file paths, or registry references like `@namespace/name` — merges the matching `.evoke` files, and submits the composition to ComfyUI for image generation.

See [Getting Started](https://jesse0michael.github.io/evoke/getting-started) for more.

## Status

Experimental. The parser, declaration schema, merge/resolver, tag-based selector system, local file index, registry client, and the `generate` pipeline are implemented. The hosted registry API is functional. See the [documentation](https://jesse0michael.github.io/evoke) for details.
