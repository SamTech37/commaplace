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

  function tb(name, action, text, title) {
    return { name: name, action: action, text: text, title: title, className: "tb-" + name };
  }

  var toolbar = [
    tb("bold", EasyMDE.toggleBold, "B", "Bold"),
    tb("italic", EasyMDE.toggleItalic, "i", "Italic"),
    "|",
    tb("wikilink", insertWikiLink, "[[ ]]", "Wiki link"),
    tb("link", EasyMDE.drawLink, "🔗", "External link"),
    "|",
    tb("h1", EasyMDE.toggleHeading1, "T", "Heading 1"),
    tb("h2", EasyMDE.toggleHeading2, "t", "Heading 2"),
    tb("quote", EasyMDE.toggleBlockquote, "❝", "Quote"),
    tb("tag", insertTag, "#", "Tag"),
    "|",
    tb("image", uploadImage, "🖼", "Insert image"),
    tb("code", EasyMDE.toggleCodeBlock, "</>", "Code"),
  ];
  // Only offer whole-document .md import while still a draft — it replaces the
  // entire doc (and CodeMirror's undo history with it), too destructive to
  // dangle in front of an already-published note.
  if (page.dataset.allowUpload === "1") {
    toolbar.push("|", tb("mdupload", uploadMdFile, "📄", "從 .md 檔案匯入"));
  }

  var easymde = new EasyMDE({
    element: ta,
    autofocus: true,
    spellChecker: false,
    status: false,
    autoDownloadFontAwesome: false,
    lineWrapping: true,
    previewRender: function () { return ""; }, // preview is the published page (server goldmark)
    toolbar: toolbar,
  });
  window.cmEditor = easymde;
  var cm = easymde.codemirror;
  cm.setOption("viewportMargin", Infinity);

  // First line is the title — style it large, Medium-style.
  function markTitle() {
    cm.removeLineClass(0, "text", "cm-title-line");
    cm.addLineClass(0, "text", "cm-title-line");
  }
  markTitle();
  cm.on("change", markTitle);

  // ---------- autosave ----------
  var timer, inflight = false, again = false, loaded = false, lastError = null;
  function setStatus(t) { if (statusEl) statusEl.textContent = t; }

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
    setStatus("儲存中…");
    return fetch("/api/notes/" + noteId, {
      method: "PATCH",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: "document=" + encodeURIComponent(easymde.value()),
    }).then(function (r) {
      inflight = false;
      if (!r.ok) {
        return r.text().then(function (t) {
          lastError = t || "儲存失敗";
          setStatus(lastError);
          return Promise.reject(lastError);
        });
      }
      setStatus("已儲存");
      if (again) save();
    }).catch(function (err) {
      inflight = false;
      lastError = typeof err === "string" ? err : "儲存失敗";
      setStatus(lastError);
      return Promise.reject(lastError);
    });
  }
  // Skip the first change event that fires as EasyMDE loads existing content.
  cm.on("change", function () {
    if (!loaded) { loaded = true; return; }
    setStatus("…");
    clearTimeout(timer);
    timer = setTimeout(save, 800);
  });

  // ---------- publish ----------
  var pub = document.getElementById("publish-btn");
  if (pub) {
    pub.addEventListener("click", function () {
      save().then(function () {
        fetch("/api/notes/" + noteId + "/publish", { method: "POST" })
          .then(function (r) {
            if (r.ok) return r.json();
            return r.text().then(function (t) { return Promise.reject(t || "發布失敗"); });
          })
          .then(function (d) { window.location = d.url; })
          .catch(function (msg) { setStatus(typeof msg === "string" ? msg : "發布失敗"); });
      }).catch(function () { /* save() already surfaced the error via setStatus */ });
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
    setStatus("上傳圖片中…");
    var fd = new FormData();
    fd.append("image", f);
    fetch("/api/notes/" + noteId + "/image", { method: "POST", body: fd })
      .then(function (r) { return r.ok ? r.json() : Promise.reject(); })
      .then(function (d) {
        cm.replaceSelection("![](" + d.url + ")");
        setStatus("圖片已加入");
        cm.focus();
      })
      .catch(function () { setStatus("圖片上傳失敗"); });
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
      // On a brand-new, never-edited draft this is the CM instance's first
      // change event ever, which the "skip EasyMDE's own initial load" guard
      // above would otherwise swallow — force loaded so it isn't mistaken for
      // that, then save directly instead of waiting on the debounce/listener.
      loaded = true;
      easymde.value(title + "\n\n" + body);
      save();
      cm.focus();
    };
    reader.readAsText(f);
  });

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
    var pw = popup.offsetWidth || 320;
    var left = r.left;
    if (left + pw > window.innerWidth - m)
      left = Math.max(m, window.innerWidth - pw - m);

    // Prefer dropping below the cursor; flip above only when there's more room
    // there. Cap the height to the chosen side so a long list scrolls in place
    // instead of running off-screen.
    var below = window.innerHeight - m - (r.bottom + 4);
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
