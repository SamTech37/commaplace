// Each pane loads expensive renderers only when its content needs them.
(function () {
  'use strict';
  const config = document.currentScript.dataset, scripts = new Map();
  function script(url) {
    if (!scripts.has(url)) scripts.set(url, new Promise((resolve, reject) => {
      const el = document.createElement('script'); el.src = url; el.async = true;
      if (new URL(url, location.href).origin !== location.origin) el.crossOrigin = 'anonymous';
      el.onload = resolve; el.onerror = () => { scripts.delete(url); el.remove(); reject(new Error('renderer unavailable')); };
      document.head.append(el);
    }));
    return scripts.get(url);
  }
  let mathJob, diagramJob, graphJob;
  function render() {
    if (!mathJob && document.querySelector('.math-inline:not([data-rendered]),.math-block:not([data-rendered])')) {
      if (!document.querySelector('[data-desk-math-css]')) {
        const link = document.createElement('link'); link.rel = 'stylesheet'; link.href = 'https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.css'; link.crossOrigin = 'anonymous'; link.dataset.deskMathCss = ''; document.head.append(link);
      }
      mathJob = script('https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.js').then(() => {
        document.querySelectorAll('.math-inline:not([data-rendered]),.math-block:not([data-rendered])').forEach(el => {
          const block = el.classList.contains('math-block'), source = block ? el.textContent : el.textContent.replace(/^\$|\$$/g, '');
          window.katex.render(source, el, {displayMode: block, throwOnError: false}); el.dataset.rendered = 'true';
        });
      }).catch(() => {}).finally(() => { mathJob = null; });
    }
    if (!diagramJob && document.querySelector('.mermaid:not([data-processed])')) {
      diagramJob = script('https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js').then(async () => {
        window.mermaid.initialize({startOnLoad: false, securityLevel: 'strict', theme: document.documentElement.dataset.theme === 'light' ? 'default' : 'dark'});
        await window.mermaid.run({nodes: document.querySelectorAll('.mermaid:not([data-processed])')});
      }).catch(() => {}).finally(() => { diagramJob = null; });
    }
    if (!graphJob && document.querySelector('[data-graph-source],[data-lazy-graph]')) {
      graphJob = script(config.forceSrc).then(() => script(config.graphSrc)).catch(() => { graphJob = null; });
    }
  }
  render();
  document.addEventListener('htmx:afterSwap', render);
})();
