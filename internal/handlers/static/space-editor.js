(function () {
  "use strict";
  const root = document.getElementById("space-editor"); if (!root) return;
  const title = root.querySelector("#space-title"), body = root.querySelector("#space-body"), blocks = root.querySelector("#space-blocks");
  const status = root.querySelector("#space-save-state"), publish = root.querySelector("#space-publish");
  const picker = root.querySelector("#space-picker"), review = root.querySelector("#space-publish-dialog");
  const handle = document.getElementById("private-desk").dataset.handle;
  let doc, revision = 0, change = 0, saved = 0, timer, saving, dragIndex = null, pickerRequest = 0, removed;
  const notes = new Map();
  function el(tag, text, cls) { const e = document.createElement(tag); if (text != null) e.textContent = text; if (cls) e.className = cls; return e; }
  function button(text, label, action) { const e = el("button", text); e.type = "button"; if (label) e.setAttribute("aria-label", label); e.addEventListener("click", action); return e; }
  async function request(url, opts) { const r = await fetch(url, opts); if (!r.ok) { const text = await r.text(); throw new Error(text.length < 160 && !text.includes("<") ? text : "操作失敗，請重試"); } return r.json(); }
  function changed() {
    change++; status.textContent = "儲存中…";
    clearTimeout(timer); timer = setTimeout(() => save().catch(() => {}), 650);
  }
  async function save() {
    clearTimeout(timer);
    if (!doc) throw new Error("尚未讀取完成");
    if (saving) { await saving; return save(); }
    if (saved === change && revision > 0) return;
    const captured = change;
    saving = request("/api/space", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ revision, document: doc }) });
    try { const result = await saving; revision = result.revision; saved = captured; status.textContent = "已儲存"; }
    catch (e) { status.textContent = e.message; throw e; }
    finally { saving = null; }
    if (saved !== change) return save();
  }
  window.commaSpaceSave = save;
  window.addEventListener("beforeunload", e => { if (change !== saved) { e.preventDefault(); e.returnValue = ""; } });
  function move(i, j) { if (j < 0 || j >= doc.blocks.length) return; const b = doc.blocks.splice(i, 1)[0]; doc.blocks.splice(j, 0, b); renderBlocks(); changed(); }
  function distribution(b, n) {
    const select = el("select"); select.className = "space-distribution"; select.setAttribute("aria-label", n.title + "：公開或半公開");
    [["public", "公開"], ["semi", "半公開"]].forEach(([value, label]) => { const o = el("option", label); o.value = value; select.append(o); });
    select.value = b.distribution || n.distribution;
    select.addEventListener("change", () => { b.distribution = select.value; changed(); });
    return select;
  }
  function renderBlocks() {
    blocks.replaceChildren();
    doc.blocks.forEach((b, i) => {
      const row = el("div", null, "space-edit-block"); row.dataset.index = i;
      const grip = el("span", "⠿", "space-grip"); grip.draggable = true; grip.setAttribute("aria-hidden", "true");
      grip.addEventListener("dragstart", e => { dragIndex = i; e.dataTransfer.setData("text/plain", String(i)); });
      row.addEventListener("dragover", e => { if (dragIndex !== null) e.preventDefault(); });
      row.addEventListener("drop", e => { e.preventDefault(); if (dragIndex !== null) move(dragIndex, i); dragIndex = null; });
      grip.addEventListener("dragend", () => { dragIndex = null; });
      const content = el("div", null, "space-block-content");
      if (b.type === "text") { const text = el("textarea"); text.value = b.text; text.setAttribute("aria-label", "段落"); text.addEventListener("input", () => { b.text = text.value; changed(); }); content.append(text); }
      else {
        const n = notes.get(b.id);
        const link = el("a", n ? n.title : "已移除"); link.href = "/edit/" + b.id; content.append(link);
        if (n && !n.published) content.append(el("span", "草稿", "space-draft"));
      }
      row.append(grip, content);
      if (b.type === "note" && notes.has(b.id)) row.append(distribution(b, notes.get(b.id)));
      const controls = el("div", null, "space-block-controls");
      const up = button("↑", "向上移動", () => move(i, i - 1)), down = button("↓", "向下移動", () => move(i, i + 1));
      up.disabled = i === 0; down.disabled = i === doc.blocks.length - 1;
      controls.append(up, down, button("×", "移除文章項目", () => {
        removed = { block: doc.blocks.splice(i, 1)[0], index: i }; renderBlocks(); changed();
        let undo = root.querySelector("#space-undo"); if (!undo) { undo = button("復原", "復原移除", () => { if (removed) { doc.blocks.splice(removed.index, 0, removed.block); removed = null; renderBlocks(); changed(); undo.remove(); } }); undo.id = "space-undo"; root.querySelector(".space-add").append(undo); }
      })); row.append(controls); blocks.append(row);
    });
  }
  async function pickerNotes() {
    const generation = ++pickerRequest, list = root.querySelector("#space-picker-notes"); list.replaceChildren();
    try {
      const result = await request("/api/desk/notes?q=" + encodeURIComponent(root.querySelector("#space-picker-query").value));
      if (generation !== pickerRequest) return;
      result.notes.filter(n => !doc.blocks.some(b => b.id === n.id)).forEach(n => {
        notes.set(n.id, n);
        list.append(button(n.title + (n.published ? "" : " · 草稿"), null, () => { doc.blocks.push({ type: "note", id: n.id }); renderBlocks(); changed(); picker.close(); }));
      });
    } catch (e) { list.textContent = e.message; }
  }
  root.querySelector("#space-link").addEventListener("click", () => { picker.showModal(); pickerNotes(); });
  let searchTimer;
  root.querySelector("#space-picker-query").addEventListener("input", () => { clearTimeout(searchTimer); searchTimer = setTimeout(pickerNotes, 180); });
  root.querySelector("#space-text").addEventListener("click", () => { doc.blocks.push({ type: "text", text: "" }); renderBlocks(); changed(); blocks.lastElementChild.querySelector("textarea").focus(); });
  root.querySelectorAll("[data-close-space-dialog]").forEach(b => b.addEventListener("click", () => b.closest("dialog").close()));
  async function commit(ids) {
    const controls = [...root.querySelectorAll("input,textarea,select,button")].filter(e => !e.disabled);
    controls.forEach(e => e.disabled = true);
    try { await save(); const result = await request("/api/space/publish", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ revision, publishIDs: ids }) }); location.assign(result.url); }
    catch (e) { review.close(); status.textContent = e.message; controls.forEach(e => e.disabled = false); }
  }
  publish.addEventListener("click", async () => {
    if (!title.value.trim()) { title.focus(); return; }
    const drafts = doc.blocks.filter(b => b.type === "note" && notes.has(b.id) && !notes.get(b.id).published);
    if (!drafts.length) { commit([]); return; }
    const list = root.querySelector("#space-publish-notes"); list.replaceChildren();
    drafts.forEach(b => { const n = notes.get(b.id), row = el("div", null, "space-review-row"), label = el("label"), check = el("input"); check.type = "checkbox"; check.value = b.id; label.append(check, document.createTextNode(n.title + " · 草稿")); row.append(label, distribution(b, n)); list.append(row); });
    review.showModal();
  });
  root.querySelector("#space-confirm-publish").addEventListener("click", () => commit([...review.querySelectorAll("input:checked")].map(e => e.value)));
  title.addEventListener("input", () => { doc.title = title.value; document.querySelector(".private-space-link").textContent = title.value || "我的空間"; changed(); });
  body.addEventListener("input", () => { doc.body = body.value; changed(); });
  request("/api/space").then(data => {
    doc = data.document; revision = data.revision; (data.notes || []).forEach(n => notes.set(n.id, n));
    title.value = doc.title; body.value = doc.body;
    document.querySelector(".private-space-link").textContent = doc.title || "我的空間";
    root.querySelector("#space-public-link").href = "/" + handle;
    [title, body, publish, root.querySelector("#space-link"), root.querySelector("#space-text")].forEach(e => e.disabled = false);
    root.setAttribute("aria-busy", "false"); status.textContent = "已儲存"; renderBlocks();
  }).catch(e => { status.textContent = e.message; });
})();
