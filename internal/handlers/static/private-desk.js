(function () {
  "use strict";
  const root = document.getElementById("private-desk");
  if (!root) return;
  const list = root.querySelector("#private-notes"), search = root.querySelector("#private-search"), more = root.querySelector("#private-more");
  const status = root.querySelector("#private-status"), reference = root.querySelector("#private-reference");
  const lastKey = "comma-private-last:" + root.dataset.handle;
  const editor = root.querySelector(".editor-page");
  const activeID = editor ? editor.dataset.noteId : "";
  let filter = "all", offset = 0, generation = 0, timer;
  if (location.pathname === "/me/desk" && !location.search) {
    try { const last = localStorage.getItem(lastKey); if (/^\/edit\/[a-f0-9-]{36}$/.test(last || "")) { location.replace(last); return; } } catch (_) {}
  }
  try { localStorage.setItem(lastKey, activeID ? "/edit/" + activeID : "/me/desk?space=1"); } catch (_) {}
  root.querySelector(".private-space-link").classList.toggle("is-current", !activeID);
  function button(text, label) { const b = document.createElement("button"); b.type = "button"; b.textContent = text; b.setAttribute("aria-label", label); return b; }
  async function load(append) {
    const request = ++generation;
    if (!append) offset = 0;
    more.disabled = true;
    try {
      const params = new URLSearchParams({ q: search.value, filter, offset: String(offset) });
      const response = await fetch("/api/desk/notes?" + params);
      if (!response.ok) throw new Error("無法讀取文章，請重試");
      const data = await response.json(); if (request !== generation) return;
      if (!append) list.replaceChildren();
      data.notes.forEach(n => {
        const row = document.createElement("div"); row.className = "private-note"; row.classList.toggle("is-current", n.id === activeID);
        const link = document.createElement("a"); link.href = "/edit/" + n.id; link.textContent = n.title;
        link.setAttribute("aria-label", n.title + "，" + (!n.published ? "草稿" : n.distribution === "semi" ? "半公開" : "公開"));
        const ref = button("↗", "並排閱讀「" + n.title + "」");
        ref.addEventListener("click", () => { reference.hidden = false; reference.querySelector("iframe").src = "/me/desk/pane?kind=note&ref=" + encodeURIComponent(n.id); });
        row.append(link, ref); list.append(row);
      });
      offset += data.notes.length; more.hidden = !data.more; status.textContent = "";
    } catch (e) { if (request === generation) status.textContent = e.message; }
    finally { if (request === generation) more.disabled = false; }
  }
  root.querySelectorAll("[data-note-filter]").forEach(b => b.addEventListener("click", () => {
    filter = b.dataset.noteFilter;
    root.querySelectorAll("[data-note-filter]").forEach(x => x.setAttribute("aria-pressed", String(x === b)));
    load(false);
  }));
  search.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(() => load(false), 180); });
  more.addEventListener("click", () => load(true));
  root.querySelector("[data-close-reference]").addEventListener("click", () => { reference.hidden = true; reference.querySelector("iframe").removeAttribute("src"); });
  document.addEventListener("comma:note-saved", () => load(false));
  window.addEventListener("message", async e => {
    if (e.origin !== location.origin || e.source !== reference.querySelector("iframe").contentWindow || e.data?.source !== "comma-pane" || e.data.type !== "open") return;
    try {
      const url = new URL(e.data.url, location.origin); if (url.origin !== location.origin) return;
      const response = await fetch("/api/desk/resolve?url=" + encodeURIComponent(url.pathname + url.search));
      if (!response.ok) throw new Error();
      const pane = await response.json();
      if (pane.kind === "note") reference.querySelector("iframe").src = "/me/desk/pane?kind=note&ref=" + encodeURIComponent(pane.ref);
      else { const save = window.commaSpaceSave || window.commaDeskSave; if (save) await save(); location.assign(url.href); }
    } catch (_) { status.textContent = "無法開啟這篇文章"; }
  });
  // Wait for the latest document before changing either the article or the area.
  document.addEventListener("click", async e => {
    const a = e.target.closest("a[href]");
    if (e.defaultPrevented || !a || a.target || a.hasAttribute("download") || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey || e.button !== 0 || a.origin !== location.origin || a.hash && a.pathname === location.pathname) return;
    const save = window.commaSpaceSave || window.commaDeskSave;
    if (!save) return;
    e.preventDefault();
    try { await save(); location.assign(a.href); } catch (_) { status.textContent = "儲存失敗，內容仍保留在此頁。"; }
  });
  load(false);
})();
