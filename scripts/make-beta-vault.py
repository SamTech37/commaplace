"""Generate a disposable 800-note, three-year Obsidian-style beta vault.

Usage: python scripts/make-beta-vault.py PATH [--count 800]
Never points at or modifies a real vault; PATH must not already exist.
"""
import argparse
from datetime import datetime, timedelta, timezone
import os
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("output", type=Path)
parser.add_argument("--count", type=int, default=800)
args = parser.parse_args()
if not 1 <= args.count <= 2000:
    parser.error("count must be 1..2000")
args.output.mkdir(parents=True, exist_ok=False)
dates = [datetime(2023, 1, 1, tzinfo=timezone.utc) + timedelta(days=i * 1094 // max(1, args.count - 1)) for i in range(args.count)]
paths = [f"{date.year}/筆記-{i + 1:04}.md" for i, date in enumerate(dates)]
for i, (relative, date) in enumerate(zip(paths, dates)):
    destination = paths[(i + 1) % len(paths)][:-3]
    content = f'''---
title: "Beta 筆記 {i + 1:04}"
date: {date:%Y-%m-%d}
tags:
  - beta-vault
  - 驗證
custom_property: 保留原始屬性
---
# Beta 筆記 {i + 1:04}

這是產生的測試資料，不是真實私人筆記。原始日期：{date:%Y-%m-%d}。

[[{destination}#觀察|下一篇]]

## 觀察

> [!note] 測試 Callout
> 驗證大量匯入後是否仍可閱讀。

- [x] 已匯入
- [ ] 連結跳轉

==醒目文字==、**粗體**、*斜體*、$x^2$。

| 事件 | 結果 |
| --- | --- |
| 匯入 | 待觀察 |

%% 此註解應保留在原始碼，閱讀頁不顯示。 %%

```text
[[這是程式碼，不應改写]]
```

![應移除的圖片](https://example.invalid/tracker.png)
![[attachments/audio.mp3]]
[保留的超連結](https://example.org)
'''
    file = args.output / relative
    file.parent.mkdir(parents=True, exist_ok=True)
    file.write_text(content, encoding="utf-8")
    os.utime(file, (date.timestamp(), date.timestamp()))
(args.output / ".obsidian").mkdir()
(args.output / ".obsidian" / "ignored.md").write_text("This file must not be uploaded.", encoding="utf-8")
(args.output / "ignored.png").write_bytes(b"not an image; must not be uploaded")
print(f"Created {args.count} Markdown notes in {args.output.resolve()}")
