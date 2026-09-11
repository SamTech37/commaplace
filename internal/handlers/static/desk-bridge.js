/* Same-origin bridge. Existing feed, note and CodeMirror code remains isolated per pane. */
(function () {
  'use strict';
  if (window.parent === window || !document.body.classList.contains('desk-embedded')) return;
  const send = (type, extra) => parent.postMessage({ source: 'comma-pane', type, ...extra }, location.origin);
  let scrollTimer, restoring = false, restored = false, bookmarkTimer, restoreTimer;
  let restoreY = 0, restoreAttempts = 0;
  const scrollY = () => window.cmEditor ? window.cmEditor.codemirror.getScrollInfo().top : window.scrollY;
  function reportScroll() { if (restoring) return; clearTimeout(scrollTimer); scrollTimer = setTimeout(() => send('scroll', { scroll: scrollY() }), 200); }
  const localURL = raw => { try { const u = new URL(raw, location.origin); return u.origin === location.origin ? u.pathname + u.search + u.hash : null; } catch (_) { return null; } };
  const open = url => send('open', { url });
  window.commaDeskOpen = open;
  function stopRestore() { restoring = false; restored = true; clearTimeout(restoreTimer); }
  document.addEventListener('pointerdown', () => { stopRestore(); send('active'); }, { passive: true });
  document.addEventListener('focusin', () => send('active'));
  document.addEventListener('wheel', () => { stopRestore(); send('active'); }, { passive: true });
  document.addEventListener('keydown', stopRestore);
  document.addEventListener('click', e => {
    const a = e.target.closest('a[href]'); if (!a || e.defaultPrevented || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button || a.hasAttribute('download')) return;
    const raw = a.getAttribute('href'); if (!raw || raw.startsWith('#') || a.hasAttribute('hx-get')) return;
    const path = localURL(a.href);
    if (!path) { a.target = '_blank'; a.rel = 'noopener noreferrer'; return; }
    // Auth/settings/export routes continue outside the desk, never inside an editor pane.
    if (/^\/(login|logout|auth|settings|api\/notes\/[^/]+\/(raw|image)|import|me\/avatar)(\/|\?|$)/.test(path) || /^\/write\?/.test(path) || /[?&](view|tab)=/.test(path) && !path.startsWith('/feed')) { a.target = '_top'; return; }
    e.preventDefault(); const menu = a.closest('details'); if (menu) menu.open = false; open(path);
  });
  window.addEventListener('scroll', reportScroll, { passive: true });
  function continueRestore() {
    if (!restoring) return;
    clearTimeout(restoreTimer);
    if (document.querySelector('.editor-page') && !window.cmEditor) { restoreTimer = setTimeout(continueRestore, 100); return; }
    if (window.cmEditor) window.cmEditor.codemirror.scrollTo(null, restoreY);
    else window.scrollTo(0, restoreY);
    // Infinite feeds may need multiple HTMX pages before the old position exists.
    if (Math.abs(scrollY() - restoreY) < 2 || ++restoreAttempts >= 60) stopRestore();
    else restoreTimer = setTimeout(continueRestore, 500);
  }
  function restoreScroll(y) {
    if (restored || restoring || !Number.isFinite(y)) return;
    restoreY = Math.max(0, y); restoring = true;
    requestAnimationFrame(continueRestore);
  }
  window.addEventListener('message', async e => {
    if (e.source !== parent || e.origin !== location.origin || e.data?.source !== 'comma-desk') return;
    const data = e.data;
    if (data.type === 'restore') restoreScroll(data.scroll);
    if (data.type === 'materials') window.commaDeskMaterials = Array.isArray(data.ids) ? data.ids.filter(id => /^[0-9a-f-]{36}$/i.test(id)).slice(0,64) : [];
    if (data.type === 'bookmarks') refreshBookmarks();
    if (data.type === 'flush') { try { if (window.commaDeskSave) await window.commaDeskSave(); send('flushed', { requestID: data.requestID, ok: true }); } catch (_) { send('flushed', { requestID: data.requestID, ok: false }); } }
  });
  const editor = document.querySelector('.editor-page');
  if (editor) {
    new MutationObserver(() => send('dirty', { dirty: editor.dataset.saveState === 'dirty' || editor.dataset.saveState === 'saving' || editor.dataset.saveState === 'error' })).observe(editor, { attributes: true, attributeFilter: ['data-save-state'] });
    const attachEditor = () => { if (!window.cmEditor) return; const cm = window.cmEditor.codemirror; cm.on('scroll', reportScroll); new ResizeObserver(() => cm.refresh()).observe(document.querySelector('.editor-compose')); };
    if (document.readyState === 'complete') attachEditor(); else window.addEventListener('load',attachEditor,{once:true});
    // Note deletion is an explicit content operation. Leave it to the existing page.
    editor.querySelectorAll('form[action^="/delete/"]').forEach(form => form.target = '_top');
  }
  const title = document.querySelector('.article-title'); if (title) send('title', { title: title.textContent });
  document.addEventListener('htmx:afterRequest', e => { if (e.detail.successful && e.detail.requestConfig?.path === '/api/save') send('saved'); });
  async function refreshBookmarks() {
    const controls = [...document.querySelectorAll('.masonry-card[data-note-id],.note-card[data-note-id]')].map(card => ({ id: card.dataset.noteId, button: card.nextElementSibling?.querySelector('[data-bookmark]') }));
    document.querySelectorAll('form[hx-post="/api/save"]').forEach(form => controls.push({ id: form.querySelector('[name="note_id"]').value, button: form.querySelector('.save-btn') }));
    for (let start=0;start<controls.length;start+=50) {
      const chunk = controls.slice(start,start+50), ids=chunk.map(c=>c.id);
      try { const response=await fetch('/api/desk/bookmarks?ids='+encodeURIComponent(ids.join(',')));if(!response.ok)return;const saved=await response.json();
        chunk.forEach(control=>{const b=control.button;if(b){b.disabled=!Object.hasOwn(saved,control.id);b.setAttribute('aria-pressed',String(!!saved[control.id]));b.classList.toggle('saved',!!saved[control.id]);b.textContent=saved[control.id]?'已收藏':'收藏';}});
      } catch (_) { /* Buttons remain disabled until state can be read safely. */ }
    }
  }
  function decorateCards() {
    document.querySelectorAll('.masonry-card[data-note-id],.note-card[data-note-id]').forEach(card => {
      if (card.dataset.deskActions) return; card.dataset.deskActions='true';
      const actions=document.createElement('div');actions.className='desk-card-actions';
      const save=document.createElement('button');save.type='button';save.textContent='收藏';save.dataset.bookmark=card.dataset.noteId;save.disabled=true;
      save.addEventListener('click',async()=>{save.disabled=true;try{const response=await fetch('/api/save',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'note_id='+encodeURIComponent(card.dataset.noteId)});if(!response.ok)throw Error();send('saved');await refreshBookmarks();}catch(_){save.textContent='重試收藏';}finally{save.disabled=false;}});
      actions.append(save);card.after(actions);
    });
    clearTimeout(bookmarkTimer);bookmarkTimer=setTimeout(refreshBookmarks,100);
  }
  decorateCards();
  document.addEventListener('htmx:afterSwap',decorateCards);
  document.addEventListener('htmx:afterSwap',continueRestore);
  // The source document never gets collected or inserted just by becoming visible.
  send('ready');
})();
