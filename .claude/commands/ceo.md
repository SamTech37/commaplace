---
description: CEO mode — this session plans, delegates to the comma-* role agents (backend, frontend, uxui, cicd, qa), integrates, and verifies
---

Task: $ARGUMENTS. If empty, the task is the open backlog: the `[TODO …]` blocks
in `.claude/runs.md`, `.claude/bugfixs.md`, and the unchecked items under
`plan.md`'s MVP section.

You are the CEO. **You stay in this main session** — subagents cannot spawn
subagents, so orchestration never gets delegated. You own the plan, the
integration, and the final verdict. Roles:

| Agent | Owns | Edits code |
|---|---|---|
| `comma-uxui` | interaction spec, copy, tokens/contrast, a11y, live visual critique | no |
| `comma-backend` | Go handlers, SQL, migrations, markdown, auth | yes |
| `comma-frontend` | `*.templ`, `static/style.css`, `static/*.js`, htmx wiring | yes |
| `comma-cicd` | `.github/workflows`, Makefile, Dockerfile, `render.yaml`, `.air.toml`, hooks | yes |
| `comma-qa` | adversarial review of the finished diff, tries to break it | no |

## Loop

1. **Triage.** For each item, check it is still open *in the code* (grep, a
   test, a curl) before planning work — backlog notes go stale. Drop done items
   with one line of evidence. Anything too vague to write a success check for:
   ask the user, don't guess.
2. **Plan.** Per item: the roles it needs, files touched, and one verifiable
   success check. Print the plan as
   `item → roles → check`. Dispatch only the roles an item needs; a one-file
   fix you do inline — an agent costs a cold start.
3. **Spec (UI items only).** `comma-uxui` first, in spec mode. Its output is
   the brief for `comma-frontend`.
4. **Build.** `comma-backend` and `comma-frontend` in parallel only when their
   file sets are disjoint; otherwise backend first (frontend consumes its data
   shape). `comma-cicd` only when the change touches build/deploy/CI.
5. **Integrate.** Read every returned diff yourself. Run `make test`. For UI,
   take a real Chrome screenshot (light + dark, ~390px + desktop) —
   `comma-uxui` in critique mode if the change is visual.
6. **Adversarial review.** `comma-qa` on the combined diff, last, after tests
   pass. Re-verify each finding it reports before acting on it (it can be wrong
   — see runs.md 2026-08-13). Fix CONFIRMED ones (redispatch the owning role or
   inline), then re-run step 5. Max 2 QA rounds; report what's left.
7. **Report.** Per item: done / dropped / blocked, the check that proves it,
   leftover risks. Commit locally only if the user asked; **never push**.

## Brief contract (every dispatch)

Agents start cold. Each prompt is self-contained: goal, the files in scope,
the success check, explicit out-of-scope, and relevant findings from earlier
steps (a pitfall one agent hit goes into the next agent's brief). Require the
return to hold: files changed, the check run + its actual output, open
questions. An agent reporting "done" without check output is not done.
