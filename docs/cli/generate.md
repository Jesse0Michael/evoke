---
title: evoke generate
parent: CLI
nav_order: 1
---

# evoke generate

Compose `.evoke` files by selector, local path, or registry reference, merge them, and submit the result to a generation pipeline (currently ComfyUI).

```console
$ evoke generate <input>...
```

## Input types

Each positional argument is classified as one of four input types:

| Type | Pattern | Example |
|:-----|:--------|:--------|
| **Selector** | Tag or facet:tag expression | `character`, `c:nurse+modern` |
| **Local path** | Starts with `./`, `../`, is absolute, or ends in `.evoke` | `./sumi.evoke` |
| **Registry reference** | Starts with `@` | `@jesse/sumi` |
| **Literal prompt** | Contains spaces | `"a scientist in a lab"` |

### Selectors

A selector matches files from the local index by tag. Tags are declared in the `TAGS` block of each `.evoke` file.

Simple tag selectors:
```console
$ evoke generate character winter
```

Facet-qualified selectors restrict matches to files that provide a specific declaration:
```console
$ evoke generate c:nurse+modern e:forest
```

Facet aliases: `c` = CHARACTER, `ap` = APPEARANCE, `a` = APPAREL, `e` = ENVIRONMENT, `p` = PROMPT.

Multiple tags joined with `+` require all tags to be present.

### Local paths

Direct file references bypass the index:
```console
$ evoke generate ./sumi.evoke ./winter-coat.evoke
```

### Registry references

Pull files from the hosted registry:
```console
$ evoke generate @jesse/sumi @jesse/winter-coat
```

Registry references are cached locally in `~/.evoke/library/` and tracked in a manifest file.

### Literal prompts

Any argument containing spaces is treated as a literal prompt string and added directly to the PROMPT declaration in the composition. Use shell quoting to pass multi-word strings:

```console
$ evoke generate sumi.evoke "a female scientist in a science lab"
```

This merges the `sumi.evoke` file with the literal text appended to the positive prompt. Literal prompts compose with file-based PROMPT declarations — they accumulate just like any other PROMPT contribution.

## Selector resolution

Before selectors can be used, source paths must be configured and indexed:

```console
$ evoke settings set path ~/my-evoke-files
$ evoke index
```

The index is a SQLite database at `~/.evoke/index.db` that stores tags and declarations for fast selector matching.

## Flags

| Flag | Default | Description |
|:-----|:--------|:------------|
| `-b` | `1` | Number of images to generate. Each iteration re-resolves selectors independently, so when multiple files match a tag, each generation randomly picks one for variety. |
| `-v`, `--verbose` | `false` | Print the merged composition and ComfyUI request payload. |

## Batch mode

Use `-b` to trigger multiple generations from the same set of inputs:

```console
$ evoke generate -b 5 anime character formal
```

Each of the 5 generations independently resolves selector inputs. When a selector matches multiple files, a different random pick is made each time — so you get variety across the batch rather than 5 identical images.

Static inputs (local paths, registry references, and literal prompts) are resolved once and shared across all iterations.

## Environment variables

| Variable | Default | Description |
|:---------|:--------|:------------|
| `COMFY_URL` | `http://127.0.0.1:8188` | ComfyUI server URL |

## Output

The command prints which files were selected for each input, then submits the merged composition to ComfyUI:

```console
$ evoke generate character winter
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
