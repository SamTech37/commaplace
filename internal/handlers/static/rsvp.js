(function () {
  "use strict";

  // RSVP (rapid serial visual presentation) reader: flashes one token at a
  // time over the article body. Latin runs flash as whole words; CJK and
  // punctuation flash one character at a time (Chinese has no word spaces).
  // Pure frontend, note pages only. Entry: a .rsvp-start button in the menu.

  function tokenize(text) {
    var tokens = [];
    var re = /[A-Za-z0-9'’\-]+|[^\s]/gu;
    var m;
    while ((m = re.exec(text)) !== null) tokens.push(m[0]);
    return tokens;
  }

  function build() {
    var o = document.createElement("div");
    o.className = "rsvp-overlay";
    o.setAttribute("role", "dialog");
    o.setAttribute("aria-label", "快速閱讀");
    o.innerHTML =
      '<button type="button" class="rsvp-close" aria-label="關閉">✕</button>' +
      '<div class="rsvp-stage"><span class="rsvp-word"></span></div>' +
      '<div class="rsvp-bar"><div class="rsvp-bar-fill"></div></div>' +
      '<div class="rsvp-controls">' +
      '<button type="button" class="rsvp-btn rsvp-restart" aria-label="重頭">⟲</button>' +
      '<button type="button" class="rsvp-btn rsvp-play" aria-label="播放/暫停">▶</button>' +
      '<label class="rsvp-speed">速度 <span class="rsvp-wpm">400</span> 字/分' +
      '<input type="range" class="rsvp-range" min="150" max="900" step="50" value="400"></label>' +
      "</div>";
    return o;
  }

  function start(text) {
    var tokens = tokenize(text);
    if (!tokens.length) return;

    var overlay = build();
    document.body.appendChild(overlay);
    document.documentElement.style.overflow = "hidden";

    var wordEl = overlay.querySelector(".rsvp-word");
    var fillEl = overlay.querySelector(".rsvp-bar-fill");
    var playBtn = overlay.querySelector(".rsvp-play");
    var wpmEl = overlay.querySelector(".rsvp-wpm");
    var range = overlay.querySelector(".rsvp-range");

    var i = 0;
    var wpm = 400;
    var playing = false;
    var timer = null;

    function render() {
      wordEl.textContent = tokens[i] || "";
      fillEl.style.width = ((i / tokens.length) * 100).toFixed(1) + "%";
    }
    function delay() {
      // slightly longer pause on sentence-ending punctuation
      var base = 60000 / wpm;
      var t = tokens[i - 1] || "";
      if (/[。！？.!?…；;]/.test(t)) return base * 2.2;
      if (/[，、,:：]/.test(t)) return base * 1.5;
      return base;
    }
    function tick() {
      if (i >= tokens.length) {
        pause();
        i = tokens.length - 1;
        render();
        return;
      }
      render();
      i++;
      timer = setTimeout(tick, delay());
    }
    function play() {
      if (playing) return;
      if (i >= tokens.length) i = 0;
      playing = true;
      playBtn.textContent = "❚❚";
      tick();
    }
    function pause() {
      playing = false;
      playBtn.textContent = "▶";
      if (timer) clearTimeout(timer);
      timer = null;
    }
    function toggle() {
      playing ? pause() : play();
    }
    function close() {
      pause();
      overlay.remove();
      document.documentElement.style.overflow = "";
      document.removeEventListener("keydown", onKey);
    }
    function onKey(e) {
      if (e.key === "Escape") close();
      else if (e.key === " ") {
        e.preventDefault();
        toggle();
      }
    }

    playBtn.addEventListener("click", toggle);
    overlay.querySelector(".rsvp-close").addEventListener("click", close);
    overlay.querySelector(".rsvp-restart").addEventListener("click", function () {
      i = 0;
      render();
    });
    range.addEventListener("input", function () {
      wpm = parseInt(range.value, 10);
      wpmEl.textContent = wpm;
    });
    overlay.addEventListener("click", function (e) {
      if (e.target === overlay) close();
    });
    document.addEventListener("keydown", onKey);

    render();
    play();
  }

  document.addEventListener("DOMContentLoaded", function () {
    var btn = document.querySelector(".rsvp-start");
    if (!btn) return;
    btn.addEventListener("click", function () {
      var body = document.querySelector(".article-body");
      if (body) start(body.textContent || "");
    });
  });
})();
