// Force-directed graph engine, reused by:
//   - /graph (global)            <div data-graph-source="/api/graph" ...>
//   - per-note pages (local)     <div data-graph-source="/api/graph/local?note=..." ...>
//
// Container HTML expected:
//   <div data-graph-source="…" data-graph-height="420">
//     <canvas></canvas>
//     <div class="graph-empty" hidden></div>     (optional)
//     <div class="graph-tooltip" hidden></div>   (optional)
//   </div>
(function () {
  const palette = ['#fb7185','#fbbf24','#a78bfa','#34d399','#60a5fa','#f472b6','#94a3b8','#fb923c'];
  function hashStr(s) { let h = 2166136261; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = (h * 16777619) >>> 0; } return h; }
  function colorFor(author) { return palette[hashStr(author || '') % palette.length]; }

  function initGraph(wrap) {
    const url = wrap.dataset.graphSource;
    if (!url) return;
    const canvas = wrap.querySelector('canvas');
    if (!canvas) return;
    const empty = wrap.querySelector('.graph-empty');
    const tooltip = wrap.querySelector('.graph-tooltip');
    const ctx = canvas.getContext('2d');

    const fixedHeight = parseInt(wrap.dataset.graphHeight || '0', 10) || 0;
    const isCompact = !!fixedHeight;

    let dpr = window.devicePixelRatio || 1;
    function resize() {
      const w = wrap.clientWidth;
      const h = fixedHeight || Math.max(420, Math.round(window.innerHeight * 0.72));
      canvas.style.width = w + 'px';
      canvas.style.height = h + 'px';
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    }
    window.addEventListener('resize', resize);
    resize();

    // ---- state ----
    let nodes = [];
    let edges = [];
    let nodeById = new Map();
    let hovered = null;
    let dragging = null;
    let centerNode = null;

    // ---- fetch & init ----
    fetch(url).then(r => r.json()).then(data => {
      if (!data.nodes || data.nodes.length === 0) {
        if (empty) { empty.hidden = false; }
        canvas.hidden = true;
        return;
      }
      const cw = canvas.clientWidth, ch = canvas.clientHeight;
      nodes = data.nodes.map(n => ({
        id: n.id,
        title: n.title,
        url: n.url,
        author: n.author,
        isExternal: !!n.ext,
        x: cw / 2 + (Math.random() - 0.5) * cw * 0.6,
        y: ch / 2 + (Math.random() - 0.5) * ch * 0.6,
        vx: 0, vy: 0,
        degree: 0,
        color: colorFor(n.author),
      }));
      nodeById = new Map(nodes.map(n => [n.id, n]));
      edges = (data.edges || []).map(e => {
        const a = nodeById.get(e.s), b = nodeById.get(e.t);
        if (!a || !b) return null;
        a.degree++; b.degree++;
        return { a, b };
      }).filter(Boolean);
      for (const n of nodes) n.r = (isCompact ? 4 : 3.5) + Math.min(8, Math.sqrt(n.degree) * 1.6);
      if (data.center) {
        centerNode = nodeById.get(data.center) || null;
        if (centerNode) {
          centerNode.r += 2;
          // pin center at middle initially
          centerNode.x = cw / 2;
          centerNode.y = ch / 2;
        }
      }
      requestAnimationFrame(tick);
    }).catch(err => {
      if (empty) { empty.hidden = false; empty.textContent = '載入失敗：' + err; }
      canvas.hidden = true;
    });

    // ---- simulation ----
    const REPULSION = isCompact ? 800 : 1400;
    const SPRING_LEN = isCompact ? 45 : 60;
    const SPRING_K = 0.02;
    const CENTER_K = isCompact ? 0.01 : 0.005;
    const FRICTION = 0.85;
    let alpha = 1.0;

    function step() {
      if (alpha < 0.005 && !dragging) return;
      const cw = canvas.clientWidth, ch = canvas.clientHeight;
      const cx = cw / 2, cy = ch / 2;

      for (let i = 0; i < nodes.length; i++) {
        const a = nodes[i];
        for (let j = i + 1; j < nodes.length; j++) {
          const b = nodes[j];
          let dx = a.x - b.x, dy = a.y - b.y;
          let d2 = dx * dx + dy * dy;
          if (d2 < 0.01) { dx = Math.random() - 0.5; dy = Math.random() - 0.5; d2 = 1; }
          const d = Math.sqrt(d2);
          const f = (REPULSION * alpha) / d2;
          const fx = (dx / d) * f, fy = (dy / d) * f;
          a.vx += fx; a.vy += fy;
          b.vx -= fx; b.vy -= fy;
        }
      }
      for (const e of edges) {
        const dx = e.b.x - e.a.x, dy = e.b.y - e.a.y;
        const d = Math.sqrt(dx * dx + dy * dy) || 0.01;
        const diff = d - SPRING_LEN;
        const f = SPRING_K * diff;
        const fx = (dx / d) * f, fy = (dy / d) * f;
        e.a.vx += fx; e.a.vy += fy;
        e.b.vx -= fx; e.b.vy -= fy;
      }
      for (const n of nodes) {
        const k = (n === centerNode) ? CENTER_K * 6 : CENTER_K;
        n.vx += (cx - n.x) * k;
        n.vy += (cy - n.y) * k;
      }
      for (const n of nodes) {
        if (n === dragging) { n.vx = 0; n.vy = 0; continue; }
        n.vx *= FRICTION; n.vy *= FRICTION;
        n.x += n.vx; n.y += n.vy;
      }
      alpha *= 0.995;
    }

    function draw() {
      const cw = canvas.clientWidth, ch = canvas.clientHeight;
      ctx.clearRect(0, 0, cw, ch);

      ctx.strokeStyle = 'rgba(200,200,200,0.18)';
      ctx.lineWidth = 1;
      ctx.beginPath();
      for (const e of edges) {
        ctx.moveTo(e.a.x, e.a.y);
        ctx.lineTo(e.b.x, e.b.y);
      }
      ctx.stroke();

      for (const n of nodes) {
        ctx.beginPath();
        ctx.arc(n.x, n.y, n.r, 0, Math.PI * 2);
        if (n.isExternal) {
          ctx.fillStyle = '#1c1b19';
          ctx.fill();
          ctx.lineWidth = 1.5;
          ctx.strokeStyle = n.color;
          ctx.stroke();
        } else {
          ctx.fillStyle = n.color;
          ctx.fill();
        }
        if (n === centerNode) {
          ctx.lineWidth = 2;
          ctx.strokeStyle = '#fff';
          ctx.stroke();
        }
        if (n === hovered || n === dragging) {
          ctx.lineWidth = 2;
          ctx.strokeStyle = '#fff';
          ctx.stroke();
        }
      }

      ctx.fillStyle = '#ececec';
      ctx.font = (isCompact ? '11px' : '12px') + ' system-ui, sans-serif';
      ctx.textBaseline = 'middle';
      for (const n of nodes) {
        // labels: hover always, plus high-degree (global) / center+neighbors (local)
        const labelMe = (n === hovered) ||
                       (isCompact && (n === centerNode || nodes.length <= 12)) ||
                       (!isCompact && n.degree >= 4);
        if (labelMe) {
          ctx.fillText(n.title, n.x + n.r + 4, n.y);
        }
      }
    }

    function tick() { step(); draw(); requestAnimationFrame(tick); }

    // ---- pointer interaction ----
    function pointerPos(e) {
      const rect = canvas.getBoundingClientRect();
      return { x: e.clientX - rect.left, y: e.clientY - rect.top };
    }
    function nodeAt(p) {
      for (let i = nodes.length - 1; i >= 0; i--) {
        const n = nodes[i];
        const dx = p.x - n.x, dy = p.y - n.y;
        if (dx * dx + dy * dy <= (n.r + 4) * (n.r + 4)) return n;
      }
      return null;
    }
    canvas.addEventListener('mousemove', (e) => {
      const p = pointerPos(e);
      if (dragging) {
        dragging.x = p.x; dragging.y = p.y;
        alpha = Math.max(alpha, 0.5);
        return;
      }
      const h = nodeAt(p);
      if (h !== hovered) {
        hovered = h;
        if (h && tooltip) {
          tooltip.hidden = false;
          const prefix = h.isExternal ? '外 · ' : '@';
          tooltip.textContent = h.title + '  ' + prefix + h.author;
          tooltip.style.left = (p.x + 14) + 'px';
          tooltip.style.top = (p.y + 14) + 'px';
          canvas.style.cursor = 'pointer';
        } else if (tooltip) {
          tooltip.hidden = true;
          canvas.style.cursor = 'default';
        }
      } else if (h && tooltip) {
        tooltip.style.left = (p.x + 14) + 'px';
        tooltip.style.top = (p.y + 14) + 'px';
      }
    });
    canvas.addEventListener('mousedown', (e) => {
      const p = pointerPos(e);
      const n = nodeAt(p);
      if (n) { dragging = n; canvas.style.cursor = 'grabbing'; }
    });
    window.addEventListener('mouseup', () => {
      if (dragging) { dragging = null; canvas.style.cursor = 'default'; alpha = Math.max(alpha, 0.3); }
    });
    canvas.addEventListener('dblclick', (e) => {
      const p = pointerPos(e);
      const n = nodeAt(p);
      if (n && n !== centerNode) window.location.href = n.url;
    });
  }

  // Init every container on the page.
  document.querySelectorAll('[data-graph-source]').forEach(initGraph);
})();
