// Medium-style note editor built on EasyMDE (CodeMirror 5). The textarea is the
// single writing surface; markdown stays the source of truth. CM handles
// IME/caret/undo/mobile. We wire: custom text toolbar (no FontAwesome), wiki-link
// autocomplete, async image upload, debounced autosave, and publish.
(function () {
  var ta = document.getElementById("doc");
  var page = document.querySelector(".editor-page");
  if (!ta || !page || typeof EasyMDE === "undefined") return;

  var noteId = page.dataset.noteId;
  var statusEl = document.getElementById("save-status");
  var popup = document.getElementById("ac-popup");
  var list = popup ? popup.querySelector("ul") : null;
  var recoveryEl = document.getElementById("editor-recovery");
  var wordCountEl = document.getElementById("word-count");
  var characterCountEl = document.getElementById("character-count");
  var cursorPositionEl = document.getElementById("cursor-position");
  var previewEl = document.getElementById("preview");
  var previewButton = null;

  function tb(name, action, text, title) {
    return { name: name, action: action, text: text, title: title, className: "tb-" + name };
  }

  var toolbar = [
    tb("bold", EasyMDE.toggleBold, "粗體", "粗體 (Ctrl/Cmd + B)"),
    tb("italic", EasyMDE.toggleItalic, "斜體", "斜體 (Ctrl/Cmd + I)"),
    "|",
    tb("h1", EasyMDE.toggleHeading1, "標題", "一級標題"),
    tb("h2", EasyMDE.toggleHeading2, "小標", "二級標題"),
    tb("quote", EasyMDE.toggleBlockquote, "縮排", "縮排段落（Markdown 引用）"),
    "|",
    tb("bullets", EasyMDE.toggleUnorderedList, "• 清單", "項目符號清單"),
    tb("numbers", EasyMDE.toggleOrderedList, "1. 編號", "編號清單"),
    "|",
    tb("wikilink", insertWikiLink, "Wiki", "Wiki 連結"),
    tb("link", EasyMDE.drawLink, "連結", "外部連結"),
    tb("tag", insertTag, "標籤", "標籤"),
    "|",
    tb("image", uploadImage, "圖片", "插入圖片"),
    tb("code", EasyMDE.toggleCodeBlock, "程式碼", "程式碼區塊"),
    "|",
    tb("preview", toggleLivePreview, "預覽", "顯示或隱藏即時預覽"),
  ];
  // Only offer whole-document .md import while still a draft — it replaces the
  // entire doc (and CodeMirror's undo history with it), too destructive to
  // dangle in front of an already-published note.
  if (page.dataset.allowUpload === "1") {
    toolbar.push("|", tb("mdupload", uploadMdFile, "匯入", "從 .md 檔案匯入"));
  }

  var easymde = new EasyMDE({
    element: ta,
    autofocus: true,
    spellChecker: false,
    status: false,
    autoDownloadFontAwesome: false,
    lineWrapping: true,
    previewImagesInEditor: true,
    placeholder: "先寫標題，按 Enter 開始正文…",
    previewRender: function () { return ""; }, // preview is the published page (server goldmark)
    toolbar: toolbar,
  });
  window.cmEditor = easymde;
  var cm = easymde.codemirror;
  cm.setOption("viewportMargin", Infinity);

  // EasyMDE exposes tooltips but does not consistently add accessible names.
  // Keep every formatting action keyboard-readable and prevent form submits.
  var toolbarEl = page.querySelector(".editor-toolbar");
  if (toolbarEl) {
    toolbarEl.setAttribute("aria-label", "文字格式");
    toolbarEl.querySelectorAll("button").forEach(function (button) {
      button.type = "button";
      if (!button.getAttribute("aria-label") && button.title) {
        button.setAttribute("aria-label", button.title);
      }
    });
    toolbarEl.querySelectorAll("i.separator").forEach(function (separator) {
      separator.setAttribute("aria-hidden", "true");
    });
    previewButton = toolbarEl.querySelector(".tb-preview");
    if (previewButton) previewButton.setAttribute("aria-pressed", "true");
  }

  // First line is the title — style it large, Medium-style.
  function markTitle() {
    cm.removeLineClass(0, "text", "cm-title-line");
    cm.addLineClass(0, "text", "cm-title-line");
  }
  markTitle();
  cm.on("change", markTitle);

  // ---------- document status ----------
  function countWords(text) {
    var cjk = text.match(/[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/g) || [];
    var latin = text
      .replace(/[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/g, " ")
      .trim()
      .split(/\s+/)
      .filter(Boolean);
    return cjk.length + latin.length;
  }

  function updateDocumentStatus() {
    var value = easymde.value();
    if (wordCountEl) wordCountEl.textContent = countWords(value) + " 字";
    if (characterCountEl) characterCountEl.textContent = value.length + " 個字元";
    var cursor = cm.getCursor();
    if (cursorPositionEl) {
      cursorPositionEl.textContent = "第 " + (cursor.line + 1) + " 行，第 " + (cursor.ch + 1) + " 欄";
    }
  }
  updateDocumentStatus();
  cm.on("change", updateDocumentStatus);
  cm.on("cursorActivity", updateDocumentStatus);

  // ---------- live preview ----------
  // The server renderer is also used by published notes, so the preview stays
  // faithful to wiki links, embeds, tags, images, and Markdown extensions.
  var previewTimer = null;
  var previewRequest = 0;

  function documentParts(value) {
    var newline = value.indexOf("\n");
    if (newline < 0) return { title: value.trim(), body: "" };
    return {
      title: value.slice(0, newline).trim(),
      body: value.slice(newline + 1).replace(/^\s+/, ""),
    };
  }

  function renderPreview() {
    if (!previewEl) return;
    var request = ++previewRequest;
    var parts = documentParts(easymde.value());
    previewEl.setAttribute("aria-busy", "true");
    previewEl.classList.add("preview-loading");
    fetch("/preview", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: "body_md=" + encodeURIComponent(parts.body),
    }).then(function (response) {
      if (!response.ok) return Promise.reject();
      return response.text();
    }).then(function (html) {
      if (request !== previewRequest) return;
      previewEl.replaceChildren();
      if (parts.title) {
        var title = document.createElement("h1");
        title.className = "editor-live-preview-title";
        title.textContent = parts.title;
        previewEl.appendChild(title);
      }
      if (html.trim()) {
        previewEl.appendChild(document.createRange().createContextualFragment(html));
      } else if (!parts.title) {
        var empty = document.createElement("p");
        empty.className = "editor-preview-empty";
        empty.textContent = "開始輸入後，排版會即時顯示在這裡。";
        previewEl.appendChild(empty);
      }
    }).catch(function () {
      if (request !== previewRequest) return;
      previewEl.replaceChildren();
      var error = document.createElement("p");
      error.className = "editor-preview-empty";
      error.textContent = "預覽暫時無法更新，內容仍會照常儲存。";
      previewEl.appendChild(error);
    }).finally(function () {
      if (request !== previewRequest) return;
      previewEl.setAttribute("aria-busy", "false");
      previewEl.classList.remove("preview-loading");
    });
  }

  function schedulePreview() {
    clearTimeout(previewTimer);
    previewTimer = setTimeout(renderPreview, 300);
  }

  function toggleLivePreview() {
    var collapsed = page.classList.toggle("preview-collapsed");
    if (previewButton) {
      previewButton.setAttribute("aria-pressed", collapsed ? "false" : "true");
      previewButton.title = collapsed ? "顯示即時預覽" : "隱藏即時預覽";
    }
    window.setTimeout(function () { cm.refresh(); }, 0);
  }

  renderPreview();
  cm.on("change", schedulePreview);

  // Keep a local safety copy only while the server has not acknowledged the
  // latest text. A writer can explicitly recover or ignore it on the next open.
  var draftKey = "commaplace:draft:" + noteId;
  function readDraftBackup() {
    try {
      var raw = localStorage.getItem(draftKey);
      return raw ? JSON.parse(raw) : null;
    } catch (_err) {
      return null;
    }
  }

  function cacheDraft(value) {
    try {
      localStorage.setItem(draftKey, JSON.stringify({ document: value, savedAt: Date.now() }));
    } catch (_err) {}
  }

  function clearDraftBackup() {
    try {
      localStorage.removeItem(draftKey);
    } catch (_err) {}
  }

  var backup = readDraftBackup();
  if (recoveryEl && backup && typeof backup.document === "string" && backup.document !== easymde.value()) {
    recoveryEl.hidden = false;
    var recoverButton = recoveryEl.querySelector("[data-recover-draft]");
    var discardButton = recoveryEl.querySelector("[data-discard-draft]");
    if (recoverButton) {
      recoverButton.addEventListener("click", function () {
        easymde.value(backup.document);
        recoveryEl.hidden = true;
        cm.focus();
        save().catch(function () {});
      });
    }
    if (discardButton) {
      discardButton.addEventListener("click", function () {
        clearDraftBackup();
        recoveryEl.hidden = true;
        cm.focus();
      });
    }
  }

  // ---------- autosave ----------
  var timer, inflight = false, again = false, lastError = null;
  function setStatus(t, state) {
    if (statusEl) statusEl.textContent = t;
    page.dataset.saveState = state || "saved";
  }

  function userFacingError(text, fallback) {
    var value = (text || "").trim();
    if (!value || /<\/?[a-z][\s\S]*>/i.test(value) || value.length > 120) {
      return fallback;
    }
    return value;
  }

  // Returns a Promise that resolves once the in-flight save (if any) completes,
  // and rejects if that save failed — callers (e.g. publish) must not proceed
  // past a failed autosave with stale server-side content.
  function save() {
    clearTimeout(timer);
    if (inflight) {
      again = true;
      return new Promise(function (res, rej) {
        var poll = setInterval(function () {
          if (!inflight) { clearInterval(poll); if (lastError) rej(lastError); else res(); }
        }, 50);
      });
    }
    inflight = true; again = false; lastError = null;
    var requestValue = easymde.value();
    setStatus("儲存中…", "saving");
    return fetch("/api/notes/" + noteId, {
      method: "PATCH",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: "document=" + encodeURIComponent(requestValue),
    }).then(function (r) {
      inflight = false;
      if (!r.ok) {
        return r.text().then(function (t) {
          lastError = userFacingError(t, "儲存失敗，內容已保留在此裝置");
          setStatus(lastError, "error");
          return Promise.reject(lastError);
        });
      }
      if (easymde.value() === requestValue) clearDraftBackup();
      if (again) {
        save().catch(function () {});
      } else {
        setStatus("已儲存", "saved");
      }
    }).catch(function (err) {
      inflight = false;
      lastError = typeof err === "string" ? err : "儲存失敗，內容已保留在此裝置";
      setStatus(lastError, "error");
      return Promise.reject(lastError);
    });
  }
  // The listener is attached after EasyMDE has loaded the textarea, so its first
  // event is a real edit and must never be skipped.
  cm.on("change", function () {
    cacheDraft(easymde.value());
    setStatus("尚未儲存", "dirty");
    clearTimeout(timer);
    timer = setTimeout(function () { save().catch(function () {}); }, 800);
  });

  // ---------- publish ----------
  var pub = document.getElementById("publish-btn");
  if (pub) {
    pub.addEventListener("click", function () {
      pub.disabled = true;
      pub.setAttribute("aria-busy", "true");
      save().then(function () {
        fetch("/api/notes/" + noteId + "/publish", { method: "POST" })
          .then(function (r) {
            if (r.ok) return r.json();
            return r.text().then(function (t) {
              return Promise.reject(userFacingError(t, "發布失敗，請再試一次"));
            });
          })
          .then(function (d) { window.location = d.url; })
          .catch(function (msg) {
            pub.disabled = false;
            pub.removeAttribute("aria-busy");
            setStatus(typeof msg === "string" ? msg : "發布失敗", "error");
          });
      }).catch(function () {
        pub.disabled = false;
        pub.removeAttribute("aria-busy");
        // save() already surfaced the error via setStatus.
      });
    });
  }

  // ---------- toolbar custom actions ----------
  function insertTag(ed) {
    var c = ed.codemirror;
    var sel = c.getSelection();
    c.replaceSelection("#" + (sel || "tag"));
    if (!sel) c.focus();
  }

  function insertWikiLink(ed) {
    var c = ed.codemirror;
    var sel = c.getSelection();
    c.replaceSelection("[[" + sel + "]]");
    if (!sel) {
      // no selection: place caret inside [[ ]] so autocomplete kicks in
      var cur = c.getCursor();
      c.setCursor({ line: cur.line, ch: cur.ch - 2 });
    }
    c.focus();
  }

  var fileInput = document.createElement("input");
  fileInput.type = "file";
  fileInput.accept = "image/png,image/jpeg,image/webp";
  fileInput.style.display = "none";
  document.body.appendChild(fileInput);
  function uploadImage() {
    fileInput.click();
  }
  fileInput.addEventListener("change", function () {
    var f = fileInput.files[0];
    fileInput.value = "";
    if (!f) return;
    setStatus("上傳圖片中…", "saving");
    var fd = new FormData();
    fd.append("image", f);
    fetch("/api/notes/" + noteId + "/image", { method: "POST", body: fd })
      .then(function (r) { return r.ok ? r.json() : Promise.reject(); })
      .then(function (d) {
        cm.replaceSelection("![](" + d.url + ")");
        setStatus("圖片已加入", "dirty");
        cm.focus();
      })
      .catch(function () { setStatus("圖片上傳失敗", "error"); });
  });

  // ---------- .md file import ----------
  var mdInput = document.createElement("input");
  mdInput.type = "file";
  mdInput.accept = ".md,.markdown,text/markdown";
  mdInput.style.display = "none";
  document.body.appendChild(mdInput);
  function uploadMdFile() {
    mdInput.click();
  }
  mdInput.addEventListener("change", function () {
    var f = mdInput.files[0];
    mdInput.value = "";
    if (!f) return;
    var reader = new FileReader();
    reader.onload = function (e) {
      var text = e.target.result;
      var m = text.match(/^#\s+(.+)/m);
      var title = m ? m[1].trim() : f.name.replace(/\.(md|markdown)$/i, "");
      var body = m ? text.replace(/^#[^\n]*\n?/, "").replace(/^\s+/, "") : text;
      // Save directly instead of making an imported document wait for the
      // debounce used during ordinary typing.
      easymde.value(title + "\n\n" + body);
      save().catch(function () {});
      cm.focus();
    };
    reader.readAsText(f);
  });

  // On touch devices the formatting bar sits above the bottom dock. When the
  // software keyboard opens, hide the dock and let the toolbar use that space.
  function syncKeyboardState() {
    if (!window.visualViewport) return;
    var keyboardOpen = window.innerHeight - window.visualViewport.height > 160;
    document.body.classList.toggle("editor-keyboard-open", keyboardOpen);
  }
  if (window.visualViewport) {
    window.visualViewport.addEventListener("resize", syncKeyboardState);
    syncKeyboardState();
  }

  // ---------- [[ wiki-link autocomplete ----------
  if (!popup || !list) return;
  // The popup is position:fixed, but .content keeps a persistent transform from
  // its page-fade animation, which would make .content (not the viewport) the
  // containing block and throw the popup off toward the page corner. Reparent to
  // <body> so fixed coordinates resolve against the viewport as intended.
  document.body.appendChild(popup);
  var activeIdx = -1, queryStart = null, fetchTimer;
  var acMode = "wiki"; // "wiki" | "tag"

  function closePopup() {
    clearTimeout(fetchTimer);
    popup.hidden = true;
    activeIdx = -1;
    queryStart = null;
    list.innerHTML = "";
  }

  // Find an unclosed "[[" on the current line before the cursor; returns the
  // {line, ch} just after it, or null.
  function triggerStart() {
    var cur = cm.getCursor();
    var line = cm.getLine(cur.line);
    for (var i = cur.ch - 1; i >= 1; i--) {
      if (line[i] === "]" && line[i - 1] === "]") return null;
      if (line[i] === "[" && line[i - 1] === "[") return { line: cur.line, ch: i + 1 };
    }
    return null;
  }

  // Mirrors markdown.ExtractInlineTags: a tag-char run right before the
  // cursor, immediately preceded by "#" at a word boundary.
  function isTagChar(c) {
    return /[A-Za-z0-9_\-\/]/.test(c) || c.charCodeAt(0) > 127;
  }

  function triggerTagStart() {
    var cur = cm.getCursor();
    var line = cm.getLine(cur.line);
    var i = cur.ch;
    while (i > 0 && isTagChar(line[i - 1])) i--;
    if (i === 0 || line[i - 1] !== "#") return null;
    var before = line[i - 2];
    if (before && /[A-Za-z0-9]/.test(before)) return null; // word boundary
    return { line: cur.line, ch: i };
  }

  function setActive(i) {
    var items = list.children;
    if (!items.length) return;
    activeIdx = (i + items.length) % items.length;
    for (var k = 0; k < items.length; k++) {
      items[k].classList.toggle("active", k === activeIdx);
    }
    items[activeIdx].scrollIntoView({ block: "nearest" });
  }

  function positionPopup() {
    // cm.cursorCoords("window") miscalculates with viewportMargin:Infinity;
    // read the actual cursor DOM element instead.
    var cursorEl = cm.getWrapperElement().querySelector(".CodeMirror-cursor");
    if (!cursorEl) return;
    var r = cursorEl.getBoundingClientRect();
    var m = 8; // viewport margin
    var bottomInset = m;
    if (window.matchMedia("(max-width: 800px)").matches) {
      var dock = document.querySelector(".mobile-dock");
      bottomInset += toolbarEl ? toolbarEl.offsetHeight : 0;
      if (!document.body.classList.contains("editor-keyboard-open") && dock) {
        bottomInset += dock.offsetHeight;
      }
    }
    var pw = popup.offsetWidth || 320;
    var left = r.left;
    if (left + pw > window.innerWidth - m)
      left = Math.max(m, window.innerWidth - pw - m);

    // Prefer dropping below the cursor; flip above only when there's more room
    // there. Cap the height to the chosen side so a long list scrolls in place
    // instead of running off-screen.
    var below = window.innerHeight - bottomInset - (r.bottom + 4);
    var above = r.top - 4 - m;
    var ph = popup.scrollHeight;
    var top;
    if (ph <= below || below >= above) {
      top = r.bottom + 4;
      popup.style.maxHeight = below + "px";
    } else {
      popup.style.maxHeight = above + "px";
      top = r.top - Math.min(ph, above) - 4;
    }
    popup.style.top = top + "px";
    popup.style.left = left + "px";
  }

  function fetchSuggest(q) {
    var url = acMode === "tag"
      ? "/api/tags/suggest?q=" + encodeURIComponent(q)
      : "/api/wiki/suggest?q=" + encodeURIComponent(q);
    fetch(url)
      .then(function (r) { return r.text(); })
      .then(function (html) {
        list.innerHTML = html;
        if (!list.children.length) { closePopup(); return; }
        popup.hidden = false;
        positionPopup();
        setActive(0);
      })
      .catch(closePopup);
  }

  function insertActive() {
    var items = list.children;
    if (!items.length || activeIdx < 0 || !queryStart) { closePopup(); return; }
    var insert = items[activeIdx].getAttribute("data-insert") || "";
    if (acMode === "tag") {
      cm.replaceRange(insert, queryStart, cm.getCursor());
      closePopup();
    } else {
      var partial = insert.endsWith("/"); // "@bob/" — keep searching, don't close
      cm.replaceRange(insert + (partial ? "" : "]]"), queryStart, cm.getCursor());
      if (!partial) closePopup();
    }
    cm.focus();
  }

  // longest common prefix of the suggestion inserts
  function lcp(strs) {
    if (!strs.length) return "";
    var p = strs[0];
    for (var i = 1; i < strs.length; i++) {
      while (strs[i].indexOf(p) !== 0) {
        p = p.slice(0, -1);
        if (!p) return "";
      }
    }
    return p;
  }

  // Tab extends the query to the longest common prefix (e.g. "@b" -> "@bob/"),
  // re-fetching as it grows; only a full match inserts + closes.
  function tabComplete() {
    var items = list.children;
    if (!items.length || !queryStart) { closePopup(); return; }
    var inserts = Array.prototype.map.call(items, function (el) {
      return el.getAttribute("data-insert") || "";
    });
    var prefix = lcp(inserts);
    var curQuery = cm.getRange(queryStart, cm.getCursor());
    if (prefix.length > curQuery.length) {
      // extend to the common prefix; cursorActivity re-fetches with it
      cm.replaceRange(prefix, queryStart, cm.getCursor());
      cm.focus();
    } else {
      if (activeIdx < 0) setActive(0);
      insertActive();
    }
  }

  cm.on("cursorActivity", function () {
    var start = triggerStart();
    acMode = "wiki";
    if (!start) { start = triggerTagStart(); acMode = "tag"; }
    if (!start) { closePopup(); return; }
    // Trigger origin changed (e.g. wiki "[[" -> tag "#", or vice versa) —
    // the popup may still hold stale items from the other mode's in-flight
    // fetch; clear it so insertActive() can't apply the new mode's insert
    // rules to an old mode's suggestion.
    if (queryStart && (start.line !== queryStart.line || start.ch !== queryStart.ch)) {
      closePopup();
    }
    queryStart = start;
    var q = cm.getRange(start, cm.getCursor());
    clearTimeout(fetchTimer);
    fetchTimer = setTimeout(function () { fetchSuggest(q); }, 120);
  });

  cm.on("keydown", function (_cm, e) {
    if (popup.hidden) return;
    if (e.key === "ArrowDown") { e.preventDefault(); setActive(activeIdx + 1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setActive(activeIdx - 1); }
    else if (e.key === "Enter") { e.preventDefault(); insertActive(); }
    else if (e.key === "Tab") { e.preventDefault(); tabComplete(); }
    else if (e.key === "Escape") { closePopup(); }
  });

  list.addEventListener("mousedown", function (e) {
    var li = e.target.closest(".ac-item");
    if (!li) return;
    e.preventDefault();
    activeIdx = Array.prototype.indexOf.call(list.children, li);
    insertActive();
  });
})();
