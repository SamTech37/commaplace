(function () {
  "use strict";

  // Reader font/size preference — applies to the note article body.
  // Persisted in localStorage; re-applied pre-paint by an inline IIFE in
  // _base.html (same pattern as theme/motion). The control lives on note
  // pages only, but the html[data-reader-*] attributes drive every note page.
  var DEFAULT = { size: "m", font: "serif" };

  function apply(kind, val) {
    var html = document.documentElement;
    var attr = "data-reader-" + kind;
    if (val === DEFAULT[kind]) html.removeAttribute(attr);
    else html.setAttribute(attr, val);
  }

  function get(kind) {
    try {
      return localStorage.getItem("reader-" + kind);
    } catch (e) {
      return null;
    }
  }

  function syncButtons() {
    var cur = { size: get("size") || DEFAULT.size, font: get("font") || DEFAULT.font };
    document.querySelectorAll("[data-reader-set]").forEach(function (b) {
      var kind = b.getAttribute("data-reader-set");
      b.setAttribute("aria-pressed", cur[kind] === b.getAttribute("data-val") ? "true" : "false");
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector("[data-reader-set]")) return;
    syncButtons();
    document.querySelectorAll("[data-reader-set]").forEach(function (b) {
      b.addEventListener("click", function () {
        var kind = b.getAttribute("data-reader-set");
        var val = b.getAttribute("data-val");
        apply(kind, val);
        try {
          localStorage.setItem("reader-" + kind, val);
        } catch (e) {}
        syncButtons();
      });
    });
  });
})();
