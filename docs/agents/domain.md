# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`GLOSSARY.md`** at the repo root: the domain terms (note, title, link, revision, seq, tombstone, version, replica, ...).
- **`docs/adr/`**: read ADRs that touch the area you're about to work in.

This is a single-context repo: there is no `GLOSSARY-MAP.md` and no per-context `docs/adr/`.

## File structure

```
/
├── GLOSSARY.md
├── docs/adr/
│   ├── 0001-notes-are-plain-markdown.md
│   ├── 0002-sync-with-seq-and-tombstones.md
│   └── ...
└── ...
```

## Writing ADRs and glossary entries

- **ADRs** are short files, `docs/adr/NNNN-<slug>.md`, numbered in order. Each has Status, the ticket(s) it comes from, Context, Decision and Consequences: a few paragraphs, not an essay.
- An ADR can be "Accepted" before its code is written. If a ticket changes the decision while it is being built, update the ADR in that ticket's PR. If a later decision replaces it, set its Status to "Superseded by ADR N" rather than deleting it.
- **`GLOSSARY.md`** has one line or short paragraph per term, in alphabetical order. Add a term when a ticket introduces one, in that ticket's PR.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `GLOSSARY.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal: either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR 6 (the web app is online only), but worth reopening because…_
