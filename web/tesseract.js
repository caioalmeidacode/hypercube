const Tesseract = (() => {
  const V = [];
  for (let i = 0; i < 16; i++) V.push([(i & 1) ? 1 : -1, (i & 2) ? 1 : -1, (i & 4) ? 1 : -1, (i & 8) ? 1 : -1]);
  const E = [];
  for (let i = 0; i < 16; i++) for (let j = i + 1; j < 16; j++) { const d = i ^ j; if (d && !(d & (d - 1))) E.push([i, j]); }
  const items = new Set();
  let raf = 0;

  function rot(p, a, b, t) {
    const c = Math.cos(t), s = Math.sin(t), x = p[a], y = p[b];
    p[a] = x * c - y * s; p[b] = x * s + y * c;
  }

  function draw(it, time) {
    const { cv, ctx, opts } = it;
    const dpr = window.devicePixelRatio || 1;
    const w = cv.clientWidth, h = cv.clientHeight;
    if (!w || !h) return;
    if (cv.width !== Math.round(w * dpr)) { cv.width = Math.round(w * dpr); cv.height = Math.round(h * dpr); }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    const t = time * 0.00035 * (opts.speed || 1) + (it.phase || 0);
    const pts = V.map(v => {
      const p = v.slice();
      rot(p, 0, 3, t);
      rot(p, 1, 3, t * 0.62);
      rot(p, 2, 3, t * 0.31);
      const k4 = 1 / (3.2 - p[3]);
      let x = p[0] * k4, y = p[1] * k4, z = p[2] * k4;
      const q = [x, y, z];
      rot(q, 0, 2, 0.62 + (opts.spin ? t * 0.3 : 0));
      rot(q, 1, 2, -0.45);
      const k3 = 1 / (2.6 - q[2]);
      const scale = Math.min(w, h) * (opts.scale || 1.05);
      return [w / 2 + q[0] * k3 * scale, h / 2 + q[1] * k3 * scale, q[2], p[3]];
    });
    ctx.lineCap = "round";
    for (const [a, b] of E) {
      const pa = pts[a], pb = pts[b];
      const depth = ((pa[2] + pb[2]) / 2 + 0.6) / 1.2;
      const alpha = 0.18 + 0.82 * Math.max(0, Math.min(1, depth));
      ctx.strokeStyle = `rgba(255,255,255,${alpha.toFixed(3)})`;
      ctx.lineWidth = (opts.line || 1.2) * (0.7 + depth * 0.6);
      ctx.beginPath(); ctx.moveTo(pa[0], pa[1]); ctx.lineTo(pb[0], pb[1]); ctx.stroke();
    }
    if (opts.dots) {
      for (const p of pts) {
        const depth = (p[2] + 0.6) / 1.2;
        ctx.fillStyle = `rgba(255,255,255,${(0.3 + depth * 0.7).toFixed(3)})`;
        ctx.beginPath(); ctx.arc(p[0], p[1], 1.3 + depth * 1.2, 0, Math.PI * 2); ctx.fill();
      }
    }
  }

  function loop(time) {
    raf = 0;
    for (const it of items) {
      if (!it.cv.isConnected) { items.delete(it); continue; }
      draw(it, document.body.classList.contains("reduced") ? 2400 : time);
    }
    if (items.size && !document.hidden) raf = requestAnimationFrame(loop);
  }

  document.addEventListener("visibilitychange", () => { if (!document.hidden && !raf && items.size) raf = requestAnimationFrame(loop); });

  return {
    attach(cv, opts = {}) {
      const it = { cv, ctx: cv.getContext("2d"), opts, phase: Math.random() * 10 };
      items.add(it);
      if (!raf) raf = requestAnimationFrame(loop);
      return () => items.delete(it);
    },
  };
})();
