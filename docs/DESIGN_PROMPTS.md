# Commaplace — Design Direction

**美學**：小紅書式 feed card × Medium 閱讀體驗 × 純黑白 × 滑頁動畫
**圓角**：全站保留圓弧，卡片 16px，按鈕 8px，pill 999px
**顏色**：只有 #000、#fff、灰階，零色彩

---

## 色彩系統

```css
/* Light */
--bg: #ffffff;
--bg-2: #f8f8f8;
--bg-3: #f0f0f0;
--text: #111111;
--text-2: #555555;
--text-3: #999999;
--border: #e8e8e8;
--border-2: #cccccc;
--black: #000000;
--white: #ffffff;
--radius-card: 16px;
--radius-btn: 8px;
--radius-pill: 999px;
--ease: cubic-bezier(0.16, 1, 0.3, 1);

/* Dark */
--bg: #0c0c0c;
--bg-2: #161616;
--bg-3: #1e1e1e;
--text: #f2f2f2;
--text-2: #999999;
--text-3: #555555;
--border: #222222;
--border-2: #333333;
```

---

## Feed — 小紅書 Waterfall 卡片

- **瀑布流** masonry 雙欄（手機）→ 三欄（桌機）
- 卡片 border-radius: 16px，overflow: hidden
- 卡片背景：純白，box-shadow: 0 2px 12px rgba(0,0,0,0.06)
- hover：box-shadow 加深，translateY(-2px)，duration 0.3s
- 卡片標題：font-weight 600，16px，黑色
- excerpt：14px，灰色，line-clamp 3
- meta（作者、日期）：12px，淡灰，底部
- 移除所有彩色 avatar dot，改為純黑字 @handle

---

## 文章頁 — Medium 閱讀

- 正文最大寬度 680px，置中
- font-size 19px，line-height 1.85，color #444
- 標題 44px，font-weight 700，letter-spacing -0.02em
- author bar 無 card 背景，inline 水平排列
- blockquote：大字斜體，置中，上下 padding 32px

---

## Topbar

- 高度 56px，白底
- sticky + backdrop-filter: blur(20px) saturate(180%) rgba(255,255,255,0.85)
- nav links 13px，灰色，hover 黑色
- 搜尋框：圓角 pill 形狀，border: 1px solid --border

---

## 動畫系統

```css
[data-reveal] {
  opacity: 0;
  transform: translateY(24px);
  transition: opacity 0.6s ease, transform 0.6s ease;
}
[data-reveal].revealed {
  opacity: 1;
  transform: none;
}
```
- IntersectionObserver，threshold 0.1
- 卡片 stagger 0.06s/item（最多 0.36s）
- 頁面進場整體 fade-in

---

## 06-30
commaplace-fixes.css — drop-in patch, merge into style.css or load last. Headline values (all WCAG-verified on the cream bg): --text-3 #9c9080 → #6f6450 (2.5:1 → 4.6:1), --text-2 → #5f5544, dark --text-3 → #968a72, .masonry-card fill --bg → --bg-2, plus --ink/--paper aliases, an --fs-* scale, and one unified note-title rule.

claude-code-fix-prompt.md — paste straight into Claude Code. Bounded to a token/consistency pass with explicit acceptance criteria (zero AA failures, no px font-sizes, no --black/--white consumers, identical-looking note titles, minimal visual diff).

One-line rule for CLAUDE.md:

    Tokens are the source of truth: every text/background pair must pass WCAG AA (4.5:1, or 3:1 for text ≥24px), every font-size uses a --fs-* token (never literal px), every radius uses a --r-* token, and token names describe role not value (--ink/--paper, never --black/--white).

(Merged into style.css 2026-07-05 — tokens live in :root; --black/--white/--teal/--info/--warning aliases deleted outright since nothing consumed them.)

---

## 不做

- 無彩色（accent blue、teal、red 全移除）
- 無 box-shadow 過度堆疊（最多一層）
- 不破壞 HTMX 屬性和 Go template 邏輯
