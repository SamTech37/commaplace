// Share button — Web Share API on mobile, clipboard+toast fallback elsewhere.
(function () {
  document.addEventListener('click', async (e) => {
    const btn = e.target.closest('.share-btn');
    if (!btn) return;
    e.preventDefault();
    const url = decodeURI(location.href);
    const title = btn.dataset.title || document.title;
    const shareData = { title, url };

    if (navigator.share && (!navigator.canShare || navigator.canShare(shareData))) {
      try {
        await navigator.share(shareData);
      } catch (err) {
        // AbortError (user dismissed the sheet) or other failure — no
        // fallback: falling back to clipboard after an explicit cancel
        // would be surprising.
      }
      return;
    }

    try {
      await navigator.clipboard.writeText(url);
      showToast('連結已複製');
    } catch (err) {
      showToast('複製失敗');
    }
  });

  function showToast(msg) {
    let el = document.querySelector('.toast');
    if (!el) {
      el = document.createElement('div');
      el.className = 'toast';
      document.body.appendChild(el);
    }
    el.textContent = msg;
    el.classList.remove('show');
    void el.offsetWidth; // reflow so re-adding 'show' retriggers the transition
    el.classList.add('show');
    clearTimeout(el._hideTimer);
    el._hideTimer = setTimeout(() => el.classList.remove('show'), 2200);
  }
})();
