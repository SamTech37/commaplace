---
description: Interview the user about a project idea, then write SPEC.md
---

The user wants to build: $ARGUMENTS

Interview them in depth using the AskUserQuestion tool to flesh out a complete specification before any code is written.

Cover the hard parts they might not have thought through:
- Technical implementation choices and the tradeoffs between them
- UI/UX decisions where multiple reasonable answers exist
- Edge cases, failure modes, and what happens when assumptions break
- Scope boundaries — what's explicitly *not* in v1
- Constraints they're working under (time, stack, audience, budget)

Skip obvious questions. If you can guess the answer with high confidence, don't ask, just state your assumption and move on. Dig into ambiguity, surface tradeoffs the user hasn't named, and push back when a stated requirement seems to conflict with another.

Ask in rounds: a few focused questions per turn, then incorporate the answers before the next round. Stop when you have enough to write a real spec — not before, not long after.

When done, write a complete `SPEC.md` in the current working directory covering:
- **Goal** — one paragraph, what this is and who it's for
- **Scope** — what's in, what's out (explicit non-goals)
- **Design decisions** — the choices that were made and why
- **Open questions** — what's still undecided
- **Out of scope for v0** — deferred features
