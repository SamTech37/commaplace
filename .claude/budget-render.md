# Render Hosting Budget — commaplace

Source: Render pricing (render.com/pricing) + this session's egress audit.
Postgres numbers re-derived 2026-08-30 from the actual August invoice. Figures
may change — re-verify before relying.

## Verdict
$10–100/mo for an always-on hobby/passion deploy is **sane, not insane**. Real
floor ≈ **$13/mo** (~$4.50 each across 3 founders).

## Corrections to prior assumptions
- **No-sleep does NOT need Pro.** The **Starter web instance ($7/mo)** already
  never sleeps — kills the 30 s cold start for $7, not $85 (Pro instance) or $25
  (Pro *workspace* = team governance, unrelated to uptime).
- **Free Postgres expires after 30 days** — prod needs paid PG from day one.
- **Postgres compute and storage bill separately** (Render's flexible plans).
  The 2026-06-20 version of this file priced the *legacy bundled* plans —
  "Basic-1gb $19/mo" — and that single stale line is where its $26 floor came
  from. Storage is **$0.30/GB/month**, prorated to the second, and you pick the
  size independently of the instance type.

## Minimum always-on stack
| Component | Tier | $/mo |
|-----------|------|------|
| Web service | Starter (no sleep, 0.5 CPU / 512 MB) | 7.00 |
| Postgres compute | Basic-256mb ($0.0081/hr on the invoice) | 5.91 |
| Postgres storage | 1 GB @ $0.30/GB/mo | 0.30 |
| **Floor** | (Hobby workspace $0) | **≈13.20** |
| + Key Value (cache, optional) | Starter (256 MB) | +10 |

Sanity check: Render's own write-up puts starter web + basic-256mb at "about
$13/month before bandwidth and storage growth" — same number.

**Storage only grows.** Render never shrinks a disk, so an oversized one is
permanent until you rebuild the database. The August invoice was $4.46/mo for a
15 GB disk holding 10 MB — 15x the floor's whole storage line, for nothing.
Provision small and turn **Storage Autoscaling on**; it grows at ~90% full to
the next 5 GB multiple, with a 12-hour cooldown between increases.

## Bandwidth (the real risk)
- Included: **5 GB Hobby / 25 GB Pro**, then overage.
- **The 19.7 MB CJK font on every page is the threat** — ~250 first-visits = 5 GB.
  Fix = unicode-range split. gzip does NOT help woff2 (already compressed).
- Server-rendered htmx (HTML fragments, no JSON+client-render, no JS framework
  bundle) already minimizes egress — keep it.

## Free credit
[Unverified] $50 credits are typically per-account + time-limited; likely can't
stack 3 founders onto one workspace. Treat as ~1–2 months runway on the paid stack,
not $150 of value. Verify terms.

## DBs (local docker except prod)
prod (Render, air-gapped) · `commaplace` (dev, MCP read-only) · `benchmark` (load
testing, 100K rows) · `commaplace_test` (`make test`).
