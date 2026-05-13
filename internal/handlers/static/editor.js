// Wiki link autocomplete for the note editor.
// Triggered by "[[" inside any <textarea data-autocomplete>.
// Inserts the picked suggestion's payload, then "]]", then refocuses.
(function() {
  const ta = document.querySelector('textarea[data-autocomplete]');
  if (!ta) return;
  const popup = document.getElementById('ac-popup');
  if (!popup) return;
  const list = popup.querySelector('ul');
  if (!list) return;

  let activeIdx = -1;
  let queryStart = -1; // textarea index of the char *after* "[["

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

  // Walk back from caret looking for an unclosed "[[" on the current line.
  function findTriggerStart() {
    const v = ta.value, c = ta.selectionStart;
    for (let i = c - 1; i >= 1; i--) {
      const ch = v[i];
      if (ch === '\n') return -1;
      if (ch === ']' && v[i-1] === ']') return -1;
      if (ch === '[' && v[i-1] === '[') return i + 1;
    }
    return -1;
  }

  let inflight = 0;
  async function fetchAndRender(q) {
    const myReq = ++inflight;
    const r = await fetch('/api/wiki/suggest?q=' + encodeURIComponent(q));
    if (myReq !== inflight) return; // a newer request superseded us
    if (!r.ok) { close(); return; }
    const html = (await r.text()).trim();
    if (!html) { close(); return; }
    list.innerHTML = html;
    popup.hidden = false;
    setActive(0);
  }

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
      case 'Enter':
      case 'Tab':
        e.preventDefault(); insertActive(); break;
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

  ta.addEventListener('blur', () => setTimeout(close, 150)); // allow mousedown to land
})();
