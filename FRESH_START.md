# Fresh start — rebuilding the Render stack

Parked plan, written 2026-08-30. **Nothing here is in progress.** The bleeding
is stopped and the current stack is healthy; this exists so the rebuild can be
picked up cold, by anyone, without re-deriving today's findings.

Do not execute it piecemeal. Steps 3–5 are one sitting, and they need all three
founders present (see Preconditions).

---

## Why

Today's stack costs **~$17.15/mo** and is built on a Blueprint that has never
managed it. Rebuilding lands at **~$13.20/mo** with single, clear ownership.

| line | now | after |
|---|---|---|
| web `starter` | $6.86 | $6.86 |
| Postgres compute `basic-256mb` | $5.91 | $5.91 |
| Postgres storage | 15 GB → $4.38 | 1 GB → $0.30 |
| **total** | **$17.15** | **$13.20** |

The disk is the whole saving. The database holds **10 MB** in 15 GB, and Render
never shrinks a disk — so the only way down is a new database. Storage bills
separately from compute at **$0.30/GB/month**, prorated to the second.

The second reason is correctness, and it matters more than the $4. See below.

---

## What actually went wrong (so nobody re-derives it)

Blueprint `Comma` (`exs-d95i5au7r5hc73eet4s0`) was created **56 minutes after**
the services it was supposed to manage:

| 2026-08-13 | event |
|---|---|
| 13:15:29 | `comma-db-c8h6` created |
| 13:16:41 | `comma` created |
| 14:12:19 | Blueprint `Comma` first sync |

Render's Blueprint creation flow, on hitting a name collision, "appends a suffix
to the name of each new resource to prevent collisions with your existing
resources." So from birth it managed a **parallel copy**, never prod. The copies
carry no custom domain and read as ordinary rows in the dashboard, which is why
it went unnoticed for 17 days and three duplicate services.

Two behaviours to internalise, both learned the expensive way on 2026-08-30:

- **A sync never deletes; it reconciles.** Deleting a Blueprint-owned service
  while auto-sync is on guarantees the next push rebuilds it.
- **`name` is the match key and is immutable on an existing resource.** Renaming
  a resource in `render.yaml` does not rename anything — it reads as "create a
  new one", and Render suffixes it to avoid the collision.

Doing both in one afternoon turned one duplicate service into a duplicate
service *and* a duplicate database. `render blueprints validate`'s `totalActions`
was misread as a live diff against the workspace; it is not one, and must not be
used as evidence that a sync will adopt rather than create.

---

## Current state (verified 2026-08-30)

| resource | id | notes |
|---|---|---|
| web `comma` | `srv-d9us7e2jobas73bl2150` | slug `comma-c8h6`, starter, `autoDeploy: yes` |
| db `comma-db-c8h6` | `dpg-d9us6s2jobas73bl07v0-a` | basic_256mb, 15 GB, autoscaling **off**, 10 MB used |
| Blueprint `Comma` | `exs-d95i5au7r5hc73eet4s0` | `autoSync: false`, `paused`, **owns nothing** |

DNS: apex `commaplace.app` is an **A record** to Render's shared anycast IPs
(`216.24.57.7`, `216.24.57.15`) — identical for every Render service, so it does
not move. `www` is a **CNAME to `comma-c8h6.onrender.com`**, i.e. to the slug —
that is the one record a rebuild touches.

`comma.onrender.com` currently returns **503**: Render's edge answers for the
hostname but nothing is bound to it. Suggestive, not proof — a suspended service
would look the same. The new service either claims the slug or it doesn't, and
that is only knowable at creation.

Backup: artifact **`prod-dump-33304282520`** (GitHub Actions → DB backup,
178 KB, 90-day retention). Restored into a scratch database and row-verified —
users 4, notes 33, links 57, note_images 0 — matching the live counts. Take a
fresh one anyway; a dump older than the rebuild is not a rollback.

---

## Preconditions

Control is split three ways (三權分立), so this cannot be done solo:

| holder | needed for |
|---|---|
| domain owner | `www` CNAME repoint after the slug changes |
| Google Cloud Console owner | nothing, *if* `BASE_URL` stays `https://www.commaplace.app` — the redirect URI is domain-based and survives. On standby only. |
| Render dashboard owner | everything in steps 2–5 |

Also required: the three `sync: false` secrets to hand — `GOOGLE_CLIENT_SECRET`,
`ADMIN_HANDLE`, `PLAYTEST_LOGIN_KEY`. They are not in git by design and will not
carry over.

Do it when nobody is mid-demo. The site is down between steps 3 and 5.

---

## Sequence

1. **Fresh dump.** Actions → DB backup → Run workflow. Download the artifact and
   restore it into a scratch database before continuing — an unrestored dump is
   a guess, per `docs/RUNBOOK-db-purge.md`.
2. **Commit and push the target `render.yaml`** (below). Safe while the Blueprint
   is paused: nothing syncs, prod merely redeploys the same image.
3. **Delete the Blueprint**, then the web service, then the database — in that
   order, so nothing reconciles mid-flight.
   ```
   render services delete srv-d9us7e2jobas73bl2150
   render postgres delete dpg-d9us6s2jobas73bl07v0-a
   ```
   The site is down from here.
4. **Create one new Blueprint**: dashboard → `New +` → Blueprint → this repo.
   The names are free by then, so it creates and owns everything itself — which
   is the entire point. **If the creation flow shows a name-collision notice,
   stop.** That notice is the bug; something still holds the name.
5. **Reconnect.** Set the three secrets; add `commaplace.app` and `www` as custom
   domains; read the new service's slug and repoint the `www` CNAME at it;
   restore the dump into the new database; update the `PROD_DATABASE_URL` repo
   secret (its header comment still says "comma-db", which was already stale).
6. **Turn Auto Sync back on.** It is the correct default. It was only ever wrong
   because the Blueprint underneath it was.

Verify: apex 200, `www` 301 to apex, feed renders notes, Google login completes,
`render blueprints validate ./render.yaml` reports `totalActions: 1`.

### Rollback

Between steps 3 and 5 the only copy of the data is the dump. If the rebuild
stalls, the old stack is gone — recovery is "create a database, restore the
dump, point a service at it", not "undo". That is the risk being accepted, and
it is acceptable only because there are **0 users** and the 4 accounts are the
founders.

---

## Target `render.yaml`

Differs from the committed file in four ways: database renamed to a
never-used name, `diskSizeGB: 1`, `storageAutoscalingEnabled: true`, and
`region` pinned on both resources.

`commaplace-db`, not `comma-db` — the latter has been taken at least once
(it is why the current database was born as `comma-db-c8h6`), and a collision at
creation is the exact trap this rebuild exists to escape. The database name
appears in no URL, so a free name costs nothing.

```yaml
services:
  - name: comma
    type: web
    runtime: docker
    plan: starter
    region: oregon
    dockerfilePath: ./Dockerfile
    autoDeploy: true
    envVars:
      - key: DATABASE_URL
        fromDatabase:
          name: commaplace-db
          property: connectionString
      - key: PORT
        value: "10000"
      - key: ADDR
        value: ":10000"
      # Regenerated on recreation, which logs out every open session.
      # Harmless pre-launch; set it by hand once real users exist.
      - key: SESSION_SECRET
        generateValue: true
      - key: SEED_DEV
        value: "0"
      - key: BASE_URL
        value: https://www.commaplace.app
      - key: GOOGLE_CLIENT_ID
        value: 826692770873-gvmbdlsltemcgh0aie4jtra1jhu1f91e.apps.googleusercontent.com
      - key: GOOGLE_CLIENT_SECRET
        sync: false
      - key: ADMIN_HANDLE
        sync: false
      - key: PLAYTEST_LOGIN_KEY
        sync: false

databases:
  - name: commaplace-db
    plan: basic-256mb
    region: oregon
    postgresMajorVersion: "17"
    diskSizeGB: 1
    storageAutoscalingEnabled: true
    ipAllowList:
      - source: 0.0.0.0/0
        description: everywhere
```

`storageAutoscalingEnabled: true` is the part that makes 1 GB safe rather than
brave: at ~90% full Render grows the disk to the next 5 GB multiple, with a
12-hour cooldown. Growth is one-way — which is how the current database reached
15 GB — so start at the floor deliberately.

`ipAllowList` must stay open for `db-backup.yml`: the workflow runs `pg_dump`
from a GitHub runner, and Render's internal `dpg-*-a` hostname resolves only
inside their network.

---

## Verified, with sources

Checked against Render's own tooling rather than assumed. The Blueprint
validator is **strict** — it rejects unknown fields, invalid plans and invalid
disk sizes — which is what makes a passing validation meaningful:

```
unknown field       → "field thisFieldDoesNotExist not found in type file.Database"   valid:false
plan: nonsense-plan → "nonsense-plan not a valid plan"                                valid:false
diskSizeGB: 3       → "database disk size must be a multiple of 5GB"                  valid:false
diskSizeGB: 1       → valid:true          # 1 GB is the allowed floor
```

- Storage $0.30/GB/month, billed separately from compute:
  [Flexible Plans for Render Postgres](https://docs.render.com/postgresql-refresh)
- Collision-suffix behaviour and "syncing never deletes an existing resource":
  [Render Blueprints (IaC)](https://render.com/docs/infrastructure-as-code)
- `name` immutable after creation, and the match key for adoption:
  [Blueprint YAML Reference](https://render.com/docs/blueprint-spec)
- Field names cross-checked against `https://render.com/schema/render.yaml.json`

## Not verified

- Whether `comma.onrender.com` is reclaimable. Only creating a service named
  `comma` answers it. Nothing depends on the answer — the custom domain works
  either way; it is cosmetic.
- Whether the Blueprint creation flow lets you *decline* the collision suffix.
  Step 4 sidesteps the question by leaving no name to collide with.
- Whether prod `comma`'s "Blueprint managed" dashboard badge is stale. The API
  lists the Blueprint's resources as empty, so the badge and the API disagree.
  Reading: it was created by an earlier, since-deleted Blueprint — which would
  also explain its `comma-c8h6` slug. Unprovable; deleted Blueprints are not
  queryable.
