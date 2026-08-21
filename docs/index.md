# Evoke

A declarative, composable source format and CLI for AI characters, scenes, prompts, and generative assets.

[Get started](getting-started.md) · [View on GitHub](https://github.com/jesse0michael/evoke)

---

## Prompts are compiled artifacts, not source material

Instead of maintaining one large image prompt or character card, you author small reusable `.evoke` files of structured declarations. The CLI merges any selection of them — resolved by tag-based selectors, local paths, or registry references — and sends the composition through a generation pipeline.

A character file, `sumi.evoke`:

```text
TAGS
    character

NAME
    Sumi

CHARACTER
    Sumi is an octopus humanoid and the mascot of the Evoke project.

?PROMPT
    1other, solo

APPEARANCE
    (smooth violet skin:1.25)
    octopus humanoid, small round body, large luminous eyes

?APPAREL
    teal explorer vest
```

An apparel file, `winter-coat.evoke`:

```text
TAGS
    apparel, winter

APPAREL
    heavy green winter coat, black boots
```

```console
$ evoke image character winter
```

The `image` command resolves the selectors `character` and `winter` against your indexed `.evoke` files by tag, merges the matched documents, and submits the composition to ComfyUI. The default `?APPAREL` from the character steps aside for the explicit winter coat — because the `?` default is only used when nothing more specific is selected.

## What makes it different

- **Files are typeless.** A `.evoke` file never declares that it's a "character" or "location" file. Its meaning emerges from the declarations it contains.
- **Composition is external.** Files don't import each other — the caller picks which files compose together via selectors, paths, or registry references.
- **Nothing is concatenated early.** Merging works on structured declarations; flattening to a prompt string happens last.

Read the [Design](design/index.md) section for the full reasoning.

## Status

**Experimental.** The parser, declaration schema, merge/resolver, tag-based selector system, local SQLite file index, registry client, and the `image` pipeline are all implemented. The hosted registry API is functional.

## Where to go next

| Section | What's there |
|:--------|:-------------|
| [Getting Started](getting-started.md) | Build the CLI, write your first files, and run `image` |
| [File Format](file-format/index.md) | Syntax, declarations, prefixes, merge modes |
| `skills/evoke-authoring/` | What to put *in* the blocks: rendering vs. language targets, weights, file design. Lives outside `docs/` because it is packaged as a Claude Code skill, but it is plain markdown — read `references/style-guide.md` directly |
| [CLI](cli/index.md) | Command reference for `image`, `chat`, `knowledge`, and `settings` |
| [Design](design/index.md) | The project brief: principles, resolution model, and architecture |
