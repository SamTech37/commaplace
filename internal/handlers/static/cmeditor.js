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

  var easymde = new EasyMDE({
    element: ta,
    autofocus: true,
    spellChecker: false,
    status: false,
    autoDownloadFontAwesome: false,
    lineWrapping: true,
    previewRender: function () { return ""; }, // preview is the published page (server goldmark)
    toolbar: [
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
    ],
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
  var timer, inflight = false, again = false, loaded = false;
  function setStatus(t) { if (statusEl) statusEl.textContent = t; }

  // Returns a Promise that resolves once the in-flight save (if any) completes.
  function save() {
    clearTimeout(timer);
    if (inflight) {
      again = true;
      return new Promise(function (res) {
        var poll = setInterval(function () { if (!inflight) { clearInterval(poll); res(); } }, 50);
      });
    }
    inflight = true; again = false;
    setStatus("Saving…");
    return fetch("/api/notes/" + noteId, {
      method: "PATCH",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: "document=" + encodeURIComponent(easymde.value()),
    }).then(function (r) {
      inflight = false;
      if (!r.ok) { r.text().then(function (t) { setStatus(t || "Save failed"); }); return; }
      setStatus("Saved");
      if (again) save();
    }).catch(function () { inflight = false; setStatus("Save failed"); });
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
            return r.text().then(function (t) { return Promise.reject(t || "Publish failed"); });
          })
          .then(function (d) { window.location = d.url; })
          .catch(function (msg) { setStatus(typeof msg === "string" ? msg : "Publish failed"); });
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
    setStatus("Uploading image…");
    var fd = new FormData();
    fd.append("image", f);
    fetch("/api/notes/" + noteId + "/image", { method: "POST", body: fd })
      .then(function (r) { return r.ok ? r.json() : Promise.reject(); })
      .then(function (d) {
        cm.replaceSelection("![](" + d.url + ")");
        setStatus("Image added");
        cm.focus();
      })
      .catch(function () { setStatus("Image upload failed"); });
  });

  // ---------- [[ wiki-link autocomplete ----------
  if (!popup || !list) return;
  var activeIdx = -1, queryStart = null, fetchTimer;

  function closePopup() {
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
    var coords = cm.cursorCoords(true, "window");
    popup.style.position = "fixed";
    popup.style.left = coords.left + "px";
    popup.style.top = coords.bottom + 4 + "px";
  }

  function fetchSuggest(q) {
    fetch("/api/wiki/suggest?q=" + encodeURIComponent(q))
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
    cm.replaceRange(insert + "]]", queryStart, cm.getCursor());
    closePopup();
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
    if (!start) { closePopup(); return; }
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
