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

Writing a _new_ file instead? Pick the rules below, write it, then run steps 3–4 with the partners it will really be selected with.

If `evoke` isn't on `PATH`, say so rather than skipping verification silently, and check the syntax rules in `references/file-format.md` by hand.

## Which rules apply

Route by the declaration you are writing, not by the file:

| Writing…                                                                         | Read                                              |
| :------------------------------------------------------------------------------- | :------------------------------------------------ |
| `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT`, `IMAGE`/`DETAILER` prompt text | `references/style-guide.md` §2 and **§3**         |
| `CHARACTER`, `PERSONALITY`, `BACKSTORY`, `SCENARIO`, `CHAT` instructions         | `references/style-guide.md` §2 and **§4**         |
| `VOICE` — read by no target yet                                                  | `references/style-guide.md` §2 and **§1**         |
| Both kinds in one file                                                           | §2, §3, and §4 — apply each only to its own block |
| Deciding what goes in which file, tags, `?` defaults                             | `references/style-guide.md` §5                    |
| Syntax, prefixes, merge modes, `key = value` settings                            | `references/file-format.md`                       |

**Read the section before writing, not after.** §3 and §4 contradict each other on purpose, and the failure mode is silent — prose in an `APPEARANCE` block parses fine and renders badly; tags in a `CHARACTER` block are ignored by the image pipeline entirely and read as noise by the chat model.

The one-line version of each, which is _not_ a substitute for reading them:

- **§3 rendering** — short comma-separated Danbooru-style phrases, never prose. Nothing negated, abstract, instructional, or emotional. Exclusions go in the matching `!BLOCK`. Numeric weights only, 1.1–1.3, 1–3 per character, and only on traits that failed unweighted.
- **§4 language** — prose. Negation and abstraction are necessary. Name the behavior, never the impression. Third person for `CHARACTER`/`PERSONALITY`/`BACKSTORY`/`SCENARIO`; second person only for `CHAT` instructions.
- **`VOICE`** — what the character _sounds_ like, in §3's comma-joined phrase form, and only when a voice was asked for or described. No command reads it yet, so a wrong value never shows up in an output. Speech habits and word choice are `PERSONALITY`/`CHAT`, not `VOICE`.

## Invariants

These are not style preferences — violating them breaks composition:

- **Write only what the source supports, and only the blocks you were asked for.** Every value traces to something the user said or the material states. No block gets filled in because it looks empty, and no declaration appears because a "character file" seems to want one — `APPEARANCE` is the sole place invention is forced for characters. `ENVIRONMENT` is the sole place invention is forced for locations (nothing renders an unspecified nose) — keep those choices plain and meaningless, and list them in your reply. Full rule: `references/style-guide.md` §2.
- **Subject count and framing are not character traits.** `1boy`, `solo`, `upper body`, `from below` belong to the shot file, not `APPEARANCE` — a character asserting a count fights every composition that wanted a different one. Gender is a character trait and stays in `APPEARANCE`; use the count-free tag (`male focus` / `female focus`, or `man` / `woman`) so dropping the count never misgenders the render.
- **A file must read correctly alone and in every combination it will be selected with.** It cannot assume a partner.
- **No file types, no imports.** Never invent `TYPE`, `FROM`, `IMPORT`, or a reference to another file. A file's meaning emerges from the declarations it contains.
- **One concern per file** — the smallest thing you would select on its own. If it is never selected alone, fold it into its parent; if you routinely swap half of it, split it.
- **Keep singular declarations out of shared files.** `NAME`, `SCENARIO`, `IMAGE`, `LORA`, `DETAILER`, `CHAT`, and `KNOWLEDGE` conflict when two files in a composition each supply one. An `IMAGE` block in a character file breaks the first two-character composition.
- **Never wrap one sentence across two lines.** A newline ends a value, so a wrapped sentence silently becomes two unrelated values. Let lines run long.
- **Comma-joined blocks default to one line.** They compile identically either way, so layout is a reading choice and compact wins. Exclusion blocks (`!APPEARANCE`, `!APPAREL`, `!PERSONALITY`, …) and short lists like an `APPAREL` outfit are always one comma-separated line. Separate lines are for weighted identity traits, facet-grouped `APPEARANCE`, and `PERSONALITY` clauses.
- **Technical directives live in one pipeline/style file** — camera, lens, quality anchors, sampler settings. Never in a character or apparel file.

## Shape of a file

```text
TAGS
    character

NAME
    Sumi

CHARACTER
    Sumi is an octopus humanoid and the mascot of the Evoke project.

APPEARANCE
    (smooth violet skin:1.25)
    octopus humanoid, small round body, large luminous eyes

!APPEARANCE
    human skin, pale skin, two arms, scary

?APPAREL
    green shirt, blue jeans
```

`TAGS` is metadata for selector matching, not a declaration. The index already tags every file with its own filename, and a selector resolves to _one_ match chosen at random, so `TAGS` names only the sets a file can be drawn from interchangeably: a role (`character`, `apparel`, `style`, `environment`) and any collection it shares with siblings (`npc`, `crew`). Never tag a file with its own name, never tag its content, and expect one or two tags — none is fine. See §5.

`?` is the file's **default value** — what the subject wears, or where it is, when nobody said otherwise. It's a real statement that yields, not scaffolding. Decide it by asking why someone selected the file: a character file's `APPEARANCE` is explicit and its `APPAREL` is usually `?` (nobody selects a character to get their trousers), while a location file's `ENVIRONMENT` is explicit and never `?`. See §5.1.

Comments are optional and default to none — see §2. Never open a file with a header restating what it is, what it contains, or how to invoke it.

Finish with the checklist in `references/style-guide.md` §6, running only the half that matches the blocks you touched.
