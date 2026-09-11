/* Presentation-only model, also exercised by node:test. */
(function (root) {
  'use strict';
  const minimum = 260, gap = 18;
  function key(p) { return p.kind + ':' + (p.ref || '') + ':' + (p.query || ''); }
  function positions(panes) { let x = 14; return panes.map(p => { const rect = { key: key(p), x, width: p.width }; x += p.width + gap; return rect; }); }
  function resize(panes, index, delta) {
    if (!panes[index] || !panes[index + 1]) return;
    const a = panes[index], b = panes[index + 1];
    delta = Math.max(minimum - a.width, b.width - 1200, Math.min(delta, b.width - minimum, 1200 - a.width));
    a.width += delta; b.width -= delta;
  }
  function move(panes, from, to) { if (from < 0 || to < 0 || from >= panes.length || to >= panes.length) return; panes.splice(to, 0, panes.splice(from, 1)[0]); }
  function snapshot(state) { return { windows: state.windows.map(p => ({ key: key(p), kind: p.kind, ref: p.ref || '', query: p.query || '', width: p.width, scroll: p.scroll || 0 })), active: state.active, scrollLeft: state.scrollLeft || 0 }; }
  function safeURL(raw, origin) { try { const u = new URL(raw, origin); return u.origin === origin && /^https?:$/.test(u.protocol) && !u.username && !u.password ? u.pathname + u.search + u.hash : null; } catch (_) { return null; } }
  const api = { key, positions, resize, move, snapshot, safeURL, minimum, gap };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.CommaDeskModel = api;
})(globalThis);
