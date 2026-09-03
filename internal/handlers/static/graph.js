// Graph engine v2 — d3-force layout + custom canvas renderer.
//
// Used by:
//   - /graph (global)            <div data-graph-source="/api/graph" ...>
//   - per-note pages (local)     <div data-lazy-graph="/api/graph/local?note=..." ...>
//
// Container HTML expected:
//   <div data-graph-source="…" data-graph-height="420">
//     <canvas></canvas>
//     <div class="graph-empty" hidden></div>     (optional)
//   </div>
//
// Design notes:
//   - Layout is d3-force (vendored d3-force.min.js) ticked manually inside
//     our own rAF loop, so physics, camera easing and hover transitions all
//     share one frame budget and the loop fully stops when idle.
//   - LOD rendering: zoomed out nodes are ink dots sized by degree; zooming
//     in crossfades them into the index-card look. Labels fade in between.
//   - All input is Pointer Events: mouse, touch (incl. pinch zoom) and pen
//     share one code path.
//   - Edges are batched into a handful of Path2D strokes per frame instead
//     of one beginPath per edge; offscreen nodes are culled.
(function () {
  "use strict";

  var TAU = Math.PI * 2;
  // html[data-motion] is the reader's explicit choice and outranks the OS
  // preference in both directions; with neither set the OS decides.
  var motionPref = document.documentElement.getAttribute("data-motion");
  var reducedMotion =
    motionPref === "reduced" ||
    (motionPref !== "full" &&
      !!window.matchMedia &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches);

  function clamp(v, lo, hi) {
    return v < lo ? lo : v > hi ? hi : v;
  }
  function lerp(a, b, t) {
    return a + (b - a) * t;
  }
  // 0 below e0, 1 above e1, smooth in between.
  function smoothstep(e0, e1, v) {
    var t = clamp((v - e0) / (e1 - e0), 0, 1);
    return t * t * (3 - 2 * t);
  }
  function easeOutCubic(t) {
    t = clamp(t, 0, 1);
    return 1 - Math.pow(1 - t, 3);
  }

  // ---- theme ----------------------------------------------------------
  // Parse the CSS custom properties once and pre-build every rgba() string
  // the renderer needs, so draw() never allocates color strings.
  function parseColor(str, fallback) {
    str = (str || "").trim();
    var m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(str);
    if (m) {
      var h = m[1];
      if (h.length === 3) h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
      return [
        parseInt(h.slice(0, 2), 16),
        parseInt(h.slice(2, 4), 16),
        parseInt(h.slice(4, 6), 16),
      ];
    }
    m = /^rgba?\(\s*([\d.]+)[ ,]+([\d.]+)[ ,]+([\d.]+)/.exec(str);
    if (m) return [+m[1], +m[2], +m[3]];
    return fallback;
  }
  function buildTheme() {
    var s = getComputedStyle(document.documentElement);
    var text = parseColor(s.getPropertyValue("--text"), [26, 26, 26]);
    var bg = parseColor(s.getPropertyValue("--bg-2"), [255, 255, 255]);
    var rgba = function (c, a) {
      return "rgba(" + c[0] + "," + c[1] + "," + c[2] + "," + a + ")";
    };
    return {
      text: rgba(text, 1),
      textDot: rgba(text, 0.85),
      edge: rgba(text, 0.16),
      edgeCross: rgba(text, 0.28),
      edgeHot: rgba(text, 0.55),
      label: rgba(text, 0.62),
      border: rgba(text, 0.25),
      bg: rgba(bg, 1),
      halo: rgba(text, 0.08),
    };
  }
  var theme = buildTheme();
  var themeListeners = [];
  new MutationObserver(function () {
    theme = buildTheme();
    themeListeners.forEach(function (fn) {
      fn();
    });
  }).observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["data-theme"],
  });

  // ---- per-container init ---------------------------------------------
  function initGraph(wrap) {
    var url = wrap.dataset.graphSource;
    if (!url || wrap.dataset.graphReady) return;
    wrap.dataset.graphReady = "1";
    var canvas = wrap.querySelector("canvas");
    if (!canvas) return;
    var ctx = canvas.getContext("2d");

    var empty = wrap.querySelector(".graph-empty");
    var tooltip = wrap.querySelector(".graph-tooltip");
    if (!tooltip) {
      tooltip = document.createElement("div");
      tooltip.className = "graph-tooltip";
      tooltip.hidden = true;
      wrap.appendChild(tooltip);
    }
    var fitBtn = document.createElement("button");
    fitBtn.type = "button";
    fitBtn.className = "graph-fit";
    fitBtn.textContent = "全圖";
    fitBtn.hidden = true;
    wrap.appendChild(fitBtn);

    var fixedHeight = parseInt(wrap.dataset.graphHeight || "0", 10) || 0;
    var isCompact = !!fixedHeight;

    // ---- canvas sizing ----
    var cw = 0,
      ch = 0;
    function resize() {
      var dpr = window.devicePixelRatio || 1;
      cw = wrap.clientWidth;
      ch = fixedHeight || Math.max(420, Math.round(window.innerHeight * 0.72));
      canvas.style.width = cw + "px";
      canvas.style.height = ch + "px";
      canvas.width = Math.round(cw * dpr);
      canvas.height = Math.round(ch * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      requestFrame();
    }
    resize();
    if (window.ResizeObserver) {
      var ro = new ResizeObserver(function () {
        if (wrap.clientWidth !== cw) resize();
      });
      ro.observe(wrap);
    } else {
      window.addEventListener("resize", resize);
    }

    // ---- state ----
    var nodes = [],
      edges = [],
      centerNode = null;
    var sim = null;
    var hovered = null,
      draggingNode = null;
    var focusNode = null; // hover focus target, kept while focusT fades out

    // Camera: world → screen is  s = w * zoom + pan.
    var cam = { x: 0, y: 0, zoom: 1 };
    var zoomTarget = 1;
    var zoomAnchor = null; // {sx, sy, wx, wy} pivot while easing
    var panVX = 0,
      panVY = 0; // pan inertia, css px / frame
    var autoFollow = true; // camera tracks fit until user interacts
    var followSnap = false; // camera still easing toward fit target
    var focusT = 0; // 0..1 hover-dim amount, eased
    var bornAt = 0; // entrance animation clock
    var entranceDone = reducedMotion;

    var MIN_ZOOM = 0.05,
      MAX_ZOOM = 6;
    var FONT_CARD = 'italic 10px "Newsreader",Georgia,serif';
    var FONT_AUTHOR = '7px "IBM Plex Mono",monospace';
    var FONT_LABEL = '9px "IBM Plex Mono",monospace';

    function screenToWorld(sx, sy) {
      return { x: (sx - cam.x) / cam.zoom, y: (sy - cam.y) / cam.zoom };
    }

    // Same URL shape as linkpreview.js's own previewURLFor — n.url is always
    // "/handle/slug" (server-supplied, see /api/graph's node payload).
    function previewURLForNode(n) {
      var m = (n.url || "").match(/^\/([^/]+)\/([^/]+)$/);
      return m ? "/api/preview/" + m[1] + "/" + m[2] : null;
    }

    // A small rect around the node's current screen position, in page
    // coordinates, for LinkPreview's popup placement.
    function nodeScreenRect(n) {
      var cr = canvas.getBoundingClientRect();
      var sx = cr.left + n.x * cam.zoom + cam.x;
      var sy = cr.top + n.y * cam.zoom + cam.y;
      var pad = 10;
      return { left: sx - pad, right: sx + pad, top: sy - pad, bottom: sy + pad };
    }

    // ---- data ----
    fetch(url)
      .then(function (r) {
        if (!r.ok) throw new Error(r.status);
        return r.json();
      })
      .then(function (data) {
        if (!data.nodes || data.nodes.length === 0) {
          if (empty) empty.hidden = false;
          canvas.hidden = true;
          return;
        }
        setup(data);
      })
      .catch(function (err) {
        if (empty) {
          empty.hidden = false;
          empty.textContent = "載入失敗：" + err;
        }
        canvas.hidden = true;
      });

    function setup(data) {
      nodes = data.nodes.map(function (n, i) {
        // Seed in a tight phyllotaxis disc so the layout visibly blooms
        // outward from the middle on load.
        var r = 4.5 * Math.sqrt(i),
          a = i * 2.39996;
        return {
          id: n.id,
          title: n.title || "",
          url: n.url,
          author: n.author || "",
          x: r * Math.cos(a),
          y: r * Math.sin(a),
          degree: 0,
          neighbors: null,
          // render caches, filled lazily
          label: null,
          labelW: 0,
          cardW: 0,
          cardH: 0,
          authorLabel: null,
          born: 0,
          pop: 0,
        };
      });
      var byId = new Map(
        nodes.map(function (n) {
          return [n.id, n];
        }),
      );
      edges = (data.edges || []).reduce(function (acc, e) {
        var a = byId.get(e.s),
          b = byId.get(e.t);
        if (a && b) {
          a.degree++;
          b.degree++;
          if (!a.neighbors) a.neighbors = new Set();
          if (!b.neighbors) b.neighbors = new Set();
          a.neighbors.add(b);
          b.neighbors.add(a);
          acc.push({ source: a, target: b, cross: a.author !== b.author });
        }
        return acc;
      }, []);
      centerNode = data.center ? byId.get(data.center) || null : null;

      // Staggered entrance: hubs appear first, leaves ripple outward.
      var span = Math.min(600, 80 + nodes.length * 2);
      nodes.forEach(function (n, i) {
        n.born = reducedMotion ? 0 : (i * 137) % span;
      });

      // Physics cost scales hard with node count; trade layout finesse for
      // frame rate as the graph grows.
      var big = nodes.length > 1200;
      sim = d3
        .forceSimulation(nodes)
        .force(
          "link",
          d3
            .forceLink(edges)
            .distance(isCompact ? 85 : 80)
            .strength(function (e) {
              // Hubs pull less per-edge so dense clusters can breathe.
              return (
                1 / Math.min(4, Math.max(e.source.degree, e.target.degree))
              );
            }),
        )
        .force(
          "charge",
          d3
            .forceManyBody()
            .strength(isCompact ? -180 : -240)
            .theta(big ? 1.2 : 0.9)
            .distanceMax(big ? 300 : isCompact ? 400 : 900),
        )
        .force("x", d3.forceX(0).strength(centerStrength))
        .force("y", d3.forceY(0).strength(centerStrength))
        .alphaDecay(big ? 0.05 : 0.032)
        .velocityDecay(0.32)
        .stop(); // we tick manually in the rAF loop
      if (isCompact || nodes.length <= 600) {
        sim.force(
          "collide",
          d3
            .forceCollide(function (n) {
              // compact graphs render as cards, so collide on card size instead
              return isCompact ? 42 : dotR(n) + 5;
            })
            .iterations(1),
        );
      }

      if (reducedMotion) {
        // No entrance animation: pre-settle synchronously within a time
        // budget (big graphs would block the page if we ran to completion),
        // then let the frame loop finish the rest.
        var deadline = performance.now() + 250;
        while (sim.alpha() > sim.alphaMin() && performance.now() < deadline)
          sim.tick();
        snapCameraToFit();
      }

      canvas.style.cursor = "grab";
      fitBtn.hidden = false;
      bornAt = performance.now();
      requestFrame();
    }

    function centerStrength(n) {
      if (n === centerNode) return 0.3;
      // Untethered islands drift; single notes get pulled in harder.
      return n.degree === 0 ? 0.09 : 0.035;
    }

    function dotR(n) {
      return 3 + Math.sqrt(n.degree) * 1.6;
    }

    // Card metrics are world-space constants per node; measured once.
    function cardDims(n) {
      if (!n.cardW) {
        ctx.font = FONT_CARD;
        var max = 18;
        n.label =
          n.title.length > max ? n.title.slice(0, max - 1) + "…" : n.title;
        n.labelW = ctx.measureText(n.label).width;
        ctx.font = FONT_AUTHOR;
        n.authorLabel = "@" + n.author;
        var aw = ctx.measureText(n.authorLabel).width;
        n.cardW = clamp(Math.max(n.labelW, aw) + 14, 52, 150);
        n.cardH = 30;
      }
      return n;
    }

    // ---- camera ----
    function fitTarget() {
      var minX = Infinity,
        minY = Infinity,
        maxX = -Infinity,
        maxY = -Infinity;
      for (var i = 0; i < nodes.length; i++) {
        var n = nodes[i],
          r = dotR(n) + 14;
        if (n.x - r < minX) minX = n.x - r;
        if (n.y - r < minY) minY = n.y - r;
        if (n.x + r > maxX) maxX = n.x + r;
        if (n.y + r > maxY) maxY = n.y + r;
      }
      var bw = maxX - minX || 1,
        bh = maxY - minY || 1;
      // Compact (local) graphs land fully in card mode; the global graph
      // lands fully in dot mode — never inside the crossfade band.
      var z = clamp(
        0.86 * Math.min(cw / bw, ch / bh),
        MIN_ZOOM,
        isCompact ? 1.4 : 1.0,
      );
      return {
        zoom: z,
        x: cw / 2 - (minX + bw / 2) * z,
        y: ch / 2 - (minY + bh / 2) * z,
      };
    }
    function snapCameraToFit() {
      var t = fitTarget();
      cam.x = t.x;
      cam.y = t.y;
      cam.zoom = zoomTarget = t.zoom;
      panVX = panVY = 0;
    }
    fitBtn.addEventListener("click", function () {
      autoFollow = true;
      followSnap = true;
      panVX = panVY = 0;
      zoomAnchor = null;
      requestFrame();
    });

    function userTookCamera() {
      autoFollow = false;
      followSnap = false;
    }

    // ---- frame loop ----
    var frameQueued = false;
    function requestFrame() {
      if (frameQueued) return;
      frameQueued = true;
      requestAnimationFrame(frame);
    }
    var tickMs = 0,
      tickSkipped = false;
    function frame(now) {
      frameQueued = false;
      var busy = false;

      // physics — when a tick is slower than a frame budget, run it every
      // other frame so rendering and input stay responsive
      if (sim && sim.alpha() > sim.alphaMin()) {
        if (tickMs > 32 && !tickSkipped) {
          tickSkipped = true;
        } else {
          var ts = performance.now();
          sim.tick();
          tickMs = performance.now() - ts;
          tickSkipped = false;
        }
        busy = true;
      }

      // camera: auto-fit follows the layout while it settles
      if (autoFollow && nodes.length) {
        var t = fitTarget();
        var k = followSnap ? 0.18 : 0.1;
        cam.x = lerp(cam.x, t.x, k);
        cam.y = lerp(cam.y, t.y, k);
        cam.zoom = zoomTarget = lerp(cam.zoom, t.zoom, k);
        var close =
          Math.abs(cam.zoom - t.zoom) < 0.001 &&
          Math.abs(cam.x - t.x) < 0.5 &&
          Math.abs(cam.y - t.y) < 0.5;
        if (close) followSnap = false;
        if (!close || busy) busy = true;
      }

      // wheel zoom easing toward zoomTarget, pivoting at the cursor
      if (!autoFollow && Math.abs(zoomTarget - cam.zoom) > 0.0005) {
        var nz = lerp(cam.zoom, zoomTarget, 0.35);
        if (zoomAnchor) {
          cam.x = zoomAnchor.sx - zoomAnchor.wx * nz;
          cam.y = zoomAnchor.sy - zoomAnchor.wy * nz;
        }
        cam.zoom = nz;
        busy = true;
      } else if (zoomAnchor) {
        // zoom ease finished: the node under the cursor may have changed
        if (!draggingNode && activePointers.size === 0) {
          var hz = nodeAt(zoomAnchor.sx, zoomAnchor.sy);
          if (hz !== hovered) {
            hovered = hz;
            tooltip.hidden = !hz;
            busy = true;
          }
        }
        zoomAnchor = null;
      }

      // pan inertia
      if (activePointers.size === 0 && (panVX !== 0 || panVY !== 0)) {
        cam.x += panVX;
        cam.y += panVY;
        panVX *= 0.86;
        panVY *= 0.86;
        if (Math.abs(panVX) < 0.04) panVX = 0;
        if (Math.abs(panVY) < 0.04) panVY = 0;
        busy = true;
      }

      // hover focus transition (focusNode lingers so the fade-out animates)
      if (hovered) focusNode = hovered;
      var focusGoal = hovered ? 1 : 0;
      if (Math.abs(focusT - focusGoal) > 0.005) {
        focusT = lerp(focusT, focusGoal, 0.22);
        busy = true;
      } else {
        focusT = focusGoal;
        if (!hovered) focusNode = null;
      }

      // entrance pop-in
      if (!entranceDone && nodes.length) {
        var age = now - bornAt;
        entranceDone = true;
        for (var i = 0; i < nodes.length; i++) {
          var n = nodes[i];
          n.pop = easeOutCubic((age - n.born) / 400);
          if (n.pop < 1) entranceDone = false;
        }
        if (!entranceDone) busy = true;
      }

      draw();

      if (busy || draggingNode || activePointers.size > 0) requestFrame();
      else running = false;
    }
    themeListeners.push(requestFrame);

    // ---- rendering ----
    function draw() {
      ctx.clearRect(0, 0, cw, ch);
      if (!nodes.length) return;

      var z = cam.zoom;
      // Compact (local ego) graphs are always cards; the global graph
      // crossfades dot → card as you zoom in.
      var cardT = isCompact ? 1 : smoothstep(1.05, 1.4, z);
      // dot labels fade out completely before cards start appearing, so the
      // two text layers never double-print
      var labelT = isCompact
        ? 0
        : smoothstep(0.55, 0.85, z) * (1 - smoothstep(0.9, 1.05, z));
      var dim = 1 - 0.82 * focusT; // alpha for non-neighbors

      // world-space view rect (+margin) for culling
      var vx0 = -cam.x / z - 150,
        vy0 = -cam.y / z - 150;
      var vx1 = (cw - cam.x) / z + 150,
        vy1 = (ch - cam.y) / z + 150;

      ctx.save();
      ctx.translate(cam.x, cam.y);
      ctx.scale(z, z);

      var fNode = focusNode;

      // -- edges, batched by style --
      // Focus-adjacent edges are drawn twice: once in the base batch and
      // once in the hot overlay whose alpha follows focusT, so both the
      // fade-in and the fade-out are smooth.
      var plain = new Path2D(),
        cross = new Path2D(),
        hot = new Path2D(),
        hotCross = new Path2D();
      var hasPlain = false,
        hasCross = false,
        hasHot = false,
        hasHotCross = false;
      for (var i = 0; i < edges.length; i++) {
        var e = edges[i];
        var a = e.source,
          b = e.target;
        // cull edges entirely outside the view
        if (
          (a.x < vx0 && b.x < vx0) ||
          (a.x > vx1 && b.x > vx1) ||
          (a.y < vy0 && b.y < vy0) ||
          (a.y > vy1 && b.y > vy1)
        )
          continue;
        var p = e.cross ? cross : plain;
        p.moveTo(a.x, a.y);
        p.lineTo(b.x, b.y);
        if (e.cross) hasCross = true;
        else hasPlain = true;
        if (fNode && (a === fNode || b === fNode)) {
          var hp = e.cross ? hotCross : hot;
          hp.moveTo(a.x, a.y);
          hp.lineTo(b.x, b.y);
          if (e.cross) hasHotCross = true;
          else hasHot = true;
        }
      }
      // Dash pattern must stay constant in *screen* pixels: a world-space
      // dash rasterizes thousands of sub-pixel segments per line when
      // zoomed out and destroys the frame rate.
      var dash = [4 / z, 3 / z];
      ctx.lineWidth = 0.8 / Math.max(z, 0.7);
      ctx.globalAlpha = dim;
      if (hasPlain) {
        ctx.strokeStyle = theme.edge;
        ctx.stroke(plain);
      }
      if (hasCross) {
        ctx.setLineDash(dash);
        ctx.strokeStyle = theme.edgeCross;
        ctx.stroke(cross);
        ctx.setLineDash([]);
      }
      if (hasHot || hasHotCross) {
        ctx.globalAlpha = focusT;
        ctx.lineWidth = 1.4 / Math.max(z, 0.7);
        ctx.strokeStyle = theme.edgeHot;
        if (hasHot) ctx.stroke(hot);
        if (hasHotCross) {
          ctx.setLineDash(dash);
          ctx.stroke(hotCross);
          ctx.setLineDash([]);
        }
      }
      ctx.globalAlpha = 1;

      // -- nodes --
      var dotT = 1 - cardT;
      function isDimmed(n) {
        return (
          focusT > 0 &&
          fNode &&
          n !== fNode &&
          !(fNode.neighbors && fNode.neighbors.has(n))
        );
      }

      // batched dot pass (default dots share two paths: lit / dimmed)
      if (dotT > 0.01) {
        var dotsDim = new Path2D(),
          dotsLit = new Path2D();
        var hasDim = false,
          hasLit = false;
        for (i = 0; i < nodes.length; i++) {
          var n = nodes[i];
          if (n.x < vx0 || n.x > vx1 || n.y < vy0 || n.y > vy1) continue;
          if (n === hovered || n === draggingNode || n === centerNode) continue; // drawn individually
          var r = dotR(n) * dotT * (entranceDone ? 1 : n.pop);
          if (r <= 0) continue;
          var dimmed = isDimmed(n);
          var p2 = dimmed ? dotsDim : dotsLit;
          p2.moveTo(n.x + r, n.y);
          p2.arc(n.x, n.y, r, 0, TAU);
          if (dimmed) hasDim = true;
          else hasLit = true;
        }
        ctx.fillStyle = theme.textDot;
        if (hasLit) {
          ctx.globalAlpha = 1;
          ctx.fill(dotsLit);
        }
        if (hasDim) {
          ctx.globalAlpha = dim * 0.5 + 0.08;
          ctx.fill(dotsDim);
        }
        ctx.globalAlpha = 1;
      }

      // dot labels (mid zoom)
      if (labelT > 0.02) {
        ctx.font = FONT_LABEL;
        ctx.textAlign = "center";
        ctx.textBaseline = "top";
        ctx.fillStyle = theme.label;
        for (i = 0; i < nodes.length; i++) {
          n = nodes[i];
          if (n.x < vx0 || n.x > vx1 || n.y < vy0 || n.y > vy1) continue;
          var la = labelT;
          if (isDimmed(n)) la *= dim;
          if (!entranceDone) la *= n.pop;
          if (la <= 0.02) continue;
          ctx.globalAlpha = la;
          if (!n.label) {
            cardDims(n);
            ctx.font = FONT_LABEL;
          } // cardDims clobbers ctx.font
          ctx.fillText(n.label, n.x, n.y + dotR(n) + 3);
        }
        ctx.globalAlpha = 1;
      }

      // individual pass: cards at high zoom, plus hovered/dragged/center
      // nodes at any zoom. Dot and card versions crossfade over cardT.
      for (i = 0; i < nodes.length; i++) {
        n = nodes[i];
        if (n.x < vx0 || n.x > vx1 || n.y < vy0 || n.y > vy1) continue;
        var isHov = n === hovered || n === draggingNode;
        var isCenter = n === centerNode;
        if (cardT <= 0.01 && !isHov && !isCenter) continue;

        var pop = entranceDone ? 1 : n.pop;
        if (pop <= 0) continue;
        var alpha = pop * (!isHov && isDimmed(n) ? dim : 1);

        // dot half of the crossfade (also the hover halo)
        if ((isHov || isCenter) && dotT > 0.01) {
          var rr = (dotR(n) + (isHov ? 1.5 : 1)) * (isCenter ? 1 : dotT);
          ctx.globalAlpha = alpha * dotT;
          if (isHov) {
            ctx.fillStyle = theme.halo;
            ctx.beginPath();
            ctx.arc(n.x, n.y, rr + 7 / z, 0, TAU);
            ctx.fill();
          }
          ctx.fillStyle = theme.text;
          ctx.beginPath();
          ctx.arc(n.x, n.y, rr, 0, TAU);
          ctx.fill();
          if (isCenter) {
            ctx.strokeStyle = theme.text;
            ctx.lineWidth = 1 / z;
            ctx.beginPath();
            ctx.arc(n.x, n.y, rr + 3 / z, 0, TAU);
            ctx.stroke();
          }
          ctx.globalAlpha = 1;
        }
        if (cardT <= 0.01) continue;

        // card half of the crossfade
        cardDims(n);
        var w = n.cardW,
          h = n.cardH;
        var x = n.x - w / 2,
          y = n.y - h / 2;
        ctx.globalAlpha = alpha * cardT;
        ctx.fillStyle = isCenter ? theme.text : theme.bg;
        ctx.fillRect(x, y, w, h);
        ctx.strokeStyle = isHov ? theme.text : theme.border;
        ctx.lineWidth = isHov ? 1.2 : 0.6;
        ctx.strokeRect(x, y, w, h);
        ctx.fillStyle = isCenter ? theme.bg : theme.text;
        ctx.textAlign = "left";
        ctx.textBaseline = "middle";
        ctx.font = FONT_CARD;
        ctx.fillText(n.label, x + 7, n.y - 4, w - 12);
        ctx.globalAlpha = alpha * cardT * 0.55;
        ctx.font = FONT_AUTHOR;
        ctx.fillText(n.authorLabel, x + 7, n.y + 8, w - 12);
        ctx.globalAlpha = 1;
      }

      ctx.restore();
    }

    // ---- hit testing ----
    function nodeAt(sx, sy) {
      if (!sim) return null;
      var wp = screenToWorld(sx, sy);
      var reach = Math.max(18 / cam.zoom, 70);
      var n = sim.find(wp.x, wp.y, reach);
      if (!n) return null;
      var cardT = isCompact ? 1 : smoothstep(1.05, 1.4, cam.zoom);
      if (cardT > 0.5) {
        cardDims(n);
        if (
          Math.abs(wp.x - n.x) <= n.cardW / 2 + 3 &&
          Math.abs(wp.y - n.y) <= n.cardH / 2 + 3
        )
          return n;
        return null;
      }
      var r = dotR(n) + 6 / cam.zoom;
      var dx = wp.x - n.x,
        dy = wp.y - n.y;
      return dx * dx + dy * dy <= r * r ? n : null;
    }

    // ---- pointer input ----
    // One code path for mouse / touch / pen. Two active pointers = pinch.
    var activePointers = new Map(); // pointerId -> {x, y}
    var pinch = null; // {dist, zoom, wx, wy}
    var panLast = null; // {x, y} for single-pointer pan
    var downInfo = null; // {x, y, node, t} for tap detection
    var moveSamples = []; // recent deltas for inertia

    function ptrPos(e) {
      var rect = canvas.getBoundingClientRect();
      return { x: e.clientX - rect.left, y: e.clientY - rect.top };
    }

    canvas.addEventListener("pointerdown", function (e) {
      if (!nodes.length || (e.pointerType === "mouse" && e.button !== 0))
        return;
      e.preventDefault();
      canvas.setPointerCapture(e.pointerId);
      var p = ptrPos(e);
      activePointers.set(e.pointerId, p);
      userTookCamera();
      panVX = panVY = 0;

      if (activePointers.size === 2) {
        // second finger: abort node drag, start pinch
        if (draggingNode) endDrag(false);
        var pts = Array.from(activePointers.values());
        var mx = (pts[0].x + pts[1].x) / 2,
          my = (pts[0].y + pts[1].y) / 2;
        var wp = screenToWorld(mx, my);
        pinch = {
          dist: Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y),
          zoom: cam.zoom,
          wx: wp.x,
          wy: wp.y,
        };
        panLast = null;
        downInfo = null;
        return;
      }

      var n = nodeAt(p.x, p.y);
      downInfo = { x: p.x, y: p.y, node: n, t: performance.now() };
      tooltip.hidden = true;
      if (window.LinkPreview) window.LinkPreview.hideNow();
      if (n) {
        draggingNode = n;
        var wp2 = screenToWorld(p.x, p.y);
        n.fx = wp2.x;
        n.fy = wp2.y;
        sim.alphaTarget(0.25);
        if (sim.alpha() < 0.25) sim.alpha(0.25);
        canvas.style.cursor = "grabbing";
      } else {
        panLast = p;
        moveSamples.length = 0;
        canvas.style.cursor = "grabbing";
      }
      requestFrame();
    });

    canvas.addEventListener("pointermove", function (e) {
      if (!nodes.length) return;
      var p = ptrPos(e);

      if (activePointers.has(e.pointerId)) {
        activePointers.set(e.pointerId, p);

        if (pinch && activePointers.size >= 2) {
          var pts = Array.from(activePointers.values());
          var d = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y);
          var mx = (pts[0].x + pts[1].x) / 2,
            my = (pts[0].y + pts[1].y) / 2;
          var nz = clamp(pinch.zoom * (d / pinch.dist), MIN_ZOOM, MAX_ZOOM);
          cam.zoom = zoomTarget = nz;
          cam.x = mx - pinch.wx * nz;
          cam.y = my - pinch.wy * nz;
          zoomAnchor = null;
          requestFrame();
          return;
        }
        if (draggingNode) {
          var wp = screenToWorld(p.x, p.y);
          draggingNode.fx = wp.x;
          draggingNode.fy = wp.y;
          requestFrame();
          return;
        }
        if (panLast) {
          var dx = p.x - panLast.x,
            dy = p.y - panLast.y;
          cam.x += dx;
          cam.y += dy;
          moveSamples.push({ dx: dx, dy: dy });
          if (moveSamples.length > 4) moveSamples.shift();
          panLast = p;
          requestFrame();
          return;
        }
        return;
      }

      // no button held: hover (mouse/pen only)
      if (e.pointerType === "touch") return;
      var h = nodeAt(p.x, p.y);
      if (h !== hovered) {
        hovered = h;
        canvas.style.cursor = h ? "pointer" : "grab";
        requestFrame();
        if (window.LinkPreview) {
          var url = h && previewURLForNode(h);
          if (url) {
            window.LinkPreview.scheduleShow(h, url, function () { return nodeScreenRect(h); }, function () { return hovered === h; });
          } else {
            window.LinkPreview.scheduleHide();
          }
        }
      }
      // Plain-text fallback only when the richer preview module isn't loaded
      // (e.g. a future page that embeds the graph without linkpreview.js).
      if (!window.LinkPreview) {
        if (h) {
          tooltip.hidden = false;
          tooltip.textContent = h.title + "  @" + h.author;
          tooltip.style.left = Math.min(p.x + 14, cw - 60) + "px";
          tooltip.style.top = p.y - 10 + "px";
        } else {
          tooltip.hidden = true;
        }
      }
    });

    function endDrag(reheat) {
      if (!draggingNode) return;
      draggingNode.fx = null;
      draggingNode.fy = null;
      draggingNode = null;
      sim.alphaTarget(0);
      if (reheat && sim.alpha() < 0.12) sim.alpha(0.12);
    }

    function pointerEnd(e) {
      if (!activePointers.has(e.pointerId)) return;
      var p = ptrPos(e);
      activePointers.delete(e.pointerId);

      if (pinch) {
        if (activePointers.size < 2) {
          pinch = null;
          // remaining finger continues as pan
          var rest = activePointers.values().next().value;
          panLast = rest || null;
          moveSamples.length = 0;
        }
        requestFrame();
        return;
      }

      if (draggingNode) {
        var tapped =
          downInfo &&
          downInfo.node === draggingNode &&
          Math.hypot(p.x - downInfo.x, p.y - downInfo.y) <
            (e.pointerType === "touch" ? 9 : 5);
        var target = draggingNode;
        endDrag(true);
        canvas.style.cursor = "grab";
        requestFrame();
        if (tapped && target !== centerNode && e.type !== "pointercancel") {
          window.location.href = target.url;
        }
        downInfo = null;
        return;
      }

      if (panLast) {
        panLast = null;
        var vx = 0,
          vy = 0;
        for (var i = 0; i < moveSamples.length; i++) {
          vx += moveSamples[i].dx;
          vy += moveSamples[i].dy;
        }
        if (moveSamples.length) {
          vx /= moveSamples.length;
          vy /= moveSamples.length;
        }
        if (Math.hypot(vx, vy) > 1.5) {
          panVX = vx;
          panVY = vy;
        }
        canvas.style.cursor = hovered ? "pointer" : "grab";
        requestFrame();
      }
      downInfo = null;
    }
    canvas.addEventListener("pointerup", pointerEnd);
    canvas.addEventListener("pointercancel", pointerEnd);

    canvas.addEventListener("pointerleave", function (e) {
      if (activePointers.size > 0) return;
      if (hovered) {
        hovered = null;
        tooltip.hidden = true;
        canvas.style.cursor = "grab";
        requestFrame();
      }
    });

    // wheel zoom: sets an eased target, pivoting at the cursor
    canvas.addEventListener(
      "wheel",
      function (e) {
        if (!nodes.length) return;
        e.preventDefault();
        userTookCamera();
        var p = ptrPos(e);
        var wp = screenToWorld(p.x, p.y);
        zoomAnchor = { sx: p.x, sy: p.y, wx: wp.x, wy: wp.y };
        var factor = Math.pow(
          0.9985,
          e.deltaMode === 1 ? e.deltaY * 20 : e.deltaY,
        );
        zoomTarget = clamp(zoomTarget * factor, MIN_ZOOM, MAX_ZOOM);
        requestFrame();
      },
      { passive: false },
    );
  }

  // Global graph: initialize immediately.
  document.querySelectorAll("[data-graph-source]").forEach(initGraph);

  // Local graph (per-note): lazy — initialize when <details> is opened.
  document.querySelectorAll("[data-lazy-graph]").forEach(function (wrap) {
    var details = wrap.closest("details");
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
    });
  });
})();
