(function () {
  "use strict";

  // Scroll reveal via IntersectionObserver
  function initReveal() {
    var observer = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            entry.target.classList.add("revealed");
            observer.unobserve(entry.target);
          }
        });
      },
      { threshold: 0.08, rootMargin: "0px 0px -32px 0px" }
    );

    // Auto-stagger items within [data-reveal-group] containers
    document.querySelectorAll("[data-reveal-group]").forEach(function (group) {
      var items = group.querySelectorAll("[data-reveal]");
      items.forEach(function (el, i) {
        if (!el.hasAttribute("data-reveal-delay")) {
          el.setAttribute("data-reveal-delay", Math.min(i + 1, 6));
        }
      });
    });

    document.querySelectorAll("[data-reveal]").forEach(function (el) {
      observer.observe(el);
    });
  }

  // Re-run after HTMX swaps (infinite scroll adds new cards)
  function initRevealNew(root) {
    var observer = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            entry.target.classList.add("revealed");
            observer.unobserve(entry.target);
          }
        });
      },
      { threshold: 0.08, rootMargin: "0px 0px -32px 0px" }
    );
    root.querySelectorAll("[data-reveal]:not(.revealed)").forEach(function (el) {
      observer.observe(el);
    });
  }

  document.addEventListener("DOMContentLoaded", initReveal);

  // HTMX afterSwap: reveal newly added elements
  document.addEventListener("htmx:afterSwap", function (e) {
    if (e.detail && e.detail.target) {
      // Stagger new cards within a group
      var target = e.detail.target;
      var group = target.closest("[data-reveal-group]") || target;
      var newItems = group.querySelectorAll("[data-reveal]:not(.revealed)");
      newItems.forEach(function (el, i) {
        if (!el.hasAttribute("data-reveal-delay")) {
          el.setAttribute("data-reveal-delay", Math.min(i + 1, 6));
        }
      });
      initRevealNew(group);
    }
  });

  // Smooth page content fade on HTMX navigation (optional enhancement)
  document.addEventListener("htmx:beforeSwap", function () {
    var content = document.querySelector(".content");
    if (content) {
      content.style.transition = "opacity 0.18s ease";
      content.style.opacity = "0";
    }
  });
  document.addEventListener("htmx:afterSwap", function () {
    var content = document.querySelector(".content");
    if (content) {
      content.style.opacity = "1";
    }
  });
})();
