(() => {
  const form = document.getElementById('note-manager');
  if (!form || form.dataset.initialized) return;
  form.dataset.initialized = 'true';
  const toggle = document.getElementById('note-manager-toggle');
  const all = form.querySelector('#select-all-notes');
  const count = form.querySelector('#note-selection-count');
  const boxes = () => [...form.querySelectorAll('input[name="ids"]')];
  const update = () => {
    const selected = boxes().filter(box => box.checked).length;
    count.textContent = all.checked ? '已選取整個篩選結果（含未載入）' : `已選 ${selected} 篇`;
    form.querySelectorAll('[data-bulk]').forEach(button => { button.disabled = !all.checked && selected === 0; });
  };
  toggle.addEventListener('click', () => {
    const editing = form.dataset.editing !== 'true';
    form.dataset.editing = String(editing);
    toggle.setAttribute('aria-expanded', String(editing));
    toggle.textContent = editing ? '完成編輯' : '編輯文章列';
    if (!editing) {
      all.checked = false;
      boxes().forEach(box => { box.checked = false; });
      update();
    }
  });
  form.addEventListener('click', event => {
    const button = event.target.closest('[data-select]');
    if (!button) return;
    all.checked = false;
    boxes().forEach(box => { box.checked = button.dataset.select === 'loaded'; });
    update();
  });
  form.addEventListener('change', event => {
    if (event.target === all) boxes().forEach(box => { box.checked = all.checked; });
    else if (event.target.name === 'ids') all.checked = false;
    update();
  });
  const notesList = form.querySelector('#notes-list');
  if (notesList) new MutationObserver(() => {
    if (all.checked) boxes().forEach(box => { box.checked = true; });
    update();
  }).observe(notesList, { childList: true, subtree: true });
  form.addEventListener('submit', event => {
    const button = event.submitter;
    if (!button || form.dataset.submitting || form.dataset.editing !== 'true') { event.preventDefault(); return; }
    const single = button.name === 'single';
    const action = single ? button.value.split(':')[0] : button.value;
    const selected = boxes().filter(box => box.checked).length;
    if (!single && !all.checked && !selected) { event.preventDefault(); return; }
    const scope = single ? '這篇文章' : all.checked ? '整個篩選結果的所有文章（包含尚未載入的文章）' : `所選 ${selected} 篇文章`;
    if ((action === 'delete' || (!single && all.checked)) && !window.confirm(
      action === 'delete' ? `確定刪除${scope}？刪除後會從列表移除。` : `確定將${scope}設為${action === 'publish' ? '公開' : '隱藏'}？`
    )) { event.preventDefault(); return; }
    form.dataset.submitting = 'true';
    form.setAttribute('aria-busy', 'true');
  });
  window.addEventListener('pageshow', () => { delete form.dataset.submitting; form.removeAttribute('aria-busy'); update(); });
  update();
})();
