// Cmd+K / Ctrl+K opens a fuzzy note-title palette. Plain ES5, no deps.
(function () {
  let dialog = null;
  let input = null;
  let list = null;
  let hint = null;
  let items = [];
  let activeIdx = 0;
  let debounceTimer = null;

  function ensureUI() {
    if (dialog) return;
    dialog = document.createElement('dialog');
    dialog.className = 'palette';
    dialog.innerHTML =
      '<input type="search" class="palette-input" placeholder="搜尋筆記標題…" autocomplete="off" />' +
      '<ul class="palette-list" role="listbox"></ul>' +
      '<div class="palette-hint">↑↓ 切換 · Enter 開啟 · ESC 關閉</div>';
    document.body.appendChild(dialog);
    input = dialog.querySelector('.palette-input');
    list = dialog.querySelector('.palette-list');
    hint = dialog.querySelector('.palette-hint');

    input.addEventListener('input', function () {
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(search, 100);
    });
    input.addEventListener('keydown', onKey);

    dialog.addEventListener('close', function () {
      items = [];
      activeIdx = 0;
      list.innerHTML = '';
      input.value = '';
    });
    // click outside the inner content closes
    dialog.addEventListener('click', function (e) {
      const rect = input.getBoundingClientRect();
      // crude: if click is above input area (on backdrop region of dialog), close
      if (e.target === dialog) dialog.close();
    });
  }

  function open() {
    ensureUI();
    if (dialog.open) return;
    dialog.showModal();
    input.focus();
    render();
  }

  function search() {
    const q = input.value.trim();
    if (!q) {
      items = [];
      activeIdx = 0;
      render();
      return;
    }
    fetch('/api/search/palette?q=' + encodeURIComponent(q))
      .then(function (r) { return r.json(); })
      .then(function (data) {
        items = data || [];
        activeIdx = 0;
        render();
      })
      .catch(function () {
        items = [];
        render();
      });
  }

  function render() {
    if (items.length === 0) {
      list.innerHTML = input.value.trim()
        ? '<li class="palette-empty">沒有符合的筆記</li>'
        : '<li class="palette-empty">輸入關鍵字搜尋</li>';
      return;
    }
    let html = '';
    for (let i = 0; i < items.length; i++) {
      const it = items[i];
      html +=
        '<li class="palette-item' + (i === activeIdx ? ' active' : '') + '" data-idx="' + i + '">' +
        '<span class="palette-item-title">' + esc(it.title) + '</span>' +
        '<span class="palette-item-handle">@' + esc(it.handle) + '</span>' +
        '</li>';
    }
    list.innerHTML = html;
    list.querySelectorAll('.palette-item').forEach(function (el) {
      el.addEventListener('click', function () {
        const i = parseInt(el.dataset.idx, 10);
        if (!isNaN(i) && items[i]) go(items[i].url);
      });
    });
    // scroll active into view
    const active = list.querySelector('.palette-item.active');
    if (active && active.scrollIntoView) active.scrollIntoView({ block: 'nearest' });
  }

  function go(url) {
    dialog.close();
    window.location.href = url;
  }

  function onKey(e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (items.length === 0) return;
      activeIdx = (activeIdx + 1) % items.length;
      render();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (items.length === 0) return;
      activeIdx = (activeIdx - 1 + items.length) % items.length;
      render();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (items[activeIdx]) go(items[activeIdx].url);
    }
  }

  function esc(s) {
    const d = document.createElement('div');
    d.textContent = s == null ? '' : String(s);
    return d.innerHTML;
  }

  // Global Cmd+K / Ctrl+K trigger
  document.addEventListener('keydown', function (e) {
    if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
      e.preventDefault();
      open();
    }
  });
})();
