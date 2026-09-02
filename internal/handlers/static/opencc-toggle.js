// 前端繁簡顯示切換：原文 → 简 → 繁 → 原文 三態循環。純顯示層，偏好只存
// localStorage（不影響 notes.body_md，作者原字體永遠是儲存的真相）。
// opencc.min.js（字典，~1.2MB）懶載入：只有使用者第一次切離「原文」才會抓取。
(function () {
  var STORAGE_KEY = "hanscript";
  var LABEL = { orig: "原", cn: "简", tw: "繁" };
  var TITLE = {
    orig: "顯示原文",
    cn: "切換為简体显示",
    tw: "切換為繁體顯示",
  };
  var NEXT = { orig: "cn", cn: "tw", tw: "orig" };

  var btn = document.getElementById("script-toggle");
  var mobileBtn = document.getElementById("mobile-script-toggle");
  if (!btn) return;

  var mode = "orig";
  try {
    var stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "cn" || stored === "tw") mode = stored;
  } catch (e) {}

  var loadPromise = null;
  function loadOpenCC() {
    if (loadPromise) return loadPromise;
    loadPromise = new Promise(function (resolve, reject) {
      var s = document.createElement("script");
      s.src = "/assets/opencc.min.js";
      s.onload = resolve;
      s.onerror = function (e) {
        loadPromise = null; // allow retry on the next click instead of dying forever
        reject(e);
      };
      document.head.appendChild(s);
    });
    return loadPromise;
  }

  function markIgnored() {
    var els = document.querySelectorAll(
      "textarea, .CodeMirror, .cm-editor, .EasyMDEContainer"
    );
    for (var i = 0; i < els.length; i++) els[i].classList.add("ignore-opencc");
  }

  // Active HTMLConverter handles, restored before switching mode or re-applying.
  var handlers = [];
  function restoreAll() {
    for (var i = 0; i < handlers.length; i++) handlers[i].restore();
    handlers = [];
  }

  function convertRoot(root) {
    if (mode === "orig" || typeof OpenCC === "undefined") return;
    markIgnored();
    var from = mode === "cn" ? "tw" : "cn";
    // HTMLConverter only walks into subtrees whose root already carries
    // lang === fromLang; without this, .convert() runs but touches nothing.
    root.lang = from;
    try {
      var converter = OpenCC.Converter({ from: from, to: mode });
      var h = OpenCC.HTMLConverter(converter, root, from, mode);
      h.convert();
      handlers.push(h);
    } catch (e) {
      // Swallow: mode/button/localStorage already reflect the user's choice,
      // and restoreAll() always precedes the next convertRoot() call, so a
      // later successful attempt still starts from pristine original text.
    }
  }

  // restoreAll() first guarantees every convertRoot() call starts from
  // pristine original text, so it's always safe to re-run regardless of
  // whether a prior attempt partially failed.
  function reconvert() {
    restoreAll();
    if (mode !== "orig") convertRoot(document.body);
  }

  function syncButton() {
    btn.textContent = LABEL[mode];
    var label = TITLE[NEXT[mode]];
    btn.setAttribute("aria-label", label);
    btn.title = label;
    if (mobileBtn) {
      mobileBtn.textContent = label;
      mobileBtn.setAttribute("aria-label", label);
    }
  }

  btn.addEventListener("click", function () {
    // Commit mode/button/localStorage synchronously so a rapid second click
    // (before the ~1.2MB dictionary finishes its first load) always reads the
    // just-updated mode instead of a stale one.
    var next = NEXT[mode];
    mode = next;
    syncButton();
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch (e) {}
    if (next === "orig") {
      restoreAll();
      return;
    }
    loadOpenCC()
      .then(reconvert)
      .catch(function () {
        // Load failed; loadPromise was already reset so the next click retries.
      });
  });
  if (mobileBtn) {
    mobileBtn.addEventListener("click", function () {
      btn.click();
    });
  }

  document.body.addEventListener("htmx:afterSettle", function () {
    if (mode === "orig" || typeof OpenCC === "undefined") return;
    reconvert();
  });

  syncButton();
  if (mode !== "orig") {
    loadOpenCC()
      .then(reconvert)
      .catch(function () {});
  }
})();
