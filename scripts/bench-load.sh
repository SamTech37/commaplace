#!/usr/bin/env bash
# Load batches behind docs/BENCHMARK-2026-10-12.md. Needs curl, psql, vegeta,
# a DB seeded by scripts/bench-seed.sql with pg_stat_statements installed, and
# the server running against it with DEBUG=1 (so /_dev/login mints cookies):
#
#   DB=postgres://…/benchmark scripts/bench-load.sh [single diag mix cheap writes]
#
# No section names = all of them. BASE defaults to http://localhost:8297.
set -euo pipefail
BASE=${BASE:-http://localhost:8297}
DB=${DB:?set DB to the benchmark database URL}
W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT

q() { psql "$DB" -XAtc "$1"; }
cookie() { curl -s -o /dev/null -D - "$BASE/_dev/login?as=$1" | grep -io 'Comma_session=[^;]*'; }
note_id() { q "SELECT n.id FROM notes n JOIN users u ON u.id = n.author_id WHERE u.handle = '$1' AND n.slug = '$2'"; }
readable="n.published_at IS NOT NULL AND n.hidden_at IS NULL AND n.deleted_at IS NULL"
note_urls() { q "SELECT '/' || u.handle || '/' || n.slug FROM notes n JOIN users u ON u.id = n.author_id
                 WHERE $readable ORDER BY md5(n.slug) LIMIT $1"; }
profile_urls() { q "SELECT '/' || handle FROM users ORDER BY md5(handle) LIMIT $1"; }
gets() { for u in "$@"; do printf 'GET %s%s\n\n' "$BASE" "$u"; done; }

# Mean wall time of 10 sequential GETs after one warm-up, then the two
# statements with the highest mean time while they ran (pg_stat_statements).
seq10() {
  local h=(); [[ -n ${2:-} ]] && h=(-H "Cookie: $2")
  curl -s -o /dev/null "${h[@]}" "$BASE$1"
  q "SELECT pg_stat_statements_reset(0, (SELECT oid FROM pg_database WHERE datname = current_database()), 0)" >/dev/null
  for _ in $(seq 10); do curl -s -o /dev/null -w '%{time_total} %{size_download} %{http_code}\n' "${h[@]}" "$BASE$1"; done |
    awk -v u="$1" '{s += $1; b = $2; c = $3} END {printf "%-44s %7.1f ms %10d B  %s\n", u, s * 100, b, c}'
  q "SELECT round(mean_exec_time::numeric, 1) || ' ms  ' || left(regexp_replace(query, '\s+', ' ', 'g'), 100)
     FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
       AND calls >= 10 AND query !~ 'pg_stat' ORDER BY mean_exec_time DESC LIMIT 2" | sed 's/^/    /'
}

# vegeta at a fixed rate, sampling the app's Postgres connections every 0.5 s,
# plus whole-box CPU (server + Postgres + vegeta share it) and the number of
# Postgres sessions the app opened over the run (the sampler's own excluded).
cpu() { awk '/^cpu / {print $2+$3+$4+$5+$6+$7+$8+$9, $5+$6}' /proc/stat; }
sessions() { q "SELECT sessions FROM pg_stat_database WHERE datname = current_database()"; }
attack() { # targets-file rate duration
  local c0 n0; c0=$(cpu); n0=$(sessions)
  (while :; do q "SELECT count(*) FILTER (WHERE state = 'active') || '|' || count(*) FROM pg_stat_activity
                  WHERE backend_type = 'client backend' AND datname = current_database() AND pid <> pg_backend_pid()"
               sleep 0.5; done) > "$W/conns" &
  local s=$!
  vegeta attack -targets="$1" -rate="$2" -duration="$3" -timeout=60s > "$W/res"
  kill $s; wait $s 2>/dev/null || true
  local busy opened; busy=$(echo "$c0 $(cpu)" | awk '{printf "%d", 100 * (1 - ($4 - $2) / ($3 - $1))}')
  opened=$(( $(sessions) - n0 - $(wc -l < "$W/conns") - 1 ))
  echo "-- $2/s for $3"
  vegeta report "$W/res" | grep -E '^(Requests|Latencies|Status)'
  awk -F'|' -v cpu="$busy" -v opened="$opened" '$1 > a {a = $1} $2 > t {t = $2} END {
    printf "DB client backends: max active %d, max total %d; sessions opened %d; CPU busy %d%%\n", a, t, opened, cpu}' "$W/conns"
}

single() {
  echo "== 1. per endpoint, uncontended"
  local c1 n5; c1=$(cookie u001); n5=$(note_id u005 note-5)
  seq10 /u005/note-5
  seq10 /u005
  seq10 /tag/t499
  seq10 '/feed?tab=following' "$c1"
  seq10 "/api/graph/local?note=$n5"
  seq10 "/api/graph/local?note=$n5&hops=2"
  seq10 '/api/wiki/suggest?q=note-12' "$c1"   # matches no title: worst case
  seq10 '/api/wiki/suggest?q=w1234' "$c1"     # matches 16 titles
  seq10 /feed
  seq10 /tag/t0
  seq10 '/search?q=w1234'
  seq10 /api/graph
  curl -s -o /dev/null -H 'Accept-Encoding: gzip' -w '/api/graph gzipped: %{size_download} B\n' "$BASE/api/graph"
}

# The app's search and tag-chip SQL (search.go, feed.go loadTopTagChips),
# copied here, under EXPLAIN ANALYZE.
diag() {
  echo "== diagnosis"
  explain "search, as shipped (tsv OR handle ILIKE)" "$(search_sql "(n.search_tsv @@ query OR u.handle ILIKE '%w1234%')")"
  explain "search, FTS predicate alone" "$(search_sql "n.search_tsv @@ query")"
  explain "tag chips aggregate (uncached)" "SELECT nt.tag, count(*) c FROM note_tags nt JOIN notes n ON n.id = nt.note_id
    WHERE n.updated_at > extract(epoch FROM now())::bigint - 30 * 86400 AND $readable AND n.distribution = 'public'
    GROUP BY nt.tag ORDER BY c DESC, nt.tag LIMIT 8"
}
search_sql() {
  echo "SELECT n.id, ts_headline('simple', n.body_md, query, 'StartSel=<mark>, StopSel=</mark>, MaxFragments=1, MaxWords=16, MinWords=8')
    FROM notes n JOIN users u ON u.id = n.author_id CROSS JOIN to_tsquery('simple', '(w1234:*)') query
    WHERE $1 AND $readable ORDER BY ts_rank(n.search_tsv, query) DESC LIMIT 50"
}
explain() { # label sql — warm (second) run
  q "EXPLAIN ANALYZE $2" >/dev/null
  echo "-- $1"
  q "EXPLAIN ANALYZE $2" | grep -E 'Scan on|Rows Removed by Join|Execution Time' | sed 's/^ */    /'
}

mix() {
  echo "== 2. mixed reads: 16-target round robin, 20 s per rate"
  { gets /feed /feed /feed $(note_urls 6) $(profile_urls 3) /tag/t499 /tag/t0 '/search?q=w1234'
    printf 'GET %s/feed?tab=following\nCookie: %s\n\n' "$BASE" "$(cookie u001)"; } > "$W/mix"
  for r in 5 10 20 40; do attack "$W/mix" "$r" 20s; done
  echo "== 2b. one endpoint at a time, 20/s for 10 s"
  for u in /feed /tag/t0 '/search?q=w1234' '/api/wiki/suggest?q=note-12' /tag/t499 /u005 /u005/note-5; do
    gets "$u" > "$W/one"; echo "$u"; attack "$W/one" 20 10s
  done
}

cheap() {
  echo "== 3. cheap pages: 30 note pages + 10 profiles, 15 s per rate"
  gets $(note_urls 30) $(profile_urls 10) > "$W/cheap"
  for r in 100 200 400; do attack "$W/cheap" "$r" 15s; done
}

writes() {
  echo "== 4. writes from u001..u010, 15 s per rate"
  # "# Bench draft\n\nAutosave #bench #load [[note-1]] [[note-2]]": 2 tags + 2 wiki links to recompute.
  printf 'document=%%23%%20Bench%%20draft%%0A%%0AAutosave%%20%%23bench%%20%%23load%%20%%5B%%5Bnote-1%%5D%%5D%%20%%5B%%5Bnote-2%%5D%%5D' > "$W/save.body"
  printf 'note_id=%s' "$(note_id u005 note-5)" > "$W/like.body"
  : > "$W/save"; : > "$W/like"
  for i in $(seq -w 1 10); do
    local h=u0$i c draft
    c=$(cookie "$h")
    draft=$(q "SELECT n.id FROM notes n JOIN users u ON u.id = n.author_id WHERE u.handle = '$h'
               AND n.published_at IS NULL AND n.hidden_at IS NULL AND n.deleted_at IS NULL ORDER BY n.slug LIMIT 1")
    printf 'PATCH %s/api/notes/%s\nCookie: %s\nContent-Type: application/x-www-form-urlencoded\n@%s\n\n' \
      "$BASE" "$draft" "$c" "$W/save.body" >> "$W/save"
    printf 'POST %s/api/like\nCookie: %s\nContent-Type: application/x-www-form-urlencoded\n@%s\n\n' \
      "$BASE" "$c" "$W/like.body" >> "$W/like"
  done
  echo "autosave, each user PATCHing their own draft"
  for r in 50 100; do attack "$W/save" "$r" 15s; done
  echo "like toggle, all ten users on u005/note-5"
  for r in 100 300; do attack "$W/like" "$r" 15s; done
}

for s in ${*:-single diag mix cheap writes}; do "$s"; done
