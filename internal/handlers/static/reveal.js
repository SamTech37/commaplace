(function () {
  "use strict";

  // Scroll reveal — horizontal clip-path sweep (逐行排版 feel)
  function makeObserver(stagger) {
    return new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            var el = entry.target;
            // Apply clip-path start-state inline, force a style flush, then transition.
            // Can't put clip-path in CSS [data-reveal] — it clips element to 0 width which
            // blocks IntersectionObserver. Apply inline, read offsetHeight to flush, then
            // swap to .revealed so the CSS transition fires.
            el.style.clipPath = "inset(0 100% 0 0)";
            void el.offsetHeight; // force style recalc so browser sees the start state
            requestAnimationFrame(function () {
              el.classList.add("revealed");
              el.style.clipPath = ""; // let CSS .revealed take over
              // After the clip-path transition finishes, remove it so absolutely
              // positioned children (e.g. dropdown menus) aren't clipped.
              el.addEventListener("transitionend", function clear(e) {
                if (e.propertyName === "clip-path") {
                  el.style.clipPath = "none";
                  el.removeEventListener("transitionend", clear);
                }
              });
            });
            makeObserver(false).unobserve(el);
          }
        });
      },
      { threshold: 0.08, rootMargin: "0px 0px -24px 0px" }
    );
  }

  function initReveal() {
    var observer = makeObserver(false);

    // Auto-stagger items inside [data-reveal-group]
    document.querySelectorAll("[data-reveal-group]").forEach(function (group) {
      var items = group.querySelectorAll("[data-reveal]");
      items.forEach(function (el, i) {
        if (!el.hasAttribute("data-reveal-delay")) {
          // cap at 6, 60ms per step
          el.setAttribute("data-reveal-delay", Math.min(i + 1, 6));
        }
      });
    });

    document.querySelectorAll("[data-reveal]").forEach(function (el) {
      observer.observe(el);
    });
  }

  function initRevealNew(root) {
    var observer = makeObserver(false);
    root.querySelectorAll("[data-reveal]:not(.revealed)").forEach(function (el) {
      observer.observe(el);
    });
  }

  document.addEventListener("DOMContentLoaded", initReveal);

  // Re-run after HTMX swaps (infinite scroll appends new entries)
  document.addEventListener("htmx:afterSwap", function (e) {
    if (!e.detail || !e.detail.target) return;
    var target = e.detail.target;
    var group = target.closest("[data-reveal-group]") || target;
    var newItems = group.querySelectorAll("[data-reveal]:not(.revealed)");
    newItems.forEach(function (el, i) {
      if (!el.hasAttribute("data-reveal-delay")) {
        el.setAttribute("data-reveal-delay", Math.min(i + 1, 6));
      }
    });
    initRevealNew(group);
  });

  // Smooth page fade on HTMX navigation
  document.addEventListener("htmx:beforeSwap", function () {
    var content = document.querySelector(".content");
    if (content) {
      content.style.transition = "opacity 0.15s ease";
      content.style.opacity = "0";
    }
  });
  document.addEventListener("htmx:afterSwap", function () {
    var content = document.querySelector(".content");
    if (content) { content.style.opacity = "1"; }
  });
})();
