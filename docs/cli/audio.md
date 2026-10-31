# evoke audio

Speak a line in a composition's voice.

```bash
evoke audio sumi "I didn't think you'd come back. Sit down."
```

`audio` composes `.evoke` files exactly as [`image`](image.md) and [`chat`](chat.md) do — by tag selector, local path, or `@namespace/name` registry reference — merges them, and renders the resulting [`VOICE`](../file-format/declarations.md#voice) description into speech.

For speaking every reply in a conversation rather than one line at a time, see [`evoke chat`'s `/voice`](chat.md#spoken-replies), which reads the same `VOICE`/`AUDIO` declarations through a resident backend instead of this command's one-shot runtime.

## Arguments

Arguments split by what they are. Selectors, paths, and registry references compose the character; **everything else is the line to speak**, joined in order:

```bash
evoke audio sumi "I didn't think you'd come back." "Sit down."
```

The text is runtime input, not a contribution to the composition. This is the one place `audio` departs from `image`, and deliberately: an image literal states its own subject and scene, so it suppresses inherited defaults, while a spoken line says nothing about who is speaking and must leave `VOICE` untouched.

An argument counts as text when it contains a space. A single bare word is read as a tag, so a one-word line needs quoting that includes one — or a longer line.

Files are optional. A line with no files behind it still speaks, as long as something describes the speaker.

## Flags

| Flag | Meaning |
|:-----|:--------|
| `-o`, `--output` | directory to write the audio file to (default: the working directory) |
| `-v`, `--verbose` | print the composition and the runtime invocation |

Flags precede the inputs, as in every `evoke` subcommand.

## Output

One file per invocation, named for [`NAME`](../file-format/declarations.md#name) and numbered so a repeated line never overwrites an earlier take:

```text
./Sumi_000.wav
./Sumi_2_000.wav
```

The output path is an ordinary directory argument, not a configured location — `audio` writes the file itself rather than asking a backend to.

## What describes the speaker

At least one of these, or the command stops rather than inventing a voice:

- a [`VOICE`](../file-format/declarations.md#voice) description, which becomes the speaker instruction,
- an [`AUDIO`](../file-format/declarations.md#audio) `reference` clip to clone,
- an `AUDIO` `voice` preset, for models that ship named voices.

A character file carrying both a description and a reference clip is the normal case: the description says what the character sounds like, and the clip pins which rendering of it you kept.

## Runtime

Synthesis runs through [mlx-audio](https://github.com/Blaizzy/mlx-audio) on Apple Silicon:

```bash
uv tool install --force mlx-audio --prerelease=allow --python 3.12
```

The default model is `mlx-community/VoxCPM2-bf16`, chosen because it reads a free-text speaker description — which is exactly what a `VOICE` block holds. Override it with `AUDIO model`, and point `EVOKE_AUDIO_EXECUTABLE` at a different binary if yours is not on `PATH`.

The runtime is a one-shot process: it loads the model, writes the file, and exits. Nothing stays resident between invocations, so a single line pays the model load once and there is no backend to shut down.

## Example

A character file, `sumi.evoke`:

```text
NAME
    Sumi

TAGS
    character

VOICE
    low alto with a slight rasp, unhurried
    warm but guarded

AUDIO
    model = mlx-community/VoxCPM2-bf16
```

```bash
evoke audio sumi "Sit down. We have a lot to talk about."
```

```text
sumi => sumi.evoke

voice: low alto with a slight rasp, unhurried, warm but guarded
text:  Sit down. We have a lot to talk about.
saved: ./Sumi_000.wav
```
