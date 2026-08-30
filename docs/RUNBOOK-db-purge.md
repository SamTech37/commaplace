# Runbook — back up prod, then purge it

Applies to any destructive change to the live database: wiping seed data,
squashing migrations, the 封測 → 公測 cutover. The rule is one line: **never run
a destructive migration without a verified dump from the same day.**

"Verified" means restored into a scratch database, not merely downloaded. An
unrestored dump is a guess.

## 1 — Take the dump

GitHub → Actions → **DB backup** → Run workflow. It also runs weekly on its own.

The job prints per-table row counts before uploading. Write those numbers down;
step 4 checks against them. The artifact `prod-dump-<run_id>` holds `prod.dump`
(pg_dump custom format), kept 90 days.

One-time setup: repo secret `PROD_DATABASE_URL`, from Render → `comma-db` →
Connections → **External Database URL**. Render's internal `dpg-*-a` hostname
resolves only inside Render's own network, so the runner needs the external one.

## 2 — Verify the dump restores

Download and unzip the artifact, then, with the local Postgres up (`make dev` or
`docker compose up -d postgres`):

```sh
docker compose exec -T postgres psql -U commaplace -d postgres \
  -c 'DROP DATABASE IF EXISTS restoretest' -c 'CREATE DATABASE restoretest'

docker run --rm --network host -v "$PWD:/in" postgres:17-alpine \
  pg_restore --clean --if-exists --no-owner --no-acl \
  -d 'postgres://commaplace:commaplace@localhost:5432/restoretest?sslmode=disable' \
  /in/prod.dump

docker compose exec -T postgres psql -U commaplace -d restoretest \
  -c 'select (select count(*) from users) users, (select count(*) from notes) notes'
```

Counts must match what step 1 printed. Use `postgres:17-alpine` specifically —
`pg_dump`/`pg_restore` refuse to work against a server newer than the client,
and Render runs Postgres 17.

Keep `restoretest` around until step 4 passes; it is the rollback source.

## 3 — Write the purge as a migration, not a psql session

Destructive SQL goes in `internal/db/migrations/NNN_*.sql`. The runner in
`internal/db/db.go` applies it once, inside a transaction, recorded in
`schema_migrations` — on Render, at boot, using the prod `DATABASE_URL` that is
already injected there. No external credentials, no laptop, and the exact
statements that ran are in git.

Dry-run it against a clone of the local dev DB first:

```sh
docker compose exec -T postgres psql -U commaplace -d postgres \
  -c 'DROP DATABASE IF EXISTS purgetest' -c 'CREATE DATABASE purgetest TEMPLATE commaplace'
docker compose exec -T postgres psql -U commaplace -d purgetest -v ON_ERROR_STOP=1 \
  -f - < internal/db/migrations/NNN_your_purge.sql
```

Notes on writing one:

- Everything user-owned cascades from `users(id)` (`ON DELETE CASCADE` on notes,
  likes, saves, follows, links, note_tags, reports, note_images). Deleting the
  user row is enough; do not hand-delete children.
- `auth_tokens` has **no** FK to users — it is keyed by email and issued before
  signup. Deleting users orphans rows there, so clean them in the same file.
- Seed accounts are identifiable by email domain: `@dev.local` (`seed.ApplyDev`)
  and `@demo.local` (`seed.ApplyDemo`). For a seeds-only purge prefer matching
  those over a hand-maintained keep-list of real handles.
- Match on `lower(handle)`, not `handle_ci` — the latter is only lowercase by
  convention, and a keep-list that silently misses is a deleted account.
- Confirm every handle you intend to keep actually exists first. No credentials
  needed: `curl -s -o /dev/null -w '%{http_code}' -L https://commaplace.app/<handle>`
  returns 200 for a live profile, 404 otherwise.

## 4 — Deploy and verify

Merge to `main`; Render auto-deploys and the migration runs at boot. Boot order
in `cmd/server/main.go` is `Migrate` → `ApplyDemo` → `ApplyDev`, so a purge runs
before any seeding in the same startup.

Check the surviving counts against step 1. If it went wrong, `restoretest` from
step 2 is the source to restore from.

**Re-seeding is the failure mode to watch.** `SEED_DEV=1` puts alice/bob/carol/
dave back on the next deploy, and `ApplyDemo` is ungated (it only skips because
`shawn` already exists). Confirm `render.yaml` has `SEED_DEV: "0"` and that the
value is not overridden in the Render dashboard — a var edited by hand there
stops tracking the blueprint.

## Before 封測 → 公測

Once real users exist the database stops being disposable, and this runbook stops
being optional (see Decision 3 in `DECISIONS.md`, and the migration-squash TODO in
`plan.md` under "Some Concerns" — squash while the DB can still be thrown away).
