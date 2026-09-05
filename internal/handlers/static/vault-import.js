(function () {
  "use strict";
  var open = document.getElementById("vault-import-open");
  if (!open) return;
  function el(name) { return document.getElementById("vault-import-" + name); }
  var dialog = el("dialog"), input = el("files"), start = el("start");
  var files = [], candidates = [], batch = "", busy = false, report = null, fingerprint = "";
  var page = 0, previewVersion = 0, selectionError = "";
  function relativePath(file) { return file.webkitRelativePath ? file.webkitRelativePath.split("/").slice(1).join("/") : file.name; }
  function fileSize(bytes) { return bytes < 1024 ? bytes + " B" : bytes < 1048576 ? (bytes / 1024).toFixed(1) + " KiB" : (bytes / 1048576).toFixed(1) + " MiB"; }
  function matching() {
    var query = el("filter").value.trim().toLocaleLowerCase();
    return candidates.filter(function (item) { return relativePath(item.file).toLocaleLowerCase().includes(query); });
  }
  function status(text) { el("status").textContent = text; }
  function lock(value) {
    busy = value;
    start.disabled = value || !files.length || !!selectionError || !!report;
    el("pick").disabled = value;
    el("pick-files").disabled = value;
    el("review").disabled = value;
    el("close").disabled = value;
  }
  open.addEventListener("click", function () { dialog.showModal(); });
  el("close").addEventListener("click", function () { if (!busy) dialog.close(); });
  dialog.addEventListener("cancel", function (event) { if (busy) event.preventDefault(); });
  window.addEventListener("beforeunload", function (event) {
    if (busy) { event.preventDefault(); event.returnValue = ""; }
  });
  el("pick").addEventListener("click", function () {
    if (!("webkitdirectory" in input)) { status("此瀏覽器不支援資料夾選取，請使用桌面版 Chrome、Edge 或 Firefox。"); return; }
    input.value = ""; input.click();
  });
  el("pick-files").addEventListener("click", function () { el("individual").value = ""; el("individual").click(); });
  function resetResult() {
    if (report) batch = "";
    report = null;
    el("details").hidden = el("download").hidden = el("profile").hidden = true;
    el("results").replaceChildren();
    el("progress").hidden = true;
  }
  function updateSelection() {
    resetResult();
    files = candidates.filter(function (item) { return item.selected; }).map(function (item) { return item.file; });
    var bytes = files.reduce(function (sum, file) { return sum + file.size; }, 0);
    el("selection").textContent = "已勾選 " + files.length + " / " + candidates.length + " 篇 · " + (bytes / 1048576).toFixed(1) + " MiB";
    selectionError = files.length > 2000 ? "一次最多發布 2,000 篇，請減少勾選。" : bytes > 99 * 1048576 ? "勾選的內容過大，請減少勾選。" : "";
    // Plain file selection does not expose parent directories. Never silently
    // collapse two equally named files from different folders into one upload.
    if (new Set(files.map(relativePath)).size !== files.length) selectionError = "勾選的檔案有相同名稱，請改用資料夾選取以保留路徑。";
    start.textContent = files.length ? "發布勾選的 " + files.length + " 篇" : "發布勾選的筆記";
    status(selectionError || (files.length ? "僅上傳勾選的 " + files.length + " 篇，完成後公開。" : "尚未勾選任何筆記，不會上傳。"));
    lock(false);
  }
  async function preview(item) {
    var version = ++previewVersion;
    el("preview").hidden = false;
    el("preview-name").textContent = relativePath(item.file);
    el("preview-info").textContent = "檔案大小：" + fileSize(item.file.size) + " · 正在讀取原文…";
    el("preview-text").textContent = "讀取中…";
    el("preview-text").focus();
    try {
      var text = await item.file.slice(0, 2 * 1048576).text();
      if (version !== previewVersion) return;
      el("preview-info").textContent = "檔案大小：" + fileSize(item.file.size) + (item.file.size > 2 * 1048576 ? " · 僅顯示前 2 MiB，無法匯入此檔" : " · 本機 Markdown 原文，尚未上傳");
      if (!text.length) {
        el("preview-text").textContent = item.file.size === 0 ? "這個檔案是空的（0 B），沒有可以預覽的內容。若 Obsidian 裡有文字，請先儲存筆記，再重新選取檔案。" : "未能讀到檔案內容。請確認檔案已下載到這台裝置，再重新選取；確認原文前請勿發布。";
      } else if (!text.trim()) {
        el("preview-text").textContent = "這個檔案只有空白或換行，沒有可見的文字。";
      } else {
        el("preview-text").textContent = text;
      }
    } catch (_) {
      if (version === previewVersion) {
        el("preview-info").textContent = "檔案大小：" + fileSize(item.file.size) + " · 讀取失敗";
        el("preview-text").textContent = "無法讀取此檔案。請確認檔案仍存在且已下載到這台裝置，再重新選取；確認原文前請勿發布。";
      }
    }
  }
  function renderCandidates() {
    var matches = matching(), pages = Math.max(1, Math.ceil(matches.length / 100));
    page = Math.min(page, pages - 1);
    var fragment = document.createDocumentFragment();
    matches.slice(page * 100, (page + 1) * 100).forEach(function (item) {
      var row = document.createElement("li"), label = document.createElement("label"), check = document.createElement("input"), name = document.createElement("span"), view = document.createElement("button");
      check.type = "checkbox"; check.checked = item.selected; check.disabled = item.file.size > 2 * 1048576;
      name.textContent = relativePath(item.file) + (check.disabled ? "（超過 2 MiB）" : "");
      check.addEventListener("change", function () { item.selected = check.checked; updateSelection(); });
      label.append(check, name);
      view.type = "button"; view.className = "editor-quiet-btn"; view.textContent = "查看原文"; view.setAttribute("aria-label", "查看原文 " + relativePath(item.file));
      view.addEventListener("click", function () { preview(item); });
      row.append(label, view); fragment.append(row);
    });
    el("candidates").replaceChildren(fragment);
    el("page").textContent = matches.length + " 篇符合 · " + (page + 1) + " / " + pages + " 頁";
    el("prev").disabled = page === 0; el("next").disabled = page + 1 >= pages;
  }
  function loadCandidates(source) {
    if (!source.files.length) return; // Canceling a picker keeps the review.
    candidates = Array.from(source.files).filter(function (file) {
      return /\.md$/i.test(file.name) && !(file.webkitRelativePath || file.name).split("/").some(function (part) { return part.startsWith("."); });
    }).sort(function (a, b) { return relativePath(a).localeCompare(relativePath(b)); }).map(function (file) { return { file:file, selected:false }; });
    page = 0; previewVersion++; el("filter").value = ""; el("preview").hidden = true; el("preview-text").textContent = "";
    el("review").hidden = false;
    updateSelection(); renderCandidates();
  }
  input.addEventListener("change", function () { loadCandidates(input); });
  el("individual").addEventListener("change", function () { loadCandidates(el("individual")); });
  el("filter").addEventListener("input", function () { page = 0; renderCandidates(); });
  el("prev").addEventListener("click", function () { page--; renderCandidates(); });
  el("next").addEventListener("click", function () { page++; renderCandidates(); });
  el("select").addEventListener("click", function () { matching().forEach(function (item) { if (item.file.size <= 2 * 1048576) item.selected = true; }); updateSelection(); renderCandidates(); });
  el("clear").addEventListener("click", function () { candidates.forEach(function (item) { item.selected = false; }); updateSelection(); renderCandidates(); });
  el("preview-close").addEventListener("click", function () { previewVersion++; el("preview").hidden = true; el("preview-text").textContent = ""; });
  function complete(data) {
    report = data;
    try { sessionStorage.removeItem("comma-vault-pending"); } catch (_) {}
    el("progress").value = 100;
    status("已發布 " + data.total + " 篇 · 移除 " + data.media_removed + " 處媒體 · " + data.unresolved_links + " 個未解析連結");
    el("profile").href = data.url;
    el("profile").hidden = el("download").hidden = el("details").hidden = false;
    var fragment = document.createDocumentFragment();
    // The full report is downloadable; keep the dialog bounded for big vaults.
    data.notes.slice(0, 100).forEach(function (note) {
      var li = document.createElement("li"), link = document.createElement("a");
      link.href = note.url; link.textContent = note.path; link.target = "_blank"; link.rel = "noopener";
      li.append(link);
      if (note.warnings && note.warnings.length) li.append(document.createTextNode(" — " + note.warnings.join("；")));
      fragment.append(li);
    });
    if (data.notes.length > 100) {
      var more = document.createElement("li"); more.textContent = "其餘筆記請見完整匯入報告。"; fragment.append(more);
    }
    el("results").replaceChildren(fragment);
  }
  start.addEventListener("click", async function () {
    if (busy || !files.length || selectionError || report) return;
    lock(true);
    // Fingerprint only the explicitly selected files. Changing the checked
    // subset creates a new batch rather than retrying a previous selection.
    try {
      var signature = JSON.stringify(files.map(function (f) { return [relativePath(f), f.size, f.lastModified]; }));
      var nextFingerprint = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(signature)))).map(function (b) { return b.toString(16).padStart(2, "0"); }).join("");
      if (!batch || nextFingerprint !== fingerprint) batch = crypto.randomUUID();
      fingerprint = nextFingerprint;
      try {
        var pending = JSON.parse(sessionStorage.getItem("comma-vault-pending") || "null");
        if (pending && pending.fingerprint === fingerprint) batch = pending.batch;
      } catch (_) {}
    } catch (_) { lock(false); status("此瀏覽器無法準備匯入，請使用 HTTPS 或本機網站。尚未上傳。"); return; }
    try { sessionStorage.setItem("comma-vault-pending", JSON.stringify({batch:batch, fingerprint:fingerprint})); } catch (_) {}
    el("progress").hidden = false; el("progress").value = 0;
    status("正在上傳 " + files.length + " 篇…");
    var form = new FormData();
    files.forEach(function (file) {
      // Remove only the selected root folder; retain all internal paths.
      var relative = relativePath(file);
      form.append(relative, file, file.name);
    });
    var xhr = new XMLHttpRequest(), offset = 0, buffer = "", streamError = "";
    xhr.open("POST", "/import/vault?batch=" + encodeURIComponent(batch));
    xhr.timeout = 330000;
    xhr.upload.addEventListener("progress", function (event) {
      if (event.lengthComputable) {
        var percent = Math.round(event.loaded / event.total * 100);
        el("progress").value = percent * 0.3;
        status(percent < 100 ? "正在上傳… " + percent + "%" : "上傳完成，正在準備筆記…");
      }
    });
    function read() {
      if (xhr.status !== 200) return;
      buffer += xhr.responseText.slice(offset); offset = xhr.responseText.length;
      var newline;
      while ((newline = buffer.indexOf("\n")) >= 0) {
        var line = buffer.slice(0, newline); buffer = buffer.slice(newline + 1);
        if (!line.trim()) continue;
        try {
          var data = JSON.parse(line);
          if (data.type === "progress") {
            el("progress").value = 30 + data.done / (data.total * 2) * 69;
            status(data.done < data.total ? "正在建立筆記… " + data.done + " / " + data.total : "正在解析連結… " + (data.done - data.total) + " / " + data.total);
          } else if (data.type === "complete") complete(data);
          else if (data.type === "error") streamError = data.error;
        } catch (_) { streamError = "回應無法讀取，請重試同一批次。"; }
      }
    }
    function finish() {
      read(); lock(false);
      if (report) { start.disabled = true; start.textContent = "已完成"; }
      else {
        status(streamError || (xhr.status && xhr.status !== 200 ? xhr.responseText.slice(0, 250) : "連線中斷。重試同一批次不會重複建立筆記。"));
        start.textContent = "重試這批匯入";
      }
    }
    xhr.addEventListener("progress", read);
    xhr.addEventListener("loadend", finish);
    xhr.send(form);
  });
  el("download").addEventListener("click", function () {
    if (!report) return;
    var url = URL.createObjectURL(new Blob([JSON.stringify(report, null, 2)], { type: "application/json" }));
    var link = document.createElement("a"); link.href = url; link.download = "comma-vault-import-" + batch + ".json"; link.click();
    setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
  });
})();
