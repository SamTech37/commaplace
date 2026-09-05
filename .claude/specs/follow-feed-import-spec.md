# SPEC — 追蹤可見化 × feed 分散 × 匯入擋媒體

> 2026-09-04 訪談定案。三件事一起做，三個 commit。

## 三個前置決定（使用者拍板）

| 決定 | 直接後果 |
| --- | --- |
| 匯入**維持一鍵直接發布**，不改成草稿 | feed 的 per-author 分散從「可選」變成**必須**——第一個大量匯入的人當天就會洗版 |
| **不做附件**，不引入 object storage | 匯入的媒體沒有地方可放，所以只能擋。這不是取捨，是唯一自洽的選項 |
| Render Postgres 方案**不升** | 封測十幾人，`COUNT(*)`、window function 都不需要為規模讓步 |

---

# 一、追蹤可見化（不動 DB）

## 起因

回報是「沒有追蹤創作者的功能，所以永遠 0 follower · 0 following」。功能其實完整存在
（`POST /api/follow`、`follows` 表、profile 與筆記頁各一顆按鈕、feed「追蹤中」分頁），
真正的問題是**按下去毫無回饋**，於是被合理地推論成不存在。

## 現況的三個缺陷

`follow.go:writeFollowFragment` 用 `fmt.Fprintf` 手寫 HTML，跟兩個 templ 呼叫點都對不上：

| | profile 原始 | 筆記頁原始 | swap 後回傳 |
| --- | --- | --- | --- |
| class | `inline-form follow-form` | `inline-form` | `inline-form follow-form` |
| 文案 | `Follow` / `Following` | `+ 追蹤` / `已追蹤` | `Follow` / `Following` |
| 計數 | 無 | 無 | ` · N followers` |

1. **筆記頁按一下追蹤，中文按鈕會變成英文 `Following`**，旁邊還憑空多出 ` · 1 follower`，
   而作者列本來就有一份「N 訂閱者」——同一個數字出現兩次。
2. **計數不在 swap 範圍內**。`hx-target="this"` 只換掉 form，`.profile-meta` 的計數不會動。
3. 三處三種說法：`follower` / 訂閱者 / `followers`。

## 做法

**一份 markup 一個來源。** 新增 `internal/handlers/follow.templ`，templ 頁面與 Go handler 共用：

```
templ followForm(targetID uuid.UUID, following bool)          // 只有按鈕，不含計數
templ followCount(handle, rel string, n int, attrs templ.Attributes)
```

`followCount` 吃 `templ.Attributes`：頁面傳 `nil`，handler 回傳時傳
`templ.Attributes{"hx-swap-oob": "outerHTML"}`。同一份 markup，OOB 與否只差一個屬性。
`writeFollowFragment` 與只有它在用的 `plural()` 一併刪除。

**只 OOB 一個計數。** 追蹤只改變對方的追蹤者數；自己的「追蹤中」只出現在自己的 profile，
而你不可能在自己的 profile 按追蹤（`follow.go:26` 與 `profile_page.templ:84` 各擋一次）。

**文案統一**：`N 追蹤者` / `M 追蹤中`。筆記頁作者列的「訂閱者」一併改掉，
且只把追蹤者那段包進 OOB 目標，篇數不受影響。

## 名單面板

點計數就地展開，不換頁、不新增 tab（profile 已有 時間軸／月曆／圖譜 三個 tab）。

**沿用既有的 `.action-menu` `<details>` 慣例，不寫任何新 JS**：

```html
<details class="action-menu follow-menu">
  <summary><span id="follow-count-followers">3 追蹤者</span></summary>
  <div class="action-menu-list" hx-get="/api/follows/{handle}?rel=followers"
       hx-trigger="toggle once" hx-target="this">…</div>
</details>
```

白拿 `cf1c74b` 已經做好的 Esc 關閉並還原焦點、點外面關閉、`aria-expanded` 從 open 推導。
`toggle once` 讓名單第一次展開才載入。

**路由**：`GET /api/follows/{user}?rel=followers|following` → HTML fragment。
`/api/` 前綴，與 `/{user}/{slug}` catch-all 無關。

**刻意不做可分享的名單網址**：`/{user}/followers` 會與「slug 叫 followers 的筆記」永久撞名。

**查詢**（兩個方向都吃現成索引，`LIMIT 100` 無分頁）：

```sql
-- rel=followers → idx_follows_followed
SELECT u.id, u.handle FROM follows f JOIN users u ON u.id = f.follower_id
WHERE f.followed_id = $1 ORDER BY f.created_at DESC LIMIT 100
-- rel=following → idx_follows_follower_created
SELECT u.id, u.handle FROM follows f JOIN users u ON u.id = f.followed_id
WHERE f.follower_id = $1 ORDER BY f.created_at DESC LIMIT 100
```

**名單裡放追蹤按鈕**是這份 spec 唯一會讓 0 變成 1 的機制。這不是推薦系統——沒有排序、
沒有演算法、沒有主動推播，只是一份你自己點開的名單。未登入者看得到名單（Decision 7 全公開），
只是沒有按鈕；自己那一列也沒有。

空狀態：`還沒有人追蹤` / `還沒有追蹤任何人`。依 DESIGN_PROMPTS 不補「去探索看看吧」。

## 不動 DB

`follows` 已是對的形狀，`PRIMARY KEY (follower_id, followed_id)` +
`idx_follows_followed` + `idx_follows_follower_created` 涵蓋兩個查詢方向。
十幾人規模不加計數快取欄位——那會多出一個會跟 `follows` 不同步的第二真相來源。**零 migration。**

---

# 二、feed per-author 分散

## 兩個問題，不是一個

`feed.go:141-146`：

```go
if older > 0 { fmt.Fprintf(&q, ` AND n.updated_at < $%d`, len(args)) }
fmt.Fprintf(&q, ` ORDER BY n.updated_at DESC LIMIT $%d`, len(args))
```

1. **沒有 per-author 分散。** 一個大量匯入的使用者佔滿全站 feed 前幾頁。
   因為匯入維持直接發布，這是第一個人就會發生，不是一千個人才會。
2. **同秒 timestamp 會讓筆記永久消失。** 游標是 `last.UpdatedAt`，下一頁查
   `updated_at < T`。若該頁 16 張卡片的 `updated_at` 全等於 T，下一頁從「小於 T」開始，
   **中間剩下同為 T 的筆記全部不可達**。不是排在後面，是捲不到。

第 2 點使第 1 點的修法受限：只加分散不改游標會讓情況更糟——大量匯入者每頁只出 3 篇，
其餘同秒的全部被游標吃掉。**兩個必須一起修。**

## 為什麼不用隨機排序

使用者提議「隨機即可解決」。隨機與 `?older=<timestamp>` 這個游標互斥：cursor 的前提是有序。
`/search` 當初正是為此放棄分頁（`.claude/htmx-rules.md` 第 4 條：`ts_rank` 排序沒有便宜的
cursor，`OlderURL` 留空）。改隨機等於讓 feed 失去無限捲動。

## 做法

CTE + window function，排序仍然確定、游標仍然可用：

```sql
WITH ranked AS (
  SELECT n.id,
         ROW_NUMBER() OVER (PARTITION BY n.author_id
                            ORDER BY n.updated_at DESC, n.id DESC) AS rn
  FROM notes n
  WHERE n.hidden_at IS NULL AND n.deleted_at IS NULL AND n.published_at IS NOT NULL
    [AND EXISTS (tag filter)]
    [AND (n.updated_at, n.id) < ($a, $b)]        -- 複合游標，解掉同秒跳號
)
SELECT <noteCardColumns>
FROM notes n JOIN users u ON u.id = n.author_id
JOIN ranked r ON r.id = n.id AND r.rn <= 3
ORDER BY n.updated_at DESC, n.id DESC
LIMIT $n
```

篩選條件只寫在 CTE 一處。每頁每位作者最多 3 篇；游標推進後 `rn` 重算，下一頁換他的下 3 篇。

**游標格式改為 `?older=<updated_at>&older_id=<uuid>`。** 只有 `/feed` 改。

**`olderURL` 的判斷要一起改。** 現在是 `len(cards) == FeedPageSize`；加了 `rn <= 3` 之後
頁面可能在還有筆記時就填不滿（作者少時），會讓無限捲動提早停止。改成 `len(cards) > 0`，
代價是最尾端多一次回傳零筆的請求，由 OOB sentinel 換成「沒有更多了」。

## 已知未修（明確不在這次範圍）

`/tag`、`/me/saved`、profile 時間軸用同一個單欄位游標，同秒 timestamp 的跳號問題一模一樣。
本次只修 `/feed`。要一起修就是同樣的複合游標套四處，可獨立進行。

---

# 三、匯入擋媒體

## 現況：raw HTML 已經安全，洞在 markdown 原生語法

`render.go:58` 是 `goldmark.New(...)`，**沒有** `html.WithUnsafe()`，所以 `.md` 裡的
`<img>` `<video>` `<audio>` `<iframe>` 已被 goldmark 預設轉義成純文字。這條路是通的。

沒擋的是 `![alt](url)`，而且它不只出現在內文：

```go
c.ImageURL = markdown.FirstImageURL(n.Body)   // feed.go:278 → 卡片縮圖 <img src=...>
```

匯入一篇含 `![](https://tracker.example/px.gif)` 的筆記，那個遠端網址會被**熱連結到全站
feed 的卡片縮圖**。每個看 feed 的人都會去打那台伺服器——追蹤像素、IP 外洩、對方換圖不受控。
這比「內文有張圖」嚴重得多。

另有 Obsidian vault 幾乎必然存在的 `![[Pasted image 20240101.png]]` 附件嵌入，
走的是 `embedExt`（`render.go:853`），與 `![](url)` 不同路徑，要分開處理。

## 做法

新增 `markdown.StripMedia(md string) (string, int)`，回傳清洗後的內文與移除數量：

| 語法 | 處理 |
| --- | --- |
| `![alt](url)` | 移除 |
| `![[檔名.png/jpg/gif/webp/svg/mp4/webm/mov/mp3/wav/ogg/m4a…]]` | 移除 |
| `![[筆記名]]`（無副檔名） | **保留**——這是筆記嵌入，不是附件 |
| `<img> <video> <audio> <source> <iframe> <embed> <object>` | 移除 |
| `[文字](url)`、裸網址 | **保留**——超連結是允許的 |

**套用點**：`import.go:parseUploadedNote`。單檔與批次都經過它，一處即可。

**不碰 `note_images`。** `POST /api/notes/{id}/image` 是刻意做的封面圖功能，
圖片以 bytea 存在自家 DB，沒有熱連結問題。擋的是「匯入內容裡的外部媒體引用」，
不是使用者自己上傳的封面。

**告知**：批次預覽在移除數 > 0 時顯示「已移除 N 個媒體」。只在真的發生時出現，
不在匯入頁常駐一段說明（`f387411` 才剛依 DESIGN_PROMPTS 刪掉那類文案）。
單檔路徑直接存檔後轉址，沒有顯示位置，靜默移除。

## 已知未修

`/write` 編輯器手寫的 `![](https://…)` 仍會熱連結到 feed 縮圖。本次要求的範圍是匯入。
要一起堵就是在 `saveNote` 層套同一個 `StripMedia`，可獨立進行。

---

# 驗收條件

**追蹤**
1. `POST /api/follow` 回應同時含按鈕與帶 `hx-swap-oob` 的 `#follow-count-followers`，數字為 toggle 後的值
2. 再 POST 一次，OOB 數字回到 `0 追蹤者`
3. 回應不含 `Follow` / `Following` / `follower` 任何英文字串
4. `GET /api/follows/{user}?rel=followers` 列出追蹤者 handle；`rel=following` 列出被追蹤者
5. `rel` 非法值 → 400
6. 未登入取名單 → 200 且不含 `hx-post="/api/follow"`
7. profile 與筆記頁 markup 各含一個 `id="follow-count-followers"`（OOB 目標必須存在，htmx-rules #7：不存在時 htmx 靜默不動）

**feed**
8. 一位作者發 10 篇、另一位發 1 篇 → 第一頁最多含該作者 3 篇
9. 20 篇 `updated_at` 全部相同 → 逐頁捲到底可取回全部 20 篇，無重複無遺漏
10. 捲到底最終出現「沒有更多了」

**匯入**
11. 含 `![](https://x/y.png)` 的 .md → 存下的 `body_md` 不含該語法，`FirstImageURL` 回傳 ""
12. 含 `![[a.png]]` 與 `![[某筆記]]` → 前者移除，後者保留
13. 含 `<img src=...>` / `<video>` / `<audio>` → 移除
14. 含 `[文字](https://x)` → 原樣保留
15. 批次預覽在移除數 > 0 時顯示移除筆數
