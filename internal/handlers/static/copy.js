// Copy-markdown button — delegated, works on any page that has a
// <button class="copy-md" data-raw="…">. Tiny enough to ship on every page.
(function() {
  document.addEventListener('click', async (e) => {
    const btn = e.target.closest('.copy-md');
    if (!btn) return;
    e.preventDefault();
    const url = btn.dataset.raw;
    if (!url) return;
    const original = btn.textContent;
    try {
      const res = await fetch(url);
      if (!res.ok) throw new Error('fetch failed');
      const text = await res.text();
      await navigator.clipboard.writeText(text);
      btn.textContent = '✓ 已複製';
    } catch (err) {
      btn.textContent = '✗ 複製失敗';
    }
    setTimeout(() => { btn.textContent = original; }, 1600);
  });
})();

// Close any open action-menu dropdowns when clicking outside them.
document.addEventListener('click', e => {
  document.querySelectorAll('details.action-menu[open]').forEach(d => {
    if (!d.contains(e.target)) d.removeAttribute('open');
  });
});

// Esc closes an open action-menu and hands focus back to its trigger.
// <details> gives us click-to-open and a sane tab order for free, but no
// keyboard dismiss — without this a keyboard user is stranded in the panel.
document.addEventListener('keydown', e => {
  if (e.key !== 'Escape') return;
  document.querySelectorAll('details.action-menu[open]').forEach(d => {
    d.removeAttribute('open');
    const summary = d.querySelector('summary');
    if (summary && d.contains(document.activeElement)) summary.focus();
  });
});

// Mirror the native open state onto aria-expanded so screen readers are told
// whether the panel is showing. Derived from the element rather than written
// into the markup: the attribute cannot then drift out of sync with reality.
// Note <details> fires toggle asynchronously, so this lands on the next tick.
function syncMenuExpanded(d) {
  const summary = d.querySelector('summary');
  if (summary) summary.setAttribute('aria-expanded', d.open ? 'true' : 'false');
}
document.addEventListener('toggle', e => {
  const d = e.target;
  if (d instanceof HTMLDetailsElement && d.classList.contains('action-menu')) syncMenuExpanded(d);
}, true);
document.addEventListener('DOMContentLoaded', () => {
  document.querySelectorAll('details.action-menu').forEach(syncMenuExpanded);
});
