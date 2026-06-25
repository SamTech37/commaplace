// Force-directed graph engine, reused by:
//   - /graph (global)            <div data-graph-source="/api/graph" ...>
//   - per-note pages (local)     <div data-lazy-graph="/api/graph/local?note=..." ...>
//
// Container HTML expected:
//   <div data-graph-source="…" data-graph-height="420">
//     <canvas></canvas>
//     <div class="graph-empty" hidden></div>     (optional)
//     <div class="graph-tooltip" hidden></div>   (optional)
//   </div>
(function () {
  // Read theme-aware colors from CSS vars so dark mode works on canvas.
  function themeColors() {
    const s = getComputedStyle(document.documentElement);
    return {
      text:   s.getPropertyValue("--text").trim()   || "#1a1a1a",
      bg:     s.getPropertyValue("--bg-2").trim()   || "#ffffff",
      border: s.getPropertyValue("--border").trim() || "rgba(0,0,0,0.2)",
      muted:  s.getPropertyValue("--text-3").trim() || "rgba(0,0,0,0.4)",
    };
  }

  function initGraph(wrap) {
    const url = wrap.dataset.graphSource;
    if (!url) return;
    const canvas = wrap.querySelector("canvas");
    if (!canvas) return;
    const empty   = wrap.querySelector(".graph-empty");
    const tooltip = wrap.querySelector(".graph-tooltip");
    const ctx = canvas.getContext("2d");

    const fixedHeight = parseInt(wrap.dataset.graphHeight || "0", 10) || 0;
    const isCompact = !!fixedHeight;

    let dpr = window.devicePixelRatio || 1;
    function resize() {
      const w = wrap.clientWidth;
      const h = fixedHeight || Math.max(420, Math.round(window.innerHeight * 0.72));
      canvas.style.width  = w + "px";
      canvas.style.height = h + "px";
      canvas.width  = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    }
    window.addEventListener("resize", resize);
    resize();

    let nodes = [], edges = [], nodeById = new Map();
    let hovered = null, dragging = null, centerNode = null, animating = false;

    // Viewport: panX/panY in CSS-pixel space, zoom is a scale factor.
    let panX = 0, panY = 0, zoom = 1;
    let panVx = 0, panVy = 0;           // inertia velocity (CSS px / frame)
    let panning = false, panLastX = 0, panLastY = 0;
    let autoFitDone = false;
    let fitBtnRect = null;

    const PAN_FRICTION = 0.80;          // high friction — viewport stops quickly
    const MIN_ZOOM = 0.10, MAX_ZOOM = 4.0;

    function screenToWorld(p) {
      return { x: (p.x - panX) / zoom, y: (p.y - panY) / zoom };
    }

    fetch(url)
      .then(r => r.json())
      .then(data => {
        if (!data.nodes || data.nodes.length === 0) {
          if (empty) empty.hidden = false;
          canvas.hidden = true;
          return;
        }
        const cw = canvas.clientWidth, ch = canvas.clientHeight;
        nodes = data.nodes.map(n => ({
          id: n.id, title: n.title || "", url: n.url, author: n.author || "",
          isExternal: !!n.ext,
          x: cw / 2 + (Math.random() - 0.5) * cw * 0.6,
          y: ch / 2 + (Math.random() - 0.5) * ch * 0.6,
          vx: 0, vy: 0, degree: 0,
        }));
        nodeById = new Map(nodes.map(n => [n.id, n]));
        edges = (data.edges || []).map(e => {
          const a = nodeById.get(e.s), b = nodeById.get(e.t);
          if (!a || !b) return null;
          a.degree++; b.degree++;
          return { a, b, cross: !!(a.isExternal || b.isExternal) };
        }).filter(Boolean);
        if (data.center) {
          centerNode = nodeById.get(data.center) || null;
          if (centerNode) { centerNode.x = cw / 2; centerNode.y = ch / 2; }
        }
        canvas.style.cursor = "grab";
        startAnim();
      })
      .catch(err => {
        if (empty) { empty.hidden = false; empty.textContent = "載入失敗：" + err; }
        canvas.hidden = true;
      });

    // ---- simulation ----
    const REPULSION  = isCompact ? 1200 : 4000;
    const SPRING_LEN = isCompact ? 70   : 150;
    const SPRING_K   = 0.018;
    // ponytail: CENTER_K kept tiny; repulsion is now alpha-independent so the
    // equilibrium holds at any alpha — previously repulsion decayed to 0 while
    // CENTER_K stayed constant, causing everything to clump.
    const CENTER_K   = isCompact ? 0.004 : 0.0008;
    const FRICTION   = 0.85;
    let totalKE = Infinity;

    function step() {
      const cw = canvas.clientWidth, ch = canvas.clientHeight;
      const cx = cw / 2, cy = ch / 2;
      // Repulsion: constant (NOT alpha-scaled). Gives a stable equilibrium
      // independent of simulation temperature.
      for (let i = 0; i < nodes.length; i++) {
        const a = nodes[i];
        for (let j = i + 1; j < nodes.length; j++) {
          const b = nodes[j];
          let dx = a.x - b.x, dy = a.y - b.y;
          let d2 = dx * dx + dy * dy;
          if (d2 < 1) { dx = Math.random() - 0.5; dy = Math.random() - 0.5; d2 = 1; }
          const d = Math.sqrt(d2);
          // +200 softening prevents runaway force at very short distances
          const f = REPULSION / (d2 + 200);
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
        const k = n === centerNode ? CENTER_K * 6 : CENTER_K;
        n.vx += (cx - n.x) * k;
        n.vy += (cy - n.y) * k;
      }
      totalKE = 0;
      for (const n of nodes) {
        if (n === dragging) { n.vx = 0; n.vy = 0; continue; }
        n.vx *= FRICTION; n.vy *= FRICTION;
        n.x += n.vx; n.y += n.vy;
        totalKE += n.vx * n.vx + n.vy * n.vy;
      }
    }

    function connectedTo(node) {
      const set = new Set();
      for (const e of edges) {
        if (e.a === node) set.add(e.b);
        if (e.b === node) set.add(e.a);
      }
      return set;
    }

    // Compute card dimensions from text at draw time (world-space units).
    function cardDims(n) {
      const fontSize = isCompact ? 9 : 10;
      if (n.isExternal) {
        ctx.font = fontSize + 'px "IBM Plex Mono",monospace';
        const tw = ctx.measureText("↗ @" + n.author).width;
        return { w: Math.max(50, tw + 12), h: isCompact ? 22 : 26 };
      }
      ctx.font = 'italic ' + fontSize + 'px "Newsreader",Georgia,serif';
      const maxLen = isCompact ? 11 : 14;
      const label = n.title.length > maxLen ? n.title.slice(0, maxLen - 1) + "…" : n.title;
      const tw = ctx.measureText(label).width;
      return { w: Math.max(50, tw + 12), h: isCompact ? 28 : 36 };
    }

    // Compute zoom/pan so all nodes fit in the canvas with 10% padding.
    function fitAll() {
      if (nodes.length === 0) return;
      const cw = canvas.clientWidth, ch = canvas.clientHeight;
      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
      for (const n of nodes) {
        const { w, h } = cardDims(n);
        minX = Math.min(minX, n.x - w / 2); minY = Math.min(minY, n.y - h / 2);
        maxX = Math.max(maxX, n.x + w / 2); maxY = Math.max(maxY, n.y + h / 2);
      }
      const bbW = maxX - minX || 1, bbH = maxY - minY || 1;
      let newZoom = 0.80 * Math.min(cw / bbW, ch / bbH);
      newZoom = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, newZoom));
      panX = cw / 2 - (minX + bbW / 2) * newZoom;
      panY = ch / 2 - (minY + bbH / 2) * newZoom;
      zoom = newZoom;
      panVx = 0; panVy = 0;
    }

    function draw() {
      const cw = canvas.clientWidth, ch = canvas.clientHeight;
      ctx.clearRect(0, 0, cw, ch);

      const C = themeColors();
      const neighbors = hovered ? connectedTo(hovered) : null;

      // All nodes/edges are drawn in world space via the viewport transform.
      ctx.save();
      ctx.translate(panX, panY);
      ctx.scale(zoom, zoom);

      // Edges
      for (const e of edges) {
        const highlighted = hovered && (e.a === hovered || e.b === hovered);
        ctx.lineWidth = 0.75;
        if (e.cross) {
          ctx.setLineDash([3, 2]);
          ctx.strokeStyle = highlighted ? C.text : C.muted;
        } else {
          ctx.setLineDash([]);
          ctx.strokeStyle = highlighted ? C.text + "99" : C.text + "33";
        }
        ctx.beginPath();
        ctx.moveTo(e.a.x, e.a.y);
        ctx.lineTo(e.b.x, e.b.y);
        ctx.stroke();
      }
      ctx.setLineDash([]);

      // Nodes (index cards)
      for (const n of nodes) {
        const { w, h } = cardDims(n);
        const x = n.x - w / 2, y = n.y - h / 2;
        const isCurrent = n === centerNode;
        const isHov = n === hovered || n === dragging;
        const isDimmed = neighbors && !isCurrent && n !== hovered && !neighbors.has(n);

        ctx.globalAlpha = isDimmed ? 0.3 : 1;

        ctx.fillStyle = isCurrent ? C.text : C.bg;
        ctx.fillRect(x, y, w, h);

        if (n.isExternal) {
          ctx.setLineDash([3, 2]);
          ctx.strokeStyle = C.text;
          ctx.lineWidth = 0.75;
        } else {
          ctx.setLineDash([]);
          ctx.strokeStyle = isHov ? C.text : C.border;
          ctx.lineWidth = isHov ? 1 : 0.5;
        }
        ctx.strokeRect(x, y, w, h);
        ctx.setLineDash([]);

        ctx.fillStyle = isCurrent ? C.bg : C.text;
        ctx.textAlign = "left";
        const fontSize = isCompact ? 9 : 10;
        if (n.isExternal) {
          ctx.font = fontSize + 'px "IBM Plex Mono",monospace';
          ctx.textBaseline = "middle";
          ctx.fillText("↗ @" + n.author, x + 5, n.y, w - 8);
        } else {
          const maxLen = isCompact ? 11 : 14;
          const title = n.title.length > maxLen ? n.title.slice(0, maxLen - 1) + "…" : n.title;
          ctx.font = "italic " + fontSize + 'px "Newsreader",Georgia,serif';
          ctx.textBaseline = "middle";
          ctx.fillText(title, x + 5, n.y, w - 8);
        }
        ctx.globalAlpha = 1;
      }

      ctx.restore();

      // "fit" button drawn in screen space (outside the viewport transform).
      if (nodes.length > 0) {
        const bw = 36, bh = 20, margin = 8;
        const bx = cw - bw - margin, by = margin;
        fitBtnRect = { x: bx, y: by, w: bw, h: bh };
        ctx.fillStyle = C.bg;
        ctx.strokeStyle = C.border;
        ctx.lineWidth = 0.5;
        ctx.setLineDash([]);
        ctx.fillRect(bx, by, bw, bh);
        ctx.strokeRect(bx, by, bw, bh);
        ctx.fillStyle = C.text;
        ctx.font = '10px "IBM Plex Mono",monospace';
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.fillText("fit", bx + bw / 2, by + bh / 2);
      }
    }

    function startAnim() {
      if (animating) return;
      animating = true;
      totalKE = Infinity;
      function tick() {
        // Apply pan inertia only when not actively panning.
        if (!panning && (panVx !== 0 || panVy !== 0)) {
          panX += panVx; panY += panVy;
          panVx *= PAN_FRICTION; panVy *= PAN_FRICTION;
          if (Math.abs(panVx) < 0.01) panVx = 0;
          if (Math.abs(panVy) < 0.01) panVy = 0;
        }
        step();
        draw();
        const settling  = totalKE > nodes.length * 0.05 || dragging;
        const panMoving = panning || panVx !== 0 || panVy !== 0;
        if (settling || panMoving) {
          requestAnimationFrame(tick);
        } else {
          animating = false;
          // Auto-fit once after the simulation first settles.
          if (!autoFitDone) {
            autoFitDone = true;
            fitAll();
            draw();
          }
        }
      }
      requestAnimationFrame(tick);
    }

    // ---- pointer interaction ----
    function pointerPos(e) {
      const rect = canvas.getBoundingClientRect();
      return { x: e.clientX - rect.left, y: e.clientY - rect.top };
    }

    // nodeAt takes world-space coordinates.
    function nodeAt(wp) {
      for (let i = nodes.length - 1; i >= 0; i--) {
        const n = nodes[i];
        const { w, h } = cardDims(n);
        if (wp.x >= n.x - w / 2 && wp.x <= n.x + w / 2 &&
            wp.y >= n.y - h / 2 && wp.y <= n.y + h / 2) return n;
      }
      return null;
    }

    function inFitBtn(p) {
      return fitBtnRect &&
        p.x >= fitBtnRect.x && p.x <= fitBtnRect.x + fitBtnRect.w &&
        p.y >= fitBtnRect.y && p.y <= fitBtnRect.y + fitBtnRect.h;
    }

    canvas.addEventListener("mousemove", e => {
      const p = pointerPos(e);
      if (panning) {
        panX += p.x - panLastX;
        panY += p.y - panLastY;
        panVx = p.x - panLastX;
        panVy = p.y - panLastY;
        panLastX = p.x;
        panLastY = p.y;
        if (!animating) startAnim();
        return;
      }
      const wp = screenToWorld(p);
      if (dragging) {
        dragging.x = wp.x;
        dragging.y = wp.y;
        totalKE = Infinity;
        if (!animating) startAnim();
        return;
      }
      const h = nodeAt(wp);
      if (h !== hovered) {
        hovered = h;
        if (!animating) draw();
        if (h && tooltip) {
          tooltip.hidden = false;
          tooltip.textContent = h.title + (h.isExternal ? "  外 · @" : "  @") + h.author;
          tooltip.style.left = p.x + 14 + "px";
          tooltip.style.top  = p.y - 8 + "px";
          canvas.style.cursor = "pointer";
        } else if (tooltip) {
          tooltip.hidden = true;
          canvas.style.cursor = inFitBtn(p) ? "pointer" : "grab";
        }
      } else if (h && tooltip) {
        tooltip.style.left = p.x + 14 + "px";
        tooltip.style.top  = p.y - 8 + "px";
      } else if (!h) {
        canvas.style.cursor = inFitBtn(p) ? "pointer" : "grab";
      }
    });

    canvas.addEventListener("mouseleave", () => {
      panning = false;
      if (hovered || dragging) {
        hovered = null;
        if (tooltip) tooltip.hidden = true;
        canvas.style.cursor = "grab";
        if (!animating) draw();
      }
    });

    // Single click = navigate; drag = drag. Distinguish by movement.
    let downPos = null, downNode = null;
    canvas.addEventListener("mousedown", e => {
      if (e.button !== 0) return;
      const p = pointerPos(e);
      if (inFitBtn(p)) {
        fitAll();
        if (!animating) draw();
        return;
      }
      const wp = screenToWorld(p);
      const n = nodeAt(wp);
      downPos = p;
      downNode = n;
      if (n) {
        dragging = n;
        canvas.style.cursor = "grabbing";
      } else {
        panning = true;
        panLastX = p.x;
        panLastY = p.y;
        panVx = 0; panVy = 0;
        canvas.style.cursor = "grabbing";
      }
    });

    window.addEventListener("mouseup", e => {
      if (panning) {
        panning = false;
        canvas.style.cursor = hovered ? "pointer" : "grab";
        if (!animating && (Math.abs(panVx) > 0.01 || Math.abs(panVy) > 0.01)) startAnim();
        return;
      }
      if (!dragging) return;
      const p = pointerPos(e);
      const moved = downPos && Math.hypot(p.x - downPos.x, p.y - downPos.y) < 5;
      const wasNode = downNode;
      dragging = null;
      downPos = null;
      downNode = null;
      canvas.style.cursor = hovered ? "pointer" : "grab";
      totalKE = Infinity;
      if (!animating) startAnim();
      // Navigate on click (not drag)
      if (moved && wasNode && wasNode !== centerNode) {
        window.location.href = wasNode.url;
      }
    });

    // Scroll to zoom, pivoting at cursor position.
    canvas.addEventListener("wheel", e => {
      e.preventDefault();
      const p = pointerPos(e);
      const wx = (p.x - panX) / zoom;
      const wy = (p.y - panY) / zoom;
      let newZoom = zoom * Math.pow(0.998, e.deltaY);
      newZoom = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, newZoom));
      panX = p.x - wx * newZoom;
      panY = p.y - wy * newZoom;
      zoom = newZoom;
      if (!animating) draw();
    }, { passive: false });
  }

  // Global graph: initialize immediately.
  document.querySelectorAll("[data-graph-source]").forEach(initGraph);

  // Local graph (per-note): lazy — initialize when <details> is opened.
  document.querySelectorAll("[data-lazy-graph]").forEach(wrap => {
    const details = wrap.closest("details");
    if (!details) {
      wrap.dataset.graphSource = wrap.dataset.lazyGraph;
      initGraph(wrap);
      return;
    }
    details.addEventListener("toggle", function onToggle() {
      if (!details.open) return;
      details.removeEventListener("toggle", onToggle);
      wrap.dataset.graphSource = wrap.dataset.lazyGraph;
      initGraph(wrap);
    }, { once: false });
  });
})();
