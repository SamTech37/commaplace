# Graph Zoom / Pan Spec

## Problems being fixed

1. **Nodes escape the viewport** — simulation pushes nodes past canvas edges; no way to retrieve them.
2. **No zoom** — large graphs can't be seen all at once.
3. **Pan is missing / dragging canvas background does nothing** — users can only drag individual nodes.
4. **Pan inertia too slippery** — when it does exist, momentum carries too far.

---

## Solution: viewport transform

Wrap every canvas draw call in a `(panX, panY, zoom)` transform:

```
ctx.save()
ctx.translate(panX, panY)
ctx.scale(zoom, zoom)
// draw edges and nodes in world coords
ctx.restore()
// draw overlay buttons in screen coords (outside save/restore)
```

All pointer events must be unmapped to world coordinates before hit-testing nodes:

```
worldX = (screenX - panX) / zoom
worldY = (screenY - panY) / zoom
```

Nodes continue to store `(x, y)` in **world** coordinates. The simulation runs in world space and is completely unaware of the viewport.

---

## Coordinate spaces

| Space  | Description                          | Formula from screen             |
|--------|--------------------------------------|---------------------------------|
| Screen | Pixel on the canvas element          | raw `clientX/Y - rect.left/top` |
| World  | Simulation / node position space     | `(screenX - panX) / zoom`       |

---

## Features

### 1. Drag-to-pan (background drag)

- **Trigger**: mousedown on empty canvas (no node under cursor).
- **Behaviour**: accumulate `(panX, panY)` delta on every mousemove while panning.
- **Inertia**: on mouseup store last-frame velocity; apply `PAN_FRICTION` each frame until velocity is negligible.
- **Friction constant**: `PAN_FRICTION = 0.80` — high friction, viewport stops within ~3–5 frames after release.
- Cursor: `grab` while idle over empty space, `grabbing` while panning.

### 2. Mouse-wheel zoom

- **Event**: `wheel` (passive: false, `preventDefault` to stop page scroll).
- **Formula**: `zoom *= Math.pow(0.998, event.deltaY)` (or `0.001 * deltaY` linear, pick whichever feels right in testing).
- **Clamp**: `zoom = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, zoom))`.
- **Pivot at cursor**: adjust pan so the world point under the cursor stays fixed:
  ```
  // before zoom change:
  worldX = (screenX - panX) / zoom
  // after zoom change:
  panX = screenX - worldX * newZoom
  panY = screenY - worldY * newZoom
  ```

### 3. "Fit all" button

- **Position**: overlaid inside the canvas, top-right corner, drawn in screen space after `ctx.restore()`.
- **Label**: `⊞` or `fit` (decide at implementation).
- **Action**: compute bounding box of all node card corners in world space, then:
  ```
  const padding = 0.10   // 10 % margin on each side
  zoom  = (1 - 2*padding) * Math.min(cw / bbW, ch / bbH)
  panX  = cw/2 - (bbMinX + bbW/2) * zoom
  panY  = ch/2 - (bbMinY + bbH/2) * zoom
  ```
  Clamp zoom to `[MIN_ZOOM, MAX_ZOOM]`.
- **Auto-fit**: call once automatically after the simulation first settles (i.e. when `animating` transitions `true → false` for the first time). Do NOT auto-fit on subsequent settle events (user may have manually panned/zoomed by then).

### 4. Zoom buttons (optional, lower priority)

`+` and `−` buttons overlaid bottom-right, drawn in screen space.  
Each click: `zoom *= 1.25` or `zoom /= 1.25`, pivot at canvas centre.  
Include only if space allows without cluttering the compact widget variant.

---

## Constants

```js
const PAN_FRICTION = 0.80;   // high → stops fast
const MIN_ZOOM     = 0.10;
const MAX_ZOOM     = 4.0;
```

The existing `FRICTION = 0.85` governs the simulation (node velocities), and is **unchanged**.

---

## Interaction table

| Gesture                 | Mode     | Effect                              |
|-------------------------|----------|-------------------------------------|
| Drag on node            | any      | Move node in world space (existing) |
| Drag on empty canvas    | any      | Pan viewport                        |
| Mouse wheel over canvas | any      | Zoom toward cursor                  |
| Click node (< 5 px)     | any      | Navigate to note (existing)         |
| Click "fit" button      | any      | Zoom/pan to show all nodes          |

---

## Hit-testing change (critical)

`nodeAt(p)` currently takes screen coords. After this change it must take **world** coords, or be given coords already converted.  
Recommended: convert once at the event handler and pass world coords down:

```js
function screenToWorld(p) {
  return { x: (p.x - panX) / zoom, y: (p.y - panY) / zoom };
}
```

Overlay button hit-testing stays in screen space.

---

## Boundary fix

With the viewport transform, nodes may legitimately lie outside the visible canvas — that is **correct** and expected for large graphs. The user pans to reach them or clicks "fit all" to see everything. No hard boundary clamping is needed.

---

## Files to change

| File | Change |
|---|---|
| `internal/handlers/static/graph.js` | Add `panX`, `panY`, `zoom`, pan/wheel listeners, draw-transform, fit button |
| `internal/handlers/templates/graph.html` | Description text update (mention scroll-to-zoom, drag-to-pan) |

No backend changes required.

---

## Non-goals

- Pinch-to-zoom (touch) — out of scope for this iteration.
- Minimap — out of scope.
- Persisting zoom/pan across page loads.
