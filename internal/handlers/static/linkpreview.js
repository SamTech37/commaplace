(function () {
  "use strict";

  // Hover previews are a pointer affordance; on touch a tap just navigates.
  if (!window.matchMedia || !window.matchMedia("(hover: hover)").matches) return;

  var SHOW_DELAY = 350; // long enough that scrolling past a link doesn't strobe cards
  var HIDE_DELAY = 200; // grace for the cursor to travel from link into the card
  var MARGIN = 8;

  var body = document.querySelector(".article-body");
  if (!body) return;

  // .content carries a persistent transform from its page-fade animation
  // (animation-fill-mode: both), which would become the containing block for
  // this position:fixed popup and throw it off. Reparent to <body>, same fix
  // as cmeditor.js and tagsearch.js.
  var popup = document.createElement("div");
  popup.className = "link-preview-card";
  popup.hidden = true;
  document.body.appendChild(popup);

  var cache = new Map();
  var showTimer = null;
  var hideTimer = null;
  var current = null; // anchor the popup belongs to

  // previewURLFor is the one place that decides what is previewable.
  // External URL previews plug in here.
  function previewURLFor(a) {
    if (!a.classList.contains("wiki-resolved") && !a.classList.contains("wikilink-cross")) return null;
    if (a.classList.contains("wiki-cross-unresolved")) return null; // no note behind it
    var path = a.getAttribute("href") || "";
    var m = path.split("#")[0].match(/^\/([^/]+)\/([^/]+)$/);
    if (!m) return null;
    return "/api/preview/" + m[1] + "/" + m[2];
  }

  function position(anchor) {
    var r = anchor.getBoundingClientRect();
    var pw = popup.offsetWidth;
    var ph = popup.offsetHeight;

    // Prefer a side that clears the text column entirely, so the card sits
    // beside what you are reading instead of on top of it.
    var left, beside = true;
    if (window.innerWidth - r.right >= pw + MARGIN * 2) left = r.right + MARGIN;
    else if (r.left >= pw + MARGIN * 2) left = r.left - pw - MARGIN;
    else {
      left = Math.max(MARGIN, Math.min(r.left, window.innerWidth - pw - MARGIN));
      beside = false;
    }

    var top;
    if (beside) {
      // Beside the link: line the card up with it, only clamped to the viewport.
      top = Math.max(MARGIN, Math.min(r.top - 4, window.innerHeight - ph - MARGIN));
    } else {
      // Over the column: drop below, flip above only when there is more room.
      var below = window.innerHeight - MARGIN - (r.bottom + 4);
      var above = r.top - 4 - MARGIN;
      if (ph <= below || below >= above) top = r.bottom + 4;
      else top = Math.max(MARGIN, r.top - ph - 4);
    }

    popup.style.left = left + "px";
    popup.style.top = top + "px";
  }

  function show(anchor, html) {
    if (!html) return;
    popup.innerHTML = html;
    popup.hidden = false;
    current = anchor;
    position(anchor);
  }

  function hide() {
    popup.hidden = true;
    popup.innerHTML = "";
    current = null;
  }

  function open(anchor) {
    var url = previewURLFor(anchor);
    if (!url) return;
    if (cache.has(url)) {
      show(anchor, cache.get(url));
      return;
    }
    fetch(url)
      .then(function (res) { return res.ok ? res.text() : ""; })
      .catch(function () { return ""; })
      .then(function (html) {
        cache.set(url, html);
        // The pointer may have moved on during the fetch.
        if (anchor.matches(":hover")) show(anchor, html);
      });
  }

  function cancelTimers() {
    clearTimeout(showTimer);
    clearTimeout(hideTimer);
  }

  body.addEventListener("mouseover", function (e) {
    var a = e.target.closest("a");
    if (!a || !body.contains(a) || !previewURLFor(a) || a === current) return;
    cancelTimers();
    showTimer = setTimeout(function () { open(a); }, SHOW_DELAY);
  });

  body.addEventListener("mouseout", function (e) {
    var a = e.target.closest("a");
    if (!a) return;
    clearTimeout(showTimer);
    hideTimer = setTimeout(hide, HIDE_DELAY);
  });

  popup.addEventListener("mouseenter", cancelTimers);
  popup.addEventListener("mouseleave", function () {
    hideTimer = setTimeout(hide, HIDE_DELAY);
  });

  window.addEventListener("scroll", function () {
    cancelTimers();
    if (current) hide();
  }, { passive: true });

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") { cancelTimers(); hide(); }
  });
})();
