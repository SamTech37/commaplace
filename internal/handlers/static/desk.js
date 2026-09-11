(function () {
  'use strict';
  const root = document.getElementById('comma-desk');
  if (!root) return;
  const M = window.CommaDeskModel, board = root.querySelector('.desk-board'), plane = root.querySelector('.desk-plane'), tabs = root.querySelector('.desk-tabs');
  const status = root.querySelector('#desk-status'), dialog = root.querySelector('#desk-add-dialog');
  const retry = root.querySelector('#desk-retry'), reload = root.querySelector('#desk-reload'), overwrite = root.querySelector('#desk-overwrite');
  const panes = new Map(), pending = new Map(), requests = new Map(), frameKeys = new WeakMap(), loaded = new Map();
  const cacheKey = 'comma:desk:' + root.dataset.userId;
  root.inert = true; root.setAttribute('aria-busy', 'true');
  let state = { windows: [], active: '', scrollLeft: 0 }, revision = 0, dirty = false, saving = false, conflict = false, initialized = false;
  let saveTimer, scrollTimer, noteTimer, cacheTimer, searchController, gesture, requestID = 0, jumpTarget = null, modalSearchID = 0;
  let layoutFrame = 0, visibilityFrame = 0, geometry = [], lastUsed = 0, modalTimer, modalController;
  const message = text => { status.textContent = text; };
  function button(text, action) { const b = document.createElement('button'); b.type = 'button'; b.textContent = text; b.addEventListener('click', action); return b; }
  async function api(url, options) {
    const r = await fetch(url, { credentials: 'same-origin', ...options });
    if (!r.ok) { const error = new Error(r.status === 401 ? '登入已過期，請重新登入後再儲存。' : r.status === 409 ? '另一張工作桌已更新，請選擇要保留哪張桌面。' : '操作失敗，請重試。'); error.status = r.status; throw error; }
    return r.json();
  }
  function cache() { try { localStorage.setItem(cacheKey, JSON.stringify({ revision, state: M.snapshot(state), pending: dirty || saving })); } catch (_) {} }
  function changed() { dirty = true; clearTimeout(cacheTimer); cacheTimer = setTimeout(cache, 350); if (!conflict && !saveTimer) saveTimer = setTimeout(save, 1200); }
  function showConflict() { conflict = true; reload.hidden = overwrite.hidden = false; message('另一張工作桌已更新。文章不受影響，請選擇桌面排列。'); }
  async function save() {
    clearTimeout(saveTimer); saveTimer = null;
    if (!initialized || saving || conflict || !dirty) return;
    saving = true; dirty = false; const sent = M.snapshot(state); retry.hidden = true;
    try {
      const result = await api('/api/desk/state', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ revision, state: sent }) });
      revision = result.revision;
      if (!dirty) message('');
    } catch (err) { dirty = true; cache(); if (err.status === 409) showConflict(); else { message(err.message); retry.hidden = false; } }
    finally { saving = false; cache(); if (dirty && !conflict && retry.hidden) saveTimer = setTimeout(save, 1200); }
  }
  function descriptor(p) { const q = new URLSearchParams({ kind: p.kind }); if (p.ref) q.set('ref', p.ref); if (p.query) q.set('q', p.query); return '/me/desk/pane?' + q; }
  function notify(p, type, extra) { const entry = panes.get(M.key(p)); if (entry?.ready) entry.frame.contentWindow?.postMessage({ source: 'comma-desk', type, ...extra }, location.origin); }
  function materials() { const ids = state.windows.filter(p => p.kind === 'note' || p.kind === 'edit').map(p => p.ref); state.windows.forEach(p => notify(p, 'materials', { ids: ids.filter(id => id !== p.ref) })); }
  function activate(key, persist = true) {
    if (!panes.has(key)) return;
    const different = state.active !== key; state.active = key;
    if (!different && persist) return;
    panes.forEach((entry, k) => { entry.el.dataset.active = String(k === key); if (k === key) entry.tab.setAttribute('aria-current', 'page'); else entry.tab.removeAttribute('aria-current'); });
    const tab = panes.get(key).tab;
    if (tab.offsetLeft < tabs.scrollLeft) tabs.scrollLeft = tab.offsetLeft;
    else if (tab.offsetLeft + tab.offsetWidth > tabs.scrollLeft + tabs.clientWidth) tabs.scrollLeft = tab.offsetLeft + tab.offsetWidth - tabs.clientWidth;
    if (different && persist) changed();
  }
  function jump(key) {
    const entry = panes.get(key); if (!entry) return;
    activate(key); jumpTarget = key; board.scrollTo({ left: Math.max(0, entry.el.offsetLeft - 14), behavior: 'instant' }); scheduleVisible();
    clearTimeout(scrollTimer); scrollTimer = setTimeout(() => { jumpTarget = null; }, 300);
  }
  function layout() {
    const viewportWidth = board.getBoundingClientRect().width;
    geometry = M.viewport(state.windows, viewportWidth);
    geometry.forEach((rect, i) => {
      const entry = panes.get(rect.key);
      entry.el.style.left = rect.x + 'px'; entry.el.style.width = rect.width + 'px';
      entry.divider.style.left = (rect.x + rect.width + 1) + 'px'; entry.divider.hidden = board.clientWidth < 560 || i === geometry.length - 1;
      entry.divider.setAttribute('aria-valuenow', String(Math.round(rect.width)));
    });
    const last = geometry.at(-1);
    const extent = last ? last.x + last.width + 14 : 0;
    plane.style.width = extent > viewportWidth + .5 ? extent + 'px' : '100%';
    scheduleVisible();
  }
  function scheduleLayout() { if (!layoutFrame) layoutFrame = requestAnimationFrame(() => { layoutFrame = 0; layout(); }); }
  function scheduleVisible() { if (!visibilityFrame) visibilityFrame = requestAnimationFrame(() => { visibilityFrame = 0; updateVisible(); }); }
  function updateVisible() {
    const left = board.scrollLeft, right = left + board.clientWidth;
    const visible = new Set(geometry.filter(r => r.x + r.width > left && r.x < right).map(r => r.key));
    for (const p of state.windows) {
      const key = M.key(p), entry = panes.get(key);
      if (!visible.has(key)) continue;
      loaded.set(key, ++lastUsed);
      if (!entry.loaded) { entry.loaded = true; entry.frame.src = descriptor(p); }
    }
    const keep = M.retainedKeys(state.windows, visible, loaded);
    panes.forEach((entry, key) => {
      if (!entry.loaded || keep.has(key)) return;
      entry.loaded = entry.ready = false; loaded.delete(key); entry.frame.src = 'about:blank';
    });
    root.querySelector('[data-desk-scroll="-1"]').disabled = left < 1;
    root.querySelector('[data-desk-scroll="1"]').disabled = left + board.clientWidth >= board.scrollWidth - 1;
  }
  function invalidate(p) {
    const entry = panes.get(M.key(p)); if (!entry?.loaded) return;
    entry.ready = false; entry.loaded = false; loaded.delete(M.key(p)); entry.frame.src = 'about:blank'; scheduleVisible();
  }
  function reconcile() {
    const wanted = new Set(state.windows.map(M.key));
    panes.forEach((entry, key) => { if (!wanted.has(key)) { entry.el.remove(); entry.divider.remove(); entry.tab.remove(); loaded.delete(key); panes.delete(key); } });
    state.windows.forEach(p => {
      const key = M.key(p); p.key = key;
      let entry = panes.get(key);
      if (!entry) {
        const el = document.createElement('section'); el.className = 'desk-pane'; el.dataset.key = key; el.dataset.kind = p.kind; el.setAttribute('aria-label', p.title || '文章');
        const frame = document.createElement('iframe'); frame.title = p.title || '文章'; frame.referrerPolicy = 'same-origin'; el.append(frame);
        const tab = document.createElement('span'); tab.className = 'desk-tab'; tab.dataset.key = key;
        const label = button(p.title || '文章', () => jump(key)); label.className = 'desk-tab-label'; label.dataset.dragKey = key; label.setAttribute('aria-description', '點選閱讀；拖曳或使用 Alt 加左右方向鍵重新排序');
        const close = button('×', () => closePane(key)); close.className = 'desk-tab-close'; close.setAttribute('aria-label', '收起 ' + (p.title || '文章')); tab.append(label, close);
        const divider = document.createElement('button'); divider.type = 'button'; divider.className = 'desk-divider'; divider.dataset.resizeKey = key; divider.setAttribute('role', 'separator'); divider.setAttribute('aria-orientation', 'vertical'); divider.setAttribute('aria-label', '調整 ' + (p.title || '文章') + ' 與右側欄位的寬度'); divider.setAttribute('aria-valuemin', '260'); divider.setAttribute('aria-valuemax', '1200');
        entry = { el, frame, tab, label, divider, dirty: false, loaded: false, ready: false }; panes.set(key, entry); plane.append(el, divider); frameKeys.set(frame.contentWindow, key);
        frame.addEventListener('load', () => {
          if (!entry.loaded) return;
          try {
            if (frame.contentWindow.location.href === 'about:blank') return;
            if (!frame.contentDocument?.body.classList.contains('desk-embedded')) { message('部分內容無法載入，請確認登入或文章權限。'); return; }
          } catch (_) { return; }
          const current = state.windows.find(w => M.key(w) === key); if (!current) return;
          entry.ready = true;
          notify(current, 'restore', { scroll: current.scroll || 0 });
          notify(current, 'materials', { ids: state.windows.filter(w => ['note','edit'].includes(w.kind) && w.ref !== current.ref).map(w => w.ref) });
        });
      }
      entry.label.textContent = p.title || '文章'; entry.frame.title = p.title || '文章';
      // Only tabs move in the DOM. Moving an iframe would reload it and lose editor state.
      const at = tabs.children[state.windows.indexOf(p)]; if (at !== entry.tab) tabs.insertBefore(entry.tab, at || null);
    });
    if (!wanted.has(state.active)) state.active = state.windows[0]?.key || '';
    layout(); activate(state.active, false); materials();
  }
  function addPane(p) {
    const key = M.key(p), existing = state.windows.find(x => M.key(x) === key);
    if (existing) { jump(key); if (dialog.open) dialog.close(); return; }
    if (state.windows.length >= 64) { message('工作桌已開啟 64 個內容，請先收起一些索引。'); return; }
    state.windows.push(p); state.active = key; reconcile(); changed(); jump(key); if (dialog.open) dialog.close();
  }
  async function openURL(raw) {
    if (!initialized) return;
    const path = M.safeURL(raw, location.origin); if (!path) { try { const url = new URL(raw, location.origin); if (['http:', 'https:'].includes(url.protocol)) window.open(url.href, '_blank', 'noopener,noreferrer'); } catch (_) {} return; }
    if (path === '/write') return createDraft();
    if (path === '/random') { try { const r = await fetch(path); if (!r.ok) throw Error(); return openURL(r.url); } catch (_) { message('暫時無法漫遊，請重試。'); return; } }
    if (pending.has(path)) return pending.get(path);
    const request = api('/api/desk/resolve?url=' + encodeURIComponent(path)).then(addPane).catch(err => message(err.message)).finally(() => pending.delete(path));
    pending.set(path, request); return request;
  }
  let creating = false;
  window.commaDeskOpen = openURL;
  async function createDraft() { if (!initialized || creating) return; if (state.windows.length >= 64) { message('請先收起一些索引。'); return; } creating = true; try { addPane(await api('/api/desk/drafts', { method: 'POST' })); } catch (err) { message(err.message); } finally { creating = false; } }
  function flush(key) {
    const entry = panes.get(key); if (!entry || entry.el.dataset.kind !== 'edit') return Promise.resolve();
    const editor = entry.frame.contentDocument?.querySelector('.editor-page');
    if (!entry.dirty && (!editor || !['dirty','saving','error'].includes(editor.dataset.saveState))) return Promise.resolve();
    return new Promise((resolve, reject) => { const id = ++requestID; const timeout = setTimeout(() => { requests.delete(id); reject(new Error('文章尚未儲存成功，請稍後再收起。')); }, 12000); requests.set(id, { key, resolve, reject, timeout }); entry.frame.contentWindow.postMessage({ source: 'comma-desk', type: 'flush', requestID: id }, location.origin); });
  }
  async function closePane(key) {
    try { await flush(key); const index = state.windows.findIndex(p => M.key(p) === key); state.windows = state.windows.filter(p => M.key(p) !== key); if (state.active === key) state.active = M.key(state.windows[Math.min(index, state.windows.length - 1)] || {kind:''}); reconcile(); if (state.active) jump(state.active); else { board.scrollLeft = state.scrollLeft = 0; } changed(); }
    catch (err) { message(err.message); }
  }
  async function useCloud() {
    try { await Promise.all([...panes.keys()].map(flush)); const env = await api('/api/desk/state'); state = env.state; revision = env.revision; conflict = dirty = false; reload.hidden = overwrite.hidden = true; reconcile(); board.scrollLeft = state.scrollLeft; cache(); message(''); }
    catch (err) { message(err.message); }
  }
  reload.addEventListener('click', useCloud);
  overwrite.addEventListener('click', async () => { try { const env = await api('/api/desk/state'); revision = env.revision; conflict = false; dirty = true; reload.hidden = overwrite.hidden = true; await save(); } catch (err) { message(err.message); } });
  retry.addEventListener('click', save);
  let notesOffset = 0;
  async function loadNotes(append = false) {
    if (searchController) searchController.abort(); searchController = new AbortController();
    if (!append) notesOffset = 0;
    try { const data = await api('/api/desk/notes?q=' + encodeURIComponent(root.querySelector('#desk-note-search').value) + '&offset=' + notesOffset, { signal: searchController.signal });
      const target = root.querySelector('#desk-my-notes'); if (!append) target.replaceChildren();
      data.notes.forEach(n => { target.append(button(n.title, () => openURL(n.url))); }); notesOffset += data.notes.length; root.querySelector('#desk-more-notes').hidden = !data.more;
    } catch (err) { if (err.name !== 'AbortError') message(err.message); }
  }
  const noteSearch = root.querySelector('#desk-note-search'), searchToggle = root.querySelector('#desk-search-toggle');
  function closeNoteSearch() {
    noteSearch.hidden = true; searchToggle.setAttribute('aria-expanded', 'false');
    clearTimeout(noteTimer);
    if (noteSearch.value) { noteSearch.value = ''; loadNotes(); }
    searchToggle.focus();
  }
  searchToggle.addEventListener('click', () => {
    if (!noteSearch.hidden) { closeNoteSearch(); return; }
    noteSearch.hidden = false; searchToggle.setAttribute('aria-expanded', 'true'); noteSearch.focus();
  });
  noteSearch.addEventListener('keydown', e => { if (e.key === 'Escape') { e.preventDefault(); closeNoteSearch(); } });
  noteSearch.addEventListener('input', () => { clearTimeout(noteTimer); noteTimer = setTimeout(() => loadNotes(), 200); });
  root.querySelector('#desk-more-notes').addEventListener('click', () => loadNotes(true));
  async function modalNotes() { const id = ++modalSearchID; modalController?.abort(); modalController = new AbortController(); try { const data = await api('/api/desk/notes?q=' + encodeURIComponent(root.querySelector('#desk-open-search').value), {signal: modalController.signal}); if (id !== modalSearchID) return; const target = root.querySelector('#desk-open-results'); target.replaceChildren(); data.notes.forEach(n => target.append(button(n.title, () => openURL(n.url)))); } catch (err) { if (err.name !== 'AbortError') message(err.message); } }
  root.querySelector('#desk-open-search').addEventListener('input', () => { clearTimeout(modalTimer); modalTimer = setTimeout(modalNotes, 200); });
  root.querySelector('#desk-add').addEventListener('click', () => { dialog.showModal(); modalNotes(); });
  root.querySelector('[data-desk-dismiss]').addEventListener('click', () => dialog.close());
  root.addEventListener('click', e => { const link = e.target.closest('[data-desk-url]'); if (link) openURL(link.dataset.deskUrl); if (e.target.closest('[data-desk-new]')) createDraft(); const arrow = e.target.closest('[data-desk-scroll]'); if (arrow) { jumpTarget = null; board.scrollBy({ left: Number(arrow.dataset.deskScroll) * Math.max(300, board.clientWidth * .7), behavior: 'smooth' }); } });
  document.querySelector('.topbar').addEventListener('click', e => { const a = e.target.closest('a'); if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.button) return; if (['/write','/feed','/graph','/random'].includes(a.getAttribute('href'))) { e.preventDefault(); openURL(a.href); } });
  document.querySelector('.search-form').addEventListener('submit', e => { e.preventDefault(); openURL('/search?q=' + encodeURIComponent(e.target.querySelector('[name=q]').value)); });
  board.addEventListener('wheel', () => { jumpTarget = null; }, { passive: true });
  board.addEventListener('scroll', () => { scheduleVisible(); if (Math.abs(state.scrollLeft - board.scrollLeft) < 1) return; state.scrollLeft = board.scrollLeft; clearTimeout(scrollTimer); scrollTimer = setTimeout(() => {
    if (!gesture && !jumpTarget) { const r = board.getBoundingClientRect(); let best = null, score = -1; panes.forEach((entry, key) => { const p = entry.el.getBoundingClientRect(), visible = Math.max(0, Math.min(p.right,r.right)-Math.max(p.left,r.left)), value = visible/Math.min(p.width,r.width) - Math.abs((p.left+p.right)/2-(r.left+r.right)/2)/r.width*.15; if (value>score) { score=value; best=key; } }); if (best) activate(best); }
    jumpTarget = null; changed();
  }, 160); }, { passive: true });
  root.addEventListener('pointerdown', e => {
    const drag = e.target.closest('[data-drag-key]'), resize = e.target.closest('[data-resize-key]'); if (e.button || (!drag&&!resize)) return;
    e.preventDefault(); const key = drag?.dataset.dragKey || resize.dataset.resizeKey; activate(key);
    if (resize) geometry.forEach((r,i) => { state.windows[i].width = Math.max(260, Math.min(1200, r.width)); });
    gesture = { id: e.pointerId, key, resize: !!resize, x: e.clientX, y: e.clientY, moved: false, target: null, original: state.windows.map(p=>({...p})) };
    root.setPointerCapture(e.pointerId); root.dataset.dragging = 'true';
  });
  root.addEventListener('pointermove', e => {
    if (!gesture || gesture.id !== e.pointerId) return;
    gesture.moved ||= Math.hypot(e.clientX-gesture.x,e.clientY-gesture.y)>6;
    if (gesture.resize) { state.windows = gesture.original.map(p=>({...p})); M.resize(state.windows,state.windows.findIndex(p=>M.key(p)===gesture.key),e.clientX-gesture.x); scheduleLayout(); }
    else if (gesture.moved) { const tab = document.elementFromPoint(e.clientX,e.clientY)?.closest('.desk-tab'); gesture.target = tab?.dataset.key; panes.forEach((entry,key)=>{entry.tab.dataset.drop=String(key===gesture.target&&key!==gesture.key);}); }
  });
  function finish(cancelled) {
    if (!gesture) return; const g = gesture; gesture = null; root.dataset.dragging='false'; if (root.hasPointerCapture(g.id)) root.releasePointerCapture(g.id);
    if (cancelled) state.windows=g.original;
    else if (!g.resize && g.moved && g.target) M.move(state.windows,state.windows.findIndex(p=>M.key(p)===g.key),state.windows.findIndex(p=>M.key(p)===g.target));
    panes.forEach(e=>delete e.tab.dataset.drop); reconcile(); if (!cancelled) { changed(); if (!g.moved) jump(g.key); }
  }
  root.addEventListener('pointerup',()=>finish(false));root.addEventListener('pointercancel',()=>finish(true));root.addEventListener('lostpointercapture',()=>finish(true));
  root.addEventListener('keydown',e=>{ if(e.key==='Escape'&&gesture){finish(true);return;} const tab=e.target.closest('[data-drag-key]'),divider=e.target.closest('[data-resize-key]');if(!['ArrowLeft','ArrowRight'].includes(e.key)||(!divider&&(!tab||!e.altKey)))return;e.preventDefault();const key=tab?.dataset.dragKey||divider.dataset.resizeKey,i=state.windows.findIndex(p=>M.key(p)===key),d=e.key==='ArrowLeft'?-1:1;if(divider){geometry.forEach((r,j)=>{state.windows[j].width=Math.max(260,Math.min(1200,r.width));});M.resize(state.windows,i,d*(e.shiftKey?40:10));}else M.move(state.windows,i,i+d);reconcile();(divider?panes.get(key).divider:panes.get(key).label).focus();changed();});
  window.addEventListener('message', e => {
    if (e.origin !== location.origin || e.data?.source !== 'comma-pane') return;
    const key = frameKeys.get(e.source), entry = panes.get(key); if (!entry?.loaded) return;
    const p=state.windows.find(p=>M.key(p)===key),data=e.data;
    if (!p) return;
    if(data.type==='active')activate(key);
    if(data.type==='open'&&typeof data.url==='string')openURL(data.url);
    if(data.type==='scroll'&&Number.isFinite(data.scroll)){const y=Math.max(0,Math.min(10000000,data.scroll));if(p.scroll!==y){p.scroll=y;changed();}}
    if(data.type==='dirty'){entry.dirty=!!data.dirty;}
    if(data.type==='title'&&typeof data.title==='string'){p.title=data.title.slice(0,300)||'未命名文章';entry.label.textContent=p.title;entry.frame.title=p.title;entry.el.setAttribute('aria-label',p.title);entry.tab.querySelector('.desk-tab-close').setAttribute('aria-label','收起 '+p.title);entry.divider.setAttribute('aria-label','調整 '+p.title+' 與右側欄位的寬度');}
    if(data.type==='flushed'){const request=requests.get(data.requestID);if(request&&request.key===key){clearTimeout(request.timeout);requests.delete(data.requestID);if(data.ok){entry.dirty=false;request.resolve();}else request.reject(new Error('文章儲存失敗，已保留在桌面。'));}}
    if(data.type==='ready'){entry.ready=true;notify(p,'restore',{scroll:p.scroll||0});notify(p,'materials',{ids:state.windows.filter(w=>['note','edit'].includes(w.kind)&&w.ref!==p.ref).map(w=>w.ref)});}
    if(data.type==='saved'){state.windows.forEach(other=>{if(other.kind==='saved')invalidate(other);else notify(other,'bookmarks');});}
    if(data.type==='note-saved'){state.windows.filter(other=>other.kind==='note'&&other.ref===p.ref).forEach(invalidate);}
    if(data.type==='published'&&typeof data.url==='string'){loadNotes();}
  });
  new ResizeObserver(scheduleLayout).observe(board);
  window.addEventListener('beforeunload',e=>{if([...panes.values()].some(p=>p.dirty)){e.preventDefault();e.returnValue='';}});
  window.addEventListener('pagehide',()=>{cache();if(dirty&&!saving&&!conflict)fetch('/api/desk/state',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({revision,state:M.snapshot(state)}),keepalive:true}).catch(()=>{});});
  window.addEventListener('online',()=>{if(dirty&&!conflict)save();});
  loadNotes();
  (async()=>{
    try{const env=await api('/api/desk/state');state=env.state;revision=env.revision;initialized=true;reconcile();board.scrollLeft=state.scrollLeft||0;
      try{const local=JSON.parse(localStorage.getItem(cacheKey)||'null');if(local?.pending&&Array.isArray(local.state?.windows)&&JSON.stringify(M.snapshot(local.state))!==JSON.stringify(M.snapshot(state))){
        message('這台裝置有尚未同步的桌面排列。');
        const restore=button('復原本機排列',async()=>{try{await Promise.all([...panes.keys()].map(flush));state=local.state;dirty=true;await save();if(!conflict&&!dirty){await useCloud();restore.remove();}}catch(err){message(err.message);}});
        root.querySelector('.desk-feedback').append(restore);
      }}
      catch(_){}
    }catch(err){message(err.message);retry.hidden=false;retry.textContent='重新載入';retry.onclick=()=>location.reload();}
    finally { root.inert = false; root.removeAttribute('aria-busy'); }
  })();
})();
