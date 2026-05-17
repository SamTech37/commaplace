(function() {
  const ta = document.querySelector('textarea[data-autocomplete]');
  if (!ta) return;
  const popup = document.getElementById('ac-popup');
  if (!popup) return;
  const list = popup.querySelector('ul');
  if (!list) return;
  const preview = document.getElementById('preview');

  let activeIdx = -1;
  let queryStart = -1; // index in textarea *after* "[["

  function close() {
    popup.hidden = true;
    activeIdx = -1;
    queryStart = -1;
    list.innerHTML = '';
  }

  function setActive(i) {
    const items = list.children;
    if (!items.length) return;
    activeIdx = (i + items.length) % items.length;
    [...items].forEach((el, idx) => el.classList.toggle('active', idx === activeIdx));
    items[activeIdx].scrollIntoView({ block: 'nearest' });
  }

  function findTriggerStart() {
    const v = ta.value, c = ta.selectionStart;
    for (let i = c - 1; i >= 1; i--) {
      if (v[i] === '\n') return -1;
      if (v[i] === ']' && v[i-1] === ']') return -1;
      if (v[i] === '[' && v[i-1] === '[') return i + 1;
    }
    return -1;
  }

  function lcp(strings) {
    if (!strings.length) return '';
    let prefix = strings[0];
    for (let i = 1; i < strings.length; i++) {
      while (!strings[i].startsWith(prefix)) {
        prefix = prefix.slice(0, -1);
        if (!prefix) return '';
      }
    }
    return prefix;
  }

  // Full insert: writes data-insert + "]]" and closes.
  function insertActive() {
    const items = list.children;
    if (!items.length || activeIdx < 0) { close(); return; }
    const insert = items[activeIdx].getAttribute('data-insert') || '';
    const before = ta.value.slice(0, queryStart);
    const after  = ta.value.slice(ta.selectionStart);
    ta.value = before + insert + ']]' + after;
    const pos = before.length + insert.length + 2;
    ta.selectionStart = ta.selectionEnd = pos;
    ta.focus();
    ta.dispatchEvent(new Event('input', { bubbles: true }));
    close();
  }

  // Tab: longest common prefix across all suggestions.
  // If LCP extends beyond what's typed, complete to it and re-fetch.
  // If already at LCP (nothing to extend), full-insert the active item.
  function tabComplete() {
    const items = list.children;
    if (!items.length) { close(); return; }

    const inserts = [...items].map(el => el.getAttribute('data-insert') || '');
    const prefix = lcp(inserts);
    const currentQuery = ta.value.slice(queryStart, ta.selectionStart);

    if (prefix.length > currentQuery.length) {
      // Extend to LCP
      const before = ta.value.slice(0, queryStart);
      const after  = ta.value.slice(ta.selectionStart);
      ta.value = before + prefix + after;
      ta.selectionStart = ta.selectionEnd = before.length + prefix.length;
      ta.focus();
      // Re-fetch with extended query so popup updates
      ta.dispatchEvent(new Event('input', { bubbles: true }));
    } else {
      // Already at LCP — accept the active item in full
      if (activeIdx < 0) setActive(0);
      insertActive();
    }
  }

  let fetchTimer = null;
  let inflight = 0;

  function fetchAndRender(q) {
    clearTimeout(fetchTimer);
    fetchTimer = setTimeout(async () => {
      const myReq = ++inflight;
      const r = await fetch('/api/wiki/suggest?q=' + encodeURIComponent(q));
      if (myReq !== inflight) return;
      if (!r.ok) { close(); return; }
      const html = (await r.text()).trim();
      if (!html) { close(); return; }
      list.innerHTML = html;
      popup.hidden = false;
      setActive(0);
    }, 120);
  }

  ta.addEventListener('input', () => {
    const start = findTriggerStart();
    if (start < 0) { close(); return; }
    queryStart = start;
    fetchAndRender(ta.value.slice(start, ta.selectionStart));
  });

  ta.addEventListener('keydown', (e) => {
    if (popup.hidden) return;
    switch (e.key) {
      case 'ArrowDown': e.preventDefault(); setActive(activeIdx + 1); break;
      case 'ArrowUp':   e.preventDefault(); setActive(activeIdx - 1); break;
      case 'Enter':     e.preventDefault(); insertActive(); break;
      case 'Tab':       e.preventDefault(); tabComplete(); break;
      case 'Escape':    e.preventDefault(); close(); break;
    }
  });

  list.addEventListener('mousedown', (e) => {
    const el = e.target.closest('.ac-item');
    if (!el) return;
    e.preventDefault();
    activeIdx = [...list.children].indexOf(el);
    insertActive();
  });

  ta.addEventListener('blur', () => setTimeout(close, 150));

  // Preview fade: dim while HTMX request is in flight, restore after settle.
  // Listen on body with e.detail.elt check — the reliable HTMX pattern.
  if (preview) {
    document.body.addEventListener('htmx:beforeRequest', (e) => {
      if (e.detail.elt === ta) preview.classList.add('preview-loading');
    });
    document.body.addEventListener('htmx:afterSettle', (e) => {
      if (e.detail.elt === ta) preview.classList.remove('preview-loading');
    });
  }
})();
