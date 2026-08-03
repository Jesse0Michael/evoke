# Getting Started

## Build the CLI

Evoke is a single Go binary. From a clone of the repository:

```console
$ go install ./cmd/evoke
```

That installs `evoke` to your `GOPATH/bin`. Everything below assumes it's on your path.

Commands available:

- `evoke image` — compose files by selector, path, or registry reference and generate images
- `evoke chat` — compose files into a character and start an interactive chat
- `evoke login` — sign in to the registry
- `evoke settings` — manage user settings (source paths)

See the [CLI reference](cli/index.md) for details.

## Write your first file

A `.evoke` file is a list of **declaration blocks**. A declaration name sits at the start of a line; its values are indented beneath it. Files also declare `TAGS` for discovery by the selector system.

Create `sumi.evoke`:

```text
# Sumi — our octopus mascot

TAGS
    character
    mascot
    octopus

NAME
    Sumi

CHARACTER
    an octopus humanoid, and the mascot of this project

PERSONALITY
    curious
    playful
    endlessly helpful

APPEARANCE
    octopus humanoid
    small round body
    smooth violet skin
    eight tapering tentacles
    large luminous eyes

?APPAREL
    little teal explorer's vest
```

A file does not have to be complete. A file with only an `APPAREL` block is just as valid as a full character — validation only rejects declarations that are *illegal*, never files that are *partial*.

## Compose files

The whole point of Evoke is that small files combine. You never reference one file from another; instead the caller selects files to compose via selectors, paths, or registry references. Add an apparel file:

```text
# winter-coat.evoke
TAGS
    apparel
    winter
    cold-weather

APPAREL
    heavy green winter coat
    black boots

!APPAREL
    sandals
    short sleeves
```

The `!` prefix routes those values to the **negative** channel (things to exclude — for an image target, a negative prompt). See [Prefixes & Channels](file-format/prefixes.md).

## Generate an image

Set up source paths so the index knows where your files are:

```console
$ evoke settings set path ~/my-evoke-files
```

Then compose and generate:

```console
$ evoke image character winter
```

The `image` command resolves each argument as a selector (matching files by tag), a local file path, or a registry reference (`@namespace/name`). It merges the matched documents — applying default suppression, conflict detection, and dedup — and submits the composition to ComfyUI.

In the example above, `character` matches `sumi.evoke` (it has the `character` tag) and `winter` matches `winter-coat.evoke`. The explicit `APPAREL` from the winter coat suppresses the default `?APPAREL` from the character file.

## Learn the format

- [Syntax](file-format/syntax.md) — the exact line-oriented rules the parser enforces
- [Declarations](file-format/declarations.md) — the nine built-in declarations
- [Prefixes & Channels](file-format/prefixes.md) — what `!` and `?` mean
- [Merge Modes](file-format/merge-modes.md) — how repeated values combine
