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
      btn.textContent = '✓ copied';
    } catch (err) {
      btn.textContent = '✗ copy failed';
    }
    setTimeout(() => { btn.textContent = original; }, 1600);
  });
})();
