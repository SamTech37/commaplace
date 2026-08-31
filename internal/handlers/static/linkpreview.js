(function () {
  "use strict";

  // Hover previews are a pointer affordance; on touch a tap just navigates.
  if (!window.matchMedia || !window.matchMedia("(hover: hover)").matches) return;

  var SHOW_DELAY = 350; // long enough that scrolling past a link doesn't strobe cards
  var HIDE_DELAY = 200; // grace for the cursor to travel from link into the card
  var MARGIN = 8;

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
  var activeKey = null; // whatever hover target is currently shown/pending

  function position(rect) {
    var pw = popup.offsetWidth;
    var ph = popup.offsetHeight;

    // Prefer a side that clears the text column entirely, so the card sits
    // beside what you are reading instead of on top of it.
    var left, beside = true;
    if (window.innerWidth - rect.right >= pw + MARGIN * 2) left = rect.right + MARGIN;
    else if (rect.left >= pw + MARGIN * 2) left = rect.left - pw - MARGIN;
    else {
      left = Math.max(MARGIN, Math.min(rect.left, window.innerWidth - pw - MARGIN));
      beside = false;
    }

    var top;
    if (beside) {
      // Beside the anchor: line the card up with it, only clamped to the viewport.
      top = Math.max(MARGIN, Math.min(rect.top - 4, window.innerHeight - ph - MARGIN));
    } else {
      // Over the column: drop below, flip above only when there is more room.
      var below = window.innerHeight - MARGIN - (rect.bottom + 4);
      var above = rect.top - 4 - MARGIN;
      if (ph <= below || below >= above) top = rect.bottom + 4;
      else top = Math.max(MARGIN, rect.top - ph - 4);
    }

    popup.style.left = left + "px";
    popup.style.top = top + "px";
  }

  function show(rect, html) {
    if (!html) return;
    popup.innerHTML = html;
    popup.hidden = false;
    position(rect);
  }

  function hide() {
    popup.hidden = true;
    popup.innerHTML = "";
    activeKey = null;
  }

  function cancelTimers() {
    clearTimeout(showTimer);
    clearTimeout(hideTimer);
  }

  // scheduleShow: key identifies the hover target (an anchor element, or a
  // graph node object) so a second hover over the same target while the
  // timer is pending doesn't restart it. rectFn is called once the fetch
  // resolves (not now — the pointer may have moved) to place the popup;
  // isActive() reports whether that target is still the live hover.
  function scheduleShow(key, url, rectFn, isActive) {
    if (key === activeKey) return;
    cancelTimers();
    activeKey = key;
    showTimer = setTimeout(function () { open(key, url, rectFn, isActive); }, SHOW_DELAY);
  }

  function open(key, url, rectFn, isActive) {
    if (cache.has(url)) {
      if (isActive()) show(rectFn(), cache.get(url));
      return;
    }
    fetch(url)
      .then(function (res) { return res.ok ? res.text() : ""; })
      .catch(function () { return ""; })
      .then(function (html) {
        cache.set(url, html);
        if (key === activeKey && isActive()) show(rectFn(), html);
      });
  }

  function scheduleHide() {
    clearTimeout(showTimer);
    hideTimer = setTimeout(hide, HIDE_DELAY);
  }

  function cancelHide() {
    clearTimeout(hideTimer);
  }

  window.LinkPreview = {
    scheduleShow: scheduleShow,
    scheduleHide: scheduleHide,
    cancelHide: cancelHide,
    hideNow: hide,
  };

  popup.addEventListener("mouseenter", cancelHide);
  popup.addEventListener("mouseleave", scheduleHide);

  window.addEventListener("scroll", function () {
    cancelTimers();
    if (!popup.hidden) hide();
  }, { passive: true });

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") { cancelTimers(); hide(); }
  });

  // ---- wiring for <a> hover: wikilinks in note bodies, plus the connected/
  // backlinks sections' mini-card and backlink-row anchors, which point at
  // notes exactly the same way (previewURLFor is the one place that decides
  // what is previewable — external URL previews would plug in here too). ----
  function previewURLFor(a) {
    var known = a.classList.contains("mini-card") || a.classList.contains("backlink-row");
    if (!known) {
      if (!a.classList.contains("wiki-resolved") && !a.classList.contains("wikilink-cross")) return null;
      if (a.classList.contains("wiki-cross-unresolved")) return null; // no note behind it
    }
    var path = a.getAttribute("href") || "";
    var m = path.split("#")[0].match(/^\/([^/]+)\/([^/]+)$/);
    if (!m) return null;
    return "/api/preview/" + m[1] + "/" + m[2];
  }

  var hoverAnchor = null;
  document.addEventListener("mouseover", function (e) {
    var a = e.target.closest("a");
    if (!a || a === hoverAnchor) return;
    var url = previewURLFor(a);
    if (!url) return;
    hoverAnchor = a;
    scheduleShow(a, url, function () { return a.getBoundingClientRect(); }, function () { return a.matches(":hover"); });
  });

  document.addEventListener("mouseout", function (e) {
    var a = e.target.closest("a");
    if (!a || a !== hoverAnchor) return;
    hoverAnchor = null;
    scheduleHide();
  });
})();
