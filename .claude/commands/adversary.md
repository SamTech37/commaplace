---
description: Spawn a subagent with zero context to adversarially review a change, write up its assumptions/design decisions, and publish both as an Artifact
---

Target: $ARGUMENTS — a commit range, PR number/URL, or branch name. If empty:
use the working tree's uncommitted changes; if the tree is clean, use the
most recent commit (`git show HEAD`).

Launch exactly one **fresh** subagent via the Agent tool — `subagent_type:
"general-purpose"`, never `"fork"`. The whole point is that it hasn't seen
this conversation and doesn't share whatever blind spots led to the diff
looking fine to me. Give it a fully self-contained prompt (it knows nothing
of this session) instructing it to:

1. Resolve the target itself (`git diff`, `git show`, `git log`, `gh pr diff`
   as appropriate) and read only that diff — not the rest of this
   conversation, which it doesn't have anyway.
2. Adversarially review it for two things only:
   - **Correctness bugs** — concrete input/state that produces a wrong
     result or crash, not style nits.
   - **Unneeded complexity** — abstractions, indirection, or generality the
     diff didn't need to introduce.
3. Separately, reconstruct the **assumptions and design decisions** embedded
   in the diff and *why* they were probably made — inferred from the code,
   comments, and commit messages, not asked of anyone. This is the part that
   tells me where to point my own attention instead of re-reading the whole
   diff cold.
4. Load the `artifact-design` skill, then publish one HTML Artifact with two
   sections: **Findings** (ranked most-severe first; state plainly if none
   survived) and **Assumptions & Design Decisions**. Give it a real favicon
   and title per that skill's rules.
5. Report back the Artifact URL plus a short plain-text summary.

After it finishes, relay to me: the Artifact URL, and a 3–5 bullet summary of
the most important findings — not the full report re-typed.
