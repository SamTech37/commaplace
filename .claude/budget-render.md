# Render Hosting Budget — commaplace

Source: Render pricing (render.com/pricing, read 2026-06-20) + this session's
egress audit. Figures may change — re-verify before relying.

## Verdict
$10–100/mo for an always-on hobby/passion deploy is **sane, not insane**. Real
floor ≈ **$26/mo** (~$9 each across 3 founders). You live in the $26–51 band until
real traffic.

## Two corrections to prior assumptions
- **No-sleep does NOT need Pro.** The **Starter web instance ($7/mo)** already
  never sleeps — kills the 30 s cold start for $7, not $85 (Pro instance) or $25
  (Pro *workspace* = team governance, unrelated to uptime).
- **Free Postgres expires after 30 days** — prod needs paid PG from day one.

## Minimum always-on stack
| Component | Tier | $/mo |
|-----------|------|------|
| Web service | Starter (no sleep, 0.5 CPU / 512 MB) | 7 |
| Postgres | Basic-1gb (0.5 CPU / 1 GB, 100 conns) | 19 |
| **Floor** | (Hobby workspace $0) | **26** |
| + Key Value (cache, optional) | Starter (256 MB) | +10 |
| **With cache** | | **36** |

Headroom: Standard web ($25) + Basic-4gb PG ($75) = ~$100 = serious capacity.

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
