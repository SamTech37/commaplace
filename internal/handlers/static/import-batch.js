// Batch-import editor: discard + save-all helpers. Per-item save is HTMX.
(function () {
  document.addEventListener('click', function (e) {
    const discardBtn = e.target.closest('[data-batch-discard]');
    if (discardBtn) {
      const item = discardBtn.closest('.batch-item');
      if (item && confirm('捨棄這篇？輸入的內容會消失。')) {
        item.remove();
      }
      return;
    }

    const saveAllBtn = e.target.closest('[data-batch-save-all]');
    if (saveAllBtn) {
      const pending = document.querySelectorAll('.batch-item[data-status="pending"]');
      if (pending.length === 0) {
        saveAllBtn.textContent = '沒有待儲存的項目';
        return;
      }
      // Fire each pending form via HTMX. window.htmx is loaded via _base.html.
      pending.forEach(function (item) {
        const form = item.querySelector('form.batch-item-form');
        if (form && window.htmx) {
          window.htmx.trigger(form, 'submit');
        } else if (form) {
          form.requestSubmit();
        }
      });
    }
  });
})();
