# Evoke

A declarative, composable source format and compiler for AI characters, scenes, prompts, and agent configurations.

The core idea: **prompts are compiled artifacts, not source material.** Instead of maintaining one large image prompt, system prompt, or character card, you author small reusable `.evoke` files of structured declarations, and a compiler merges any selection of them into a neutral resolved representation, then renders that into a target format.

## Documentation

📖 **[jesse0michael.github.io/evoke](https://jesse0michael.github.io/evoke)** — file format, CLI reference, and the full design brief.

The docs live in [`docs/`](docs/) and are published with GitHub Pages (Just the Docs theme, native Jekyll build — no build step).

## Quick start

```console
$ go build -o bin/evoke ./cmd/cli
$ ./bin/evoke validate examples/sumi.evoke
examples/sumi.evoke: valid
$ ./bin/evoke declarations
```

A `.evoke` file is a list of declaration blocks:

```text
NAME
    Sumi

APPEARANCE
    small round body
    smooth violet skin
    eight tapering tentacles
    large luminous eyes

?APPAREL
    little teal explorer's vest
```

See [Getting Started](https://jesse0michael.github.io/evoke/getting-started) for more.

## Status

Experimental. Milestones 1–2 are done: the parser, declaration schema, and per-file validation are implemented and wired into `evoke validate` and `evoke declarations`. The resolver and rendering targets are next — see the [roadmap](https://jesse0michael.github.io/evoke/design/registry#roadmap).
