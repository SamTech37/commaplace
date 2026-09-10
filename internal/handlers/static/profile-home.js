(function () {
  function ready(fn) {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", fn);
    } else {
      fn();
    }
  }

  function slug(value) {
    var out = "";
    var prevDash = true;
    for (var char of String(value || "").trim().toLowerCase()) {
      if (/[\p{L}\p{N}]/u.test(char)) {
        out += char;
        prevDash = false;
      } else if (!prevDash) {
        out += "-";
        prevDash = true;
      }
    }
    return out.replace(/^-+|-+$/g, "");
  }

  function normalizeTag(value) {
    var out = "";
    var prevDash = true;
    for (var char of String(value || "").trim().toLowerCase()) {
      if (char === "-" || /[\p{L}\p{N}]/u.test(char)) {
        out += char;
        prevDash = false;
      } else if (!prevDash) {
        out += "-";
        prevDash = true;
      }
    }
    return out.replace(/^-+|-+$/g, "");
  }

  function noteRef(value) {
    var ref = String(value || "").trim();
    if (ref.startsWith("[[") && ref.endsWith("]]")) ref = ref.slice(2, -2);
    try {
      var url = new URL(ref, window.location.origin);
      if (url.pathname && ref.includes("/")) ref = url.pathname;
    } catch (_) {}
    ref = ref.replace(/^\/+|\/+$/g, "");
    if (ref.includes("/")) ref = ref.slice(ref.lastIndexOf("/") + 1);
    return slug(ref);
  }

  function normalizeRef(type, value) {
    if (type === "note") return noteRef(value);
    if (type === "tag") return normalizeTag(value);
    return "";
  }

  function kindLabel(type) {
    if (type === "note") return "手選筆記";
    if (type === "tag") return "主題房間";
    if (type === "text") return "自由文字";
    return "展示櫃";
  }

  function displayTitle(item) {
    return item.title || (item.type === "tag" && item.ref) || kindLabel(item.type);
  }

  function emptyText(type, ref) {
    if (type === "note" && ref) return "儲存後連到 " + ref;
    if (type === "tag" && ref) return "儲存後整理 " + ref;
    if (type === "note") return "待選一篇公開筆記";
    if (type === "tag") return "待放一篇公開筆記";
    return "空展示櫃";
  }

  function appendWikiText(parent, text) {
    var re = /\[\[([^\]]+)\]\]/g;
    var last = 0;
    var match;
    while ((match = re.exec(text))) {
      if (match.index > last) {
        parent.appendChild(document.createTextNode(text.slice(last, match.index)));
      }
      var link = document.createElement("span");
      link.className = "wiki-unresolved";
      link.textContent = match[1].trim();
      parent.appendChild(link);
      last = re.lastIndex;
    }
    if (last < text.length) {
      parent.appendChild(document.createTextNode(text.slice(last)));
    }
  }

  function renderGuide(markdown, target) {
    if (!target) return;
    target.replaceChildren();

    var lines = String(markdown || "").trim().split(/\r?\n/);
    var paragraph = [];

    function flushParagraph() {
      if (paragraph.length === 0) return;
      var p = document.createElement("p");
      appendWikiText(p, paragraph.join(" "));
      target.appendChild(p);
      paragraph = [];
    }

    lines.forEach(function (line) {
      var trimmed = line.trim();
      if (!trimmed) {
        flushParagraph();
        return;
      }
      var heading = trimmed.match(/^(#{2,3})\s+(.+)$/);
      if (heading) {
        flushParagraph();
        var level = heading[1].length === 2 ? "h2" : "h3";
        var h = document.createElement(level);
        appendWikiText(h, heading[2]);
        target.appendChild(h);
        return;
      }
      paragraph.push(trimmed);
    });
    flushParagraph();
  }

  function values(row) {
    var type = row.querySelector("[data-showcase-type]").value.trim();
    var title = row.querySelector("[data-showcase-title]").value.trim();
    var body = row.querySelector("[data-showcase-body]").value.trim();
    var refInput = row.querySelector("[data-showcase-ref]");
    var ref = normalizeRef(type, refInput.value);
    return { type: type, title: title, ref: ref, body: body };
  }

  function blank(item) {
    if (!item.type) return true;
    if (item.type === "text") return !item.title && !item.body;
    return !item.title && !item.ref && !item.body;
  }

  function ensureBody(article, head) {
    var body = article.querySelector("[data-profile-showcase-body]");
    if (body) return body;
    body = document.createElement("div");
    body.className = "profile-showcase-body";
    body.dataset.profileShowcaseBody = "";
    head.insertAdjacentElement("afterend", body);
    return body;
  }

  function createEmptyShowcase(item) {
    var article = document.createElement("article");
    article.className = "profile-showcase";
    article.dataset.profileShowcase = "";

    var head = document.createElement("div");
    head.className = "profile-showcase-head";

    var kind = document.createElement("span");
    kind.dataset.showcaseKind = "";

    var title = document.createElement("h3");
    title.dataset.profileShowcaseTitle = "";

    head.append(kind, title);
    article.appendChild(head);

    if (item.type !== "text") {
      var note = document.createElement("div");
      note.className = "profile-showcase-note is-empty";
      var strong = document.createElement("strong");
      strong.textContent = emptyText(item.type, item.ref);
      note.appendChild(strong);
      article.appendChild(note);
    }

    return article;
  }

  ready(function () {
    var form = document.querySelector("[data-profile-home-form]");
    var preview = document.querySelector("[data-profile-home-preview]");
    if (!form || !preview) return;

    var titleInput = form.querySelector("[data-profile-home-title-input]");
    var bodyInput = form.querySelector("[data-profile-home-body-input]");
    var previewTitle = preview.querySelector("[data-profile-home-title]");
    var previewBody = preview.querySelector("[data-profile-home-body]");
    var previewGrid = preview.querySelector("[data-profile-home-grid]");
    var fallbackTitle = previewTitle ? previewTitle.dataset.fallback || "" : "";
    var originalShowcases = new Map();

    preview.querySelectorAll("[data-profile-showcase]").forEach(function (showcase) {
      originalShowcases.set(showcase.dataset.showcaseKey, showcase.outerHTML);
    });

    function rows() {
      return Array.from(form.querySelectorAll("[data-showcase-row]"));
    }

    function showcaseContainer() {
      var existing = preview.querySelector("[data-profile-showcases]");
      if (existing || !previewGrid) return existing;
      var created = document.createElement("div");
      created.className = "profile-showcases";
      created.dataset.profileShowcases = "";
      previewGrid.appendChild(created);
      return created;
    }

    function updateLegends() {
      rows().forEach(function (row, index) {
        var legend = row.querySelector("legend");
        if (legend) legend.textContent = "展示櫃 " + (index + 1);
      });
    }

    function updateRowChrome(row) {
      var type = row.querySelector("[data-showcase-type]").value;
      var ref = row.querySelector("[data-showcase-ref]");
      if (!ref) return;
      if (type === "tag") ref.placeholder = "philosophy";
      else if (type === "text") ref.placeholder = "";
      else ref.placeholder = "writing-is-thinking";
    }

    function renderShowcase(item) {
      var key = item.type + ":" + item.ref;
      var html = originalShowcases.get(key);
      var article;
      if (html) {
        var template = document.createElement("template");
        template.innerHTML = html;
        article = template.content.firstElementChild;
      } else {
        article = createEmptyShowcase(item);
      }

      article.dataset.showcaseKey = key;
      article.classList.toggle(
        "is-empty",
        item.type !== "text" && !article.querySelector(".profile-showcase-note:not(.is-empty)")
      );

      var kind = article.querySelector("[data-showcase-kind]");
      if (kind) kind.textContent = kindLabel(item.type);

      var title = article.querySelector("[data-profile-showcase-title]");
      if (title) title.textContent = displayTitle(item);

      var head = article.querySelector(".profile-showcase-head");
      var body = ensureBody(article, head);
      renderGuide(item.body, body);

      return article;
    }

    function syncTitle() {
      if (!titleInput || !previewTitle) return;
      previewTitle.textContent = titleInput.value.trim() || fallbackTitle;
    }

    function syncBody() {
      if (!bodyInput || !previewBody) return;
      renderGuide(bodyInput.value, previewBody);
    }

    function syncShowcases() {
      rows().forEach(updateRowChrome);
      var container = showcaseContainer();
      if (!container) return;
      var items = rows().map(values).filter(function (item) {
        return !blank(item);
      });
      container.replaceChildren();
      items.slice(0, 6).forEach(function (item) {
        container.appendChild(renderShowcase(item));
      });
      container.hidden = items.length === 0;
    }

    form.addEventListener("input", function (event) {
      if (event.target.matches("[data-profile-home-title-input]")) syncTitle();
      if (event.target.matches("[data-profile-home-body-input]")) syncBody();
      if (event.target.matches("[data-showcase-title], [data-showcase-ref], [data-showcase-body]")) syncShowcases();
    });

    form.addEventListener("change", function (event) {
      if (event.target.matches("[data-showcase-type]")) syncShowcases();
    });

    form.addEventListener("click", function (event) {
      var button = event.target.closest("[data-showcase-up], [data-showcase-down], [data-showcase-clear]");
      if (!button) return;
      var row = button.closest("[data-showcase-row]");
      if (!row) return;
      if (button.matches("[data-showcase-up]") && row.previousElementSibling) {
        row.parentNode.insertBefore(row, row.previousElementSibling);
      } else if (button.matches("[data-showcase-down]") && row.nextElementSibling) {
        row.parentNode.insertBefore(row.nextElementSibling, row);
      } else if (button.matches("[data-showcase-clear]")) {
        row.querySelector("[data-showcase-type]").value = "";
        row.querySelector("[data-showcase-title]").value = "";
        row.querySelector("[data-showcase-ref]").value = "";
        row.querySelector("[data-showcase-body]").value = "";
      }
      updateLegends();
      syncShowcases();
    });

    rows().forEach(updateRowChrome);
  });
})();
