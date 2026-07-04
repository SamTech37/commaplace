(function () {
  var input = document.getElementById("tag-search");
  var popup = document.getElementById("tag-search-popup");
  if (!input || !popup) return;
  var list = popup.querySelector("ul");
  var timer;

  // .content carries a persistent transform from its page-fade animation
  // (animation-fill-mode: both), which would become the containing block for
  // this position:fixed popup and throw it off. Reparent to <body> so fixed
  // coordinates resolve against the viewport, same fix as cmeditor.js.
  document.body.appendChild(popup);

  function close() {
    popup.hidden = true;
    list.innerHTML = "";
  }

  function position() {
    var r = input.getBoundingClientRect();
    popup.style.left = r.left + "px";
    popup.style.top = (r.bottom + 4) + "px";
    popup.style.minWidth = r.width + "px";
  }

  input.addEventListener("input", function () {
    clearTimeout(timer);
    var q = input.value.trim();
    timer = setTimeout(function () {
      fetch("/api/tags/suggest?q=" + encodeURIComponent(q))
        .then(function (r) { return r.text(); })
        .then(function (html) {
          list.innerHTML = html;
          if (!list.children.length) { close(); return; }
          popup.hidden = false;
          position();
        })
        .catch(close);
    }, 150);
  });

  list.addEventListener("mousedown", function (e) {
    var li = e.target.closest(".ac-item");
    if (!li) return;
    e.preventDefault();
    window.location = "/tag/" + encodeURIComponent(li.getAttribute("data-insert"));
  });

  document.addEventListener("mousedown", function (e) {
    if (!popup.contains(e.target) && e.target !== input) close();
  });
})();
