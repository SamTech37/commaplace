const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../internal/handlers/static/reveal.js'), 'utf8');

function setup(embedded = false) {
  const handlers = {}, observers = [], frames = [], classes = new Set();
  const el = {
    style: {}, offsetHeight: 100,
    classList: { add: c => classes.add(c), contains: c => classes.has(c) },
    addEventListener() {}, removeEventListener() {},
    hasAttribute: () => true, setAttribute() {},
  };
  class Observer {
    constructor(callback) { this.callback = callback; this.targets = new Set(); observers.push(this); }
    observe(target) { this.targets.add(target); }
    unobserve(target) { this.targets.delete(target); }
    enter() { this.callback([...this.targets].map(target => ({ target, isIntersecting: true })), this); }
  }
  const content = { style: {} };
  const document = {
    body: { classList: { contains: c => embedded && c === 'desk-embedded' } },
    addEventListener: (type, fn) => (handlers[type] ||= []).push(fn),
    querySelectorAll: selector => selector === '[data-reveal]' ? [el] : [],
    querySelector: () => content,
  };
  vm.runInNewContext(source, { document, IntersectionObserver: Observer, requestAnimationFrame: fn => frames.push(fn) });
  const fire = (type, event = {}) => (handlers[type] || []).forEach(fn => fn(event));
  return { el, content, observers, frames, fire };
}

test('a revealed element leaves its original observer and does not replay when re-entering', () => {
  const s = setup();
  s.fire('DOMContentLoaded');
  const observer = s.observers.find(o => o.targets.has(s.el));
  observer.enter();
  assert.equal(observer.targets.has(s.el), false, 'detach before clipping can alter intersection');
  s.frames.splice(0).forEach(fn => fn());
  observer.enter();
  assert.equal(s.frames.length, 0, 'scrolling back must not schedule another reveal');
});

test('desk panes register neither clipping observers nor whole-content HTMX fades', () => {
  const s = setup(true);
  s.fire('DOMContentLoaded');
  s.fire('htmx:beforeSwap');
  s.fire('htmx:afterSwap', { detail: { target: { closest: () => null, querySelectorAll: () => [s.el] } } });
  assert.equal(s.observers.length, 0);
  assert.equal(s.frames.length, 0);
  assert.equal(s.el.style.clipPath, undefined);
  assert.equal(s.content.style.opacity, undefined);
});
