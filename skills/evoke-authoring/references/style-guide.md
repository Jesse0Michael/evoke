# Style Guide

Rules for writing the _content_ of `.evoke` files. Format mechanics live in [File Format](file-format.md); this covers what to put in the blocks.

**Start here.** Which rules apply depends only on which declaration you are writing:

| Writing…                                                                      | Read                                              |
| :---------------------------------------------------------------------------- | :------------------------------------------------ |
| `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT`, `IMAGE`, `LORA`, `DETAILER` | §2 and §3 — §3.12 first for `ENVIRONMENT`         |
| `CHARACTER`, `PERSONALITY`, `BACKSTORY`, `SCENARIO`, `CHAT` instructions      | §2 and §4                                         |
| `VOICE`                                                                       | §2 and §1's `VOICE` note — no target reads it yet |

§3 and §4 give opposite advice on purpose. Applying §3's rules to a chat block, or §4's to an image block, produces confidently wrong output both times.

---

## 1. Targets

A declaration is written for the **output targets** that consume it. Rules are grouped by target _class_, so a new target (audio, video) joins a class and inherits its rules.

| Class              | Today                    | Behavior                                                                                         |
| :----------------- | :----------------------- | :----------------------------------------------------------------------------------------------- |
| **Rendering** (§3) | `evoke image` → SDXL     | Doesn't reason. Renders every content word. Ignores negation and abstraction. Hard token budget. |
| **Language** (§4)  | `evoke chat` → llama.cpp | Reasons. Understands negation, abstraction, nuance. Wants prose.                                 |

§3 and §4 contradict each other by design. Neither is a universal rule about prompts.

### Who reads what

Ground truth from `internal/generate/comfyui/comfyui.go` and `internal/chat/prompt.go`. Keep accurate if those change. A dash costs nothing — that's what makes new targets cheap.

| Declaration                    | `image` (rendering)                         | `chat` (language)                       |
| :----------------------------- | :------------------------------------------ | :-------------------------------------- |
| `NAME`                         | output dir only — **never in the prompt**   | `"You are {name}."`                     |
| `CHARACTER`                    | —                                           | identity                                |
| `PERSONALITY` / `!PERSONALITY` | —                                           | `"Personality:"` / `"Traits to avoid:"` |
| `BACKSTORY`                    | —                                           | `"Backstory:"`                          |
| `SCENARIO`                     | —                                           | opening turn                            |
| `VOICE`                        | — (see below)                               | — (see below)                           |
| `APPEARANCE` / `!`             | positive / negative                         | —                                       |
| `APPAREL` / `!`                | apparel conditioning                        | —                                       |
| `ENVIRONMENT` / `!`            | environment conditioning                    | —                                       |
| `PROMPT` / `!`                 | shot composition (positive / negative)      | —                                       |
| `IMAGE` / `!`                  | sampler settings + text at prompt **front** | —                                       |
| `LORA`                         | LoRA chain                                  | —                                       |
| `DETAILER` / `!`               | per-region inpaint prompts                  | —                                       |
| `CHAT`                         | —                                           | runtime, sampling, system instructions  |
| `KNOWLEDGE`                    | —                                           | retrieval                               |

- **No declaration is read by more than one target.** Every block is written for exactly one class, so it never has to satisfy two sets of contradictory rules. Expect that to change eventually — a voice target would likely read `PERSONALITY` — and a declaration that gains a second reader must then satisfy the strictest class that reads it.
- `CHARACTER`/`PERSONALITY`/`BACKSTORY`/`SCENARIO` cost the image prompt nothing. Write them as long as the character needs.
- Chat can't see `APPEARANCE`/`APPAREL`/`ENVIRONMENT`/`PROMPT`. Anything the character should _know_ about itself goes in `CHARACTER` or `BACKSTORY`.
- The image prompt has no "who" block. Everything drawable about a subject — species, build, apparent age, features — goes in `APPEARANCE`.

### `VOICE` — a block with no target

`VOICE` records what a character **sounds** like: timbre, pitch, pace, accent, vocal texture. No command reads it. It parses, merges, and appears in `evoke inspect`, and then nothing renders it — there is no audio target yet.

That does not make it scratch space. Three rules:

- **Write it only when the user asks for a voice, or the source describes one.** §2 applies unchanged, and applies harder here: nothing renders `VOICE`, so an invented one is never caught by a bad output. It sits in the file looking sourced until someone builds the target and ships it.
- **Sound, not speech.** How the character sounds is `VOICE`. What they say and how they word it is `PERSONALITY` and the `CHAT` instructions — and only those reach a model today. "Rarely uses contractions," "trails off mid-sentence," "answers questions with questions" are language traits; putting them in `VOICE` means no target reads them at all, which is the one genuinely silent failure in this format.
- **Character descriptors are conditioning, not leakage.** Prompt-to-voice models are conditioned on a description of the _speaker_, not on timbre words — `18 year old guy, comic relief, young, puberty, goofball, humorous, quickly delivered` is a real input to one, and every token in it shapes the output. When the source is itself a synthesis prompt, that prompt **is** the block: paste it, and strip only engine and quality configuration (`High-fidelity speech quality`, sample rate, provider, model name), which belongs to a settings block. Do not "clean it up" into `low alto, slight rasp` — translating a working synthesis prompt into pure timbre vocabulary discards the conditioning the model actually reads, and nothing downstream will ever tell you it happened. This bullet outranks the one above when the source came from a voice model.
- **§3 form, phrases not prose.** `low alto, slight rasp, unhurried cadence`, one comma-joined line, no trailing period. The consumer is a synthesis target conditioned on a description, which reads phrases and not prose, and a language target reads phrases fine — so phrases are the form that survives either outcome. Skip weights: there is no model whose prior you are fighting.
- **Positive only.** Describe the voice the character has. There is no exclusion list to write here — a voice has one description, and "not shrill" is not a thing to say about it.

Engine configuration is not this block's job. A synthesis backend, model, and its settings belong in a structured declaration of their own when one exists — `VOICE` is to that what `APPEARANCE` is to `IMAGE`.

### Render order

Position is weight — CLIP dilutes later tokens (~75/chunk).

```text
positive:  IMAGE → PROMPT → APPEARANCE → APPAREL → ENVIRONMENT
negative:  !IMAGE → !PROMPT → !APPEARANCE → !APPAREL → !ENVIRONMENT
```

Bloat early and you starve `APPAREL`/`ENVIRONMENT` at the tail.

---

## 2. Every target

**Write only what the source supports, and only the blocks you were asked for.** This outranks every other rule here. Each value has to trace to something the user said or the source material states. A declaration you have no material for is left out — not filled from genre convention, not extrapolated from the material you do have, not written because the block exists and looks empty. Blocks are optional. A file with four declarations is a normal file.

The failure is invisible on the page: invented prose reads exactly like sourced prose, so nobody catches it at review time, and once written it becomes canon the character then asserts as fact. Thin notes produce a short file. Say in your reply what you left out and why — that is the useful answer, not a fuller-looking file.

**Relocate before you drop, and never drop silently.** This rule has a symmetric failure the wording above hides: writing less than the source supports. When converting existing material — another tool's prompt, a character sheet, an export — every token in it was put there on purpose by someone. A token that doesn't belong in the block you are currently writing almost always belongs in a _different_ block, and the order to check is: does it name something drawable (`APPEARANCE`), worn (`APPAREL`), a place (`ENVIRONMENT`), a medium or art style (`IMAGE`), a behavior or narrative role (`PERSONALITY`), or a thing to suppress (`!BLOCK`)? Only after all of those fail is dropping it correct — and then it goes in your reply as an explicit list, with the reason. "It didn't fit the block I was writing" is not a reason to delete an author's stated intent. A conversion that silently discards a third of its input looks identical, on the page, to one that kept everything.

**Form is not fact.** §2 governs _what_ a block asserts, never _how_ it is worded. Rewriting terse source fragments into the prose a language block requires — `Male - Drakona - Warrior` into `Blitz is a Drakona warrior` — invents nothing and is required by §4.3. Pasting the fragments verbatim to "stay faithful" is the actual error: it puts tags in a prose block, which §4.3 forbids and a chat model reads as noise. Conversely, dropping a fact to make a sentence read better _is_ a §2 violation. Change the form freely; change the facts never.

Rendering blocks are the one place some invention is unavoidable: notes never specify a nose, and `APPEARANCE` cannot render an omission. There, choose the plainest values consistent with what _is_ stated, invent nothing that carries meaning — scars, tattoos, missing fingers, insignia are history and belong to whoever owns the canon — and list the choices you made in your reply. Language blocks are the opposite. An invented `PERSONALITY` pattern or `BACKSTORY` event is something the character will act on, in the voice of someone who knows their own life, contradicting the real material. Leave the block out.

- **Where the lines fall is a convention, and one of its rules has teeth** — see §2.1.
- **Each file stays in its lane.** Character files describe characters, style files describe medium. A file that asserts a camera lens sabotages every composition.
- **Write for recombination.** Situational detail belongs in a separate file.
- **Comments are optional, and the default is none.** A `#` line earns its place only by carrying what the declarations cannot — why a weight sits where it does, what a partner file has to supply, a value that looks like a mistake and isn't. A header that says what the file is, summarizes its own contents, notes where the material came from, or lists example invocations is noise: the declarations already say it, and the caller decides the invocation.
- **A comment can never qualify a value.** The parser discards comment lines before building the `Document` (`pkg/evoke/parse.go`), so `# APPEARANCE is a starting guess` reaches nothing. The values ship; the caveat doesn't. It is absent from the image prompt, absent from the chat system prompt, and absent from `knowledge.db` — where an invented `CHARACTER` or `BACKSTORY` line is retrieved and read as established fact, the same failure the format already avoids by refusing to embed `!` channels. Never write a comment that licenses content you would otherwise not write. Uncertainty goes in your reply to the user, where someone can act on it.

### 2.1 Layout

Almost none of this changes what compiles. A comma-joined block is joined with `, ` and a newline-joined one with a newline, so where the lines fall is a reading choice — which is what makes it worth making the same choice every time. The one exception is the value boundary, which is not a matter of taste at all.

**A blank line between declarations.** One, every time. This is the form `evoke inspect` and `evoke render` emit, so a file written this way round-trips through the tools unchanged, and a file written without them reformats itself the moment anything writes it back out. Blank lines never end a block ([File Format](file-format.md)), so they cost nothing inside one either.

**A line break is a value boundary.** Break only where a comma would go — never inside a phrase. A line ending `a tarmac road with a dashed white` above one reading `centre line in the background` is two unrelated values, not one wrapped phrase, and dedup, merge, and every join treat them as such. In a newline-joined block the same slip splits one sentence in half:

```text
# bad — two values, and the comma-join yields "...ended her courier, work, and now..."
CHARACTER
    She lost the use of her left hand in the accident that ended her courier
    work, and now writes and fights one-handed.

# good — one value; a sentence stays whole however long it runs
CHARACTER
    She lost the use of her left hand in the accident that ended her courier work, and now writes and fights one-handed.
```

A line may hold several sentences when they form one unit; the rule is that a sentence never spans lines, not that a line holds one sentence.

**Break long blocks for width.** A break at a phrase boundary is free, so spend it. Aim for a line you can take in at a glance — around 100 characters — breaking at the nearest comma and keeping phrases that describe the same thing together. A single phrase longer than that stays whole: the target is a reading width, not a limit to enforce.

```text
# one long line — hard to scan, and hard to edit one element of
ENVIRONMENT
    a roadside bus stop in autumn, a tarmac road with a dashed white centre line in the background, a paved waiting apron, a metal ticketing terminal, a bare dirt clearing, orange and deep red leaved trees, weathered wooden fence posts

# identical output, grouped by what each line is describing
ENVIRONMENT
    a roadside bus stop in autumn, a tarmac road with a dashed white centre line in the background
    a paved waiting apron, a metal ticketing terminal, a bare dirt clearing
    orange and deep red leaved trees, weathered wooden fence posts
```

Short blocks stay on one line — `TAGS`, an exclusion list, a two- or three-item `APPAREL` outfit — because there is nothing to group and a one-per-line list of bare nouns is an outline of nothing. Two things take their own line regardless of width: weighted identity traits, which you tune individually, and `PERSONALITY` clauses, which each run a full sentence. `CHARACTER`, `BACKSTORY`, `SCENARIO`, and `CHAT` instructions are newline-joined, so there a line _is_ a value and grouping is not available.

**No trailing comma.** The newline is already the separator, and the parser stores each line verbatim, so a trailing comma survives into the join as a doubled one — an empty token in the sequence:

```text
ENVIRONMENT
    pale blue and white tilework, large steaming pools,
    stone arches along the back wall
```

```text
$ evoke inspect bathhouse
ENVIRONMENT
    pale blue and white tilework, large steaming pools,, stone arches along the back wall
```

It also defeats dedup, which is exact-match on the trimmed line: `warm damp air,` and `warm damp air` are two different values. No trailing period either, on any comma-joined block — every rendering declaration plus `PERSONALITY`/`!PERSONALITY`. Newline-joined blocks punctuate normally.

**Don't restructure a file to help dedup.** Dedup only fires when two files happen to write a value identically, which would require every file in the composition to group the same way. Splitting one file buys nothing, and a duplicated negative token costs almost nothing.

---

## 3. Rendering targets

`APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT`, `DETAILER`, and `IMAGE` text.

§3.1–§3.11 govern the _form_ of a value: how to phrase it so the tokenizer reads what you meant. §3.12–§3.14 govern the _selection_: what belongs in the block at all, what the prior hands you before you write a single modifier, and what to change once you have seen a render. A block can satisfy every rule in the first group and still come back wrong; when it does, the answer is in the second.

### 3.1 Never negate in a positive block

The tokenizer renders every content word regardless of modifiers. `no clothing` renders clothing. **Test:** if every modifier were stripped and the word rendered literally, would that be fine?

| Wrong               | Right                       |
| :------------------ | :-------------------------- |
| `without jewelry`   | omit, or `!APPAREL jewelry` |
| `missing legs`      | `two legs`                  |
| `free of blemishes` | `smooth clear skin`         |

Exclusions go in the matching `!BLOCK`.

### 3.2 Negatives are bare nouns

Same tokenizer rule inside `!BLOCK`. Write the thing to suppress, not a negation of it: `beard`, not `no beard`. Keep negatives terser than positives. Group them densely — a negative block is a token list, not an outline, and one noun phrase per line turns 30 tokens into 30 lines for no change in output.

### 3.2b A negative is a correction, not a wishlist

A `!BLOCK` is the fix list for the ways _this file's positive_ goes wrong. It is not a list of things you don't want to see in an image.

**The test is adjacency.** A negative earns its place when the positive itself plausibly produces it: `brown hair` on a green-haired character, `large bow` where the bow is small, `sleeved jacket` where the garment is a sleeveless vest, `modern` on a place written as pre-industrial. If nothing you wrote could drift that way, the negative is inert — it spends budget suppressing something that was never going to appear.

Two things establish adjacency, and either is sufficient:

- **You saw it.** A render of this file came back with it. The strongest case, and it needs no further argument.
- **It is the known overshoot of something the positive states.** Traits on a continuum land past where you aimed (§3.2c), and generic nouns arrive with a default register (§3.13) — both drift in a direction you can name before the first render. `young adult` overshoots young; a bare `village shop interior` renders contemporary. Writing the far end into the negative corrects a positive you just wrote, which is not a guess about what the model might do.

What earns no place is the token corresponding to nothing you wrote. A generic quality or anatomy negative — `extra fingers`, `bad hands`, `watermark` — is adjacent to no particular subject and belongs in the pipeline file that owns quality (§3.9). Repetition across files is not itself the fault: if the same drift really is adjacent to ten files' positives, ten files may legitimately name it. Hoisting it into a shared file is then a convenience worth having, not a rule you were breaking.

**Why "just in case" is wrong, mechanically.** The negative channel is a prompt with a token budget, and it _accumulates_ across every file in a composition — an explicit positive never suppresses it (see [File Format](file-format.md), merge modes). Twenty speculative tokens dilute the three that were working, by the same relative-weight arithmetic as §3.6. **An unnecessary negative is not free; it is paid for by the necessary ones.**

**Fix the positive first.** Most drift is caused by a bad positive token, not a missing negative — see §3.11. `collar` rendering a dog collar, `tail` rendering an animal tail, `bound` rendering bondage, `television` in an `ENVIRONMENT` putting the subject inside the screen. A negative that papers over a bad positive leaves the bad positive in place and spends budget hiding it. Reach for `!BLOCK` only once the positive is correct and the drift persists.

| Situation                                            | Negative                                                            |
| :--------------------------------------------------- | :------------------------------------------------------------------ |
| a green-haired character keeps rendering brown       | `brown hair, brunette` — observed                                   |
| a character's small bow keeps rendering huge         | `large bow, oversized bow` — observed                               |
| a positive stating `young adult, early twenties`     | `loli, child, teenage girl` — the overshoot of a stated trait, §3.2c |
| a positive stating `rustic one room cabin interior`  | `modern, suburban, tidy` — the register the noun defaults to, §3.13  |
| a character who has never once rendered a dog collar | **nothing** — nothing in the positive invites one                   |
| `extra fingers`, `bad hands`, `watermark`            | **nothing here** — adjacent to no subject; pipeline file, §3.9      |

### 3.2c Traits on a continuum: state the target, negate the overshoot

Build, apparent age, hair length, bust, height, saturation, tidiness, opulence, decay. These are not categories the model selects from — they are directions it travels, and one word gives it the direction without saying where to stop. `chubby` is not a body; it is "heavier than default," and the render lands somewhere past it. `old`, `long hair`, `ornate`, `rundown` all behave the same way.

Bracket the axis from both ends:

- **The positive names the target with several concrete, mutually reinforcing descriptors** — enough that they only all hold at one point on the scale. Prefer shapes to judgements: `heavyset, plump, sturdy build, broad shoulders, wide hips, thick arms, soft round belly` locates a body, while `out of shape` and `husky` are opinions about one and locate nothing.
- **The negative names the far end**, where the render would otherwise drift: `obese, morbidly obese, belly rolls, skin folds, bloated, shapeless`.

The pair does the work; either half alone leaves the axis open. This is also the one negative you can write before seeing anything (§3.2b), because the overshoot direction is implied by the positive you just wrote.

The same shape handles a **named tag that renders the wrong thing** — though that one you do have to observe first. Replace the name with the geometry you actually wanted, and put the name into the negative, because the geometry still invites it. A hairstyle that kept coming back as a side bun became `rolled hair ends`, `spiral curl at each side of the jaw`, `symmetrical hairstyle`, with `side bun, hair bun, updo, asymmetrical hair` suppressed.

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

CLIP is a bag-of-words encoder with no syntax awareness. It expects comma-separated descriptors and [fails on full sentences](https://github.com/lllyasviel/stable-diffusion-webui-forge/discussions/1182); grammar words (`with`, `her`, `running toward`) spend tokens and buy nothing.

Two independent axes:

| Axis              | Options                                                                                                          | Decides        |
| :---------------- | :--------------------------------------------------------------------------------------------------------------- | :------------- |
| **Text encoder**  | CLIP-only (SD1.5, all SDXL) → phrases<br>T5 (Flux, SD3.5) → full sentences                                       | **form**       |
| **Training data** | Booru (Illustrious, Pony, NoobAI) → Danbooru tags<br>Natural-caption (Juggernaut, RealVisXL) → plain descriptors | **vocabulary** |

Long descriptive prose belongs to T5 pipelines and hosted models (Midjourney, DALL·E 3) — never SDXL, however photorealistic the checkpoint. Evoke targets Illustrious-family SDXL (`perfectionRealisticILXL_70`; `riMixIllustriousAnima_riMixV2` is the code default), so: **short phrases, Danbooru tags where a canonical one exists, photographic vocabulary otherwise.**

```text
# good
APPEARANCE
    long wavy strawberry blonde hair, blue eyes, freckles across nose and cheeks, fair skin

# bad — CLIP discards the grammar and reads the same bag of words, minus the budget
APPEARANCE
    a young woman in her early twenties with striking bright blue eyes and fair skin with subtle freckles scattered across her nose
```

**A bag of words has no pronouns and no attachment.** `it`, `its`, `there`, `the same` bind to nothing at all, and an adjective attaches to whichever noun the encoder finds convenient — usually the nearest, rarely the one you meant. Repeat the noun instead of referring back to it, and repeat an attribute onto each noun that needs it rather than trusting one mention to distribute.

```text
# bad — "it" is inert, and "dark" attaches to "stone"
a circle chalked on dark stone with red candles set around it

# good
a dark ritual pentacle chalked on the floor, red candles set around the ritual pentacle
```

This is not the decomposition §3.4 warns against. §3.4 is about spending tokens to explain a concept the model already holds; this spends them on the one link the encoder cannot make for itself.

Realistic Illustrious merges are hybrids: Danbooru structure tags (`1girl`, `upper body`, `from below` — in `PROMPT`, §3.10) plus photographic vocabulary for light and texture (`soft diffused light`, `shallow depth of field`, `visible skin pores`). The anime quality stack (`masterpiece, best quality`) drags them back toward illustration — prefer `photorealistic`, `detailed skin texture`.

When the target checkpoint is unknown, use phrases. They degrade gracefully on T5; prose degrades badly on every CLIP model.

### 3.6 Weights fight the model's prior

SDXL — Illustrious especially — averages distinctive subjects toward generic: unusual skin tones normalize, non-human features soften, atypical proportions regress, and an unusual place resolves toward the commonest version of its noun (§3.13). Position doesn't fix this; position controls _when_ a token is attended to, not whether it beats a prior. Weight is the right tool. Use it.

**Test: does the trait survive unweighted?** Not "is it important" — everything in a character file feels important, and weight is relative, so weighting everything is weighting nothing. Generate flat once, weight what came back wrong. `blue eyes` renders fine alone; `violet skin` doesn't.

The predictor, before you've tested, is **rarity, not centrality**. The prior fights a trait exactly when the training data rarely paired it with the rest of the description — species, non-human skin and eye colors, atypical proportions, extra or missing limbs. A trait can be the single most defining thing about a character and still need no weight: `very tall broad build` is common, renders flat, and weighting it only steals relative strength from the traits that needed it. Same trait, different character: `blue-grey skin` earns a weight, `tan skin` never does.

| Rule   | Value                                                                                                                                                                                                         |
| :----- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Form   | Numeric `(trait:1.25)` only. **Never** `(word)` or `((word))` — [multiplier and nesting order are implementation-dependent](https://www.generativelabs.com/insights/prompt-syntax-for-stable-diffusion-faq).  |
| Range  | 1.1–1.3. Above ~1.4 gives the artifact version, not more of the trait.                                                                                                                                        |
| Budget | 1–3 per **subject** — per character, per location — not per file.                                                                                                                                             |
| Where  | The block the file exists to assert (§5.1): a character file's `APPEARANCE`, a location file's `ENVIRONMENT`, an apparel file's `APPAREL`. Not a pipeline or style file, whose anchors fight no prior.        |

**Weight goes on the block the file was selected for.** Which block it sits in is what stops a weight fighting a composition the element is incidental to. A location file's `ENVIRONMENT` applies exactly when someone asked for that location, so weighting the one element that makes the place recognizable cannot distort a composition the file is absent from. What a weight must not do is shout from a block that merely came along with the subject, or from a file the caller selected for something else entirely.

Needs >1.4 → out of distribution. Negate what it drifts toward, or use a `LORA`.

Weight and position are complementary — a defining trait goes early _and_ weighted:

```text
APPEARANCE
    (smooth violet skin:1.25)
    (eight tapering tentacles:1.2)
    large luminous eyes, small round body

!APPEARANCE
    human skin, pale skin, two arms
```

The weighted lines sit alone because you tune them individually; everything else groups by width (§2.1).

**Escaping is mandatory.** A Danbooru tag containing parentheses silently becomes a weight unless escaped: `vex_\(lol\)`. Evoke JSON-escapes on the way to ComfyUI, so one backslash in the file arrives correctly. (A1111 uses `/(`; ComfyUI wants `\(`.)

### 3.7 No alternatives

`sitting or standing`, `red or blue dress` — the model attempts both. State it or omit it.

### 3.8 Be specific, then stop

Specificity beats length; later tokens are diluted.

- Describe textures and materials — wet sheen, matte finish, rough grain, soft drape.
- Describe spatial relationships.
- No mood words, atmosphere padding, or redundant synonyms.
- When pruning, keep distinctive anchors, drop generic ones. The model knows what a 1950s diner looks like.

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

Camera, lens, aperture, resolution, quality anchors, sampler settings → **one** style file you select explicitly. This is also what keeps character files checkpoint-neutral: the checkpoint-specific vocabulary lives in the file you swap.

`IMAGE` text lands at the prompt front — right for quality anchors, wrong for eye color.

**The `base` setting belongs there too, and only there.** `base` names the model architecture — `sdxl` (the default, so pipeline files for it may omit it) or `anima` — and it is what makes the composition compile against that architecture's node graph and defaults. A character file that names a base has decided which model its callers may use; the pipeline file that supplies the `checkpoint` or the `unet` is the file entitled to that call.

A `LORA` is the one place a base is a property of the asset rather than a choice: the weights were trained against one base model, so `base = anima` on a `LORA` means "load me only under `anima`," and a mismatch is skipped rather than warned. It defaults exactly as it does on `IMAGE` — omitted means `sdxl` — so write it only for weights built against something else, and never write `base = sdxl`. An untagged `LORA` is an SDXL `LORA`: it loads under `sdxl` and is skipped everywhere else, which is what stops SDXL weights being handed to an architecture whose keys they do not match.

**The exception is a style the source binds to one character.** When the material says this character is rendered in this medium — a bot whose stored prompt carries `dreamworks, 3d animation, Pixar`, an existing asset you are transcribing — that is a fact about the character as authored, and it belongs in an `IMAGE` block in the character file. Do not silently relocate it to a style file the caller has to know to select, and do not drop it (§2). Say once that `IMAGE` is singular, so two such characters in one composition warn and the first wins, and let the author decide; the split into a shared style file is a refactor they may want later and never something to perform on their behalf mid-conversion.

### 3.10 Subject count and framing live in `PROMPT`, never in `APPEARANCE`

`1girl`, `1boy`, `2girls`, `solo`, `upper body`, `full body`, `portrait`, `from below`, `from behind`, `looking at viewer`, `cowboy shot` — none of these is a trait of the character. They are instructions about the picture, and `PROMPT` is the channel for them.

The rule is about the **channel**, not the file. `APPEARANCE` accumulates, so a count written there is permanent: it argues with every shot you ask the character for, contradicts the first two-character composition, fights `from behind` and `back turned`, pins the subject count in a crowd scene, and nothing downstream can retract it. `PROMPT` is retractable — one explicit `PROMPT` anywhere in the composition drops every `?PROMPT` line — which is exactly what makes it the right home for a claim that is only true until a caller says otherwise.

So a character file **does** get to state its default shot, as `?PROMPT`. It is the only file that knows whether the subject is a girl, a boy, or a non-human, so it is the only file that can write the count at all — a pipeline file can't, and the vague genderless composition it would have to write instead is worth nothing.

```text
# wrong — the count is in APPEARANCE, where nothing can retract it
APPEARANCE
    (1boy, firbolg:1.3)
    (blue-grey skin:1.2)
    male focus, very tall broad build, long pale beard, wide flat nose

# right — identity in APPEARANCE, composition in ?PROMPT
?PROMPT
    1boy, solo

APPEARANCE
    (firbolg:1.3)
    (blue-grey skin:1.2)
    male focus, very tall broad build, long pale beard, wide flat nose
```

Two weights, not five: the species and the non-human skin tone are what Illustrious averages away. The build, beard, and nose render fine flat and stay unweighted no matter how defining they feel (§3.6).

**Keep the count-free gender tag in `APPEARANCE` as well.** This is not redundancy. Override is whole-channel, so a shot file that writes `PROMPT from behind, full body` erases the character's `1boy` along with its `solo` — and a shot file has no obligation to restate a gender it doesn't know. `male focus` / `female focus` on booru-trained checkpoints (Illustrious, Pony, NoobAI), `man` / `woman` on natural-caption ones, is the statement that survives any shot file, because it names who the subject is without deciding how many are in frame. Secondary traits — `beard`, `flat chest`, `broad shoulders`, `wide hips` — reinforce it but never replace it. The division: **`APPEARANCE` says what the subject is, `?PROMPT` says how many and how framed.**

A `?PROMPT` block has to be complete on its own, like any default (§5.1). A shot file replaces the whole channel, so `?PROMPT 1girl` with the `solo` on a second line it forgot is not completed by the shot that displaces it.

Shot files state `PROMPT` explicitly, and several of them accumulate with each other — explicit contributions never suppress one another, only defaults. Two shot files in one composition both apply, which is why a shot file should describe one concern.

### 3.11 Words with a second, literal meaning

To the model these name an _object_, or a tagged genre, rather than the sense you meant. Append when you find new ones.

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
| `lavender` as a color                   | fields of the flower         | `pale purple`, `light purple`                   |
| `tendrils`                              | tentacles                    |                                                 |
| `bound`                                 | bondage                      |                                                 |
| `tail`                                  | an animal tail               |                                                 |
| `cuffs`                                 | handcuffs                    |                                                 |
| `collar`                                | a choker or dog collar       | `shirt-collar`                                  |

**Disambiguating beats avoiding.** The right-hand column swaps the word out, but most ambiguous nouns are fixable in place with one qualifier that rules the other reading out: `a ship captain's wheel` rather than `a ship's wheel`, `a register till` rather than `a till`, `a lifesaver ring` rather than `a life ring`, `windows barred with iron` rather than `barred windows`. Swap the word when the wrong reading owns it outright (`hourglass figure`); qualify it when the word is merely underdetermined out of context.

### 3.12 A rendering block describes one frame

Every noun in the positive channel bids for space in a single image. There is no "off-camera," no "elsewhere in the building," and no way to say that two of the things you listed stand a hundred metres apart. The model resolves a list of unplaced nouns by picking the most photogenic and centring it — which is how a location's signature feature ends up filling the frame with the subject pushed out of it, and how two rooms become one impossible room.

This is the rule good source material breaks most easily. A floor plan, a map, a wiki infobox, an architectural description, a location's entry in a setting bible — each describes **the place**, completely and from no viewpoint at all. The block describes **one view of it**. Transcribing the first into the second is the default failure, and on the page it looks like diligence.

**Test: could someone standing in one spot see all of this?** If not, this is more than one file. Counting enclosures is the quick version — a block naming a kitchen, a bedroom, and a bathroom is three files, and the one that stays is the one the caller meant by selecting it. Splitting is cheap and composes; a block trying to be the whole location renders as none of it.

**Place everything in depth.** Once the selection is right, say where each element sits relative to the viewer: `in the background`, `in the distance`, `along the far wall`, `overhead`, `underfoot`, `around the perimeter`, `through the window`. Lead with the frame itself — what kind of shot of what kind of place — then let each element take its position. An element you cannot place is usually an element from a different view, so this doubles as a check on the selection.

**Say whether you are inside or outside, and name the enclosure.** `interior` alone hands the room's shape and scale to the prior, which returns a generic one. Give it a shape, a size, and whether it is sealed: `cylindrical stone tower interior`, `one room cabin interior`, `wide earthen cave interior`, `closed tiled bathhouse interior`. An outdoor element in an interior shot has to arrive through an opening — `a window revealing dense jungle`, never a bare `dense jungle`, which just moves the camera outside.

```text
# bad — an inventory of a place, seen from nowhere
ENVIRONMENT
    village house interior, a kitchen with chequered tile and a laid table, a sitting room with a lavender rug and an armchair, a hallway hung with pictures, a bedroom with pale green wallpaper, a small blue tiled bathroom, a child's room with a red bed

# good — one room, staged in depth; the bedrooms are their own files
ENVIRONMENT
    village house interior, wooden plank floors, a kitchen with pale chequered tile and fitted counters and a cloth laid table
    a sitting room with a lavender rug and a black armchair and a potted palm, a hallway hung with framed pictures in the background
```

`APPEARANCE` and `APPAREL` obey the same arithmetic and rarely break it, because a body is already about one frame's worth of thing. `ENVIRONMENT` breaks it constantly.

### 3.13 What the prior supplies when you name something

Before you write a single modifier, the noun you chose has already produced an image. Two opposite failures follow from that, and both are settled at the moment of naming.

**A name with no prior renders nothing you meant.** Proper nouns, invented species, in-world objects, anything specific to one setting — the model either has no association or has the wrong one, from an unrelated sense of the word. Naming it harder does not help. Write the name _and_ a gloss of what it looks like: silhouette, colour, material, scale, how it sits. `star fruit growing in rows, yellow star shaped fruit on green stalks`. `a terminal cabinet built in the shape of a cat`. `a stone statue of a fat cat seated cross-legged`.

The gloss describes your reference; it does not invent one (§2). That makes this the least speculative work in the format and the most commonly skipped — the name is right there in the source and reads as sufficient. **Test: would someone have to already know this setting to draw it?** If yes, gloss it. Keep the name as well: it costs little and anchors the gloss if the model turns out to know it after all.

**A generic noun renders with a default register.** `shop interior`, `bedroom`, `village square`, `supermarket`, `cabin` — the prior for each is contemporary, tidy, well-maintained and evenly lit, because that describes most captioned photographs of them. When the thing you mean is older, poorer, rougher, dirtier, grander or emptier than that, one adjective on the noun does more than any negative: `rustic`, `low budget`, `cramped`, `derelict`, `hand-built`, `opulent`. Anchor the register first and expect to need less of the anachronism negative (§3.2b) afterwards.

The two failures are one observation from opposite ends: the prior is never neutral, and every noun you write is a request for whatever it already holds.

### 3.14 After the first render

§3.1–§3.13 are what you can settle before generating. What remains genuinely requires looking at output, and symptom does not map to block the way intuition suggests — most wrong answers are "add a negative," which is usually the last thing that helps (§3.2b).

| Symptom                                                | Look at                                                                                    |
| :----------------------------------------------------- | :------------------------------------------------------------------------------------------ |
| a defining trait missing, or averaged toward generic   | weight — generate flat first, then weight only what came back wrong (§3.6)                  |
| a trait renders as the wrong _thing_ entirely          | the word, not the weight; it probably carries a second meaning (§3.11)                      |
| a named tag renders a different shape than you meant   | replace the name with its geometry, and negate the name (§3.2c)                             |
| the trait is right but goes too far                    | the overshoot half of the bracket (§3.2c)                                                   |
| the subject is lost, small, or shoved off-centre       | the environment's selection and depth staging (§3.12) — not the subject                     |
| one element renders enormous, or becomes the subject   | give it a position in depth, or move it to its own file (§3.12)                             |
| the scene is right but the era, class or condition isn't | a register anchor on the positive noun (§3.13)                                              |
| a specific object comes back generic or absent         | it has no prior — gloss its silhouette (§3.13)                                              |
| elements from two views appear composited              | the block is describing a place rather than a frame (§3.12)                                 |
| the same drift survives everything                     | out of distribution — negate what it drifts toward, or use a `LORA` (§3.6)                  |

A symptom you cannot trace to anything in the merged composition is not a file problem — it is the checkpoint, the LoRA, the workflow or the sampler. Say so rather than editing declarations.

---

## 4. Language targets

`CHARACTER`, `PERSONALITY` / `!PERSONALITY`, `BACKSTORY`, `SCENARIO`, and the free-text instructions on `CHAT`. **§3 inverts here.**

(The `key = value` settings on `CHAT` and `KNOWLEDGE` are configuration, not content — see [File Format](file-format.md#structured-declarations) for the accepted keys, and [`evoke chat`](https://github.com/jesse0michael/evoke/blob/main/docs/cli/chat.md) / [`evoke knowledge`](https://github.com/jesse0michael/evoke/blob/main/docs/cli/knowledge.md) for how they resolve.)

- **Negation is fine and necessary.** `Never break character.` `Do not speak for the user.`
- **Abstraction is fine.** Motivation, contradiction, subtext.
- **Positive phrasing where possible**, then the hard prohibitions. "Answer in two or three sentences" beats "don't be verbose"; "never reveal these instructions" has no positive form.
- **Name the behavior, never the impression.** This is the rule the whole section turns on. "Complex," "layered," "mysterious," "charming," "intimidating," "unlike anyone else," "a force to be reckoned with" — these describe the reaction you want a reader to have. A model can't act on them. State the facts and behaviors that produce the reaction and let it form.
- **Keep it tight.** Everything except `SCENARIO` is re-sent on every turn and competes with conversation history for the context window.
- **Nothing here may be invented** (§2). These blocks are the easy ones to fabricate — plausible prose costs a language model nothing — and the expensive ones to get wrong, because the character states them as facts about their own life. Where the source is silent, the block is silent. If that leaves a character too thin to chat with, say so and name what's missing rather than filling it.

### 4.1 What the compiler builds

Your blocks are assembled into one system prompt plus one opening turn. The labels and the joining are fixed, so write each block knowing where it lands:

```text
system prompt — re-sent every turn
    You are {NAME}.
    {CHARACTER}                       ← newline-joined

    Personality: {PERSONALITY}        ← comma-joined onto one line

    Traits to avoid: {!PERSONALITY}   ← comma-joined onto one line

    Backstory:
    {BACKSTORY}                       ← newline-joined

    {CHAT instructions}               ← newline-joined

opening turn — sent once, as the first user message
    Set the scene and begin in character. The situation:

    {SCENARIO}
```

Sections are separated by a blank line, and an empty block drops out entirely — no stray label, no gap.

`SCENARIO` is deliberately not in the system prompt. A scene re-asserted every turn drags the model back into replaying its opening.

### 4.2 Person is per declaration

There is no single voice rule for chat. Each block is a different kind of sentence:

| Block               | Person                   | The character is | The human is |
| :------------------ | :----------------------- | :--------------- | :----------- |
| `CHARACTER`         | third                    | name + pronouns  | —            |
| `PERSONALITY`       | third                    | name + pronouns  | —            |
| `BACKSTORY`         | third                    | name + pronouns  | —            |
| `SCENARIO`          | third, **present tense** | name + pronouns  | **the user** |
| `CHAT` instructions | second, imperative       | **you**          | the user     |

The first four are **reference prose about a character**. `CHAT` instructions are **orders to the model** — the only block written as "you." The compiler's `You are {NAME}.` line already supplies the second-person framing; reference prose doesn't repeat it.

### 4.3 `CHARACTER` — what the character is

The stable facts needed to recognize and understand the character. Positive only; there is no `!CHARACTER`.

Lead with the clearest, most important statement, then work from major defining traits down to smaller distinctive ones. The list below is what _may_ go here, not fields to fill — cover only what applies and the source supports, and skip the rest silently:

- Species, nature, or type of being.
- Occupation, role, or social position.
- Age or apparent age; important physical condition; defining visual traits.
- Cultural, supernatural, technological, or world-specific context.
- Abilities, limitations, or unusual rules governing their existence.
- The central concept separating them from similar characters.

Do not:

- Address the character as "you," or narrate as though a scene is unfolding.
- Use poetic, theatrical, or atmospheric language — this is reference prose, not fiction.
- Inventory every minor physical detail, or explain obvious implications.
- Include personality unless it's inseparable from what they fundamentally are (→ §4.4).
- Include history unless it's needed to explain what they presently are (→ §4.5).
- Claim "mysterious" or "unlike anyone else" without the specific facts behind it.

`CHARACTER` is invisible to the image prompt, so don't write it in tags — drawable detail goes in `APPEARANCE`.

```text
CHARACTER
    Mara Vess is a courier-turned-investigator in the canal city of Otreth.
    She lost the use of her left hand in the accident that ended her courier work, and now writes and fights one-handed.
    Otreth's guilds license every messenger, so an unlicensed investigator asking questions is technically committing a crime.
```

### 4.4 `PERSONALITY` — how the character behaves

Recurring patterns of thought, emotion, behavior, and interaction. Write what the character predictably **does**, not which adjectives fit them.

For each trait, establish how it shows up in at least one of: decisions, speech, emotional reactions, relationships, conflict, problem-solving, habits, behavior under pressure, or responses to affection, embarrassment, failure, fear, or opposition.

| Instead of                         | Write the mechanism                                                                                                                                        |
| :--------------------------------- | :--------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Mara is independent and stubborn` | `Mara tries to solve problems alone and refuses help before considering whether she needs it; persistent offers read to her as doubt about her competence` |

Broad qualities need their mechanism spelled out. Flirtatious, shy, arrogant, nurturing, hostile, submissive, sarcastic — say how it shows, what triggers it, and when it stops. Contradictions are good when they follow one internal logic: `enjoys public attention but goes guarded the moment talk turns to his personal life`.

**When the source names a trait but not its mechanism**, this rule is a bar on the traits you have material for — not a license to manufacture the material. Session notes calling someone "good-humoured" record a real trait and zero behaviors; writing `greets strangers as though they are expected and offers them food before asking their business` clears §4.4 and invents three. Go back to the source for behavior it actually records — notes describe what a character _did_ far more often than they characterize them, and that is where a sourced mechanism comes from. If there is genuinely nothing, write the plain trait and flag in your reply that its mechanism is unsourced, or leave it out. A three-line sourced `PERSONALITY` beats a six-line invented one. The block is not a quota.

**Traits, not rules.** `curious` is a trait. `always ask a follow-up question` is an instruction to the model and belongs in `CHAT` (§4.7).

**Narrative role is a trait, and it lives here.** "the comedic relief of the story," "the party's tank," "the straight man," "the mentor" — these describe the function a character performs in a group, which shapes how they behave in every scene, so they are `PERSONALITY` and not `CHARACTER`. `CHARACTER` holds what someone _is_ (species, job, age, condition); the role they play _among others_ is behavior. A source that states one is handing you a real trait — keep it, in the source's own words where they work.

No trailing period — lines are comma-joined onto a single `Personality:` line, so each must stand alone. `PERSONALITY` is the one comma-joined block that earns one value per line (§2.1): each pattern is a full clause, and they get edited individually. `!PERSONALITY` does not — it is a bare list of traits to avoid, so it goes on one line.

Do not:

- Write a list of adjectives.
- Repeat `CHARACTER` or `BACKSTORY`.
- Bolt on contradictions to simulate depth.
- Give everyone sarcasm, hidden insecurity, a secret soft side, or unresolved trauma.
- Make the character universally likable, or make them automatically trust, admire, desire, or prioritize the user.
- Flatten a type to its cliché: shy ≠ constant stammering, confident ≠ arrogant, flirtatious ≠ indiscriminate sexual interest.

The test: for the patterns you wrote, could another model read them and predict how the character acts in a situation you never described? This measures how each sourced trait is _written_ — never how many the block has. Failing it means a line is an adjective in disguise, not that the block needs more lines.

```text
PERSONALITY
    answers questions with questions until she knows what someone wants from her
    goes quiet and clipped when she's worried, which reads as anger to strangers
    keeps promises she never should have made rather than admit she misjudged

!PERSONALITY
    cruel, self-pitying
```

### 4.5 `BACKSTORY` — why the character is that way

Only past events that still act on the present. Every detail must produce at least one current belief, motivation, fear, skill, loyalty, prejudice, obligation, relationship pattern, secret, false assumption, unresolved conflict, or recurring habit.

Write cause and effect: **past event → the character's reading of it → present consequence.**

```text
BACKSTORY
    After being repeatedly passed over early in her career, Mara came to treat self-reliance as the only proof of competence she trusts. She now avoids asking for help even when collaboration would obviously be faster.
```

Order events for clarity, not completeness — chronological when that helps, otherwise whatever makes the influence easiest to follow. A short backstory with real consequences beats a full biography.

Do not:

- Write a whole life history unless asked.
- Include childhood, former jobs, homes, relatives, or world lore with no present effect.
- Invent trauma for depth: dead relatives, abuse, betrayal, abandonment, and hidden grief need a reason grounded in the character concept.
- Repeat `CHARACTER` or `PERSONALITY`.
- Pad a deliberately short backstory with unsupported detail.

Backstory explains the current character. It is not a record of things that happened.

### 4.6 `SCENARIO` — what is happening right now

The active situation at the start of the conversation. Third person, present tense, and the human is **the user**. Singular — only one file in a composition may provide it.

**Write one only when asked.** `SCENARIO` is the most tempting block to invent — it takes no source material to produce a plausible one, and a character reads as more finished with a scene attached. It is also the block with the least claim to a character file: it is a _situation_, not an attribute, it makes the file singular so it collides with the next character in a composition, and it is wrong for every use except the one it was written for. A character who exists to be talked to needs `CHARACTER`, `PERSONALITY`, and `BACKSTORY`; the scenario belongs in its own file selected alongside them (`evoke chat leotorin market-day`), and only when someone wanted a scene. Absent an explicit ask, leave it out.

When you do write one, establish: where they are, what the character is doing, the existing relationship to the user, what just happened, the character's immediate objective, the obstacle or tension in the way, their practical or emotional state, and a natural reason for the two to interact.

**The character must want something beyond having a conversation. A location is not a scenario.**

| Weak                               | Stronger                                                                                                                                                                                            |
| :--------------------------------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `The user meets Mara in a tavern.` | `Mara is questioning a tavern owner about a missing courier when the user walks in carrying something that belonged to him. She doesn't know if the user is a witness, a suspect, or interference.` |

Create momentum without deciding how it resolves.

Do not:

- Write a full scene, or dialogue.
- Resolve the complication.
- Decide what the user says, thinks, feels, notices, wants, or does — or assume they agree, obey, cooperate, attack, or feel attraction.
- Make the user automatically important, trusted, desired, or uniquely interesting.
- Default to a first meeting, or open on a greeting.
- Restate `CHARACTER`/`PERSONALITY`/`BACKSTORY`, or redefine the character.
- Front-load background exposition.
- Set up a tableau where nothing is happening and nobody has a reason to act.

A usable scenario has an immediate objective, an unresolved complication, and a reason to keep talking.

### 4.7 `CHAT` instructions — rules for the model

Free-text lines on the `CHAT` block become system instructions. This is the one language block in second person, and the only place rules about _output_ belong — length, format, what never to break. Character facts go in §4.3–§4.5.

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

## 5. File design

- **One concern per file** — the smallest thing you'd select alone. Never selected alone? Fold it in. Routinely swap half of it? Split it.
- **Name the thing, not the type** — `winter-coat.evoke`, not `apparel-winter.evoke`.
- **The filename is already a tag.** The index carries every file's base name as an implicit tag, so `leotorin.evoke` answers `evoke image leotorin` with no `TAGS` block at all. Never tag a file with its own name.
- **Tags name the sets you'd draw from at random**, because that is literally what they do: a selector resolves to _one_ matching file, picked at random when several match, and re-picked per image in a batch. A tag earns its place when you'd accept any file carrying it — a role (`character`, `apparel`, `style`, `environment`) and the collections the file is drawn from alongside siblings (`npc`, `party`, `crew`). A tag only one file carries is its filename spelled longer.
- **`TAGS` is one comma-separated line.** `TAGS\n    apparel, winter`. The parser splits on commas and newlines alike, so a stacked block is extra lines for an identical result — and since one or two tags is the whole expected budget, a multi-line `TAGS` block is a visual claim that the file has more discovery surface than it does.
- **Content is not tags.** `firbolg`, `druid`, `farmer`, `balance` restate declarations in a namespace that exists to pick substitutes. A descriptor tag is worth writing only when several files share it and you'd ask for any of them (`winter` across coats). Lowercase kebab-case; one or two tags is normal and none is fine.
- **`?` is the default value** — a real statement about the subject that yields. `?APPAREL` is what the character wears when nobody dressed them; `?ENVIRONMENT` is where they are when nobody placed them. Not "optional," not scaffolding to make a file render alone: it is the file's answer, offered until a caller supplies a different one. On declarations with `key = value` settings it yields per key, so `?IMAGE` keeps the `checkpoint` a later file never mentioned while giving up the `steps` it did. See §5.1.
- **Keep singular declarations out of shared files.** `NAME`, `SCENARIO`, `IMAGE`, `LORA`, `DETAILER`, `CHAT`, `KNOWLEDGE` conflict when two files provide one (warns, takes the first). `IMAGE` in a character file breaks the first two-character composition.
- **Structured blocks layer per setting, later argument winning.** `IMAGE`, `LORA`, `DETAILER`, `CHAT`, and `KNOWLEDGE` merge setting by setting, so a shot file writes `DETAILER face` with only `max_detection = 2` and inherits the character's detector, sizes, and text. Put the tuning in the shot or pipeline file that owns that concern, and remember the override has to come _later_ in the composition than what it overrides.
- **`!` is for the file's own contradictions.** `!APPEARANCE scary` belongs in Sumi because Sumi isn't that. Generic quality negatives belong in the one style file.

### 5.1 Which blocks take `?`

The test is **why the caller selected this file**. Explicit blocks are what the selection was _for_. `?` blocks are what comes along with it.

Selecting `leotorin` asks for the character — his face is the thing you asked for, his tunic and his farm merely arrive with him. Selecting `winter-coat` or `tavern` means you wanted the outfit or the place, so nothing in those files defers to anything.

| File kind        | Explicit                                                                              | `?` default                                                                       |
| :--------------- | :------------------------------------------------------------------------------------ | :-------------------------------------------------------------------------------- |
| Character        | `APPEARANCE`/`!`, `CHARACTER`, `PERSONALITY`/`!`, `BACKSTORY`, `VOICE` when asked for | `?PROMPT` — its default composition; `?APPAREL`; `?ENVIRONMENT` only when the source establishes a home, shop, or city |
| Shot / view      | `PROMPT`/`!`                                                                          | —                                                                                 |
| Apparel          | `APPAREL`/`!`                                                                         | —                                                                                 |
| Place / location | `ENVIRONMENT`/`!`                                                                     | —                                                                                 |
| Style / pipeline | `IMAGE` text, quality anchors                                                          | settings a shot file should be free to raise                                      |

- **`APPEARANCE` is never `?`.** A character's face is not a fallback — there is no composition where you want a different file's face substituted for it, and in a two-character composition accumulation is already the behavior you want.
- **`PROMPT` in a character file is always `?`; in a shot file it is never `?`.** The character's composition is a claim about the picture that holds only until a caller asks for a different picture, so it yields. A shot file exists to assert that shot, so it doesn't. This is the same test as `winter-coat` and its `APPAREL` — the block the file exists for is explicit (§3.10).
- **A pipeline file carries no `PROMPT` at all.** It is the one file that cannot know whether the subject is a girl, a boy, or a non-human, so any composition it writes is either wrong or too vague to be worth the tokens. Camera, lighting, and quality anchors go in its `IMAGE` text (§3.9); composition belongs to the character and the shot.
- **A location file never defaults its own `ENVIRONMENT`.** Its whole purpose is to assert that place; `?ENVIRONMENT` there yields to anything and asserts nothing.
- **`?ENVIRONMENT` in a character file needs a source.** A farm, a shop, a home city the notes actually establish. Adding one so the file renders alone is §2 fabrication with a `?` in front of it.
- **Override is whole-channel, not per line.** One explicit `APPAREL` anywhere in the composition drops _every_ `?APPAREL` line — you get the coat file's outfit, not the coat plus the character's trousers. That is what makes a default easy to displace, and it is why a `?` block should be complete on its own: a default outfit missing trousers is never completed by the thing that replaces it.
- **A file's own explicit block suppresses its own default.** Declaration and channel together are the unit (`channelKey{name, argument, negative}`), so `?APPAREL` and `APPAREL` in one file means the default never fires. The flip side is that the unit is _narrow_: an explicit `APPEARANCE` leaves `?APPAREL` alone, and an explicit `!APPAREL` doesn't suppress a positive `?APPAREL`.

---

## 6. Checklist

**Before anything else (§2)**

- [ ] Can every value be pointed at something in the prompt or the source material?
- [ ] Any block written because it was empty rather than because there was material for it?
- [ ] Any declaration present that nobody asked for?
- [ ] Converting existing material — is every token in the source either placed in some block or named in your reply as dropped, with a reason?
- [ ] Anything dropped that would have fit `IMAGE`, `PERSONALITY`, `?PROMPT`, `?APPAREL`, `?ENVIRONMENT`, or a `!BLOCK` if you had looked there?
- [ ] Any fact lost while changing the form of a block, or any source fragment pasted verbatim into a prose block?

**Rendering blocks (§3)**

- [ ] Every line describes something drawable?
- [ ] Does the block describe one frame someone could stand and photograph, rather than everything the place contains (§3.12)?
- [ ] Is every element placed in depth, and does an interior name what encloses it (§3.12)?
- [ ] Any name the model has no prior for, left without a silhouette gloss (§3.13)?
- [ ] Any generic place noun left to the prior's contemporary, tidy default when the subject is none of those (§3.13)?
- [ ] Any trait on a continuum given one word instead of a target bracketed against its overshoot (§3.2c)?
- [ ] Any `it`, `its`, or `there` expected to bind to a noun (§3.5)?
- [ ] Subject count or framing (`1boy`, `solo`, `upper body`, `from below`) sitting in `APPEARANCE` instead of `PROMPT`?
- [ ] Character file's `PROMPT` marked `?`, complete on its own, and its gender also stated count-free in `APPEARANCE` so a shot file replacing the channel can't degender it?
- [ ] Negation words in a positive block?
- [ ] Every negative adjacent to something the positive states — observed, or a named overshoot — rather than a token nothing you wrote invites (§3.2b)?
- [ ] Short phrases, no grammar words, no trailing periods?
- [ ] Weights: numeric only, ≤1.3, 1–3 per subject, on the block the file exists to assert, and only on traits that actually failed flat?
- [ ] Danbooru tags with unescaped parentheses?
- [ ] Any `or`?
- [ ] Camera/lighting/quality directives that belong in the pipeline file?
- [ ] Renders sensibly alone _and_ combined with its expected partners?
- [ ] `?` on the block the file exists to assert, or explicit on a block that merely came along with the subject (§5.1) — and is every `?` block complete enough to stand as the whole channel?

**Language blocks (§4)**

- [ ] Right person for the block — third for `CHARACTER`/`PERSONALITY`/`BACKSTORY`/ `SCENARIO`, second only for `CHAT` instructions?
- [ ] Any word describing an _impression_ ("complex," "mysterious," "charming") instead of the behavior that creates it?
- [ ] `PERSONALITY` states what the character does, not adjectives — is each pattern's mechanism from the source rather than supplied to satisfy §4.4?
- [ ] Every `BACKSTORY` detail produces a present belief, behavior, or conflict?
- [ ] Any block repeating what another already established?
- [ ] `SCENARIO` has an objective, a complication, and a reason to keep talking — and decides nothing about what the user says, feels, or wants?
- [ ] Rules about output length or format in `CHAT`, not `PERSONALITY`?

**`VOICE` (§1)**

- [ ] Written because a voice was asked for or described, not because the character felt unfinished without one?
- [ ] Source was a prompt-to-voice prompt — is it kept intact, with only engine/quality config stripped, rather than rewritten into timbre words?
- [ ] Nothing about word choice or phrasing, which no target would then read?
- [ ] Comma-joined phrases, no prose, no weights?

**Both**

- [ ] Any line broken somewhere other than a comma — a phrase or sentence split across two lines (§2.1)?
- [ ] A blank line between every declaration (§2.1)?
- [ ] Any line ending in a comma, or a comma-joined block ending in a period (§2.1)?
- [ ] Long blocks broken at phrase boundaries into readable lines, and short lists left on one (§2.1)?
- [ ] Singular declarations that will conflict with a sibling?
- [ ] Tags naming the file itself, or restating its content, instead of sets you'd pick from at random?
- [ ] Comments saying what the file is, what it contains, or how to invoke it?

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
