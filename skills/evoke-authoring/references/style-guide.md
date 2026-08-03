# Style Guide

Rules for writing the _content_ of `.evoke` files. Format mechanics live in [File Format](file-format.md); this covers what to put in the blocks.

**Start here.** Which rules apply depends only on which declaration you are writing:

| Writing…                                                                      | Read      |
| :---------------------------------------------------------------------------- | :-------- |
| `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT`, `IMAGE`, `LORA`, `DETAILER` | §2 and §3 |
| `CHARACTER`, `PERSONALITY`, `BACKSTORY`, `SCENARIO`, `CHAT` instructions      | §2 and §4 |

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
| `APPEARANCE` / `!`             | positive / negative                         | —                                       |
| `APPAREL` / `!`                | apparel conditioning                        | —                                       |
| `ENVIRONMENT` / `!`            | environment conditioning                    | —                                       |
| `PROMPT` / `!`                 | positive / negative                         | —                                       |
| `IMAGE` / `!`                  | sampler settings + text at prompt **front** | —                                       |
| `LORA`                         | LoRA chain                                  | —                                       |
| `DETAILER` / `!`               | per-region inpaint prompts                  | —                                       |
| `CHAT`                         | —                                           | runtime, sampling, system instructions  |
| `KNOWLEDGE`                    | —                                           | retrieval                               |

- **No declaration is read by more than one target.** Every block is written for exactly one class, so it never has to satisfy two sets of contradictory rules. Expect that to change eventually — a voice target would likely read `PERSONALITY` — and a declaration that gains a second reader must then satisfy the strictest class that reads it.
- `CHARACTER`/`PERSONALITY`/`BACKSTORY`/`SCENARIO` cost the image prompt nothing. Write them as long as the character needs.
- Chat can't see `APPEARANCE`/`APPAREL`/`ENVIRONMENT`/`PROMPT`. Anything the character should _know_ about itself goes in `CHARACTER` or `BACKSTORY`.
- The image prompt has no "who" block. Everything drawable about a subject — species, build, apparent age, features — goes in `APPEARANCE`.

### Render order

Position is weight — CLIP dilutes later tokens (~75/chunk).

```text
positive:  IMAGE → APPEARANCE → PROMPT → APPAREL → ENVIRONMENT
negative:  !IMAGE → !APPEARANCE → !PROMPT → !APPAREL → !ENVIRONMENT
```

Bloat early and you starve `APPAREL`/`ENVIRONMENT` at the tail.

---

## 2. Every target

**Write only what the source supports, and only the blocks you were asked for.** This outranks every other rule here. Each value has to trace to something the user said or the source material states. A declaration you have no material for is left out — not filled from genre convention, not extrapolated from the material you do have, not written because the block exists and looks empty. Blocks are optional. A file with four declarations is a normal file.

The failure is invisible on the page: invented prose reads exactly like sourced prose, so nobody catches it at review time, and once written it becomes canon the character then asserts as fact. Thin notes produce a short file. Say in your reply what you left out and why — that is the useful answer, not a fuller-looking file.

Rendering blocks are the one place some invention is unavoidable: notes never specify a nose, and `APPEARANCE` cannot render an omission. There, choose the plainest values consistent with what _is_ stated, invent nothing that carries meaning — scars, tattoos, missing fingers, insignia are history and belong to whoever owns the canon — and list the choices you made in your reply. Language blocks are the opposite. An invented `PERSONALITY` pattern or `BACKSTORY` event is something the character will act on, in the voice of someone who knows their own life, contradicting the real material. Leave the block out.

- **Never break a sentence across lines.** A newline ends a value — it is not soft wrapping. Let lines run long instead; there is no length limit. This is the only hard line rule.
- **Comma-joined blocks default to one line.** Every rendering declaration plus `PERSONALITY` is comma-joined, so `a, b, c` on one line and `a`/`b`/`c` on three compile byte-identically — which makes layout a pure reading choice, and the compact one is the default. Split onto separate lines only when the lines earn it: weighted identity traits you'll tune one at a time, a long `APPEARANCE` grouped by facet (build / face / hair / skin), or `PERSONALITY` patterns that each run a full clause. A one-per-line list of bare nouns is an outline of nothing.
- **Exclusion blocks are always one line.** `!APPEARANCE`, `!APPAREL`, `!ENVIRONMENT`, `!PROMPT`, `!PERSONALITY` are flat suppression lists with no internal structure and nothing to group — `human skin, pale skin, two arms, hairless`. Same for short positive lists like an `APPAREL` outfit; break that one up only when it's long enough to group by layer.
- **One value per line in newline-joined blocks** — `CHARACTER`, `BACKSTORY`, `SCENARIO`, `CHAT` instructions. There a line _is_ a sentence, and grouping is not free.
- **Don't restructure a file to help dedup.** Dedup is exact-match on the trimmed line, so it only fires when two files happen to write a value identically — which requires every file in the composition to group the same way. Splitting one file buys nothing, and a duplicated negative token costs almost nothing.
- **Punctuation follows the join.** Comma-joined blocks take no trailing period: every rendering declaration, plus `PERSONALITY`/`!PERSONALITY`. Newline-joined blocks punctuate normally: `CHARACTER`, `BACKSTORY`, `SCENARIO`, `CHAT` instructions.
- **Each file stays in its lane.** Character files describe characters, style files describe medium. A file that asserts a camera lens sabotages every composition.
- **Write for recombination.** Situational detail belongs in a separate file.
- **Comments are optional, and the default is none.** A `#` line earns its place only by carrying what the declarations cannot — why a weight sits where it does, what a partner file has to supply, a value that looks like a mistake and isn't. A header that says what the file is, summarizes its own contents, notes where the material came from, or lists example invocations is noise: the declarations already say it, and the caller decides the invocation.
- **A comment can never qualify a value.** The parser discards comment lines before building the `Document` (`pkg/evoke/parse.go`), so `# APPEARANCE is a starting guess` reaches nothing. The values ship; the caveat doesn't. It is absent from the image prompt, absent from the chat system prompt, and absent from `knowledge.db` — where an invented `CHARACTER` or `BACKSTORY` line is retrieved and read as established fact, the same failure the format already avoids by refusing to embed `!` channels. Never write a comment that licenses content you would otherwise not write. Uncertainty goes in your reply to the user, where someone can act on it.

The wrapped sentence is the easy mistake: it looks fine in the file and only misbehaves downstream. Splitting one thought over two lines makes it two values, which dedup, merge, and every join then treat as unrelated. A line may hold several sentences when they form one unit — the rule is about sentences never spanning lines, not about one sentence per line.

```text
# bad — two values, and the comma-join yields "...ended her courier, work, and now..."
CHARACTER
    She lost the use of her left hand in the accident that ended her courier
    work, and now writes and fights one-handed.

# good — one value, however long it runs
CHARACTER
    She lost the use of her left hand in the accident that ended her courier work, and now writes and fights one-handed.
```

---

## 3. Rendering targets

`APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT`, `DETAILER`, and `IMAGE` text.

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

Realistic Illustrious merges are hybrids: Danbooru structure tags (`1girl`, `upper body`, `from below` — in the shot file, §3.10) plus photographic vocabulary for light and texture (`soft diffused light`, `shallow depth of field`, `visible skin pores`). The anime quality stack (`masterpiece, best quality`) drags them back toward illustration — prefer `photorealistic`, `detailed skin texture`.

When the target checkpoint is unknown, use phrases. They degrade gracefully on T5; prose degrades badly on every CLIP model.

### 3.6 Weights fight the model's prior

SDXL — Illustrious especially — averages distinctive characters toward generic: unusual skin tones normalize, non-human features soften, atypical proportions regress. Position doesn't fix this; position controls _when_ a token is attended to, not whether it beats a prior. Weight is the right tool. Use it.

**Test: does the trait survive unweighted?** Not "is it important" — everything in a character file feels important, and weight is relative, so weighting everything is weighting nothing. Generate flat once, weight what came back wrong. `blue eyes` renders fine alone; `violet skin` doesn't.

The predictor, before you've tested, is **rarity, not centrality**. The prior fights a trait exactly when the training data rarely paired it with the rest of the description — species, non-human skin and eye colors, atypical proportions, extra or missing limbs. A trait can be the single most defining thing about a character and still need no weight: `very tall broad build` is common, renders flat, and weighting it only steals relative strength from the traits that needed it. Same trait, different character: `blue-grey skin` earns a weight, `tan skin` never does.

| Rule   | Value                                                                                                                                                                                                         |
| :----- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Form   | Numeric `(trait:1.25)` only. **Never** `(word)` or `((word))` — [multiplier and nesting order are implementation-dependent](https://www.generativelabs.com/insights/prompt-syntax-for-stable-diffusion-faq).  |
| Range  | 1.1–1.3. Above ~1.4 gives the artifact version, not more of the trait.                                                                                                                                        |
| Budget | 1–3 per **character**, not per file.                                                                                                                                                                          |
| Where  | Identity only (`APPEARANCE`) — the trait should assert in every composition. Never in situational files (`APPAREL`, `ENVIRONMENT`, style), where the weight fights compositions the element is incidental to. |

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

The weighted lines sit alone because you tune them individually; everything else is one line (§2).

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

### 3.10 Subject count and framing belong to the shot file

`1girl`, `1boy`, `2girls`, `solo`, `upper body`, `full body`, `portrait`, `from below`, `from behind`, `looking at viewer`, `cowboy shot` — none of these is a trait of the character. They are instructions about the picture, and they belong to the shot/view file selected per image, exactly as camera and quality belong to the pipeline file (§3.9).

A character file that asserts `1boy` argues with every shot you ask it for. It contradicts the first two-character composition, fights `from behind` and `back turned`, and pins the subject count in a crowd scene — and because `APPEARANCE` accumulates, nothing downstream can retract it. This is the recombination invariant with teeth: the file has to be true in every composition it appears in, and a count is only true in one.

```text
# wrong — the character file has decided the shot
APPEARANCE
    (1boy, firbolg:1.3)
    (blue-grey skin:1.2)
    male focus, very tall broad build, long pale beard, wide flat nose

# right — identity only; a shot file supplies `1boy, solo, upper body`
APPEARANCE
    (firbolg:1.3)
    (blue-grey skin:1.2)
    male focus, very tall broad build, long pale beard, wide flat nose
```

Two weights, not five: the species and the non-human skin tone are what Illustrious averages away. The build, beard, and nose render fine flat and stay unweighted no matter how defining they feel (§3.6).

**Gender belongs in `APPEARANCE` — dropping the count must not drop the gender**, or the render misgenders the character. Use the gender tag that carries no count: `male focus` / `female focus` on booru-trained checkpoints (Illustrious, Pony, NoobAI), `man` / `woman` on natural-caption ones. Those state who the subject is without deciding how many are in frame. Secondary traits — `beard`, `flat chest`, `broad shoulders`, `wide hips` — belong there too and reinforce it, but they are reinforcement, not a substitute for saying it. If a character still renders wrong, put `1boy`/`1girl` in the shot file, which is already deciding the count and is the file allowed to.

Selected with no shot file, the model picks count and framing itself. That is the intended trade, and the fix is a shot file per framing you actually use, not a count wired into the character.

### 3.11 Words with a second, literal meaning

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

No trailing period — lines are comma-joined onto a single `Personality:` line, so each must stand alone. `PERSONALITY` is the one comma-joined block that earns one value per line (§2): each pattern is a full clause, and they get edited individually. `!PERSONALITY` does not — it is a bare list of traits to avoid, so it goes on one line.

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
- **Content is not tags.** `firbolg`, `druid`, `farmer`, `balance` restate declarations in a namespace that exists to pick substitutes. A descriptor tag is worth writing only when several files share it and you'd ask for any of them (`winter` across coats). Lowercase kebab-case; one or two tags is normal and none is fine.
- **`?` is the default value** — a real statement about the subject that yields. `?APPAREL` is what the character wears when nobody dressed them; `?ENVIRONMENT` is where they are when nobody placed them. Not "optional," not scaffolding to make a file render alone: it is the file's answer, offered until a caller supplies a different one. On declarations with `key = value` settings it yields per key, so `?IMAGE` keeps the `checkpoint` a later file never mentioned while giving up the `steps` it did. See §5.1.
- **Keep singular declarations out of shared files.** `NAME`, `SCENARIO`, `IMAGE`, `LORA`, `DETAILER`, `CHAT`, `KNOWLEDGE` conflict when two files provide one (warns, takes the first). `IMAGE` in a character file breaks the first two-character composition.
- **Structured blocks layer per setting, later argument winning.** `IMAGE`, `LORA`, `DETAILER`, `CHAT`, and `KNOWLEDGE` merge setting by setting, so a shot file writes `DETAILER face` with only `max_detection = 2` and inherits the character's detector, sizes, and text. Put the tuning in the shot or pipeline file that owns that concern, and remember the override has to come _later_ in the composition than what it overrides.
- **`!` is for the file's own contradictions.** `!APPEARANCE scary` belongs in Sumi because Sumi isn't that. Generic quality negatives belong in the one style file.

### 5.1 Which blocks take `?`

The test is **why the caller selected this file**. Explicit blocks are what the selection was _for_. `?` blocks are what comes along with it.

Selecting `leotorin` asks for the character — his face is the thing you asked for, his tunic and his farm merely arrive with him. Selecting `winter-coat` or `tavern` means you wanted the outfit or the place, so nothing in those files defers to anything.

| File kind        | Explicit                                                      | `?` default                                                                       |
| :--------------- | :------------------------------------------------------------ | :-------------------------------------------------------------------------------- |
| Character        | `APPEARANCE`/`!`, `CHARACTER`, `PERSONALITY`/`!`, `BACKSTORY` | `?APPAREL`; `?ENVIRONMENT` only when the source establishes a home, shop, or city |
| Apparel          | `APPAREL`/`!`                                                 | —                                                                                 |
| Place / location | `ENVIRONMENT`/`!`                                             | —                                                                                 |
| Style / pipeline | `IMAGE`, `PROMPT`, quality anchors                            | settings a shot file should be free to raise                                      |

- **`APPEARANCE` is never `?`.** A character's face is not a fallback — there is no composition where you want a different file's face substituted for it, and in a two-character composition accumulation is already the behavior you want.
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

**Rendering blocks (§3)**

- [ ] Every line describes something drawable?
- [ ] Subject count or framing (`1boy`, `solo`, `upper body`, `from below`) in a character file instead of the shot file?
- [ ] Negation words in a positive block?
- [ ] Short phrases, no grammar words, no trailing periods?
- [ ] Weights: numeric only, ≤1.3, 1–3 per character, identity files only, and only on traits that actually failed flat?
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

**Both**

- [ ] Any sentence wrapped across two lines?
- [ ] Singular declarations that will conflict with a sibling?
- [ ] Exclusion blocks and short lists broken across lines instead of comma-joined onto one?
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
