---
title: CLI Vision
parent: Design
nav_order: 4
---

# CLI Vision
{: .no_toc }

The full command surface Evoke is being built toward. Only [`validate`](../cli/validate) and [`declarations`](../cli/declarations) exist today; the rest are described here as design.

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

## `evoke compile`
{: .d-inline-block }

Planned
{: .label .label-yellow }

Load, validate, merge, and render selected files to a target.

```console
$ evoke compile sumi.evoke winter-coat.evoke pine-forest.evoke --target prompt
```

Suggested options: `--target`, `--output`, `--format`, `--strict`, `--show-sources`.

```console
$ evoke compile files... --target resolved-json
$ evoke compile files... --target system-prompt
$ evoke compile files... --target prompt
```

`compile` produces output only. It must stay distinct from any future command that calls an external image or LLM backend.

## `evoke explain`
{: .d-inline-block }

Planned
{: .label .label-yellow }

Show how declarations resolved and where each value came from — explicit values, selected defaults, ignored defaults, deduplicated values, conflicts, and source file/line. See the [`explain` target](targets#explain).

```console
$ evoke explain files...
```

## `evoke fmt`
{: .d-inline-block }

Planned
{: .label .label-yellow }

Format `.evoke` source into canonical style (uppercase names, consistent indentation). Postponed until the parser is stable.

```console
$ evoke fmt sumi.evoke
```

## Future runtime commands

These are possibilities, and must not block the compiler. The line they must not cross: `compile` only ever produces output, while these may call configured external backends.

```console
$ evoke generate   # may call a configured image-generation backend
$ evoke chat
$ evoke run
$ evoke render
```

## Future registry commands

Covered in [Registry & Roadmap](registry):

```console
$ evoke search sumi
$ evoke pull characters/sumi@3
$ evoke push ./sumi.evoke
$ evoke publish ./sumi.evoke
$ evoke inspect characters/sumi@3
```
