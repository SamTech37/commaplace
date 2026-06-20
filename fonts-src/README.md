# Original (un-split) Source Han Serif variable fonts

Kept OUT of `internal/handlers/static/` so `go:embed` does NOT bundle them into
the binary (each is ~19.7MB). They are the *source* for the unicode-range split.

- `SourceHanSerifTC-VF.otf.woff2` — Traditional. Already split into
  `internal/handlers/static/fonts/tc/` (668 chunks + result.css) via cn-font-split.
- `SourceHanSerifSC-VF.otf.woff2` — Simplified. NOT yet split; do this when
  simplified-Chinese support ships (plan.md Should-Have).

Re-split command:
  npx -y cn-font-split@latest run -i fonts-src/SourceHanSerifSC-VF.otf.woff2 \
    -o internal/handlers/static/fonts/sc \
    --css.fontFamily "Source Han Serif" --css.fontWeight "300 900" \
    --css.fontDisplay swap --css.fileName result.css --testHtml false --reporter false
