// Run with node --test scripts/vault-import-ui.test.cjs. No browser or packages.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const { webcrypto } = require('node:crypto');

class Element {
  constructor() { this.children = []; this.listeners = {}; this.value = ''; this.files = []; }
  addEventListener(name, fn) { this.listeners[name] = fn; }
  async fire(name) { await this.listeners[name]?.({}); }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.children = children.flatMap(c => c.fragment ? c.children : [c]); }
  setAttribute(key, value) { this[key] = value; }
  click() {}
  focus() {}
  showModal() {}
  close() {}
}
function setup(blockStorage = false) {
  const elements = new Map(), requests = [], storage = new Map(), transports = [];
  const el = name => {
    if (!elements.has(name)) elements.set(name, new Element());
    return elements.get(name);
  };
  class FormData { constructor() { this.parts = []; } append(...args) { this.parts.push(args); } }
  class XHR extends Element {
    constructor() { super(); this.upload = new Element(); transports.push(this); }
    open(method, url) { this.url = url; }
    send(body) { requests.push(body.parts); }
  }
  const context = {
    document: {
      getElementById: id => el(id.replace('vault-import-', '')),
      createElement: () => new Element(),
      createDocumentFragment: () => Object.assign(new Element(), { fragment: true }),
      createTextNode: text => ({ textContent: text }),
    },
    window: new Element(), crypto: webcrypto, TextEncoder, Uint8Array,
    FormData, XMLHttpRequest: XHR, setTimeout,
    sessionStorage: { getItem: key => { if (blockStorage) throw Error('blocked'); return storage.get(key); }, setItem: (key, value) => { if (blockStorage) throw Error('blocked'); storage.set(key, value); }, removeItem: key => storage.delete(key) },
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../internal/handlers/static/vault-import.js'), 'utf8'), context);
  return { el, requests, transports };
}
function file(name, relative = '', body = 'body') {
  return { name, webkitRelativePath: relative, size: body.length, lastModified: 1, slice: () => ({ text: async () => body }) };
}
function rowFor(el, text) { return el('candidates').children.find(row => row.children[0].children[1].textContent === text); }
async function choose(el, input, files) { el(input).files = files; await el(input).fire('change'); }
async function check(el, name, value) { const box = rowFor(el, name).children[0].children[0]; box.checked = value; await box.fire('change'); }

test('folder review and raw preview never upload; unchecked secret never enters multipart', async () => {
  const { el, requests } = setup();
  const secret = file('secret.md', 'vault/private/secret.md', '<img src="https://example.invalid/tracker">SECRET');
  await choose(el, 'files', [file('public.md', 'vault/public.md'), secret]);
  assert.equal(el('start').disabled, true);
  assert.equal(requests.length, 0);
  await rowFor(el, 'private/secret.md').children[1].fire('click');
  await new Promise(resolve => setImmediate(resolve));
  assert.match(el('preview-text').textContent, /SECRET/);
  assert.equal(el('preview-text').children.length, 0); // raw text, no HTML/image execution
  assert.equal(requests.length, 0);
  await el('select').fire('click');
  await check(el, 'private/secret.md', false);
  await el('start').fire('click');
  assert.equal(requests.length, 1);
  assert.equal(requests[0].length, 1);
  assert.equal(requests[0][0][0], 'public.md');
  assert.notEqual(requests[0][0][1], secret);
});

test('individual Markdown files upload with filenames and require explicit checks', async () => {
  const { el, requests } = setup();
  await choose(el, 'individual', [file('chosen.md'), file('private.md')]);
  assert.equal(el('start').disabled, true);
  await check(el, 'chosen.md', true);
  await el('start').fire('click');
  assert.equal(requests[0].length, 1);
  assert.equal(requests[0][0][0], 'chosen.md');
});

test('empty, whitespace and unreadable previews always explain their state without uploading', async () => {
  const { el, requests } = setup();
  const broken = file('broken.md');
  broken.slice = () => ({ text: async () => { throw Error('unavailable'); } });
  const missing = file('missing.md', '', '');
  missing.size = 200;
  await choose(el, 'individual', [file('empty.md', '', ''), file('space.md', '', '\n  \t'), broken, missing]);
  for (const [name, expected] of [['empty.md', /空的（0 B）/], ['space.md', /只有空白或換行/], ['broken.md', /無法讀取此檔案/], ['missing.md', /未能讀到檔案內容/]]) {
    await rowFor(el, name).children[1].fire('click');
    await new Promise(resolve => setImmediate(resolve));
    assert.match(el('preview-text').textContent, expected);
    assert.match(el('preview-info').textContent, /檔案大小：/);
  }
  assert.equal(requests.length, 0);
  assert.equal(el('start').disabled, true);
});

test('slow preview cannot overwrite a newer preview or reappear after closing', async () => {
  const { el } = setup();
  let resolveSlow;
  const slow = file('slow.md');
  slow.slice = () => ({ text: () => new Promise(resolve => { resolveSlow = resolve; }) });
  await choose(el, 'individual', [slow, file('new.md', '', 'New content')]);
  await rowFor(el, 'slow.md').children[1].fire('click');
  await rowFor(el, 'new.md').children[1].fire('click');
  resolveSlow('Old content');
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(el('preview-text').textContent, 'New content');
  await rowFor(el, 'slow.md').children[1].fire('click');
  await el('preview-close').fire('click');
  resolveSlow('Old content');
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(el('preview').hidden, true);
  assert.equal(el('preview-text').textContent, '');
});

test('filter selection is explicit across pages; replacement resets checks and preview', async () => {
  const { el, requests } = setup();
  const files = Array.from({length: 201}, (_,i) => file(`n${i}.md`, `vault/public/n${i}.md`));
  files.push(file('secret.md', 'vault/private/secret.md'));
  await choose(el, 'files', files);
  assert.equal(el('candidates').children.length, 100);
  el('filter').value = 'public/'; await el('filter').fire('input'); await el('select').fire('click');
  assert.match(el('selection').textContent, /201 \/ 202/);
  await el('clear').fire('click'); assert.equal(el('start').disabled, true);
  await choose(el, 'individual', [file('replacement.md')]);
  assert.match(el('selection').textContent, /0 \/ 1/);
  assert.equal(el('preview').hidden, true);
  assert.equal(requests.length, 0);
});

test('duplicate plain filenames cannot silently collapse selected files', async () => {
  const { el, requests } = setup();
  await choose(el, 'individual', [file('same.md'), file('same.md')]);
  await el('select').fire('click');
  assert.equal(el('start').disabled, true);
  await el('start').fire('click');
  assert.equal(requests.length, 0);
});

test('retry preserves batch without storage; changing selected files gets a new batch', async () => {
  const { el, requests, transports } = setup(true);
  await choose(el, 'individual', [file('one.md'), file('two.md')]);
  await check(el, 'one.md', true);
  await el('start').fire('click');
  await transports[0].fire('loadend');
  await el('start').fire('click');
  assert.equal(transports[0].url, transports[1].url);
  await transports[1].fire('loadend');
  await check(el, 'two.md', true);
  await el('start').fire('click');
  assert.notEqual(transports[1].url, transports[2].url);
  assert.equal(requests[2].length, 2);
});
