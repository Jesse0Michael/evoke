# Evoke

A declarative, composable source format and CLI for AI characters, scenes, prompts, and generative assets.

The core idea: **prompts are compiled artifacts, not source material.** Instead of maintaining one large image prompt or character card, you author small reusable `.evoke` files of structured declarations. The CLI merges any selection of them — by tag-based selectors, local paths, or registry references — resolves channels and defaults, and sends the result through a generation pipeline (currently ComfyUI).

## Documentation

📖 **[`docs/`](docs/index.md)** — [file format](docs/file-format/index.md), [CLI reference](docs/cli/index.md), and [design principles](docs/design/index.md).

The docs are plain markdown with no front matter and no theme-specific markup, so they read the same in an editor, on GitHub, or rendered. `docs/mkdocs.yml` supplies titles and ordering for an optional local preview (`make docs`); there is no build step, no CI, and no committed site output.

✍️ **[`skills/evoke-authoring/`](skills/evoke-authoring/SKILL.md)** — how to *write* `.evoke` content, packaged as a Claude Code skill: the [style guide](skills/evoke-authoring/references/style-guide.md) and a condensed [format reference](skills/evoke-authoring/references/file-format.md). Both are ordinary markdown worth reading directly. To use it in a repo of `.evoke` files:

```bash
claude
> /plugin marketplace add jesse0michael/evoke
> /plugin install evoke
```

## Quick start

```console
$ go install ./cmd/evoke
$ evoke image character shot
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
    an octopus humanoid, and the mascot of this project

APPEARANCE
    octopus humanoid
    small round body
    smooth violet skin
    eight tapering tentacles
    large luminous eyes

?APPAREL
    little teal explorer's vest
```

The `image` command resolves inputs — tag-based selectors like `character`, local file paths, or registry references like `@namespace/name` — merges the matching `.evoke` files, and submits the composition to ComfyUI for image generation.

See [Getting Started](docs/getting-started.md) for more, and the [Style Guide](skills/evoke-authoring/references/style-guide.md) for how to write the content of the blocks.

## Status

Experimental. The parser, declaration schema, merge/resolver, tag-based selector system, local file index, registry client, and the `image` pipeline are implemented. The hosted registry API is functional. See the [documentation](docs/index.md) for details.
