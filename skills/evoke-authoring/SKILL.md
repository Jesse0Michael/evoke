---
name: evoke-authoring
description: Write, fix, and review `.evoke` files — the declarative source format for AI characters and generative assets. Use for any work on a `.evoke` file, or when authoring content for the declarations it holds (NAME, CHARACTER, PERSONALITY, BACKSTORY, VOICE, APPEARANCE, APPAREL, ENVIRONMENT, SCENARIO, PROMPT, IMAGE, LORA, DETAILER, CHAT, KNOWLEDGE) — characters, apparel, environments, styles, or collections consumed by `evoke image` and `evoke chat`.
---

# Authoring `.evoke` files

An `.evoke` file is a list of **declaration blocks**: a declaration name at column 1, indented value lines beneath it. Files are typeless and never reference each other — the _caller_ selects which files compose together (`evoke image sumi winter-coat forest`), and the compiler merges the selection into either an image prompt (`evoke image`) or a chat system prompt (`evoke chat`).

## Diagnose before editing

Most work here is fixing a composition whose output came out wrong, not authoring from scratch. Find the token responsible before changing anything.

1. **Reproduce the composition.** Run `evoke inspect` with the exact inputs that produced the bad output — same selectors, paths, and `@namespace/name` refs, in the same order.

   ```bash
   evoke inspect sumi winter-coat forest
   ```

   Not just the file you suspect. The output is a merge of several files, and the cause is often a partner file, a `?` default that failed to suppress, or a singular declaration two files both supplied. `inspect` also fails loudly on syntax errors, unknown declarations, and unsupported prefixes, and warns on singular conflicts — read its stderr before its stdout.

2. **Locate the token in the merged output, not in the sources.** The merged composition is what the target actually received. Find the specific line or phrase producing the symptom: a negation word rendering the thing it negates, a value in the wrong channel, prose where phrases belong, a weight fighting a partner file, a block the command doesn't even read.

   If the symptom isn't traceable to something in the merged composition, the file isn't the problem — the checkpoint, LoRA, workflow template, sampler settings, or the target's own limits are. Editing declarations will not fix it. Say so instead of rewriting the file.

3. **Make the smallest edit that removes it.** One line, in the one file whose concern it is. Rewriting a file that mostly works is how you break the compositions that were already fine.

4. **Re-inspect the same composition** and compare against what you started with: the symptom gone, nothing else moved. Then inspect the edited file alone, and with its other usual partners. A fix that only holds in one composition is not a fix — that failure mode is exactly what this format exists to prevent.

Writing a _new_ file instead? Use the procedure below, then run steps 3–4 with the partners it will really be selected with.

If `evoke` isn't on `PATH`, say so rather than skipping verification silently, and check the syntax rules in `references/file-format.md` by hand.

## Authoring a new file

A file is built by extracting the facts a brief or source actually gives you and placing each one in the single declaration it belongs to. Nothing enters the file that isn't a placed fact — not a template's worth of blocks, not a richer-sounding synonym, not a category of detail the source never mentioned.

### 1. List the facts, one entry per idea

Write the brief's facts as a flat list, one clause per idea. Collapse synonyms into the idea they share — "glamorous, elegant, sophisticated" describing one mood is **one** fact, however many words the brief spent on it, and it gets one entry in whichever word is clearest. This list is the file's ceiling: a declaration only ever contains an entry from it, phrased per the rules in `references/style-guide.md`. When the list is exhausted, the file is finished. A four-declaration file that placed every fact from a short brief did not fail to try hard enough — it is a correctly finished file.

### 2. Decide what the file is

Look at the fact list, not the subject matter, and ask: **does any fact establish an identity meant to outlive this one moment** — a name, or a trait the caller would still expect to be true in a different scene, outfit, or mood?

- **Yes** → it's a character. `NAME` carries the name, `CHARACTER` carries the identity facts, and the facts that merely *come along* with the character — a default outfit, a default shot — become `?` (`references/style-guide.md` §5.1 says exactly which blocks that applies to).
- **No** → every fact is scoped to this one render. There is no identity to name, so `NAME` and `CHARACTER` get no entry at all, and nothing in the file is `?` either — a default exists to yield to something more specific, and a one-off has no more-specific version still to come. Every populated block is written plain.
- **The fact list is only an outfit, only a place, or only camera/pipeline settings, with no subject at all** → it's an apparel, environment, or style file. It never carries `NAME`/`CHARACTER`, regardless of how the subject who eventually wears or stands in it happens to be described elsewhere.

Run the test on two different briefs and it sorts them without needing to know anything else about them: "Mara Vess, a courier-turned-investigator who lost the use of her left hand" carries a name and traits true of Mara in every scene she appears in — `NAME`, `CHARACTER`. "A courier pausing under an awning to read a message in the rain" carries a role, a pose, and a setting, none tied to a name or expected to outlive the frame — no `NAME`, no `CHARACTER`; the courier, the awning, and the rain go straight into `APPEARANCE`/`PROMPT`/`ENVIRONMENT`, written plain.

### 3. Place each fact in exactly one declaration

| A fact describes...                                                                       | Goes in...             |
| :------------------------------------------------------------------------------------------ | :---------------------- |
| a name, or an identity meant to persist                                                     | `NAME` / `CHARACTER`    |
| something drawable about the subject — species, build, expression, features                | `APPEARANCE`            |
| what the subject wears                                                                      | `APPAREL`               |
| where the scene takes place                                                                 | `ENVIRONMENT`           |
| how many subjects, and the shot or action                                                   | `PROMPT`                |
| a recurring behavior or disposition (chat only)                                             | `PERSONALITY`           |
| past events that still explain the present (chat only)                                      | `BACKSTORY`             |

A fact with nowhere on this list to go is a fact this format doesn't render — say so in your reply instead of forcing it into the nearest declaration.

`IMAGE`, `LORA`, and `DETAILER` are missing from that table on purpose — they aren't populated from the brief's facts at all. They're generator/pipeline configuration: model checkpoint or `unet`, sampler settings, a trained LoRA weight, a detailer region's inpaint settings. Write one only when the task explicitly calls for defining or overriding that configuration for a specific model or architecture. A brief describing a subject or scene — however cinematic, glamorous, or detailed the language — never on its own produces an entry in any of the three; that language is facts for `APPEARANCE`, `ENVIRONMENT`, or `PROMPT` instead. Full rule: `references/style-guide.md` §3.9.

### 4. Stop, then apply the phrasing rules

Once every fact has a home, write each populated block using the rules for its target class (`references/style-guide.md` §3 for rendering blocks, §4 for language blocks) and run the §6 checklist. Nothing is added at this stage either — §3/§4 govern *how* a placed fact is phrased, never license adding a new one.

## Which rules apply

Route by the declaration you are writing, not by the file:

| Writing…                                                                         | Read                                              |
| :------------------------------------------------------------------------------- | :------------------------------------------------ |
| `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT`, `IMAGE`/`DETAILER` prompt text | `references/style-guide.md` §2 and **§3**         |
| `ENVIRONMENT`, or any block transcribed from a map, floor plan, or setting bible | `references/style-guide.md` §2, §3, and **§3.12** |
| Any block sourced from a picture — reference art, an asset you are transcribing, a render you are correcting | `references/style-guide.md` **§2.2**, then §3 |
| `CHARACTER`, `PERSONALITY`, `BACKSTORY`, `SCENARIO`, `CHAT` instructions         | `references/style-guide.md` §2 and **§4**         |
| `VOICE` — read by no target yet                                                  | `references/style-guide.md` §2 and **§1**         |
| Both kinds in one file                                                           | §2, §3, and §4 — apply each only to its own block |
| Deciding what goes in which file, tags, `?` defaults                             | `references/style-guide.md` §5                    |
| Deciding whether to write `NAME`/`CHARACTER` at all, or a `LORA`/`DETAILER` block | `references/style-guide.md` §5 and **§3.9**       |
| Syntax, prefixes, merge modes, `key = value` settings                            | `references/file-format.md`                       |

**Read the section before writing, not after.** §3 and §4 contradict each other on purpose, and the failure mode is silent — prose in an `APPEARANCE` block parses fine and renders badly; tags in a `CHARACTER` block are ignored by the image pipeline entirely and read as noise by the chat model.

The one-line version of each, which is _not_ a substitute for reading them:

- **§3 rendering** — short comma-separated Danbooru-style phrases, never prose. Nothing negated, abstract, instructional, or emotional. Exclusions go in the matching `!BLOCK`, and each one has to be adjacent to something the positive states. Numeric weights only, 1.1–1.3, 1–3 per subject, on the block the file exists to assert, and only on traits that failed unweighted. A block describes one frame, not everything a place contains.
- **§4 language** — prose. Negation and abstraction are necessary. Name the behavior, never the impression. Third person for `CHARACTER`/`PERSONALITY`/`BACKSTORY`/`SCENARIO`; second person only for `CHAT` instructions.
- **`VOICE`** — what the character _sounds_ like, in §3's comma-joined phrase form, and only when a voice was asked for or described. No command reads it yet, so a wrong value never shows up in an output. Speech habits and word choice are `PERSONALITY`/`CHAT`, not `VOICE`. When the source is itself a prompt-to-voice prompt, that prompt **is** the block — keep its character descriptors, strip only engine config, and never rewrite it into timbre words.

## Invariants

These are not style preferences — violating them breaks composition:

- **Write only what the source supports, and only the blocks you were asked for.** Every value traces to something the user said or the material states. No block gets filled in because it looks empty, and no declaration appears because a "character file" seems to want one — `APPEARANCE` is the sole place invention is forced for characters. `ENVIRONMENT` is the sole place invention is forced for locations (nothing renders an unspecified nose) — keep those choices plain and meaningless, and list them in your reply. Full rule: `references/style-guide.md` §2.
- **`NAME` and `CHARACTER` are for defining a character, and only that.** `NAME` sets the output directory and the chat greeting, `CHARACTER` states what a persistent identity fundamentally is — so neither gets an entry unless the fact-list test in "Authoring a new file" step 2 finds an identity meant to outlive one render. Full rule: `references/style-guide.md` §5.
- **`LORA` and `DETAILER` are only relevant when the task explicitly asks for generator/pipeline tuning.** They're not descriptive flavor to add for realism, and a brief describing a subject or scene — however detailed — never asks for either on its own. When one genuinely is called for: `LORA`'s `model` setting is the filename of a trained LoRA you know is part of the project's assets — a name that merely sounds thematic (`beauty`, `makeup`) is not one, and if you don't know a matching file exists, leave the block out and say so. `DETAILER` configures a per-region inpaint pass whose canonical settings belong to the pipeline/style file; write one in a character or shot file only to override a setting an existing `DETAILER` for that region already established, never to introduce the region from nothing — and always with its required argument and `key = value` settings, never a bare comma list of region names. Full rule: `references/style-guide.md` §3.9.
- **Converting existing material? Relocate before you drop.** Every token in a source prompt was put there deliberately. Before discarding one, check whether it belongs in another block — drawable (`APPEARANCE`), worn (`?APPAREL`), a place (`?ENVIRONMENT`), a medium or art style (`IMAGE`), a behavior or narrative role (`PERSONALITY`), or something to suppress (`!BLOCK`). Whatever genuinely doesn't fit goes in your reply as an explicit list with reasons, never silently. Changing a block's _form_ — terse fragments into the prose §4.3 requires — is not invention; losing a _fact_ while doing it is. Full rule: `references/style-guide.md` §2.
- **Subject count and framing go in `PROMPT`, never in `APPEARANCE`.** `1boy`, `solo`, `upper body`, `from below` describe the picture, not the character, and `APPEARANCE` accumulates so a count written there can never be retracted. A character file states its default shot as `?PROMPT` — it is the only file that knows whether the subject is a girl, a boy, or a non-human — and any shot file's explicit `PROMPT` replaces that whole channel. Gender is also a character trait, so keep the count-free tag (`male focus` / `female focus`, or `man` / `woman`) in `APPEARANCE` as well: it is what survives a shot file that replaces `PROMPT` without knowing the subject's gender. Full rule: `references/style-guide.md` §3.10.
- **A rendering block describes one frame, not everything a place contains.** Every noun bids for space in a single image, and a block transcribed from a map, floor plan, or setting bible describes a location from no viewpoint at all — the model then centres whichever element is most photogenic and pushes the subject out. Ask whether someone standing in one spot could see all of it; if not, it is more than one file. Place what survives in depth, and say what encloses an interior. Then **reserve the near ground for the subject**, which is two separate things: a described surface at the viewer's own level for them to stand on (a named vantage is a camera position, not a floor — without ground they are omitted from the render entirely), and one object beside it whose height in feet a reader already knows (a door, a gate, a fence, a cart — never vegetation, terrain, or a stated measurement, or they render as a giant). Full rule: `references/style-guide.md` §3.12.
- **Name a thing the model has no prior for, and gloss what it looks like.** Proper nouns, invented species and in-world objects render as nothing you meant, and a generic noun (`shop interior`, `cabin`, `village square`) renders with a contemporary, tidy default. Both are settled at the moment of naming: add a silhouette gloss to the first, a register adjective to the second. For a species, the gloss comes from the description of the **species**, not of the individual — a character's own description assumes the ears, the nose and the fur that are the only reason the word reads at all, which is why non-human characters come back as their nearest human neighbour. The gloss describes your reference and invents nothing — it is the least speculative work in the format and the most often skipped. Full rule: `references/style-guide.md` §3.13.
- **An accumulating channel has no way to say "sometimes."** `APPEARANCE`, `APPAREL` and `ENVIRONMENT` combine and nothing downstream retracts them, so a trait that is only true under some condition — wings that appear when the armour activates, a form taken occasionally, a scar acquired partway through — is a separate file the caller selects, never a line in the character's own block. Same argument as the subject count above. Full rule: `references/style-guide.md` §5.
- **A file must read correctly alone and in every combination it will be selected with.** It cannot assume a partner.
- **No file types, no imports.** Never invent `TYPE`, `FROM`, `IMPORT`, or a reference to another file. A file's meaning emerges from the declarations it contains.
- **One concern per file** — the smallest thing you would select on its own. If it is never selected alone, fold it into its parent; if you routinely swap half of it, split it.
- **Keep singular declarations out of shared files.** `NAME`, `SCENARIO`, `IMAGE`, `LORA`, `DETAILER`, `CHAT`, and `KNOWLEDGE` conflict when two files in a composition each supply one. An `IMAGE` block in a character file breaks the first two-character composition.
- **A line break is a value boundary — break only where a comma would go.** A newline ends a value, so a phrase or sentence split across two lines silently becomes two unrelated values. Everything else about layout is convention; this one is correctness.
- **Layout: a blank line between every declaration, no line ending in a comma, long blocks broken at phrase boundaries to about 100 characters.** Comma-joined blocks compile identically however the lines fall, so group them by what each line describes. Short lists — `TAGS`, exclusion blocks, a two- or three-item `APPAREL` outfit — stay on one line. Weighted identity traits and `PERSONALITY` clauses take a line each regardless. Full rule: `references/style-guide.md` §2.1.
- **Technical directives live in one pipeline/style file** — camera, lens, quality anchors, sampler settings. Never in a character or apparel file. `IMAGE` isn't populated from the brief's facts like a rendering block (step 3 above); write one only when the task is actually defining or overriding that pipeline's settings for a model, never because the brief's language happened to sound cinematic or moody.
- **A synonym is not a second fact.** Several words for one idea (`stunning, breathtaking, mesmerizing, gorgeous, radiant`) collapse to the single clearest entry when listing facts (step 1) — they never become several lines. A block only grows by gaining a new fact, never by restating one already placed.

## Shape of a file

The fact list found a name and a persisting identity (step 2), so this is a character — its `PROMPT` and `APPAREL` are `?` defaults that yield to whatever the caller pairs it with:

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

!APPEARANCE
    human skin, pale skin, two arms, scary

?APPAREL
    green shirt, blue jeans
```

The fact list found no name and nothing meant to outlive this render (step 2), so this is a scene — no `NAME`, no `CHARACTER`, and nothing is `?` because nothing more specific is coming:

```text
PROMPT
    solo, 1girl, female focus

APPEARANCE
    applying makeup at a mirror, glamorous, confident expression

APPAREL
    loose open shirt, thong panties

ENVIRONMENT
    bathroom vanity, mirror, makeup products, soft lighting
```

Blocks may appear in any order — nothing in the parser or the merge reads it — but write them in the order they are applied: identity and chat blocks, then `PROMPT`, `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, then `IMAGE`/`LORA`/`DETAILER`. A file then reads in the order its output is assembled.

`TAGS` is metadata for selector matching, not a declaration. The index already tags every file with its own filename, and a selector resolves to _one_ match chosen at random, so `TAGS` names only the sets a file can be drawn from interchangeably: a role (`character`, `apparel`, `style`, `environment`) and any collection it shares with siblings (`npc`, `crew`). Never tag a file with its own name, never tag its content, and expect one or two tags — none is fine. See §5.

`?` is the file's **default value** — what the subject wears, where it is, or how it is framed when nobody said otherwise. It's a real statement that yields, not scaffolding. Decide it by asking why someone selected the file: a character file's `APPEARANCE` is explicit while its `PROMPT` and `APPAREL` are `?` (nobody selects a character to get their trousers, or to be told the shot is a solo), and a location file's `ENVIRONMENT` is explicit and never `?`. A scene file — no character behind it — has nothing to yield to at all, so every block in it is explicit, same as the location file's reasoning applied everywhere at once. See §5.1.

Comments are optional and default to none — see §2. Never open a file with a header restating what it is, what it contains, or how to invoke it.

Finish with the checklist in `references/style-guide.md` §6, running only the half that matches the blocks you touched.
