/* Presentation-only model, also exercised by node:test. */
(function (root) {
  'use strict';
  const minimum = 260, gap = 18;
  function key(p) { return p.kind + ':' + (p.ref || '') + ':' + (p.query || ''); }
  function positions(panes) { let x = 14; return panes.map(p => { const rect = { key: key(p), x, width: p.width }; x += p.width + gap; return rect; }); }
  function viewport(panes, width) {
    const available = Math.max(0, width - 28), count = panes.length;
    if (!count) return [];
    if (available < minimum * count + gap * (count - 1)) {
      return positions(panes.map(p => ({ ...p, width: Math.min(p.width, available) })));
    }
    const budget = Math.min(1200 * count, available - gap * (count - 1));
    const result = panes.map(p => ({ ...p, width: minimum }));
    let remaining = budget - minimum * count;
    let candidates = result.map((_, i) => i);
    while (remaining > .01 && candidates.length) {
      const total = candidates.reduce((n, i) => n + Math.max(0, panes[i].width - minimum), 0);
      let used = 0;
      for (const i of candidates) {
        const share = total ? Math.max(0, panes[i].width - minimum) / total : 1 / candidates.length;
        const addition = Math.min(1200 - result[i].width, remaining * share);
        result[i].width += addition; used += addition;
      }
      remaining -= used;
      candidates = candidates.filter(i => result[i].width < 1199.99);
    }
    return positions(result);
  }
  function retainedKeys(windows, visible, loaded, limit = 4) {
    const keep = new Set(visible);
    const readers = windows.filter(p => loaded.has(key(p)) && p.kind !== 'edit' && !keep.has(key(p)))
      .sort((a, b) => loaded.get(key(b)) - loaded.get(key(a)));
    let slots = Math.max(0, limit - windows.filter(p => p.kind !== 'edit' && keep.has(key(p))).length);
    readers.slice(0, slots).forEach(p => keep.add(key(p)));
    windows.filter(p => p.kind === 'edit' && loaded.has(key(p))).forEach(p => keep.add(key(p)));
    return keep;
  }
  function resize(panes, index, delta) {
    if (!panes[index] || !panes[index + 1]) return;
    const a = panes[index], b = panes[index + 1];
    delta = Math.max(minimum - a.width, b.width - 1200, Math.min(delta, b.width - minimum, 1200 - a.width));
    a.width += delta; b.width -= delta;
  }
  function move(panes, from, to) { if (from < 0 || to < 0 || from >= panes.length || to >= panes.length) return; panes.splice(to, 0, panes.splice(from, 1)[0]); }
  function snapshot(state) { return { windows: state.windows.map(p => ({ key: key(p), kind: p.kind, ref: p.ref || '', query: p.query || '', width: p.width, scroll: p.scroll || 0 })), active: state.active, scrollLeft: state.scrollLeft || 0 }; }
  function safeURL(raw, origin) { try { const u = new URL(raw, origin); return u.origin === origin && /^https?:$/.test(u.protocol) && !u.username && !u.password ? u.pathname + u.search + u.hash : null; } catch (_) { return null; } }
  const api = { key, positions, viewport, retainedKeys, resize, move, snapshot, safeURL, minimum, gap };
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else root.CommaDeskModel = api;
})(globalThis);
