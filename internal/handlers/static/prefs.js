// Display preferences (appearance + motion) as segmented controls in the
// account / settings menu. Each button carries data-pref-set + data-val, the
// same shape reader.js uses. Values are applied to <html> as data-* attributes
// and persisted in localStorage; an inline IIFE in layout.templ re-applies them
// pre-paint so there is no flash of the wrong theme on the next page load.
(function () {
  "use strict";

  // First segment of each control means "don't override" — the attribute is
  // removed and the OS preference decides. Matches reader.js's convention of
  // dropping the attribute when the value is the default.
  var THEME = { key: "theme", values: ["auto", "light", "dark"], fallback: "auto" };
  var MOTION = { key: "motion", values: ["system", "full", "reduced"], fallback: "system" };

  function read(name, spec) {
    try {
      var v = localStorage.getItem(name);
      if (spec.values.indexOf(v) !== -1) return v;
    } catch (e) {}
    return spec.fallback;
  }

  function write(name, value) {
    try {
      localStorage.setItem(name, value);
    } catch (e) {}
  }

  function applyTheme(value) {
    var resolved = value;
    if (value === "auto") {
      resolved = matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
    }
    document.documentElement.setAttribute("data-theme", resolved);
  }

  function applyMotion(value) {
    // "system" leaves the attribute off so the @media (prefers-reduced-motion)
    // block alone decides; "full" is what that block tests against to let a
    // reader opt back into animation despite an OS-level reduce setting.
    if (value === "system") document.documentElement.removeAttribute("data-motion");
    else document.documentElement.setAttribute("data-motion", value);
  }

  function syncButtons() {
    var current = { theme: read("theme", THEME), motion: read("motion", MOTION) };
    var btns = document.querySelectorAll("[data-pref-set]");
    for (var i = 0; i < btns.length; i++) {
      var kind = btns[i].getAttribute("data-pref-set");
      if (!(kind in current)) continue; // script/繁簡 is owned by opencc-toggle.js
      btns[i].setAttribute(
        "aria-pressed",
        current[kind] === btns[i].getAttribute("data-val") ? "true" : "false"
      );
    }
  }

  function choose(kind, value) {
    if (kind === "theme") {
      applyTheme(value);
      write("theme", value);
      // Signed-in readers get the choice stored server-side too, so it follows
      // them across devices. Visitors just get a 204 back.
      try {
        fetch("/settings/theme", {
          method: "POST",
          headers: { "Content-Type": "application/x-www-form-urlencoded" },
          body: "theme=" + encodeURIComponent(value),
          credentials: "same-origin",
          // A failed sync is not worth surfacing: the choice is already applied
          // and stored locally. Caught on the promise, since a network error
          // rejects rather than throwing where the try block could see it.
        }).catch(function () {});
      } catch (e) {}
    } else if (kind === "motion") {
      applyMotion(value);
      write("motion", value);
    }
    syncButtons();
  }

  document.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-pref-set]");
    if (!btn) return;
    var kind = btn.getAttribute("data-pref-set");
    if (kind !== "theme" && kind !== "motion") return;
    choose(kind, btn.getAttribute("data-val"));
  });

  // On "auto", follow the OS if it changes while the page is open.
  if (window.matchMedia) {
    var mq = matchMedia("(prefers-color-scheme: light)");
    var onChange = function () {
      if (read("theme", THEME) === "auto") applyTheme("auto");
    };
    if (mq.addEventListener) mq.addEventListener("change", onChange);
    else if (mq.addListener) mq.addListener(onChange);
  }

  document.addEventListener("DOMContentLoaded", syncButtons);
})();
