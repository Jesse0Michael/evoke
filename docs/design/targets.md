---
title: Targets
parent: Design
nav_order: 3
---

# Rendering Targets
{: .no_toc }

Planned
{: .label .label-yellow }

## Table of contents
{: .no_toc .text-delta }

1. TOC
{:toc}

---

A target renderer takes the [resolved document](resolution) and turns it into one concrete output format. Renderers sit behind a common interface and use declaration-defined ordering, never map or file order, so output is deterministic.

```go
type Renderer interface {
    Name() string
    Validate(ResolvedDocument) []Diagnostic
    Render(context.Context, ResolvedDocument) ([]byte, error)
}
```

## `resolved-json`

The neutral resolved representation, serialized. Meant to be implemented **first**, because it makes parser and merge behavior directly observable — the primary debugging surface.

```console
$ evoke compile files... --target resolved-json
```

## `prompt`

A deterministic positive and negative image prompt.

```console
$ evoke compile sumi.evoke winter-coat.evoke pine-forest.evoke --target prompt
```

```text
Positive:
Sumi, small, round, violet skin, glowing speckles, heavy green winter coat,
black boots, pine forest, morning mist

Negative:
scary, slimy, monstrous, sandals, short sleeves
```

Ordering follows declaration render order: name/identity → appearance → apparel → pose → location → environment → lighting → style → generic prompt.

## `system-prompt`

One coherent LLM system prompt, assembled from the agent-facing declarations — identity, personality, speech, scenario, objectives, rules, boundaries.

```text
You are Ashley.

Identity:
Ashley is an adult emergency-room nurse who grew up in Wisconsin.

Personality:
She is warm, competent, and easily flustered by direct flirting.

Scenario:
Ashley is checking the user's temperature after they arrived with a fever.

Behavior:
- Remain in character.
- Do not narrate the user's dialogue.
- Ask one question at a time.
```

Intended to be copy-pasteable straight into an existing chat tool.

## `agent-json`

A structured agent package — system prompt, developer prompt, initial messages, example messages, metadata.

```json
{
  "system_prompt": "...",
  "developer_prompt": "...",
  "initial_messages": [
    { "role": "assistant", "content": "Good morning. My name is Ashley, and I'll be your nurse today." }
  ],
  "example_messages": [],
  "metadata": { "name": "Ashley" }
}
```

Provider-specific payloads (OpenAI, Anthropic, ChatML, Ollama, Character Card V2/V3) are handled by adapters on top of this neutral shape.

## `explain`

A provenance-oriented view: what resolved, what was deduplicated, and — importantly — which defaults were *ignored* and why.

```console
$ evoke explain sumi.evoke winter-coat.evoke pine-forest.evoke
```

```text
APPAREL

  heavy green winter coat
    source: winter-coat.evoke:2

  black boots
    source: winter-coat.evoke:3

Ignored defaults:

  green shirt
    source: sumi.evoke:14

  blue jeans
    source: sumi.evoke:15
```

This target depends on the provenance model that is [deferred in the MVP](principles#no-provenance-in-the-mvp).
