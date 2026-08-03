---
name: evoke-authoring
description: Write, fix, and review `.evoke` files — the declarative source format for AI characters and generative assets. Use for any work on a `.evoke` file, or when authoring content for the declarations it holds (NAME, CHARACTER, PERSONALITY, BACKSTORY, APPEARANCE, APPAREL, ENVIRONMENT, SCENARIO, PROMPT, IMAGE, LORA, DETAILER, CHAT, KNOWLEDGE) — characters, apparel, environments, styles, or collections consumed by `evoke image` and `evoke chat`.
---

# Authoring `.evoke` files

An `.evoke` file is a list of **declaration blocks**: a declaration name at column 1, indented value lines beneath it. Files are typeless and never reference each other — the *caller* selects which files compose together (`evoke image sumi winter-coat forest`), and the compiler merges the selection into either an image prompt (`evoke image`) or a chat system prompt (`evoke chat`).

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

Writing a *new* file instead? Pick the rules below, write it, then run steps 3–4 with the partners it will really be selected with.

If `evoke` isn't on `PATH`, say so rather than skipping verification silently, and check the syntax rules in `references/file-format.md` by hand.

## Which rules apply

Route by the declaration you are writing, not by the file:

| Writing…                                                                        | Read                                              |
| :------------------------------------------------------------------------------ | :------------------------------------------------ |
| `APPEARANCE`, `APPAREL`, `ENVIRONMENT`, `PROMPT`, `IMAGE`/`DETAILER` prompt text | `references/style-guide.md` §2 and **§3**         |
| `CHARACTER`, `PERSONALITY`, `BACKSTORY`, `SCENARIO`, `CHAT` instructions         | `references/style-guide.md` §2 and **§4**         |
| Both kinds in one file                                                          | §2, §3, and §4 — apply each only to its own block |
| Deciding what goes in which file, tags, `?` defaults                            | `references/style-guide.md` §5                    |
| Syntax, prefixes, merge modes, `key = value` settings                           | `references/file-format.md`                       |

**Read the section before writing, not after.** §3 and §4 contradict each other on purpose, and the failure mode is silent — prose in an `APPEARANCE` block parses fine and renders badly; tags in a `CHARACTER` block are ignored by the image pipeline entirely and read as noise by the chat model.

The one-line version of each, which is *not* a substitute for reading them:

- **§3 rendering** — short comma-separated Danbooru-style phrases, never prose. Nothing negated, abstract, instructional, or emotional. Exclusions go in the matching `!BLOCK`. Numeric weights only, 1.1–1.3, 1–3 per character, and only on traits that failed unweighted.
- **§4 language** — prose. Negation and abstraction are necessary. Name the behavior, never the impression. Third person for `CHARACTER`/`PERSONALITY`/`BACKSTORY`/`SCENARIO`; second person only for `CHAT` instructions.

## Invariants

These are not style preferences — violating them breaks composition:

- **A file must read correctly alone and in every combination it will be selected with.** It cannot assume a partner.
- **No file types, no imports.** Never invent `TYPE`, `FROM`, `IMPORT`, or a reference to another file. A file's meaning emerges from the declarations it contains.
- **One concern per file** — the smallest thing you would select on its own. If it is never selected alone, fold it into its parent; if you routinely swap half of it, split it.
- **Keep singular declarations out of shared files.** `NAME`, `SCENARIO`, `IMAGE`, `LORA`, `DETAILER`, `CHAT`, and `KNOWLEDGE` conflict when two files in a composition each supply one. An `IMAGE` block in a character file breaks the first two-character composition.
- **Never wrap one sentence across two lines.** A newline ends a value, so a wrapped sentence silently becomes two unrelated values. Let lines run long. How you *group* values on a line is otherwise free — comma-joined blocks compile identically either way. Match the file you're editing; don't restructure one to fit a house style.
- **Technical directives live in one pipeline/style file** — camera, lens, quality anchors, sampler settings. Never in a character or apparel file.

## Shape of a file

```text
TAGS
    character
    mascot

NAME
    Sumi

CHARACTER
    Sumi is an octopus humanoid and the mascot of the Evoke project.

APPEARANCE
    (smooth violet skin:1.25)
    octopus humanoid
    small round body
    large luminous eyes

!APPEARANCE
    human skin, pale skin, two arms, scary

?APPAREL
    green shirt
    blue jeans
```

`TAGS` is metadata for selector matching, not a declaration — lowercase kebab-case, a role tag (`character`, `apparel`, `style`, `environment`) plus descriptors, tagged for how you will *select* the file rather than what it is.

Finish with the checklist in `references/style-guide.md` §6, running only the half that matches the blocks you touched.
