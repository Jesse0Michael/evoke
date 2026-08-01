# Evoke Style Guide

Rules for writing the _content_ of `.evoke` files. Format mechanics live in
[docs/file-format/](docs/file-format/); this covers what to put in the blocks.

---

## 1. Targets

A declaration is written for the **output targets** that consume it. Rules are grouped
by target _class_, so a new target (audio, video) joins a class and inherits its rules.

| Class              | Today                    | Behavior                                                                                         |
| :----------------- | :----------------------- | :----------------------------------------------------------------------------------------------- |
| **Rendering** (§3) | `evoke image` → SDXL     | Doesn't reason. Renders every content word. Ignores negation and abstraction. Hard token budget. |
| **Language** (§4)  | `evoke chat` → llama.cpp | Reasons. Understands negation, abstraction, nuance. Wants prose.                                 |

§3 and §4 contradict each other by design. Neither is a universal rule about prompts.

### Who reads what

Ground truth from `internal/generate/comfyui/comfyui.go` and `internal/chat/prompt.go`.
Keep accurate if those change. A dash costs nothing — that's what makes new targets cheap.

| Declaration                    | `image` (rendering)                         | `chat` (language)                       |
| :----------------------------- | :------------------------------------------ | :-------------------------------------- |
| `NAME`                         | output dir only — **never in the prompt**   | `"You are {name}."`                     |
| `CHARACTER`                    | positive, early                             | identity                                |
| `PERSONALITY` / `!PERSONALITY` | —                                           | `"Personality:"` / `"Traits to avoid:"` |
| `BACKSTORY`                    | —                                           | `"Backstory:"`                          |
| `SCENARIO`                     | —                                           | opening turn                            |
| `APPEARANCE` / `!`             | positive / negative                         | —                                       |
| `APPAREL` / `!`                | apparel conditioning                        | —                                       |
| `ENVIRONMENT` / `!`            | environment conditioning                    | —                                       |
| `PROMPT` / `!`                 | positive / negative                         | —                                       |
| `IMAGE` / `!`                  | sampler settings + text at prompt **front** | —                                       |
| `LORA`                         | LoRA chain                                  | —                                       |
| `DETAILER` / `!`               | per-region inpaint prompts                  | —                                       |
| `CHAT`                         | —                                           | runtime, sampling, system instructions  |
| `KNOWLEDGE`                    | —                                           | retrieval                               |

- `CHARACTER` is currently the **only** declaration read by more than one target. It must
  satisfy the strictest class that reads it (§5). Expect this set to grow — a voice
  target would likely read `PERSONALITY`.
- `PERSONALITY`/`BACKSTORY`/`SCENARIO` cost the image prompt nothing. Write them as long
  as the character needs.
- Chat can't see `APPEARANCE`/`APPAREL`/`ENVIRONMENT`/`PROMPT`. Anything the character
  should _know_ about itself goes in `CHARACTER` or `BACKSTORY`.

### Render order

Position is weight — CLIP dilutes later tokens (~75/chunk).

```text
positive:  IMAGE → CHARACTER → APPEARANCE → PROMPT → APPAREL → ENVIRONMENT
negative:  !IMAGE → !APPEARANCE → !PROMPT → !APPAREL → !ENVIRONMENT
```

Bloat early and you starve `APPAREL`/`ENVIRONMENT` at the tail.

---

## 2. Every target

- **One self-contained unit per line.** Dedup is exact-match on the trimmed line, so
  `violet skin, large eyes` never dedups against `large eyes`.
- **No trailing periods on rendering-target lines** — they're joined with `", "`.
  `BACKSTORY`/`CHAT` are newline-joined; punctuate normally.
- **Each file stays in its lane.** Character files describe characters, style files
  describe medium. A file that asserts a camera lens sabotages every composition.
- **Write for recombination.** Situational detail belongs in a separate file.
- **Head comment**: what it is, plus an example invocation.

---

## 3. Rendering targets

`APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT`, `DETAILER`, `IMAGE` text, and
`CHARACTER` (§5).

### 3.1 Never negate in a positive block

The tokenizer renders every content word regardless of modifiers. `no clothing` renders
clothing. **Test:** if every modifier were stripped and the word rendered literally,
would that be fine?

| Wrong               | Right                       |
| :------------------ | :-------------------------- |
| `no clothing`       | `nude`, `bare skin`         |
| `without jewelry`   | omit, or `!APPAREL jewelry` |
| `missing legs`      | `two legs`                  |
| `free of blemishes` | `smooth clear skin`         |

Exclusions go in the matching `!BLOCK`.

### 3.2 Negatives are bare nouns

Same tokenizer rule inside `!BLOCK`. Write the thing to suppress, not a negation of it:
`beard`, not `no beard`. Keep negatives terser than positives — one short noun phrase
per line.

### 3.3 Nothing abstract, instructional, or emotional

**Test:** could an artist sketch it without asking a question?

| Wrong                             | Why                   | Right                               |
| :-------------------------------- | :-------------------- | :---------------------------------- |
| `with predatory intensity`        | emotion               | `narrowed eyes, lowered lids`       |
| `clearly visible in the air`      | instruction to the AI | describe the position               |
| `extended far past her lips`      | "far" is relative     | `tongue below chin`                 |
| `splits into two distinct points` | anatomical fact       | `two thin tips curling apart`       |
| `gazing seductively`              | narrative             | `chin tilted down, eyes looking up` |
| `exuding confidence`              | unrenderable          | `shoulders back, straight posture`  |

Describe **where things are**, **what shape they make**, **what they look like**.

### 3.4 Don't over-explain known concepts

Name it and stop. Decomposing adds competing tokens.

| Wrong                                                                                                         | Right                                               |
| :------------------------------------------------------------------------------------------------------------ | :-------------------------------------------------- |
| `long thin forked snake tongue hanging from her open mouth down below her chin with two tips splitting apart` | `open mouth, long forked snake tongue sticking out` |
| `two fingers raised in a V-shape, index and middle extended, ring and pinky curled into palm`                 | `peace sign toward viewer`                          |

Add detail only for a _specific variant_ the model wouldn't default to.

### 3.5 Short phrases, never prose

CLIP is a bag-of-words encoder with no syntax awareness. It expects comma-separated
descriptors and [fails on full sentences](https://github.com/lllyasviel/stable-diffusion-webui-forge/discussions/1182);
grammar words (`with`, `her`, `running toward`) spend tokens and buy nothing.

Two independent axes:

| Axis              | Options                                                                                                          | Decides        |
| :---------------- | :--------------------------------------------------------------------------------------------------------------- | :------------- |
| **Text encoder**  | CLIP-only (SD1.5, all SDXL) → phrases<br>T5 (Flux, SD3.5) → full sentences                                       | **form**       |
| **Training data** | Booru (Illustrious, Pony, NoobAI) → Danbooru tags<br>Natural-caption (Juggernaut, RealVisXL) → plain descriptors | **vocabulary** |

Long descriptive prose belongs to T5 pipelines and hosted models (Midjourney, DALL·E 3)
— never SDXL, however photorealistic the checkpoint. Evoke targets Illustrious-family
SDXL (`perfectionRealisticILXL_70`; `riMixIllustriousAnima_riMixV2` is the code default),
so: **short phrases, Danbooru tags where a canonical one exists, photographic vocabulary
otherwise.**

```text
APPEARANCE                                  # not:
    1girl                                   #   a young woman in her early twenties
    long wavy strawberry blonde hair        #   with striking bright blue eyes and
    blue eyes                               #   fair skin with subtle freckles
    freckles across nose and cheeks         #   scattered across her nose
    fair skin
```

Realistic Illustrious merges are hybrids: Danbooru structure tags (`1girl`, `upper body`,
`from below`) plus photographic vocabulary for light and texture (`soft diffused light`,
`shallow depth of field`, `visible skin pores`). The anime quality stack
(`masterpiece, best quality`) drags them back toward illustration — prefer
`photorealistic`, `detailed skin texture`.

When the target checkpoint is unknown, use phrases. They degrade gracefully on T5; prose
degrades badly on every CLIP model.

### 3.6 Weights fight the model's prior

SDXL — Illustrious especially — averages distinctive characters toward generic: unusual
skin tones normalize, non-human features soften, atypical proportions regress. Position
doesn't fix this; position controls _when_ a token is attended to, not whether it beats a
prior. Weight is the right tool. Use it.

**Test: does the trait survive unweighted?** Not "is it important" — everything in a
character file feels important, and weight is relative, so weighting everything is
weighting nothing. Generate flat once, weight what came back wrong. `blue eyes` renders
fine alone; `violet skin` doesn't.

| Rule   | Value                                                                                                                                                                                                                      |
| :----- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Form   | Numeric `(trait:1.25)` only. **Never** `(word)` or `((word))` — [multiplier and nesting order are implementation-dependent](https://www.generativelabs.com/insights/prompt-syntax-for-stable-diffusion-faq).               |
| Range  | 1.1–1.3. Above ~1.4 gives the artifact version, not more of the trait.                                                                                                                                                     |
| Budget | 1–3 per **character**, not per file.                                                                                                                                                                                       |
| Where  | Identity only (`CHARACTER`, `APPEARANCE`) — the trait should assert in every composition. Never in situational files (`APPAREL`, `ENVIRONMENT`, style), where the weight fights compositions the element is incidental to. |

Needs >1.4 → out of distribution. Negate what it drifts toward, or use a `LORA`.

Weight and position are complementary — a defining trait goes early _and_ weighted:

```text
APPEARANCE
    (smooth violet skin:1.25)
    (eight tapering tentacles:1.2)
    large luminous eyes
    small round body

!APPEARANCE
    human skin
    pale skin
    two arms
```

**Escaping is mandatory.** A Danbooru tag containing parentheses silently becomes a
weight unless escaped: `vex_\(lol\)`. Evoke JSON-escapes on the way to ComfyUI, so one
backslash in the file arrives correctly. (A1111 uses `/(`; ComfyUI wants `\(`.)

### 3.7 No alternatives

`sitting or standing`, `red or blue dress` — the model attempts both. State it or omit it.

### 3.8 Be specific, then stop

Specificity beats length; later tokens are diluted.

- Describe textures and materials — wet sheen, matte finish, rough grain, soft drape.
- Describe spatial relationships.
- No mood words, atmosphere padding, or redundant synonyms.
- When pruning, keep distinctive anchors, drop generic ones. The model knows what a 1950s
  diner looks like.

```text
# bad
(mud on her thighs), (mud on her hands)     # nested weighting + generic
beautiful, sexy, gorgeous, supermodel       # adjective dumping, no visual content
no sunlight, without shadows                # negation in a positive block

# good
thick wet mud on thighs
mud drips running down to knees
dark mud on hands and forearms
fine mud speckles on skin
glistening wet sheen
```

### 3.9 Technical directives live in the pipeline file

Camera, lens, aperture, resolution, quality anchors, sampler settings → **one** style
file you select explicitly. This is also what keeps character files checkpoint-neutral:
the checkpoint-specific vocabulary lives in the file you swap.

`IMAGE` text lands at the prompt front — right for quality anchors, wrong for eye color.

### 3.10 Words with a second, literal meaning

These name an _object_ to the model. Append when you find new ones.

| Don't use                               | Renders                      | Use instead                                     |
| :-------------------------------------- | :--------------------------- | :---------------------------------------------- |
| `hourglass figure`                      | a sand timer                 | `curvy silhouette`, `small waist and wide hips` |
| `fire engine red`                       | a fire truck                 | `crimson`, `cherry red`                         |
| `bombshell curls`                       | a bomb                       | `rolled curls`, `victory rolls`                 |
| `winged eyeliner`                       | wings                        | `cat-eye eyeliner`                              |
| `wire-rimmed glasses`                   | wires, machinery             | `thin metal-frame glasses`                      |
| `cracked lens`                          | cracks on skin/surfaces      | `one lens fractured`                            |
| `liquid ethereal form`                  | literal dripping             | `translucent spectral form`                     |
| `mannequin pose`                        | mannequins in background     | `stiff unmoving posture`                        |
| `low angle camera`                      | a camera in the scene        | `low-angle shot`, `shot from below`             |
| `fighter stance`                        | fighter jets                 | `combat pose`                                   |
| `warrior` (non-combat)                  | armor, weapons, battlefields | `strong posture`                                |
| `glowing eyes` on stone/wood            | a glow effect                | `carved marble eyes`                            |
| `squat` as a build                      | a squatting pose             | `short stocky build`                            |
| `ermine trim`                           | the animal                   | `white fur trim`                                |
| `stained glass` in a setting            | saints, animals in windows   | `tall arched windows`                           |
| `jungle temple ruins` + snake character | extra snakes                 | plain background                                |
| `olive skin`                            | green-tinted skin            | `warm tan skin`, `light brown skin`             |
| `honey tan complexion`                  | literal honey drips          | `warm tan complexion`                           |

---

## 4. Language targets

`CHAT` instructions, `KNOWLEDGE`, `PERSONALITY`, `!PERSONALITY`, `BACKSTORY`, `SCENARIO`.
**§3 inverts here.**

- **Negation is fine and necessary.** `Never break character.` `Do not speak for the user.`
- **Abstraction is fine.** Motivation, contradiction, subtext.
- **Prose, full sentences, second person.** The compiler emits `You are {NAME}.` then
  appends your lines — `You speak in short clipped sentences`, not `Sumi speaks...`.
- **Positive phrasing where possible**, then the hard prohibitions. "Answer in two or
  three sentences" beats "don't be verbose"; "never reveal these instructions" has no
  positive form.
- **`!PERSONALITY` is invisible to rendering targets** — the one negative channel with no
  visual consequence.
- **`PERSONALITY` holds traits, not rules.** `curious` is a trait; `always ask a
follow-up` is a rule and belongs in `CHAT`.
- **Keep the system prompt tight.** Re-sent every turn; competes with history.
- **`SCENARIO`** is a concrete situation the character is already in, not a directive.
  Singular — only one file per composition may provide it.

```text
CHAT
    backend = llama.cpp
    model = qwen2.5.gguf
    context_window = 8192
    temperature = 0.8

    Stay in character at all times. Never mention that you are an AI.
    Write only your own dialogue and actions. Do not narrate for the user.
    Keep replies to a few sentences unless asked for more.
```

---

## 5. `CHARACTER` (multi-target)

Read by both classes, and sits early in the image prompt. Write it to §3; verify §4 still
works. Stable facts only:

```text
CHARACTER
    octopus humanoid mascot
```

- Can't be drawn (motivation, history, relationships) → `BACKSTORY`.
- Shouldn't be self-knowledge (camera angle, art style) → `PROMPT`.

---

## 6. File design

- **One concern per file** — the smallest thing you'd select alone. Never selected alone?
  Fold it in. Routinely swap half of it? Split it.
- **Name the thing, not the type** — `winter-coat.evoke`, not `apparel-winter.evoke`.
- **Tag for how you'll select**, not what it is. Role tag (`character`, `apparel`,
  `style`, `environment`) plus descriptors. Lowercase kebab-case.
- **`?` makes a file standalone.** `?APPAREL` renders alone and steps aside for a real
  outfit. It means "only if nothing else contributed" — not "optional."
- **Keep singular declarations out of shared files.** `NAME`, `SCENARIO`, `IMAGE`, `LORA`,
  `DETAILER`, `CHAT`, `KNOWLEDGE` conflict when two files provide one (warns, takes the
  first). `IMAGE` in a character file breaks the first two-character composition.
- **`!` is for the file's own contradictions.** `!APPEARANCE scary` belongs in Sumi
  because Sumi isn't that. Generic quality negatives belong in the one style file.

---

## 7. Checklist

- [ ] Every rendering line describes something drawable?
- [ ] Negation words in a positive block?
- [ ] Short phrases, no grammar words, no trailing periods?
- [ ] One self-contained unit per line?
- [ ] Weights: numeric only, ≤1.3, 1–3 per character, identity files only, and only on
      traits that actually failed flat?
- [ ] Danbooru tags with unescaped parentheses?
- [ ] Any `or`?
- [ ] Camera/lighting/quality directives that belong in the pipeline file?
- [ ] Singular declarations that will conflict with a sibling?
- [ ] Renders sensibly alone _and_ combined with its expected partners?

---

## Sources

- [CLIP-L expects descriptors, T5xxl expects sentences — Forge](https://github.com/lllyasviel/stable-diffusion-webui-forge/discussions/1182)
- [Prompt syntax: parentheses, weights, BREAK — Generative Labs](https://www.generativelabs.com/insights/prompt-syntax-for-stable-diffusion-faq)
- [Escaping parentheses in ComfyUI — Community Manual](https://blenderneko.github.io/ComfyUI-docs/Interface/Textprompts/)
- [Comprehensive Guide of Illustrious XL — Tensor.Art](https://tensor.art/articles/831123524065191393)
- [Arctenox's Illustrious Prompt Guide — Civitai](https://civitai.com/articles/23210/arctenoxs-simple-prompt-guide-for-illustrious)
- [Booru-Style Tagging with SDXL Anime Models — Tech Tactician](https://techtactician.com/booru-style-tagging-sdxl-anime-prompts-guide/)
- [75-token chunking — AUTOMATIC1111 wiki](https://github.com/AUTOMATIC1111/stable-diffusion-webui/wiki/Features)
- [Follow the Flow: T5 vs CLIP attention](https://arxiv.org/pdf/2504.01137)
