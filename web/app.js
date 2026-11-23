"use strict";

const qs = new URLSearchParams(location.search);
const TOKEN = qs.get("t") || sessionStorage.getItem("hc_t") || "";
sessionStorage.setItem("hc_t", TOKEN);
if (qs.has("t")) history.replaceState(null, "", "/");

async function api(method, path, body, raw) {
  const opt = { method, headers: { "X-Hypercube": TOKEN } };
  if (body instanceof FormData) opt.body = body;
  else if (raw) { opt.body = body; }
  else if (body !== undefined) { opt.body = JSON.stringify(body); opt.headers["Content-Type"] = "application/json"; }
  const r = await fetch(path, opt);
  let data = null;
  try { data = await r.json(); } catch { }
  if (!r.ok) throw new Error((data && data.error) || `Erro ${r.status}`);
  return data;
}

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const LOADERS = { vanilla: "Vanilla", fabric: "Fabric", forge: "Forge", neoforge: "NeoForge", quilt: "Quilt" };
function fmtNum(n) {
  n = +n || 0;
  if (n >= 1e9) return (n / 1e9).toFixed(1).replace(/\.0$/, "") + "B";
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, "") + "k";
  return String(n);
}
function fmtAgo(ts) {
  if (!ts) return "nunca jogado";
  const s = Date.now() / 1000 - ts;
  if (s < 60) return "agora mesmo";
  if (s < 3600) return `há ${Math.floor(s / 60)} min`;
  if (s < 86400) return `há ${Math.floor(s / 3600)} h`;
  if (s < 86400 * 30) return `há ${Math.floor(s / 86400)} d`;
  return new Date(ts * 1000).toLocaleDateString("pt-BR");
}
function fmtDur(sec) {
  sec = +sec || 0;
  if (sec < 60) return "0 min";
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60);
  return h ? `${h} h ${m} min` : `${m} min`;
}
function fmtBytes(b) {
  if (b > 1 << 30) return (b / (1 << 30)).toFixed(1) + " GB";
  if (b > 1 << 20) return (b / (1 << 20)).toFixed(1) + " MB";
  if (b > 1 << 10) return Math.round(b / 1024) + " KB";
  return b + " B";
}
function debounce(fn, ms) { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; }
function loaderText(i) {
  if (!i.loader || i.loader === "vanilla") return "Vanilla";
  const v = (i.loaderVersion || "").replace(i.mcVersion + "-", "");
  return `${LOADERS[i.loader] || i.loader} ${v}`;
}
const isCustomIcon = i => !!(i.icon && i.icon.startsWith("custom:"));
const instIconURL = i => `/api/instances/${i.id}/icon?v=${encodeURIComponent(i.icon.slice(7))}`;
function pickImage() {
  return new Promise(res => {
    const inp = document.createElement("input");
    inp.type = "file"; inp.accept = "image/png,image/jpeg,image/webp,image/gif";
    inp.onchange = () => {
      const f = inp.files[0];
      if (!f) return res(null);
      if (!/^image\/(png|jpeg|webp|gif)$/.test(f.type)) { toast("Use uma imagem PNG, JPG, WEBP ou GIF", "error"); return res(null); }
      if (f.size > 25 * 1024 * 1024) { toast("A imagem deve ter no máximo 25 MB", "error"); return res(null); }
      res(f);
    };
    inp.click();
  });
}
async function uploadInstanceImage(id, file) {
  await api("POST", `/api/instances/${id}/icon`, file, true);
}
function instIcon(inst, cls = "ico") {
  if (inst.icon && inst.icon.startsWith("custom:")) {
    return `<div class="${cls} custom"><img src="${instIconURL(inst)}" alt=""></div>`;
  }
  return `<div class="${cls}">${glyph(inst.icon || "tesseract")}</div>`;
}

const S = {
  instances: [], running: {}, tasks: {}, settings: {}, profile: { name: "", avatar: "" }, mcAccount: null, version: "",
  route: { view: "library" },
  versions: null,
  mods: null, modsFor: null, filter: "", selected: new Set(), updates: {}, lastSel: null,
  logs: {}, logFollow: true,
  browse: { source: "modrinth", type: "mod", q: "", sort: "relevance", hits: [], total: 0, offset: 0, loading: false, done: false, err: "", inst: null, installing: new Set(), installed: new Set() },
  libFilter: "", libSort: "recent",
};
const inst = id => S.instances.find(i => i.id === id);
const taskFor = id => Object.values(S.tasks).find(t => t.instance === id && t.status === "running");

function toast(text, kind = "info", ms = 4200) {
  const el = document.createElement("div");
  el.className = "toast " + kind;
  el.innerHTML = svg(kind === "error" ? "info" : kind === "ok" ? "check" : "sparkle") + `<div>${esc(text)}</div>`;
  $("#toasts").appendChild(el);
  const close = () => { el.classList.add("out"); setTimeout(() => el.remove(), 260); };
  el.onclick = close;
  setTimeout(close, kind === "error" ? 8000 : ms);
}
const fail = e => toast(e.message || String(e), "error");

function modal({ title, sub = "", body = "", foot = "", wide = false, onMount, onClose }) {
  const ov = document.createElement("div");
  ov.className = "overlay";
  ov.innerHTML = `<div class="modal ${wide ? "wide" : ""}" role="dialog">
    <div class="modal-head"><div><h2>${title}</h2>${sub ? `<p>${sub}</p>` : ""}</div>
    <button class="btn ghost sm icon" data-x>${svg("x")}</button></div>
    <div class="modal-body">${body}</div>${foot ? `<div class="modal-foot">${foot}</div>` : ""}</div>`;
  document.body.appendChild(ov);
  let closed = false;
  const close = () => {
    if (closed) return; closed = true;
    ov.classList.add("out");
    setTimeout(() => ov.remove(), 200);
    onClose && onClose();
    document.removeEventListener("keydown", onKey, true);
  };
  const onKey = e => { if (e.key === "Escape") { e.stopPropagation(); close(); } };
  document.addEventListener("keydown", onKey, true);
  ov.addEventListener("mousedown", e => { if (e.target === ov) close(); });
  $("[data-x]", ov).onclick = close;
  const m = $(".modal", ov);
  onMount && onMount(m, close);
  return { el: m, close };
}

function confirmBox(title, text, okLabel = "Confirmar", danger = false) {
  return new Promise(res => {
    let ok = false;
    modal({
      title, sub: esc(text),
      foot: `<button class="btn ghost" data-c>Cancelar</button><button class="btn ${danger ? "danger" : "primary"}" data-ok>${okLabel}</button>`,
      onMount(m, close) {
        $("[data-c]", m).onclick = close;
        $("[data-ok]", m).onclick = () => { ok = true; close(); };
        setTimeout(() => $("[data-ok]", m).focus(), 50);
      },
      onClose: () => res(ok),
    });
  });
}

function promptBox(title, label, value = "", okLabel = "Salvar") {
  return new Promise(res => {
    let out = null;
    modal({
      title,
      body: `<label class="field"><span>${esc(label)}</span><input class="input" data-in value="${esc(value)}" maxlength="60"></label>`,
      foot: `<button class="btn ghost" data-c>Cancelar</button><button class="btn primary" data-ok>${okLabel}</button>`,
      onMount(m, close) {
        const inp = $("[data-in]", m);
        setTimeout(() => { inp.focus(); inp.select(); }, 60);
        const ok = () => { if (inp.value.trim()) { out = inp.value.trim(); close(); } };
        inp.onkeydown = e => { if (e.key === "Enter") ok(); };
        $("[data-c]", m).onclick = close;
        $("[data-ok]", m).onclick = ok;
      },
      onClose: () => res(out),
    });
  });
}

let openMenuEl = null;
function closeMenu() { if (openMenuEl) { openMenuEl.remove(); openMenuEl = null; } }
function menu(anchor, items, at) {
  closeMenu();
  const el = document.createElement("div");
  el.className = "menu";
  const renderItems = list => {
    el.innerHTML = "";
    for (const it of list) {
      if (!it) continue;
      if (it === "-") { el.appendChild(document.createElement("hr")); continue; }
      if (it.header) { const h = document.createElement("div"); h.className = "mh"; h.textContent = it.header; el.appendChild(h); continue; }
      const b = document.createElement("button");
      if (it.danger) b.className = "danger";
      if (it.on) b.classList.add("on");
      b.innerHTML = (it.icon ? svg(it.icon) : `<span style="width:15px"></span>`) + `<span class="nm2">${esc(it.label)}</span>` +
        (it.on ? `<span class="ck2">${svg("check")}</span>` : "") + (it.sub ? `<span class="ck2" style="transform:rotate(-90deg);display:flex">${svg("chevron")}</span>` : "");
      b.onclick = e => {
        e.stopPropagation();
        if (it.sub) { renderItems([{ label: "Voltar", icon: "chevronLeft", sub: null, back: true, run: () => renderItems(list) }, "-", ...it.sub()]); return; }
        if (it.back) { it.run(); return; }
        closeMenu(); it.run && it.run();
      };
      el.appendChild(b);
    }
  };
  renderItems(items);
  document.body.appendChild(el);
  let x, y;
  if (at) { x = at.x; y = at.y; } else { const r = anchor.getBoundingClientRect(); x = r.left; y = r.bottom + 6; if (anchor.dataset.alignRight !== undefined) x = r.right - el.offsetWidth; }
  const mw = el.offsetWidth, mh = el.offsetHeight;
  if (x + mw > innerWidth - 10) x = innerWidth - mw - 10;
  if (y + mh > innerHeight - 10) y = Math.max(10, (at ? at.y : anchor.getBoundingClientRect().top - 6) - mh);
  el.style.left = x + "px"; el.style.top = y + "px";
  openMenuEl = el;
}
document.addEventListener("mousedown", e => { if (openMenuEl && !openMenuEl.contains(e.target)) closeMenu(); });
window.addEventListener("blur", closeMenu);

function placePills(root = document) {
  requestAnimationFrame(() => {
    $$(".seg", root).forEach(seg => {
      const on = $("button.on", seg), pill = $(".pill", seg);
      if (!pill) return;
      if (!on) { pill.style.width = "0px"; return; }
      pill.style.left = on.offsetLeft + "px"; pill.style.width = on.offsetWidth + "px";
    });
    $$(".tabs", root).forEach(tabs => {
      const on = $("button.on", tabs), ink = $(".ink", tabs);
      if (on && ink) { ink.style.left = on.offsetLeft + "px"; ink.style.width = on.offsetWidth + "px"; }
    });
  });
}
function segHTML(name, options, value) {
  return `<div class="seg" data-seg="${name}"><span class="pill"></span>${options.map(([v, l, ic]) =>
    `<button data-v="${esc(v)}" class="${v === value ? "on" : ""}">${ic ? svg(ic) : ""}${esc(l)}</button>`).join("")}</div>`;
}
function bindSeg(root, name, fn) {
  const seg = $(`[data-seg="${name}"]`, root);
  if (!seg) return;
  seg.addEventListener("click", e => {
    const b = e.target.closest("button"); if (!b) return;
    $$("button", seg).forEach(x => x.classList.toggle("on", x === b));
    placePills(seg.parentElement);
    fn(b.dataset.v);
  });
}

let sideCanvasStop = null;
function renderSidebar() {
  const sb = $("#sidebar");
  if (!$(".brand", sb)) {
    sb.innerHTML = `
      <div class="brand"><canvas id="logo"></canvas><div><div class="word">Hypercube</div><div class="ver mono" id="ver"></div></div></div>
      <nav class="nav" id="nav"></nav>
      <div class="nav-label"><span>Instâncias</span><button title="Nova instância" id="side-new">${svg("plus")}</button></div>
      <div class="side-instances" id="side-insts"></div>
      <div class="side-bottom"><div class="tasks" id="tasks"></div><div id="acct"></div></div>`;
    sideCanvasStop = Tesseract.attach($("#logo"), { line: 1.15, scale: 1.25 });
    $("#side-new").onclick = openCreate;
  }
  $("#ver").textContent = "v" + S.version;
  const r = S.route;
  const navs = [["library", "Biblioteca", "library"], ["explore", "Explorar", "compass"], ["settings", "Configurações", "settings"]];
  $("#nav").innerHTML = navs.map(([v, l, ic]) => `<button class="nav-item ${r.view === v ? "active" : ""}" data-nav="${v}">${svg(ic)}<span>${l}</span></button>`).join("");
  $$("#nav [data-nav]").forEach(b => b.onclick = () => go({ view: b.dataset.nav }));
  renderSideInstances();
  renderProfileChip();
  renderTasks();
}
function renderSideInstances() {
  const el = $("#side-insts"); if (!el) return;
  el.innerHTML = S.instances.map(i => `<button class="side-inst ${S.route.view === "instance" && S.route.id === i.id ? "active" : ""}" data-id="${i.id}">
    ${instIcon(i)}<span class="nm">${esc(i.name)}</span>${S.running[i.id] ? `<span class="dot-run"></span>` : ""}</button>`).join("") ||
    `<div class="hint" style="padding:6px 11px">Nenhuma instância ainda</div>`;
  $$(".side-inst", el).forEach(b => b.onclick = () => go({ view: "instance", id: b.dataset.id, tab: "mods" }));
}
function avatarHTML(cls = "avatar") {
  const p = S.profile;
  return `<div class="${cls}">${p.avatar ? `<img src="${esc(p.avatar)}" alt="">` : `<span>${esc((p.name || "?")[0].toUpperCase())}</span>`}</div>`;
}
function renderProfileChip() {
  const el = $("#acct"); if (!el) return;
  const p = S.profile;
  el.innerHTML = `<button class="account" id="acct-btn">${avatarHTML()}<div style="min-width:0;flex:1">
    <div class="an">${esc(p.name || "Sem perfil")}</div><div class="at">${S.mcAccount ? "Minecraft: " + esc(S.mcAccount.name) : "Conta Minecraft não conectada"}</div></div></button>`;
  $("#acct-btn").onclick = e => menu(e.currentTarget, [
    { header: p.name },
    { label: "Editar perfil", icon: "edit", run: () => openProfileEditor() },
    S.mcAccount ? { label: `Conta Minecraft: ${S.mcAccount.name}`, icon: "user", run: () => go({ view: "settings" }) } : { label: "Conectar conta Minecraft", icon: "microsoft", run: startMCLogin },
    { label: "Exportar backup", icon: "download", run: () => exportBackup(true) },
    { label: "Importar backup", icon: "upload", run: () => pickBackup(false) },
  ]);
}
function renderTasks() {
  const el = $("#tasks"); if (!el) return;
  const list = Object.values(S.tasks);
  const ids = new Set(list.map(t => t.id));
  $$(".task", el).forEach(n => { if (!ids.has(n.dataset.id)) n.remove(); });
  for (const t of list) {
    let n = $(`.task[data-id="${t.id}"]`, el);
    if (!n) { n = document.createElement("div"); n.className = "task"; n.dataset.id = t.id; n.innerHTML = `<div class="tt"><span class="a"></span><span class="b mono muted"></span></div><div class="td"></div><div class="bar"><i></i></div>`; el.appendChild(n); }
    n.classList.toggle("error", t.status === "error");
    $(".a", n).textContent = t.title;
    const pct = t.total > 0 ? Math.min(1, t.done / t.total) : 0;
    $(".b", n).textContent = t.status === "error" ? "erro" : t.status === "done" ? "ok" : t.total > 0 ? Math.round(pct * 100) + "%" : "";
    $(".td", n).textContent = t.status === "error" ? t.error : t.detail || "";
    const bar = $(".bar", n);
    bar.classList.toggle("indet", t.status === "running" && !(t.total > 0));
    bar.style.display = t.status === "running" ? "" : "none";
    $("i", n).style.setProperty("--v", pct);
  }
}

function go(route) {
  const prev = S.route;
  S.route = { tab: "mods", ...route };
  if (route.view === "instance" && (prev.view !== "instance" || prev.id !== route.id)) {
    S.mods = null; S.modsFor = null; S._modsShown = false; S.selected.clear(); S.filter = ""; S.updates = {};
    S.browse.hits = []; S.browse.offset = 0; S.browse.done = false; S.browse.inst = route.id;
  }
  renderSidebar();
  renderMain(true);
}

function renderMain(animate) {
  const main = $("#main");
  const keepScroll = !animate && $(".view", main) ? $(".view", main).scrollTop : 0;
  const v = document.createElement("div");
  v.className = "view" + (animate ? " enter" : "");
  main.innerHTML = "";
  main.appendChild(v);
  const r = S.route;
  if (r.view === "library") viewLibrary(v, animate);
  else if (r.view === "instance") viewInstance(v, animate);
  else if (r.view === "explore") viewExplore(v);
  else if (r.view === "settings") viewSettings(v);
  if (keepScroll) v.scrollTop = keepScroll;
  placePills(v);
}

function viewLibrary(v, animate) {
  let list = S.instances.filter(i => !S.libFilter || (i.name + " " + i.mcVersion + " " + i.loader).toLowerCase().includes(S.libFilter.toLowerCase()));
  if (S.libSort === "name") list = [...list].sort((a, b) => a.name.localeCompare(b.name));
  if (S.libSort === "version") list = [...list].sort((a, b) => b.mcVersion.localeCompare(a.mcVersion, undefined, { numeric: true }));
  const total = S.instances.length;
  v.innerHTML = `
    <div class="page-head"><div><h1>Biblioteca</h1><div class="sub">${total ? `${total} instância${total > 1 ? "s" : ""}` : "Seu espaço para mundos modificados"}</div></div>
      <button class="btn primary" id="new-inst">${svg("plus")}Nova instância</button></div>
    ${total ? `<div class="lib-tools">
      <div class="search"><input class="input" id="lib-q" placeholder="Buscar instâncias" value="${esc(S.libFilter)}">${svg("search")}</div>
      ${segHTML("libsort", [["recent", "Recentes"], ["name", "Nome"], ["version", "Versão"]], S.libSort)}
    </div>
    <div class="grid" id="lib-grid">${list.map((i, n) => cardHTML(i, animate ? n : -1)).join("")}
      ${!S.libFilter ? `<button class="card new" id="new-card" style="--i:${list.length}"><div><div class="plus">${svg("plus")}</div><div style="font-weight:500">Nova instância</div><div class="hint" style="margin-top:3px">Versão + modloader</div></div></button>` : ""}
    </div>` : `<div class="empty"><canvas id="empty-cv"></canvas><h3>Crie sua primeira instância</h3><p>Escolha uma versão do Minecraft e um modloader — Fabric, Forge, NeoForge ou Quilt.</p>
      <button class="btn primary lg" id="new-inst2">${svg("plus")}Nova instância</button></div>`}`;
  $("#new-inst").onclick = openCreate;
  const n2 = $("#new-inst2", v); if (n2) { n2.onclick = openCreate; Tesseract.attach($("#empty-cv", v), { line: 1.4, dots: true, scale: 1.1 }); }
  const nc = $("#new-card", v); if (nc) nc.onclick = openCreate;
  const q = $("#lib-q", v);
  if (q) q.oninput = debounce(() => { S.libFilter = q.value; renderMain(false); setTimeout(() => { const n = $("#lib-q"); n.focus(); n.setSelectionRange(n.value.length, n.value.length); }); }, 120);
  bindSeg(v, "libsort", val => { S.libSort = val; renderMain(false); });
  bindCards(v);
}

function cardHTML(i, n) {
  const run = S.running[i.id], t = taskFor(i.id);
  const pct = t && t.total > 0 ? t.done / t.total : 0;
  return `<div class="card ${n < 0 ? "" : ""}" data-id="${i.id}" style="--i:${n < 0 ? 0 : n};${n < 0 ? "animation:none" : ""}">
    <div class="art">${isCustomIcon(i) ? `<img class="art-img" src="${instIconURL(i)}" alt="">` : `<div class="grid-bg"></div>${instIcon(i)}`}
      <button class="play-fab ${run ? "stop" : ""}" data-play="${i.id}" title="${run ? "Parar" : "Jogar"}">${svg(run ? "stop" : "play")}</button></div>
    <div class="nm">${esc(i.name)}</div>
    <div class="meta"><span class="mono">${esc(i.mcVersion)}</span><span>·</span><span>${esc(loaderText(i))}</span></div>
    <div class="foot">${run ? `<span class="running-badge"><span class="dot-run"></span>Em execução</span>` : t ? `<span>${esc(t.detail || t.title)}</span>` : `<span>${fmtAgo(i.lastPlayed)}</span>`}
      <span class="mono">${i.playtime ? fmtDur(i.playtime) : ""}</span></div>
    ${t ? `<div class="bar ${t.total > 0 ? "" : "indet"}"><i style="--v:${pct}"></i></div>` : ""}
  </div>`;
}

function bindCards(root) {
  $$(".card[data-id]", root).forEach(c => {
    c.onclick = e => { if (e.target.closest("[data-play]")) return; go({ view: "instance", id: c.dataset.id, tab: "mods" }); };
    c.onmousemove = e => { const r = c.getBoundingClientRect(); c.style.setProperty("--mx", (e.clientX - r.left) + "px"); c.style.setProperty("--my", (e.clientY - r.top) + "px"); };
    c.oncontextmenu = e => { e.preventDefault(); instanceMenu(c.dataset.id, null, { x: e.clientX, y: e.clientY }); };
  });
  $$("[data-play]", root).forEach(b => b.onclick = e => { e.stopPropagation(); togglePlay(b.dataset.play); });
}

function updateCard(id) {
  const old = $(`.card[data-id="${id}"]`);
  const i = inst(id);
  if (!old || !i) return;
  const tmp = document.createElement("div");
  tmp.innerHTML = cardHTML(i, -1);
  const n = tmp.firstElementChild;
  old.replaceWith(n);
  bindCards(n.parentElement);
}

function instanceMenu(id, anchor, at) {
  const i = inst(id);
  if (!i) return;
  menu(anchor, [
    { label: S.running[id] ? "Parar" : "Jogar", icon: S.running[id] ? "stop" : "play", run: () => togglePlay(id) },
    { label: "Abrir pasta", icon: "folder", run: () => api("POST", `/api/instances/${id}/open`).catch(fail) },
    { label: "Trocar imagem / GIF", icon: "upload", run: () => changeInstanceImage(id) },
    { label: "Duplicar", icon: "dup", run: () => duplicateInstance(id) },
    { label: "Reparar arquivos", icon: "wrench", run: () => api("POST", `/api/instances/${id}/install`).then(() => toast("Verificando arquivos…")).catch(fail) },
    "-",
    { label: "Excluir instância", icon: "trash", danger: true, run: () => deleteInstance(id) },
  ], at);
}

async function changeInstanceImage(id) {
  const f = await pickImage(); if (!f) return;
  try {
    await uploadInstanceImage(id, f);
    toast("Imagem do modpack atualizada", "ok");
    setTimeout(() => { if (S.route.tab === "config" && S.route.id === id) renderTab(); }, 150);
  } catch (e) { fail(e); }
}

async function togglePlay(id) {
  try {
    if (S.running[id]) { await api("POST", `/api/instances/${id}/kill`); return; }
    if (!S.profile.name) { showOnboarding(); return; }
    if (!S.mcAccount) { needAccountPrompt(); return; }
    await api("POST", `/api/instances/${id}/launch`);
    toast(`Iniciando ${inst(id)?.name || ""}…`);
  } catch (e) { fail(e); }
}

async function deleteInstance(id) {
  const i = inst(id);
  if (!(await confirmBox("Excluir instância?", `“${i.name}” e todos os mundos, mods e configs dela serão apagados permanentemente.`, "Excluir", true))) return;
  try {
    await api("DELETE", `/api/instances/${id}`);
    S.instances = S.instances.filter(x => x.id !== id);
    if (S.route.id === id) go({ view: "library" }); else { renderSidebar(); renderMain(false); }
    toast("Instância excluída");
  } catch (e) { fail(e); }
}
async function duplicateInstance(id) {
  try { const n = await api("POST", `/api/instances/${id}/duplicate`); toast("Instância duplicada", "ok"); await refreshState(); go({ view: "instance", id: n.id }); }
  catch (e) { fail(e); }
}

async function loadVersions() {
  if (S.versions) return S.versions;
  S.versions = await api("GET", "/api/versions");
  return S.versions;
}

function openCreate(editId) {
  const editing = typeof editId === "string" ? inst(editId) : null;
  const st = {
    name: editing ? editing.name : "", icon: editing ? (editing.icon || "tesseract") : GLYPHS[Math.floor(Math.random() * 8)],
    mc: editing ? editing.mcVersion : "", loader: editing ? editing.loader : "fabric", lv: editing ? editing.loaderVersion : "",
    vfilter: "release", vq: "", lvs: null, lvErr: "",
  };
  const body = `<div class="create">
    <div>
      <div class="preview-ico"><div class="grid-bg" style="position:absolute;inset:0;background-image:linear-gradient(var(--line) 1px,transparent 1px),linear-gradient(90deg,var(--line) 1px,transparent 1px);background-size:16px 16px;mask-image:radial-gradient(circle,#000,transparent 70%)"></div><div class="ico" id="c-prev"></div></div>
      ${editing ? "" : `<label class="field" style="margin-bottom:14px"><span>Nome</span><input class="input" id="c-name" placeholder="Minha instância" maxlength="48"></label>
      <div class="field-label" style="margin-bottom:7px">Ícone</div><div class="icon-picker" id="c-icons"></div>`}
    </div>
    <div style="display:flex;flex-direction:column;gap:16px;min-width:0">
      <div><div class="field-label" style="margin-bottom:7px">Modloader</div><div class="loader-grid" id="c-loaders">${Object.entries(LOADERS).map(([k, l]) => `<button data-l="${k}">${svg(k)}${l}</button>`).join("")}</div></div>
      <div>
        <div class="row" style="margin-bottom:8px"><div class="field-label grow">Versão do Minecraft</div>${segHTML("vf", [["release", "Releases"], ["snapshot", "Snapshots"], ["old", "Antigas"]], "release")}</div>
        <div class="search" style="margin-bottom:8px"><input class="input" id="c-vq" placeholder="Filtrar versões (ex.: 1.20.1)">${svg("search")}</div>
        <div class="vlist" id="c-vlist"><div class="loading"><div class="spinner"></div></div></div>
      </div>
      <label class="field" id="c-lvwrap"><span>Versão do loader</span><select class="select input" id="c-lv"></select><span class="hint" id="c-lvhint"></span></label>
    </div></div>`;
  modal({
    title: editing ? "Alterar versão" : "Nova instância",
    sub: editing ? "Os arquivos do jogo serão reinstalados. Mods e mundos são mantidos." : "Escolha a versão e o modloader — o Hypercube baixa tudo o que for preciso.",
    wide: true, body,
    foot: `<span class="hint grow" id="c-sum"></span><button class="btn ghost" data-c>Cancelar</button><button class="btn primary" id="c-ok" disabled>${editing ? "Aplicar" : svg("plus") + "Criar instância"}</button>`,
    onMount(m, close) {
      $("[data-c]", m).onclick = close;
      const prev = () => {
        const p = $("#c-prev", m);
        p.classList.toggle("custom", !!st.file);
        p.innerHTML = st.file ? `<img src="${URL.createObjectURL(st.file)}" alt="">` : glyph(st.icon);
      };
      prev();
      if (!editing) {
        $("#c-icons", m).innerHTML = `<button data-custom class="custom-tile" title="Imagem ou GIF próprio">${svg("upload")}</button>` + GLYPHS.map(g => `<button data-g="${g}" class="${g === st.icon ? "on" : ""}" title="${g}">${glyph(g)}</button>`).join("");
        $("#c-icons", m).onclick = async e => {
          const b = e.target.closest("button"); if (!b) return;
          if (b.dataset.custom !== undefined) {
            const f = await pickImage(); if (!f) return;
            st.file = f;
            b.innerHTML = `<img src="${URL.createObjectURL(f)}" alt="">`;
          } else { st.icon = b.dataset.g; st.file = null; $("[data-custom]", m).innerHTML = svg("upload"); }
          $$("#c-icons button", m).forEach(x => x.classList.toggle("on", x === b)); prev();
        };
        $("#c-name", m).oninput = e => { st.name = e.target.value; sum(); };
        setTimeout(() => $("#c-name", m).focus(), 80);
      }
      const sum = () => {
        const ok = st.mc && (st.loader === "vanilla" || st.lv);
        $("#c-ok", m).disabled = !ok;
        $("#c-sum", m).innerHTML = st.mc ? `<span class="mono">${esc(st.mc)}</span> · ${LOADERS[st.loader]}${st.loader !== "vanilla" && st.lv ? " " + esc(st.lv.replace(st.mc + "-", "")) : ""}` : "";
      };
      const setLoader = l => {
        st.loader = l;
        $$("#c-loaders button", m).forEach(b => b.classList.toggle("on", b.dataset.l === l));
        loadLV();
      };
      const loadLV = async () => {
        const wrap = $("#c-lvwrap", m), sel = $("#c-lv", m), hint = $("#c-lvhint", m);
        if (st.loader === "vanilla") { wrap.style.display = "none"; st.lv = ""; sum(); return; }
        wrap.style.display = "";
        if (!st.mc) { sel.innerHTML = `<option>Escolha uma versão primeiro</option>`; sel.disabled = true; hint.textContent = ""; st.lv = ""; sum(); return; }
        sel.disabled = true; sel.innerHTML = `<option>Carregando…</option>`; hint.textContent = "";
        const want = st.loader + "@" + st.mc;
        try {
          const list = await api("GET", `/api/loaders/${st.loader}?mc=${encodeURIComponent(st.mc)}`);
          if (want !== st.loader + "@" + st.mc) return;
          if (!list.length) { sel.innerHTML = `<option>Indisponível</option>`; st.lv = ""; hint.textContent = `${LOADERS[st.loader]} não tem versões para o Minecraft ${st.mc}.`; sum(); return; }
          sel.disabled = false;
          const keep = editing && editing.loader === st.loader && editing.mcVersion === st.mc ? editing.loaderVersion : "";
          const rec = list.find(x => x.recommended) || list[0];
          sel.innerHTML = list.slice(0, 300).map(x => `<option value="${esc(x.version)}">${esc(x.version.replace(st.mc + "-", ""))}${x.recommended ? "  ★ recomendada" : x.stable ? "" : "  (beta)"}</option>`).join("");
          sel.value = keep || rec.version;
          st.lv = sel.value;
          hint.textContent = `${list.length} versões disponíveis`;
        } catch (e) { sel.innerHTML = `<option>Erro</option>`; hint.textContent = e.message; st.lv = ""; }
        sum();
      };
      $$("#c-loaders button", m).forEach(b => b.classList.toggle("on", b.dataset.l === st.loader));
      $("#c-lv", m).onchange = e => { st.lv = e.target.value; sum(); };
      $("#c-loaders", m).onclick = e => { const b = e.target.closest("button"); if (b) setLoader(b.dataset.l); };
      const renderV = () => {
        const box = $("#c-vlist", m);
        if (!S.versions) return;
        const q = st.vq.trim().toLowerCase();
        const vs = S.versions.versions.filter(v => (st.vfilter === "old" ? v.type.startsWith("old") : v.type === st.vfilter) && (!q || v.id.toLowerCase().includes(q)));
        box.innerHTML = vs.map(v => `<button data-v="${esc(v.id)}" class="${v.id === st.mc ? "on" : ""}"><span class="mono">${esc(v.id)}</span><span class="vt">${v.id === S.versions.latest.release ? "mais recente" : esc(new Date(v.releaseTime).getFullYear())}</span></button>`).join("") || `<div class="loading">Nada encontrado</div>`;
        const on = $("button.on", box); if (on) on.scrollIntoView({ block: "center" });
      };
      $("#c-vlist", m).onclick = e => {
        const b = e.target.closest("button[data-v]"); if (!b) return;
        st.mc = b.dataset.v; $$("#c-vlist button", m).forEach(x => x.classList.toggle("on", x === b));
        loadLV(); sum();
      };
      $("#c-vq", m).oninput = e => { st.vq = e.target.value; renderV(); };
      bindSeg(m, "vf", val => { st.vfilter = val; renderV(); });
      placePills(m);
      loadVersions().then(() => {
        if (!st.mc && !editing) st.mc = S.versions.latest.release;
        renderV(); setLoader(st.loader);
      }).catch(e => { $("#c-vlist", m).innerHTML = `<div class="loading" style="padding:20px;text-align:center">${esc(e.message)}</div>`; });
      $("#c-ok", m).onclick = async () => {
        $("#c-ok", m).disabled = true;
        try {
          if (editing) {
            await api("PATCH", `/api/instances/${editing.id}`, { mcVersion: st.mc, loader: st.loader, loaderVersion: st.loader === "vanilla" ? "" : st.lv });
            close(); toast("Reinstalando com a nova versão…");
          } else {
            const created = await api("POST", "/api/instances", { name: st.name, icon: st.icon, mcVersion: st.mc, loader: st.loader, loaderVersion: st.loader === "vanilla" ? "" : st.lv });
            if (st.file) { try { await uploadInstanceImage(created.id, st.file); created.icon = "custom:" + Date.now(); } catch (e) { fail(e); } }
            close();
            if (!S.instances.find(x => x.id === created.id)) S.instances.unshift(created);
            go({ view: "instance", id: created.id, tab: "mods" });
          }
        } catch (e) { fail(e); $("#c-ok", m).disabled = false; }
      };
      sum();
    },
  });
}

function viewInstance(v, animate) {
  const i = inst(S.route.id);
  if (!i) { v.innerHTML = `<div class="empty"><h3>Instância não encontrada</h3></div>`; return; }
  const tab = S.route.tab || "mods";
  v.innerHTML = `
    <button class="back" id="back">${svg("chevronLeft")}Biblioteca</button>
    <div class="hero" id="hero"></div>
    <div class="tabs" id="tabs"><span class="ink"></span>
      <button data-tab="mods" class="${tab === "mods" ? "on" : ""}">${svg("box")}Mods <span class="count" id="mods-count"></span></button>
      <button data-tab="browse" class="${tab === "browse" ? "on" : ""}">${svg("compass")}Explorar</button>
      <button data-tab="logs" class="${tab === "logs" ? "on" : ""}">${svg("terminal")}Console</button>
      <button data-tab="config" class="${tab === "config" ? "on" : ""}">${svg("sliders")}Ajustes</button>
    </div>
    <div id="tab-body" class="tab-body"></div>`;
  $("#back", v).onclick = () => go({ view: "library" });
  $$("#tabs [data-tab]", v).forEach(b => b.onclick = () => {
    if (S.route.tab === b.dataset.tab) return;
    S.route.tab = b.dataset.tab;
    $$("#tabs [data-tab]", v).forEach(x => x.classList.toggle("on", x === b));
    placePills(v);
    renderTab();
  });
  renderHero();
  renderTab();
}

function renderHero() {
  const el = $("#hero"); if (!el) return;
  const i = inst(S.route.id); if (!i) return;
  const run = S.running[i.id], t = taskFor(i.id);
  const pct = t && t.total > 0 ? t.done / t.total : 0;
  el.innerHTML = `<button class="hero-ico" id="h-ico" title="Trocar imagem do modpack">${instIcon(i)}<span>${svg("upload")}</span></button>
    <div class="grow" style="min-width:0">
      <h1>${esc(i.name)}</h1>
      <div class="chips"><span class="chip solid mono">${esc(i.mcVersion)}</span><span class="chip">${svg(i.loader || "vanilla")}${esc(loaderText(i))}</span>
      <span class="chip">${svg("clock")}${i.playtime ? fmtDur(i.playtime) : "0 min"}</span><span class="chip">${fmtAgo(i.lastPlayed)}</span>
      ${!i.installed && !t ? `<span class="chip">Não instalada</span>` : ""}</div>
    </div>
    <div class="row">
      <button class="btn icon" id="h-folder" title="Abrir pasta">${svg("folder")}</button>
      <button class="btn icon" id="h-more" data-align-right title="Mais">${svg("more")}</button>
      <button class="play ${run ? "running" : t ? "busy" : ""}" id="h-play">
        <span class="fill" style="transform:scaleX(${pct})"></span>
        <span class="lbl">${run ? svg("stop") + "Parar" : t ? `<div class="spinner"></div>${t.total > 0 ? Math.round(pct * 100) + "%" : "Preparando"}` : svg("play") + "Jogar"}</span>
      </button>
    </div>`;
  $("#h-play").onclick = () => togglePlay(i.id);
  $("#h-ico").onclick = () => changeInstanceImage(i.id);
  $("#h-folder").onclick = () => api("POST", `/api/instances/${i.id}/open`).catch(fail);
  $("#h-more").onclick = e => instanceMenu(i.id, e.currentTarget);
}

function renderTab() {
  const body = $("#tab-body"); if (!body) return;
  body.classList.remove("tab-body"); void body.offsetWidth; body.classList.add("tab-body");
  const tab = S.route.tab;
  if (tab === "mods") tabMods(body);
  else if (tab === "browse") tabBrowse(body, S.route.id);
  else if (tab === "logs") tabLogs(body);
  else if (tab === "config") tabConfig(body);
}

async function loadMods(id) {
  const d = await api("GET", `/api/instances/${id}/mods`);
  if (S.route.id !== id) return;
  S.mods = d; S.modsFor = id;
  S.browse.installed = new Set(d.mods.filter(m => m.projectId).map(m => m.projectId));
}

function tabMods(body) {
  const id = S.route.id;
  body.innerHTML = `
    <div class="toolbar">
      <div class="dropdown"><button class="ml-btn" id="ml-btn">${svg("layers")}<span class="k">Modlist</span><span class="t" id="ml-name">…</span>${svg("chevron")}</button></div>
      <div class="search"><input class="input" id="mod-q" placeholder="Filtrar mods" value="${esc(S.filter)}">${svg("search")}<kbd>Ctrl F</kbd></div>
      <button class="btn" id="cat-new">${svg("tag")}Categoria</button>
      <button class="btn" id="upd-btn" title="Procurar atualizações">${svg("refresh")}Atualizações</button>
      <button class="btn icon" id="add-file" title="Adicionar .jar">${svg("upload")}</button>
      <button class="btn primary" id="go-browse">${svg("plus")}Adicionar mods</button>
      <input type="file" id="file-in" accept=".jar,.zip" multiple hidden>
    </div>
    <div class="row" style="margin:-4px 0 12px"><span class="stats" id="mod-stats"></span></div>
    <div id="mod-groups"><div class="skeleton" style="height:64px;margin-bottom:8px"></div><div class="skeleton" style="height:64px;margin-bottom:8px;opacity:.6"></div><div class="skeleton" style="height:64px;opacity:.3"></div></div>`;
  const q = $("#mod-q", body);
  q.oninput = debounce(() => { S.filter = q.value; renderModGroups(false); }, 90);
  $("#cat-new", body).onclick = newCategory;
  $("#go-browse", body).onclick = () => { $(`#tabs [data-tab="browse"]`).click(); };
  $("#add-file", body).onclick = () => $("#file-in", body).click();
  $("#file-in", body).onchange = e => uploadJars([...e.target.files]);
  $("#upd-btn", body).onclick = checkUpdates;
  $("#ml-btn", body).onclick = e => modlistMenu(e.currentTarget);
  if (S.mods && S.modsFor === id) renderModGroups(true);
  loadMods(id).then(() => renderModGroups(!S._modsShown)).catch(fail);
}

function renderModGroups(animate) {
  const el = $("#mod-groups"); if (!el || !S.mods) return;
  S._modsShown = true;
  const d = S.mods;
  const ml = d.modlists.find(x => x.id === d.activeModlist) || d.modlists[0];
  $("#ml-name").textContent = ml ? ml.name : "";
  const enabled = d.mods.filter(m => m.enabled).length;
  $("#mods-count").textContent = d.mods.length || "";
  $("#mod-stats").innerHTML = d.mods.length ? `<b>${d.mods.length}</b> mods · <b>${enabled}</b> ativos${d.categories.length ? ` · ${d.categories.length} categorias` : ""}${Object.keys(S.updates).length ? ` · <b>${Object.keys(S.updates).length}</b> atualizações` : ""}` : "";
  const f = S.filter.trim().toLowerCase();
  const match = m => !f || [m.name, m.modId, m.description, (m.authors || []).join(" "), m.file].join(" ").toLowerCase().includes(f);
  const mods = d.mods.filter(match);
  const catIds = new Set(d.categories.map(c => c.id));
  const groups = d.categories.map(c => ({ ...c, mods: mods.filter(m => m.category === c.id) }));
  groups.push({ id: "", name: d.categories.length ? "Sem categoria" : "Mods", collapsed: !!S._uncatCollapsed, mods: mods.filter(m => !m.category || !catIds.has(m.category)) });
  if (!d.mods.length) {
    el.innerHTML = `<div class="empty" style="padding:50px 20px"><canvas id="mods-empty"></canvas><h3>Nenhum mod por aqui</h3>
      <p>Explore o Modrinth/CurseForge ou arraste arquivos <span class="mono">.jar</span> para esta janela.</p>
      <button class="btn primary" id="e-browse">${svg("compass")}Explorar mods</button></div>`;
    Tesseract.attach($("#mods-empty", el), { line: 1.2, scale: 1 });
    $("#e-browse", el).onclick = () => $(`#tabs [data-tab="browse"]`).click();
    return;
  }
  let n = 0;
  el.classList.toggle("selecting", S.selected.size > 0);
  el.innerHTML = groups.filter(g => g.id === "" ? g.mods.length || !f : (!f || g.mods.length)).map(g => `
    <section class="group ${g.collapsed && !f ? "collapsed" : ""}" data-cat="${g.id}">
      <div class="group-head" data-cathead="${g.id}" ${g.id ? 'draggable="true"' : ""}>
        ${g.id ? `<span class="grip">${svg("grip")}</span>` : `<span class="grip"></span>`}
        <span class="chev">${svg("chevron")}</span><span class="gn">${esc(g.name)}</span><span class="gc">${g.mods.length}</span><span class="line"></span>
        <span class="acts">${g.id ? `<button class="btn ghost sm icon" data-catren="${g.id}" title="Renomear">${svg("edit")}</button><button class="btn ghost sm icon" data-catdel="${g.id}" title="Excluir categoria">${svg("trash")}</button>` : ""}
          <button class="btn ghost sm" data-catall="${g.id}">Selecionar</button></span>
      </div>
      <div class="group-body"><div>${g.mods.length ? g.mods.map(m => modHTML(m, animate ? n++ : -1)).join("") : `<div class="group-empty">Arraste mods para cá</div>`}</div></div>
    </section>`).join("");
  bindModGroups(el);
  renderSelbar();
}

function modHTML(m, n) {
  const up = S.updates[m.key];
  const icon = m.icon ? `<img src="${esc(m.icon)}" class="${m.icon.startsWith("http") ? "smooth" : ""}" alt="" loading="lazy" onerror="this.remove()">` : svg("box");
  const src = m.source === "modrinth" ? "Modrinth" : m.source === "curseforge" ? "CurseForge" : "Local";
  return `<div class="mod ${m.enabled ? "" : "off"} ${S.selected.has(m.key) ? "sel" : ""} ${n < 0 ? "noanim" : ""}" draggable="true" data-key="${esc(m.key)}" style="--i:${Math.max(0, n)}">
    <span class="ck">${svg("check")}</span>
    <div class="mi">${icon}</div>
    <div class="info">
      <div class="t1"><span class="mn">${esc(m.name)}</span><span class="mv">${esc(m.version || "")}</span></div>
      <div class="md">${esc(m.description || m.file)}</div>
      <div class="ma">${m.authors && m.authors.length ? `<span>por ${esc(m.authors.slice(0, 3).join(", "))}</span>` : ""}<span class="tag">${src}</span>${m.dependency ? `<span class="tag">dependência</span>` : ""}<span>${fmtBytes(m.size)}</span></div>
    </div>
    <div class="right">${up ? `<button class="btn sm upd" data-upd="${esc(m.key)}" title="Atualizar para ${esc(up.version)}">${svg("update")}${esc(up.version)}</button>` : ""}
      <button class="switch ${m.enabled ? "on" : ""}" data-toggle="${esc(m.key)}" title="${m.enabled ? "Desativar" : "Ativar"}"></button></div>
    <button class="btn ghost sm icon menu-btn" data-mmenu="${esc(m.key)}">${svg("more")}</button>
  </div>`;
}

function modByKey(k) { return S.mods && S.mods.mods.find(m => m.key === k); }

function bindModGroups(el) {
  const id = S.route.id;
  el.onclick = async e => {
    const t = e.target;
    const tg = t.closest("[data-toggle]");
    if (tg) {
      e.stopPropagation();
      const m = modByKey(tg.dataset.toggle);
      const keys = S.selected.has(m.key) && S.selected.size > 1 ? [...S.selected] : [m.key];
      setEnabled(keys, !m.enabled);
      return;
    }
    const mm = t.closest("[data-mmenu]");
    if (mm) { e.stopPropagation(); modMenu(mm.dataset.mmenu, mm); return; }
    const up = t.closest("[data-upd]");
    if (up) { e.stopPropagation(); doUpdate(up.dataset.upd); return; }
    const ren = t.closest("[data-catren]");
    if (ren) { e.stopPropagation(); renameCategory(ren.dataset.catren); return; }
    const del = t.closest("[data-catdel]");
    if (del) { e.stopPropagation(); deleteCategory(del.dataset.catdel); return; }
    const all = t.closest("[data-catall]");
    if (all) {
      e.stopPropagation();
      const sec = all.closest(".group");
      const keys = $$(".mod", sec).map(x => x.dataset.key);
      const allSel = keys.every(k => S.selected.has(k));
      keys.forEach(k => allSel ? S.selected.delete(k) : S.selected.add(k));
      refreshSelection(); return;
    }
    const head = t.closest("[data-cathead]");
    if (head) {
      const cid = head.dataset.cathead, sec = head.closest(".group");
      const col = !sec.classList.contains("collapsed");
      sec.classList.toggle("collapsed", col);
      if (cid) { api("POST", `/api/instances/${id}/categories`, { action: "collapse", id: cid, value: col }).catch(() => { }); const c = S.mods.categories.find(c => c.id === cid); if (c) c.collapsed = col; }
      else S._uncatCollapsed = col;
      return;
    }
    const row = t.closest(".mod");
    if (row) {
      const k = row.dataset.key;
      if (e.shiftKey && S.lastSel) {
        const rows = $$(".mod", el).map(r => r.dataset.key);
        const a = rows.indexOf(S.lastSel), b = rows.indexOf(k);
        if (a >= 0 && b >= 0) rows.slice(Math.min(a, b), Math.max(a, b) + 1).forEach(x => S.selected.add(x));
      } else {
        S.selected.has(k) ? S.selected.delete(k) : S.selected.add(k);
      }
      S.lastSel = k;
      refreshSelection();
    }
  };
  el.oncontextmenu = e => {
    const row = e.target.closest(".mod");
    if (!row) return;
    e.preventDefault();
    modMenu(row.dataset.key, null, { x: e.clientX, y: e.clientY });
  };
  el.ondragstart = e => {
    const head = e.target.closest("[data-cathead]");
    if (head && head.dataset.cathead) {
      e.dataTransfer.setData("text/hc-cat", head.dataset.cathead);
      e.dataTransfer.effectAllowed = "move";
      return;
    }
    const row = e.target.closest(".mod");
    if (!row) return;
    const k = row.dataset.key;
    const keys = S.selected.has(k) ? [...S.selected] : [k];
    e.dataTransfer.setData("text/hc-mods", JSON.stringify(keys));
    e.dataTransfer.effectAllowed = "move";
    keys.forEach(x => { const r = el.querySelector(`.mod[data-key="${CSS.escape(x)}"]`); r && r.classList.add("dragging"); });
    if (keys.length > 1) {
      const ghost = document.createElement("div");
      ghost.className = "chip solid"; ghost.textContent = `${keys.length} mods`; ghost.style.position = "fixed"; ghost.style.top = "-100px";
      document.body.appendChild(ghost); e.dataTransfer.setDragImage(ghost, 20, 12); setTimeout(() => ghost.remove());
    }
  };
  el.ondragend = () => { $$(".dragging", el).forEach(r => r.classList.remove("dragging")); $$(".group.drop", el).forEach(g => g.classList.remove("drop")); };
  el.ondragover = e => {
    const types = e.dataTransfer.types;
    if (!types.includes("text/hc-mods") && !types.includes("text/hc-cat")) return;
    const g = e.target.closest(".group"); if (!g) return;
    e.preventDefault();
    $$(".group.drop", el).forEach(x => x !== g && x.classList.remove("drop"));
    g.classList.add("drop");
  };
  el.ondragleave = e => { const g = e.target.closest(".group"); if (g && !g.contains(e.relatedTarget)) g.classList.remove("drop"); };
  el.ondrop = async e => {
    const g = e.target.closest(".group"); if (!g) return;
    e.preventDefault();
    g.classList.remove("drop");
    const cat = g.dataset.cat;
    const mods = e.dataTransfer.getData("text/hc-mods");
    const catDrag = e.dataTransfer.getData("text/hc-cat");
    if (mods) {
      const keys = JSON.parse(mods);
      await moveToCategory(keys, cat);
    } else if (catDrag && catDrag !== cat) {
      const order = S.mods.categories.map(c => c.id).filter(x => x !== catDrag);
      const idx = cat ? order.indexOf(cat) : order.length;
      order.splice(idx, 0, catDrag);
      S.mods.categories.sort((a, b) => order.indexOf(a.id) - order.indexOf(b.id));
      renderModGroups(false);
      api("POST", `/api/instances/${id}/categories`, { action: "reorder", order }).catch(fail);
    }
  };
}

function refreshSelection() {
  $$("#mod-groups .mod").forEach(r => r.classList.toggle("sel", S.selected.has(r.dataset.key)));
  $("#mod-groups")?.classList.toggle("selecting", S.selected.size > 0);
  renderSelbar();
}

function renderSelbar() {
  let bar = $("#selbar");
  if (!S.selected.size || S.route.view !== "instance" || S.route.tab !== "mods") { bar && bar.remove(); return; }
  if (!bar) { bar = document.createElement("div"); bar.id = "selbar"; bar.className = "selbar"; document.body.appendChild(bar); }
  const n = S.selected.size;
  bar.innerHTML = `<span class="n">${n} selecionado${n > 1 ? "s" : ""}</span>
    <button class="btn sm" data-a="on">${svg("check")}Ativar</button>
    <button class="btn sm" data-a="off">${svg("x")}Desativar</button>
    <button class="btn sm" data-a="cat">${svg("tag")}Mover</button>
    <button class="btn sm" data-a="copy">${svg("copy")}Copiar para…</button>
    <button class="btn sm danger" data-a="del">${svg("trash")}</button>
    <button class="btn ghost sm icon" data-a="clear" title="Limpar seleção">${svg("x")}</button>`;
  bar.onclick = e => {
    const b = e.target.closest("[data-a]"); if (!b) return;
    const keys = [...S.selected];
    switch (b.dataset.a) {
      case "on": setEnabled(keys, true); break;
      case "off": setEnabled(keys, false); break;
      case "cat": menu(b, categoryItems(keys)); break;
      case "copy": menu(b, copyItems(keys)); break;
      case "del": removeMods(keys); break;
      case "clear": S.selected.clear(); refreshSelection(); break;
    }
  };
}

function categoryItems(keys) {
  const items = [{ header: "Mover para" }];
  for (const c of S.mods.categories) items.push({ label: c.name, icon: "tag", run: () => moveToCategory(keys, c.id) });
  items.push({ label: "Sem categoria", icon: "x", run: () => moveToCategory(keys, "") });
  items.push("-", { label: "Nova categoria…", icon: "plus", run: async () => { const cid = await newCategory(); if (cid) moveToCategory(keys, cid); } });
  return items;
}
function copyItems(keys) {
  const others = S.instances.filter(i => i.id !== S.route.id);
  if (!others.length) return [{ header: "Nenhuma outra instância" }];
  return [{ header: "Copiar para instância" }, ...others.map(i => ({
    label: `${i.name} · ${i.mcVersion} ${LOADERS[i.loader] || ""}`, icon: i.loader || "vanilla",
    run: async () => {
      try {
        const r = await api("POST", `/api/instances/${S.route.id}/mods/copy`, { keys, target: i.id });
        toast(`${r.copied} mod${r.copied !== 1 ? "s" : ""} copiado${r.copied !== 1 ? "s" : ""} para ${i.name}`, "ok");
        if (i.mcVersion !== inst(S.route.id).mcVersion || i.loader !== inst(S.route.id).loader) toast("Atenção: a instância de destino usa outra versão/loader", "info");
      } catch (e) { fail(e); }
    },
  }))];
}

function modMenu(key, anchor, at) {
  const m = modByKey(key); if (!m) return;
  const keys = S.selected.has(key) && S.selected.size > 1 ? [...S.selected] : [key];
  const up = S.updates[key];
  menu(anchor, [
    { header: keys.length > 1 ? `${keys.length} mods` : m.name },
    { label: m.enabled ? "Desativar" : "Ativar", icon: m.enabled ? "x" : "check", run: () => setEnabled(keys, !m.enabled) },
    { label: "Mover para categoria", icon: "tag", sub: () => categoryItems(keys) },
    { label: "Copiar para instância", icon: "copy", sub: () => copyItems(keys) },
    up ? { label: `Atualizar para ${up.version}`, icon: "update", run: () => doUpdate(key) } : null,
    m.pageUrl ? { label: "Abrir página do mod", icon: "external", run: () => api("POST", "/api/open-url", { url: m.pageUrl }) } : null,
    { label: "Mostrar na pasta", icon: "folder", run: () => api("POST", `/api/instances/${S.route.id}/open?sub=mods`) },
    "-",
    { label: keys.length > 1 ? `Excluir ${keys.length} mods` : "Excluir", icon: "trash", danger: true, run: () => removeMods(keys) },
  ], at);
}

async function setEnabled(keys, on) {
  keys.forEach(k => { const m = modByKey(k); if (m) m.enabled = on; });
  keys.forEach(k => {
    const r = document.querySelector(`#mod-groups .mod[data-key="${CSS.escape(k)}"]`);
    if (r) { r.classList.toggle("off", !on); $(".switch", r).classList.toggle("on", on); }
  });
  renderStatsOnly();
  try { await api("POST", `/api/instances/${S.route.id}/mods/toggle`, { keys, enabled: on }); }
  catch (e) { fail(e); reloadMods(); }
}
function renderStatsOnly() {
  const d = S.mods; if (!d) return;
  const enabled = d.mods.filter(m => m.enabled).length;
  const s = $("#mod-stats"); if (s) s.innerHTML = `<b>${d.mods.length}</b> mods · <b>${enabled}</b> ativos${d.categories.length ? ` · ${d.categories.length} categorias` : ""}`;
}
async function moveToCategory(keys, cat) {
  keys.forEach(k => { const m = modByKey(k); if (m) m.category = cat; });
  renderModGroups(false);
  try { await api("POST", `/api/instances/${S.route.id}/mods/category`, { keys, category: cat }); }
  catch (e) { fail(e); reloadMods(); }
}
async function removeMods(keys) {
  const names = keys.map(k => modByKey(k)?.name || k);
  if (!(await confirmBox(keys.length > 1 ? `Excluir ${keys.length} mods?` : `Excluir ${names[0]}?`, "Os arquivos serão removidos da pasta mods desta instância.", "Excluir", true))) return;
  try {
    await api("POST", `/api/instances/${S.route.id}/mods/delete`, { keys });
    keys.forEach(k => S.selected.delete(k));
    S.mods.mods = S.mods.mods.filter(m => !keys.includes(m.key));
    renderModGroups(false);
    toast(keys.length > 1 ? `${keys.length} mods excluídos` : "Mod excluído");
  } catch (e) { fail(e); }
}
async function newCategory() {
  const name = await promptBox("Nova categoria", "Nome da categoria", "", "Criar");
  if (!name) return null;
  try {
    const r = await api("POST", `/api/instances/${S.route.id}/categories`, { action: "create", name });
    S.mods.categories.push({ id: r.id, name, collapsed: false });
    renderModGroups(false);
    return r.id;
  } catch (e) { fail(e); return null; }
}
async function renameCategory(cid) {
  const c = S.mods.categories.find(x => x.id === cid);
  const name = await promptBox("Renomear categoria", "Nome", c.name);
  if (!name) return;
  c.name = name; renderModGroups(false);
  api("POST", `/api/instances/${S.route.id}/categories`, { action: "rename", id: cid, name }).catch(fail);
}
async function deleteCategory(cid) {
  const c = S.mods.categories.find(x => x.id === cid);
  if (!(await confirmBox(`Excluir “${c.name}”?`, "Os mods não serão apagados — apenas voltam para “Sem categoria”.", "Excluir categoria"))) return;
  S.mods.categories = S.mods.categories.filter(x => x.id !== cid);
  S.mods.mods.forEach(m => { if (m.category === cid) m.category = ""; });
  renderModGroups(false);
  api("POST", `/api/instances/${S.route.id}/categories`, { action: "delete", id: cid }).catch(fail);
}

function modlistMenu(anchor) {
  const d = S.mods; if (!d) return;
  const id = S.route.id;
  const op = async body => { try { await api("POST", `/api/instances/${id}/modlists`, body); await reloadMods(); } catch (e) { fail(e); } };
  const cur = d.modlists.find(x => x.id === d.activeModlist);
  menu(anchor, [
    { header: "Modlists desta instância" },
    ...d.modlists.map(ml => ({ label: `${ml.name}  ·  ${Object.values(ml.enabled || {}).filter(Boolean).length} ativos`, icon: "layers", on: ml.id === d.activeModlist, run: () => ml.id !== d.activeModlist && op({ action: "switch", id: ml.id }).then(() => toast(`Modlist “${ml.name}” ativada`, "ok")) })),
    "-",
    { label: "Nova modlist vazia", icon: "plus", run: async () => { const n = await promptBox("Nova modlist", "Nome", "", "Criar"); if (n) op({ action: "create", name: n }); } },
    { label: "Duplicar modlist atual", icon: "dup", run: async () => { const n = await promptBox("Duplicar modlist", "Nome", cur.name + " 2", "Duplicar"); if (n) op({ action: "create", name: n, copy: true }); } },
    { label: "Renomear", icon: "edit", run: async () => { const n = await promptBox("Renomear modlist", "Nome", cur.name); if (n) op({ action: "rename", id: cur.id, name: n }); } },
    d.modlists.length > 1 ? { label: "Excluir modlist", icon: "trash", danger: true, run: async () => { if (await confirmBox(`Excluir “${cur.name}”?`, "Os arquivos dos mods continuam na instância; só a lista é removida.", "Excluir", true)) op({ action: "delete", id: cur.id }); } } : null,
  ]);
}

const reloadMods = async () => {
  if (S.route.view !== "instance") return;
  try { await loadMods(S.route.id); if (S.route.tab === "mods") renderModGroups(false); } catch (e) { }
};
const reloadModsSoon = debounce(reloadMods, 350);

async function checkUpdates() {
  const b = $("#upd-btn"); b.disabled = true; b.innerHTML = `<div class="spinner"></div>Procurando…`;
  try {
    const ups = await api("GET", `/api/instances/${S.route.id}/mods/updates`);
    S.updates = {}; ups.forEach(u => S.updates[u.key] = u);
    renderModGroups(false);
    toast(ups.length ? `${ups.length} atualização${ups.length > 1 ? "ões" : ""} disponível${ups.length > 1 ? "is" : ""}` : "Todos os mods do Modrinth estão atualizados", ups.length ? "info" : "ok");
  } catch (e) { fail(e); }
  b.disabled = false; b.innerHTML = `${svg("refresh")}Atualizações`;
}
async function doUpdate(key) {
  const u = S.updates[key]; if (!u) return;
  try {
    await api("POST", `/api/instances/${S.route.id}/mods/update`, { key, versionId: u.versionId });
    delete S.updates[key]; toast("Mod atualizado", "ok"); reloadMods();
  } catch (e) { fail(e); }
}
async function uploadJars(files) {
  const jars = files.filter(f => /\.(jar|zip)$/i.test(f.name));
  if (!jars.length) { toast("Solte arquivos .jar", "error"); return; }
  const fd = new FormData();
  jars.forEach((f, i) => fd.append("f" + i, f, f.name));
  try {
    const r = await api("POST", `/api/instances/${S.route.id}/mods/import`, fd);
    toast(`${r.imported} arquivo${r.imported > 1 ? "s" : ""} adicionado${r.imported > 1 ? "s" : ""}`, "ok");
    reloadMods();
  } catch (e) { fail(e); }
}

function tabBrowse(body, instId) {
  const b = S.browse;
  if (b.inst !== instId) { b.inst = instId; b.hits = []; b.offset = 0; b.done = false; }
  const i = inst(instId);
  body.innerHTML = `
    <div class="browse-tools">
      ${segHTML("src", [["modrinth", "Modrinth"], ["curseforge", "CurseForge"]], b.source)}
      ${segHTML("ptype", [["mod", "Mods"], ["resourcepack", "Texturas"], ["shader", "Shaders"]], b.type)}
      <div class="search"><input class="input" id="b-q" placeholder="Buscar em ${b.source === "curseforge" ? "CurseForge" : "Modrinth"}…" value="${esc(b.q)}">${svg("search")}</div>
      <select class="select input" id="b-sort" style="width:170px">
        ${[["relevance", "Relevância"], ["downloads", "Downloads"], ["follows", "Popularidade"], ["updated", "Atualizados"], ["newest", "Novos"]].map(([v, l]) => `<option value="${v}" ${b.sort === v ? "selected" : ""}>${l}</option>`).join("")}
      </select>
    </div>
    ${i ? `<div class="hint" style="margin:-6px 0 14px">Mostrando apenas conteúdo compatível com <span class="mono">${esc(i.mcVersion)}</span> · ${esc(LOADERS[i.loader] || "")}</div>` : ""}
    <div class="hits" id="hits"></div>
    <div class="sentinel" id="sentinel"></div>`;
  const reset = () => { b.hits = []; b.offset = 0; b.done = false; b.err = ""; renderHits(true); searchMore(); };
  bindSeg(body, "src", v => { b.source = v; $("#b-q").placeholder = `Buscar em ${v === "curseforge" ? "CurseForge" : "Modrinth"}…`; reset(); });
  bindSeg(body, "ptype", v => { b.type = v; reset(); });
  $("#b-q", body).oninput = debounce(e => { b.q = $("#b-q").value; reset(); }, 300);
  $("#b-sort", body).onchange = e => { b.sort = e.target.value; reset(); };
  if (!S.mods || S.modsFor !== instId) loadMods(instId).then(() => renderHits(false)).catch(() => { });
  const io = new IntersectionObserver(es => { if (es[0].isIntersecting) searchMore(); }, { root: $(".view"), rootMargin: "400px" });
  io.observe($("#sentinel", body));
  if (b.hits.length) renderHits(false); else { renderHits(true); searchMore(); }
}

let searchSeq = 0;
async function searchMore() {
  const b = S.browse;
  if (b.loading || b.done) return;
  const i = inst(b.inst); if (!i) return;
  b.loading = true;
  const seq = ++searchSeq;
  const sn = $("#sentinel"); if (sn && b.hits.length) sn.innerHTML = `<div class="spinner"></div>`;
  try {
    const p = new URLSearchParams({ source: b.source, q: b.q, mc: i.mcVersion, loader: i.loader, sort: b.sort, offset: b.offset, type: b.type });
    const r = await api("GET", "/api/search?" + p);
    if (seq !== searchSeq) return;
    const start = b.hits.length;
    b.hits.push(...r.hits); b.total = r.total; b.offset += r.hits.length;
    b.done = r.hits.length === 0 || b.offset >= r.total;
    appendHits(start);
  } catch (e) {
    if (seq !== searchSeq) return;
    b.err = e.message; b.done = true; renderHits(false);
  } finally {
    if (seq === searchSeq) { b.loading = false; const sn = $("#sentinel"); if (sn) sn.innerHTML = b.done && b.hits.length ? `<span class="hint">${b.total} resultados</span>` : ""; }
  }
}

function hitHTML(h, n) {
  const b = S.browse;
  const installed = b.installed.has(h.id);
  const busy = b.installing.has(h.id);
  return `<div class="hit" style="--i:${n}" data-hid="${esc(h.id)}">
    <div class="hi">${h.icon ? `<img src="${esc(h.icon)}" alt="" loading="lazy" onerror="this.remove()">` : ""}</div>
    <div style="min-width:0">
      <div class="ht"><span class="hn">${esc(h.title)}</span><span class="hb">por ${esc(h.author || "—")}</span></div>
      <div class="hd">${esc(h.description)}</div>
      <div class="hf"><span>${svg("download")}${fmtNum(h.downloads)}</span><span>${svg("heart")}${fmtNum(h.follows)}</span>
        ${(h.categories || []).slice(0, 2).map(c => `<span class="tag">${esc(c)}</span>`).join("")}
        <button class="btn ghost sm icon" data-open="${esc(h.url)}" title="Abrir página">${svg("external")}</button>
        <button class="btn sm ${installed ? "installed" : "primary"}" data-inst="${esc(h.id)}" ${busy ? "disabled" : ""}>${installed ? svg("check") + "Instalado" : busy ? `<div class="spinner" style="width:14px;height:14px"></div>Instalando` : svg("download") + "Instalar"}</button>
      </div>
    </div></div>`;
}

function renderHits(skeleton) {
  const el = $("#hits"); if (!el) return;
  const b = S.browse;
  if (b.err) { el.innerHTML = `<div class="empty" style="grid-column:1/-1;padding:40px"><h3>Não foi possível buscar</h3><p>${esc(b.err)}</p>${b.source === "curseforge" ? `<button class="btn" onclick="go({view:'settings'})">${svg("key")}Configurar chave da API</button>` : ""}</div>`; return; }
  if (skeleton) { el.innerHTML = Array.from({ length: 8 }, (_, i) => `<div class="skeleton" style="opacity:${1 - i * 0.1}"></div>`).join(""); return; }
  el.innerHTML = b.hits.map((h, n) => hitHTML(h, -1)).join("") || (b.done ? `<div class="empty" style="grid-column:1/-1;padding:40px"><h3>Nada encontrado</h3><p>Tente outros termos.</p></div>` : "");
  $$(".hit", el).forEach(x => x.style.animation = "none");
  bindHits(el);
}
function appendHits(start) {
  const el = $("#hits"); if (!el) return;
  if (start === 0) el.innerHTML = "";
  const b = S.browse;
  el.insertAdjacentHTML("beforeend", b.hits.slice(start).map((h, n) => hitHTML(h, n)).join(""));
  if (!b.hits.length) el.innerHTML = `<div class="empty" style="grid-column:1/-1;padding:40px"><h3>Nada encontrado</h3><p>Tente outros termos ou outra fonte.</p></div>`;
  bindHits(el);
}
function bindHits(el) {
  el.onclick = async e => {
    const o = e.target.closest("[data-open]");
    if (o) { api("POST", "/api/open-url", { url: o.dataset.open }); return; }
    const btn = e.target.closest("[data-inst]");
    if (!btn) return;
    const pid = btn.dataset.inst, b = S.browse;
    const instId = b.inst;
    b.installing.add(pid);
    btn.disabled = true; btn.innerHTML = `<div class="spinner" style="width:14px;height:14px"></div>Instalando`;
    try {
      await api("POST", `/api/instances/${instId}/mods/install`, { source: b.source, projectId: pid, projectType: b.type });
      b.installed.add(pid);
      const h = b.hits.find(x => x.id === pid);
      toast(`${h ? h.title : "Mod"} instalado`, "ok");
      if (S.route.id === instId) loadMods(instId).catch(() => { });
    } catch (err) { fail(err); }
    b.installing.delete(pid);
    const cur = document.querySelector(`.hit[data-hid="${CSS.escape(pid)}"]`);
    if (cur) { const h = b.hits.find(x => x.id === pid); const t = document.createElement("div"); t.innerHTML = hitHTML(h, 0); t.firstElementChild.style.animation = "none"; cur.replaceWith(t.firstElementChild); }
  };
}

function viewExplore(v) {
  if (!S.instances.length) {
    v.innerHTML = `<div class="page-head"><div><h1>Explorar</h1><div class="sub">Encontre mods, texturas e shaders</div></div></div>
      <div class="empty"><canvas id="ex-cv"></canvas><h3>Crie uma instância primeiro</h3><p>Os mods são instalados por instância, compatíveis com a versão e o loader dela.</p><button class="btn primary" id="ex-new">${svg("plus")}Nova instância</button></div>`;
    Tesseract.attach($("#ex-cv", v), { line: 1.3, dots: true });
    $("#ex-new", v).onclick = openCreate;
    return;
  }
  if (!S.browse.inst || !inst(S.browse.inst)) S.browse.inst = S.instances[0].id;
  const i = inst(S.browse.inst);
  v.innerHTML = `<div class="page-head"><div><h1>Explorar</h1><div class="sub">Mods, texturas e shaders do Modrinth e CurseForge</div></div>
    <button class="ml-btn" id="ex-pick">${instIcon(i)}<span class="k">Instalar em</span><span class="t">${esc(i.name)}</span>${svg("chevron")}</button></div><div id="ex-body"></div>`;
  $("#ex-pick .ico", v).style.cssText = "width:22px;height:22px;border-radius:6px";
  $("#ex-pick", v).onclick = e => menu(e.currentTarget, S.instances.map(x => ({ label: `${x.name} · ${x.mcVersion}`, icon: x.loader || "vanilla", on: x.id === i.id, run: () => { S.browse.inst = x.id; S.browse.hits = []; S.browse.offset = 0; S.browse.done = false; S.mods = null; renderMain(false); } })));
  S.route.id = i.id;
  tabBrowse($("#ex-body", v), i.id);
}

function logClass(l) {
  if (l.startsWith("[Hypercube]")) return "hc";
  if (/\b(ERROR|FATAL|SEVERE)\b|Exception|^\s+at /.test(l)) return "err";
  if (/\bWARN/.test(l)) return "warn";
  return "";
}
function tabLogs(body) {
  const id = S.route.id;
  body.innerHTML = `<div class="toolbar">
      <div class="search"><input class="input" id="log-q" placeholder="Filtrar console">${svg("search")}</div>
      <button class="btn ${S.logFollow ? "" : "ghost"}" id="log-follow">${svg("chevron")}Rolar automaticamente</button>
      <button class="btn" id="log-copy">${svg("copy")}Copiar</button>
      <button class="btn" id="log-folder">${svg("folder")}Logs</button>
      <button class="btn" id="log-crash">${svg("file")}Crash reports</button>
    </div><div class="console" id="console"></div>`;
  const con = $("#console", body);
  const q = $("#log-q", body);
  const draw = () => {
    const lines = S.logs[id] || [];
    const f = q.value.toLowerCase();
    const shown = f ? lines.filter(l => l.toLowerCase().includes(f)) : lines;
    con.innerHTML = shown.length ? shown.slice(-3000).map(l => `<div class="${logClass(l)}">${esc(l)}</div>`).join("") : `<div class="empty-log">${lines.length ? "Nenhuma linha corresponde" : "O console aparece aqui quando o jogo estiver rodando."}</div>`;
    if (S.logFollow) con.scrollTop = con.scrollHeight;
  };
  q.oninput = debounce(draw, 120);
  $("#log-follow", body).onclick = e => { S.logFollow = !S.logFollow; e.currentTarget.classList.toggle("ghost", !S.logFollow); if (S.logFollow) con.scrollTop = con.scrollHeight; };
  $("#log-copy", body).onclick = () => { navigator.clipboard.writeText((S.logs[id] || []).join("\n")).then(() => toast("Console copiado", "ok")); };
  $("#log-folder", body).onclick = () => api("POST", `/api/instances/${id}/open?sub=logs`);
  $("#log-crash", body).onclick = () => api("POST", `/api/instances/${id}/open?sub=crash-reports`);
  api("GET", `/api/instances/${id}/logs`).then(l => { S.logs[id] = l || []; draw(); }).catch(() => draw());
  con.onscroll = () => { const atEnd = con.scrollHeight - con.scrollTop - con.clientHeight < 30; if (!atEnd && S.logFollow) { S.logFollow = false; $("#log-follow").classList.add("ghost"); } };
  body._append = line => {
    if (q.value && !line.toLowerCase().includes(q.value.toLowerCase())) return;
    const e = $(".empty-log", con); if (e) e.remove();
    const d = document.createElement("div"); d.className = logClass(line); d.textContent = line; con.appendChild(d);
    if (con.childElementCount > 4000) con.firstElementChild.remove();
    if (S.logFollow) con.scrollTop = con.scrollHeight;
  };
}

function tabConfig(body) {
  const i = inst(S.route.id);
  const mem = i.memoryMB || 0;
  body.innerHTML = `<div class="sections">
    <div class="section" style="--i:0"><h3>${svg("edit")}Identidade</h3><div class="desc">Nome e ícone exibidos na biblioteca.</div>
      <div class="form-grid">
        <label class="field full"><span>Nome</span><input class="input" id="cf-name" value="${esc(i.name)}" maxlength="48"></label>
        <div class="full"><div class="field-label" style="margin-bottom:8px">Ícone</div>
          <div class="icon-picker" id="cf-icons" style="grid-template-columns:repeat(12,1fr)">${GLYPHS.map(g => `<button data-g="${g}" class="${(i.icon || "tesseract") === g ? "on" : ""}">${glyph(g)}</button>`).join("")}</div>
          <div class="row" style="margin-top:10px"><button class="btn sm" id="cf-upload">${svg("upload")}Usar imagem própria</button><input type="file" id="cf-file" accept="image/png,image/jpeg,image/webp" hidden></div></div>
      </div></div>
    <div class="section" style="--i:1"><h3>${svg("box")}Versão</h3><div class="desc">Atualmente <span class="mono">${esc(i.mcVersion)}</span> com ${esc(loaderText(i))}.</div>
      <div class="row"><button class="btn" id="cf-version">${svg("layers")}Alterar versão / loader</button><button class="btn" id="cf-repair">${svg("wrench")}Reparar arquivos</button></div></div>
    <div class="section" style="--i:2"><h3>${svg("memory")}Java e memória</h3><div class="desc">Deixe em branco para usar os padrões globais das configurações.</div>
      <div class="setting-row"><div><div class="sl">Memória dedicada</div><div class="sd">${mem ? "Personalizada para esta instância" : "Usando o padrão global (" + (S.settings.memoryMB / 1024).toFixed(1) + " GB)"}</div></div>
        <div class="row" style="width:340px"><button class="btn sm ghost" id="cf-memreset" ${mem ? "" : "disabled"}>Padrão</button><input type="range" id="cf-mem" min="1024" max="32768" step="512" value="${mem || S.settings.memoryMB}"><span class="mem-val" id="cf-memv"></span></div></div>
      <div class="form-grid" style="margin-top:8px">
        <label class="field full"><span>Argumentos JVM</span><input class="input mono" id="cf-jvm" value="${esc(i.jvmArgs || "")}" placeholder="${esc(S.settings.jvmArgs || "-XX:+UseG1GC")}"></label>
        <label class="field full"><span>Executável Java</span><input class="input mono" id="cf-java" value="${esc(i.javaPath || "")}" placeholder="Automático (baixado da Mojang)"></label>
      </div></div>
    <div class="section" style="--i:3"><h3>${svg("folder")}Pastas</h3><div class="desc">Atalhos para os arquivos da instância.</div>
      <div class="row" style="flex-wrap:wrap">${[["", "Raiz"], ["mods", "Mods"], ["config", "Config"], ["saves", "Mundos"], ["resourcepacks", "Texturas"], ["shaderpacks", "Shaders"], ["screenshots", "Capturas"]].map(([s, l]) => `<button class="btn sm" data-sub="${s}">${svg("folder")}${l}</button>`).join("")}</div></div>
    <div class="section danger-zone" style="--i:4"><h3>${svg("trash")}Zona de perigo</h3><div class="desc">Ações permanentes.</div>
      <div class="row"><button class="btn" id="cf-dup">${svg("dup")}Duplicar instância</button><button class="btn danger" id="cf-del">${svg("trash")}Excluir instância</button></div></div>
  </div>`;
  const save = debounce(patch => api("PATCH", `/api/instances/${i.id}`, patch).catch(fail), 400);
  $("#cf-name", body).oninput = e => { i.name = e.target.value; save({ name: e.target.value }); renderSideInstances(); const h = $("#hero h1"); if (h) h.textContent = e.target.value; };
  $("#cf-icons", body).onclick = async e => {
    const b = e.target.closest("button"); if (!b) return;
    if (b.dataset.custom !== undefined) { changeInstanceImage(i.id); return; }
    i.icon = b.dataset.g; $$("#cf-icons button", body).forEach(x => x.classList.toggle("on", x === b)); save({ icon: b.dataset.g }); renderHero(); renderSideInstances();
  };
  const mr = $("#cf-mem", body), mv = $("#cf-memv", body);
  const paint = () => { const p = (mr.value - mr.min) / (mr.max - mr.min) * 100; mr.style.setProperty("--p", p + "%"); mv.textContent = (mr.value / 1024).toFixed(1) + " GB"; };
  paint();
  mr.oninput = () => { paint(); i.memoryMB = +mr.value; $("#cf-memreset", body).disabled = false; save({ memoryMB: +mr.value }); };
  $("#cf-memreset", body).onclick = () => { i.memoryMB = 0; save({ memoryMB: 0 }); mr.value = S.settings.memoryMB; paint(); $("#cf-memreset", body).disabled = true; };
  $("#cf-jvm", body).oninput = e => save({ jvmArgs: e.target.value });
  $("#cf-java", body).oninput = e => save({ javaPath: e.target.value });
  $$("[data-sub]", body).forEach(b => b.onclick = () => api("POST", `/api/instances/${i.id}/open?sub=${b.dataset.sub}`));
  $("#cf-version", body).onclick = () => openCreate(i.id);
  $("#cf-repair", body).onclick = () => api("POST", `/api/instances/${i.id}/install`).then(() => toast("Verificando e baixando arquivos…")).catch(fail);
  $("#cf-dup", body).onclick = () => duplicateInstance(i.id);
  $("#cf-del", body).onclick = () => deleteInstance(i.id);
}

function viewSettings(v) {
  const st = S.settings;
  v.innerHTML = `<div class="page-head"><div><h1>Configurações</h1><div class="sub">Perfil, backup, Java e integrações</div></div></div>
  <div class="sections">
    <div class="section" style="--i:0"><h3>${svg("user")}Perfil</h3><div class="desc">Seu nome e foto no Hypercube. Ficam salvos só neste computador e não mudam sua conta do Minecraft.</div>
      <div class="profile-row" id="s-profile"></div></div>
    <div class="section" style="--i:0"><h3>${svg("microsoft")}Conta Minecraft</h3><div class="desc">A conta usada <b>dentro do jogo</b> (seu nick, skin e acesso a servidores). Ela é separada do perfil do launcher.</div>
      <div id="s-mc"></div></div>
    <div class="section" style="--i:0"><h3>${svg("box")}Backup</h3><div class="desc">Leve seu perfil, instâncias, mods, configs e mundos para outro PC num único arquivo <span class="mono">.hcbackup</span>.</div>
      <div class="setting-row"><div><div class="sl">Incluir mundos</div><div class="sd">Desative para um backup menor, só com modpacks e configs</div></div><button class="switch on" id="s-worlds"></button></div>
      <div class="row" style="margin-top:6px"><button class="btn primary" id="s-export">${svg("download")}Exportar backup</button><button class="btn" id="s-import">${svg("upload")}Importar backup</button></div></div>
    <div class="section" style="--i:1"><h3>${svg("memory")}Java e memória</h3><div class="desc">Padrões aplicados a todas as instâncias (cada uma pode sobrescrever).</div>
      <div class="setting-row"><div><div class="sl">Memória máxima</div><div class="sd">Recomendado: 4–8 GB para modpacks</div></div>
        <div class="row" style="width:320px"><input type="range" id="s-mem" min="1024" max="32768" step="512" value="${st.memoryMB}"><span class="mem-val" id="s-memv"></span></div></div>
      <div class="form-grid" style="margin-top:8px">
        <label class="field full"><span>Argumentos JVM</span><input class="input mono" id="s-jvm" value="${esc(st.jvmArgs || "")}" placeholder="-XX:+UseG1GC -XX:MaxGCPauseMillis=50"></label>
        <label class="field full"><span>Executável Java (opcional)</span><input class="input mono" id="s-java" value="${esc(st.javaPath || "")}" placeholder="Automático — o Hypercube baixa o Java certo para cada versão"></label>
      </div></div>
    <div class="section" style="--i:2"><h3>${svg("monitor")}Jogo</h3><div class="desc">Resolução inicial da janela do Minecraft.</div>
      <div class="form-grid"><label class="field"><span>Largura</span><input class="input mono" id="s-w" type="number" value="${st.width || ""}"></label><label class="field"><span>Altura</span><input class="input mono" id="s-h" type="number" value="${st.height || ""}"></label></div></div>
    <div class="section" style="--i:3"><h3>${svg("download")}Downloads</h3>
      <div class="setting-row"><div><div class="sl">Downloads simultâneos</div><div class="sd">Mais conexões = instalação mais rápida</div></div>
        <div class="row" style="width:320px"><input type="range" id="s-conc" min="2" max="32" step="1" value="${st.concurrency || 12}"><span class="mem-val" id="s-concv"></span></div></div></div>
    <div class="section" style="--i:4"><h3>${svg("key")}Integrações</h3><div class="desc">O Modrinth funciona sem configuração. A CurseForge exige uma chave gratuita de <a href="#" data-url="https://console.curseforge.com/">console.curseforge.com</a>.</div>
      <div class="form-grid">
        <label class="field full"><span>Chave da API CurseForge</span><input class="input mono" id="s-cf" type="password" value="${esc(st.curseforgeKey || "")}" placeholder="$2a$10$…"></label>
      </div></div>
    <div class="section" style="--i:5"><h3>${svg("sparkle")}Interface</h3>
      <div class="setting-row"><div><div class="sl">Reduzir animações</div><div class="sd">Desliga transições e o hipercubo animado</div></div><button class="switch ${st.reducedMotion ? "on" : ""}" id="s-red"></button></div></div>
    <div class="section" style="--i:6"><h3>${svg("info")}Sobre</h3><div class="desc">Hypercube v${esc(S.version)} — launcher de Minecraft minimalista.</div>
      <div class="row"><button class="btn" id="s-data">${svg("folder")}Abrir pasta de dados</button><span class="hint mono">${esc(S.dataDir || "")}</span></div></div>
  </div>`;
  renderProfileSection();
  renderMCSection();
  const save = debounce(() => {
    const ns = {
      ...S.settings,
      memoryMB: +$("#s-mem").value, jvmArgs: $("#s-jvm").value, javaPath: $("#s-java").value.trim(),
      width: +$("#s-w").value || 0, height: +$("#s-h").value || 0, concurrency: +$("#s-conc").value,
      curseforgeKey: $("#s-cf").value.trim(), reducedMotion: $("#s-red").classList.contains("on"),
    };
    api("POST", "/api/settings", ns).then(r => { S.settings = r.settings; }).catch(fail);
  }, 400);
  const rng = (id, out, fmt) => { const r = $(id), o = $(out); const p = () => { r.style.setProperty("--p", (r.value - r.min) / (r.max - r.min) * 100 + "%"); o.textContent = fmt(+r.value); }; p(); r.oninput = () => { p(); save(); }; };
  rng("#s-mem", "#s-memv", x => (x / 1024).toFixed(1) + " GB");
  rng("#s-conc", "#s-concv", x => x + "×");
  ["#s-jvm", "#s-java", "#s-w", "#s-h", "#s-cf"].forEach(s => $(s).oninput = save);
  $("#s-red").onclick = e => { e.currentTarget.classList.toggle("on"); document.body.classList.toggle("reduced", e.currentTarget.classList.contains("on")); save(); };
  $("#s-worlds").onclick = e => e.currentTarget.classList.toggle("on");
  $("#s-export").onclick = () => exportBackup($("#s-worlds").classList.contains("on"));
  $("#s-import").onclick = () => pickBackup(false);
  $("#s-data").onclick = () => api("POST", "/api/open-data");
  $$("[data-url]", v).forEach(a => a.onclick = e => { e.preventDefault(); api("POST", "/api/open-url", { url: a.dataset.url }); });
}
function avatarPickerHTML(id) {
  return `<button class="avatar-pick" id="${id}" type="button" title="Escolher foto (PNG, JPG, WEBP ou GIF)">
      <div class="ap-img">${S.profile.avatar ? `<img src="${esc(S.profile.avatar)}" alt="">` : `<span class="ap-ph">${svg("user")}</span>`}</div>
      <span class="ap-hover">${svg("upload")}<span>Escolher foto</span></span>
    </button><input type="file" id="${id}-file" accept="image/png,image/jpeg,image/webp,image/gif" hidden>`;
}
function bindAvatarPicker(root, id) {
  let file = null;
  const btn = $("#" + id, root), inp = $("#" + id + "-file", root);
  const set = f => {
    if (!f) return;
    if (!/^image\/(png|jpeg|webp|gif)$/.test(f.type)) { toast("Use uma imagem PNG, JPG, WEBP ou GIF", "error"); return; }
    if (f.size > 25 * 1024 * 1024) { toast("A imagem deve ter no máximo 25 MB", "error"); return; }
    file = f;
    const url = URL.createObjectURL(f);
    $(".ap-img", btn).innerHTML = `<img src="${url}" alt="">`;
    btn.classList.remove("pop"); void btn.offsetWidth; btn.classList.add("pop");
  };
  btn.onclick = () => inp.click();
  inp.onchange = () => set(inp.files[0]);
  btn.ondragover = e => { e.preventDefault(); e.stopPropagation(); btn.classList.add("over"); };
  btn.ondragleave = () => btn.classList.remove("over");
  btn.ondrop = e => { e.preventDefault(); e.stopPropagation(); btn.classList.remove("over"); set(e.dataTransfer.files[0]); };
  return () => file;
}
async function saveProfile(name, file) {
  const r = await api("POST", "/api/profile", { name });
  S.profile = r.profile;
  if (file) { const r2 = await api("POST", "/api/profile/avatar", file, true); S.profile = r2.profile; }
  renderProfileChip(); renderProfileSection();
}

function showOnboarding() {
  if ($("#onboard")) return;
  const ob = document.createElement("div");
  ob.id = "onboard";
  ob.innerHTML = `<canvas class="ob-cv"></canvas>
    <div class="ob-card">
      <div class="ob-brand"><canvas class="ob-logo"></canvas><span class="pixel">HYPERCUBE</span></div>
      <h1>Quem vai jogar?</h1>
      <p class="muted">Escolha seu nome e uma foto para o launcher. Fica tudo salvo neste computador. Depois você pode conectar sua conta do Minecraft nas configurações.</p>
      <div class="ob-form">
        ${avatarPickerHTML("ob-av")}
        <div class="ob-fields">
          <label class="field"><span>Seu nome</span><input class="input ob-name" id="ob-name" maxlength="16" placeholder="Steve" autocomplete="off" spellcheck="false"></label>
          <div class="hint" id="ob-hint">3 a 16 caracteres: letras, números ou _</div>
        </div>
      </div>
      <button class="btn primary lg ob-go" id="ob-go" disabled>Começar ${svg("chevron")}</button>
      <button class="ob-import" id="ob-import">${svg("upload")}Já tenho um backup — importar perfil</button>
    </div>`;
  document.body.appendChild(ob);
  Tesseract.attach($(".ob-cv", ob), { line: 1.1, scale: 0.9, speed: 0.6 });
  Tesseract.attach($(".ob-logo", ob), { line: 1.1, scale: 1.25 });
  const getFile = bindAvatarPicker(ob, "ob-av");
  const name = $("#ob-name", ob), go = $("#ob-go", ob);
  const valid = () => /^[A-Za-z0-9_]{3,16}$/.test(name.value.trim());
  name.oninput = () => {
    name.value = name.value.replace(/[^A-Za-z0-9_]/g, "");
    go.disabled = !valid();
    $("#ob-hint", ob).classList.toggle("ok", valid());
    $("#ob-av .ap-ph", ob) && ($("#ob-av .ap-ph", ob).innerHTML = name.value ? `<b>${esc(name.value[0].toUpperCase())}</b>` : svg("user"));
  };
  setTimeout(() => name.focus(), 500);
  const submit = async () => {
    if (!valid()) return;
    go.disabled = true; go.innerHTML = `<div class="spinner" style="border-top-color:#000;border-color:rgba(0,0,0,.2)"></div>`;
    try {
      await saveProfile(name.value.trim(), getFile());
      closeOnboarding(`Bem-vindo, ${S.profile.name}!`);
      if (!S.mcAccount) setTimeout(needAccountPrompt, 900);
    } catch (e) { fail(e); go.disabled = false; go.innerHTML = `Começar ${svg("chevron")}`; }
  };
  name.onkeydown = e => { if (e.key === "Enter") submit(); };
  go.onclick = submit;
  $("#ob-import", ob).onclick = () => pickBackup(true);
}
function closeOnboarding(msg) {
  const ob = $("#onboard"); if (!ob) return;
  ob.classList.add("out");
  setTimeout(() => ob.remove(), 600);
  renderSidebar(); renderMain(true);
  if (msg) toast(msg, "ok");
}

function openProfileEditor() {
  modal({
    title: "Editar perfil", sub: "Nome e foto do Hypercube. Não mudam seu nick no jogo, que vem da sua conta Minecraft.",
    body: `<div class="ob-form" style="margin:4px 0">${avatarPickerHTML("pe-av")}
      <div class="ob-fields"><label class="field"><span>Nome no jogo</span><input class="input" id="pe-name" maxlength="16" value="${esc(S.profile.name)}"></label>
      </div></div>`,
    foot: `${S.profile.avatar ? `<button class="btn ghost" id="pe-rm">${svg("trash")}Remover foto</button>` : ""}<span class="grow"></span><button class="btn ghost" data-c>Cancelar</button><button class="btn primary" id="pe-ok">Salvar</button>`,
    onMount(m, close) {
      const getFile = bindAvatarPicker(m, "pe-av");
      $("[data-c]", m).onclick = close;
      const rm = $("#pe-rm", m);
      if (rm) rm.onclick = async () => { try { const r = await api("DELETE", "/api/profile/avatar"); S.profile = r.profile; renderProfileChip(); renderProfileSection(); close(); } catch (e) { fail(e); } };
      const inp = $("#pe-name", m);
      inp.oninput = () => { inp.value = inp.value.replace(/[^A-Za-z0-9_]/g, ""); };
      const ok = async () => { try { await saveProfile(inp.value.trim(), getFile()); close(); toast("Perfil atualizado", "ok"); } catch (e) { fail(e); } };
      inp.onkeydown = e => { if (e.key === "Enter") ok(); };
      $("#pe-ok", m).onclick = ok;
    },
  });
}

function renderProfileSection() {
  const el = $("#s-profile"); if (!el) return;
  const p = S.profile;
  const since = p.created ? new Date(p.created * 1000).toLocaleDateString("pt-BR") : "";
  const play = S.instances.reduce((a, i) => a + (i.playtime || 0), 0);
  el.innerHTML = `${avatarHTML("avatar xl")}
    <div class="grow" style="min-width:0">
      <div class="pr-name">${esc(p.name)}</div>
      <div class="hint mono">UUID ${esc(p.uuid || "")}</div>
      <div class="pr-stats"><span><b>${S.instances.length}</b> instâncias</span><span><b>${fmtDur(play)}</b> jogados</span>${since ? `<span>desde ${since}</span>` : ""}</div>
    </div>
    <button class="btn" id="s-edit">${svg("edit")}Editar</button>`;
  $("#s-edit", el).onclick = () => openProfileEditor();
}

async function exportBackup(worlds) {
  try {
    await api("POST", `/api/profile/export?worlds=${worlds ? 1 : 0}`);
    toast("Gerando backup… ele vai para sua pasta Downloads");
  } catch (e) { fail(e); }
}
function pickBackup(asProfile) {
  const inp = document.createElement("input");
  inp.type = "file"; inp.accept = ".hcbackup,.zip";
  inp.onchange = async () => {
    const f = inp.files[0]; if (!f) return;
    let useProfile = asProfile;
    if (!asProfile && S.profile.name) {
      useProfile = await confirmBox("Usar o perfil do backup?", "As instâncias do backup serão adicionadas. Você também quer trocar seu nome, foto e configurações pelos do backup?", "Sim, trocar perfil");
    }
    toast("Importando backup…");
    try {
      const r = await api("POST", `/api/profile/import?profile=${useProfile ? 1 : 0}`, f, true);
      if (r.settings) S.settings = r.settings;
      if (r.profile) S.profile = r.profile;
      await refreshState();
      toast(`${r.instances} instância${r.instances !== 1 ? "s" : ""} importada${r.instances !== 1 ? "s" : ""}`, "ok");
      if ($("#onboard") && S.profile.name) closeOnboarding(`Bem-vindo de volta, ${S.profile.name}!`);
      else { renderSidebar(); renderMain(false); }
    } catch (e) { fail(e); }
  };
  inp.click();
}

function mcHead(uuid, size = 64) { return `https://mc-heads.net/avatar/${encodeURIComponent(uuid)}/${size}`; }
function renderMCSection() {
  const el = $("#s-mc"); if (!el) return;
  const a = S.mcAccount;
  if (a) {
    el.innerHTML = `<div class="mc-row"><div class="avatar xl mc-head"><img src="${mcHead(a.uuid, 84)}" alt="" onerror="this.remove()"></div>
      <div class="grow" style="min-width:0"><div class="pr-name">${esc(a.name)}</div><div class="hint mono">UUID ${esc(a.uuid)}</div>
      <div class="pr-stats"><span class="running-badge"><span class="dot-run"></span>Conectada · licença verificada</span></div></div>
      <button class="btn" id="mc-out">${svg("x")}Desconectar</button></div>`;
    $("#mc-out", el).onclick = async () => {
      if (!(await confirmBox("Desconectar conta Minecraft?", "Você não vai conseguir jogar até entrar de novo. Suas instâncias e mods continuam aqui.", "Desconectar"))) return;
      try { const r = await api("DELETE", "/api/mc/account"); S.mcAccount = r.mcAccount || null; renderMCSection(); renderProfileChip(); } catch (e) { fail(e); }
    };
  } else {
    el.innerHTML = `<div class="mc-row"><div class="avatar xl mc-head">${svg("user")}</div>
      <div class="grow"><div class="pr-name" style="font-size:17px">Nenhuma conta conectada</div><div class="hint">Para jogar, entre com a conta Microsoft que tem o Minecraft: Java Edition.</div></div>
      <div class="row"><button class="btn ghost" id="mc-guide">${svg("info")}Como configurar</button><button class="btn primary" id="mc-in">${svg("microsoft")}Entrar com Microsoft</button></div></div>`;
    $("#mc-in", el).onclick = startMCLogin;
    $("#mc-guide", el).onclick = () => openMCGuide(false);
  }
}

function needAccountPrompt() {
  modal({
    title: "Conecte sua conta Minecraft",
    sub: "Para jogar, o Hypercube precisa verificar que sua conta Microsoft tem o Minecraft: Java Edition. Seu nick e skin vêm dessa conta.",
    body: `<div class="hint">Depois de conectada uma vez, você também consegue jogar sem internet com essa conta.</div>`,
    foot: `<button class="btn ghost" data-c>Agora não</button><button class="btn primary" id="np-go">${svg("microsoft")}Entrar com Microsoft</button>`,
    onMount(m, close) { $("[data-c]", m).onclick = close; $("#np-go", m).onclick = () => { close(); setTimeout(startMCLogin, 220); }; },
  });
}

function startMCLogin() {
  if (!(S.settings.msClientId || "").trim()) { openMCGuide(true); return; }
  requestMCCode();
}

async function requestMCCode(clientId) {
  try {
    const r = await api("POST", "/api/mc/login", clientId ? { clientId } : {});
    if (clientId) S.settings.msClientId = clientId;
    modal({
      title: "Entrar com sua conta Minecraft",
      sub: "Abra a página da Microsoft, digite o código e faça login. Sua senha vai só para a Microsoft.",
      body: `<div class="code-box">${esc(r.code)}</div><div class="row" style="justify-content:center"><div class="spinner"></div><span class="muted">Aguardando você entrar…</span></div>`,
      foot: `<button class="btn" id="mc-copy">${svg("copy")}Copiar código</button><button class="btn primary" id="mc-open">${svg("external")}Abrir ${esc(r.url.replace("https://", ""))}</button>`,
      onMount(m, close) {
        S._mcClose = close;
        $("#mc-copy", m).onclick = () => navigator.clipboard.writeText(r.code).then(() => toast("Código copiado", "ok"));
        $("#mc-open", m).onclick = () => { navigator.clipboard.writeText(r.code).catch(() => { }); api("POST", "/api/open-url", { url: r.url }); toast("Código copiado — é só colar na página"); };
      },
      onClose: () => { S._mcClose = null; api("POST", "/api/mc/cancel").catch(() => { }); },
    });
  } catch (e) { fail(e); }
}

function openMCGuide(thenLogin) {
  const open = u => api("POST", "/api/open-url", { url: u });
  modal({
    title: "Configurar login Microsoft", wide: true,
    sub: "A Mojang exige que cada launcher tenha um app próprio registrado no Azure. É grátis e você faz uma única vez.",
    body: `<ol class="guide">
      <li><b>Crie o app.</b> No portal do Azure, vá em <i>App registrations → New registration</i>.
        <button class="btn sm" data-u="https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade">${svg("external")}Abrir portal do Azure</button>
        <div class="hint" style="margin-top:8px">Erro <span class="mono">AADSTS50020</span> / “does not exist in tenant”? Sua conta pessoal ainda não tem um diretório próprio. Ative a conta gratuita do Azure (cria o “Default Directory”) e tente de novo numa janela anônima.</div>
        <button class="btn sm" data-u="https://azure.microsoft.com/free/">${svg("external")}Criar conta gratuita do Azure</button></li>
      <li><b>Preencha.</b> Nome: <span class="mono">Hypercube</span>. Em “Supported account types”, escolha <i>Personal Microsoft accounts only</i>. Deixe o Redirect URI vazio e clique em <i>Register</i>.</li>
      <li><b>Libere o login por código.</b> No app criado, abra <i>Authentication</i>, ative <i>Allow public client flows</i> e salve.</li>
      <li><b>Peça aprovação da Mojang.</b> Envie o formulário de acesso à API do Minecraft com o Client ID do app. Até ser aprovado, o login para na última etapa com um aviso.
        <button class="btn sm" data-u="https://help.minecraft.net/hc/en-us/articles/16254801392141">${svg("external")}Abrir instruções da Mojang</button></li>
      <li><b>Cole o Client ID aqui.</b> É o “Application (client) ID” na página <i>Overview</i> do app.
        <input class="input mono" id="g-cid" placeholder="00000000-0000-0000-0000-000000000000" value="${esc(S.settings.msClientId || "")}" style="margin-top:10px"></li>
    </ol>`,
    foot: `<span class="hint grow">O token da conta fica salvo só neste computador e nunca entra nos backups.</span><button class="btn ghost" data-c>Fechar</button><button class="btn primary" id="g-ok">${svg("microsoft")}${thenLogin ? "Salvar e entrar" : "Salvar"}</button>`,
    onMount(m, close) {
      $$("[data-u]", m).forEach(b => b.onclick = () => open(b.dataset.u));
      $("[data-c]", m).onclick = close;
      const inp = $("#g-cid", m);
      $("#g-ok", m).onclick = async () => {
        const cid = inp.value.trim();
        if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(cid)) { toast("Cole um Client ID válido (formato 00000000-0000-…)", "error"); inp.focus(); return; }
        S.settings.msClientId = cid;
        try { await api("POST", "/api/settings", S.settings); } catch (e) { fail(e); return; }
        close();
        if (thenLogin) setTimeout(() => requestMCCode(cid), 250); else toast("Client ID salvo", "ok");
      };
    },
  });
}

function onEvent(ev) {
  const d = ev.data;
  switch (ev.type) {
    case "task": {
      if (d.status === "running") S.tasks[d.id] = d;
      else {
        S.tasks[d.id] = d;
        if (d.status === "error") toast(`${d.title}: ${d.error.split("\n")[0]}`, "error");
        setTimeout(() => { delete S.tasks[d.id]; renderTasks(); if (d.instance) refreshInstanceUI(d.instance); }, d.status === "error" ? 9000 : 1600);
      }
      renderTasks();
      if (d.instance) refreshInstanceUI(d.instance);
      break;
    }
    case "instance": {
      const idx = S.instances.findIndex(i => i.id === d.instance.id);
      if (idx >= 0) S.instances[idx] = d.instance; else S.instances.unshift(d.instance);
      if (d.running) S.running[d.instance.id] = true; else delete S.running[d.instance.id];
      renderSideInstances();
      if (S.route.view === "library") { if ($(`.card[data-id="${d.instance.id}"]`)) updateCard(d.instance.id); else renderMain(false); }
      refreshInstanceUI(d.instance.id);
      break;
    }
    case "removed":
      S.instances = S.instances.filter(i => i.id !== d);
      renderSideInstances();
      if (S.route.view === "library") renderMain(false);
      break;
    case "log": {
      (S.logs[d.instance] = S.logs[d.instance] || []).push(d.line);
      if (S.logs[d.instance].length > 5000) S.logs[d.instance].splice(0, 1000);
      if (S.route.view === "instance" && S.route.id === d.instance && S.route.tab === "logs") { const b = $("#tab-body"); b && b._append && b._append(d.line); }
      break;
    }
    case "exit":
      if (d.crashed) toast(`${inst(d.instance)?.name || "O jogo"} fechou com erro (código ${d.code}). Veja o console.`, "error");
      break;
    case "mods":
      if (S.route.view === "instance" && S.route.id === d) reloadModsSoon();
      break;
    case "toast": toast(d.text, d.kind); break;
    case "profile": S.profile = d; renderProfileChip(); renderProfileSection(); break;
    case "mcaccount": {
      const was = S.mcAccount;
      S.mcAccount = d || null; renderProfileChip(); renderMCSection();
      if (S._mcClose) { S._mcClose(); if (d) toast(`Conectado como ${d.name}`, "ok"); }
      else if (!was && d) toast(`Conectado como ${d.name}`, "ok");
      break;
    }
    case "mcaccount-error": if (S._mcClose) S._mcClose(); toast("Login Minecraft: " + d, "error"); break;
  }
}
function refreshInstanceUI(id) {
  if (S.route.view === "library" && $(`.card[data-id="${id}"]`)) updateCard(id);
  if (S.route.view === "instance" && S.route.id === id) renderHero();
}

function connectEvents() {
  const es = new EventSource("/api/events?t=" + encodeURIComponent(TOKEN));
  es.onmessage = m => { try { onEvent(JSON.parse(m.data)); } catch (e) { console.error(e); } };
  let wasDown = false;
  es.onerror = () => { wasDown = true; };
  es.onopen = () => { if (wasDown) { wasDown = false; refreshState(); } };
}

async function refreshState() {
  const st = await api("GET", "/api/state");
  S.instances = st.instances; S.running = st.running || {}; S.settings = st.settings; S.profile = st.profile || { name: "" }; S.mcAccount = st.mcAccount || null;
  S.version = st.version; S.dataDir = st.dataDir;
  S.tasks = {}; (st.tasks || []).forEach(t => S.tasks[t.id] = t);
  document.body.classList.toggle("reduced", !!S.settings.reducedMotion);
}

document.addEventListener("keydown", e => {
  if (e.key === "Escape") { if (openMenuEl) { closeMenu(); return; } if (S.selected.size) { S.selected.clear(); refreshSelection(); } }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "f") {
    const s = $("#mod-q") || $("#b-q") || $("#lib-q") || $("#log-q");
    if (s) { e.preventDefault(); s.focus(); s.select(); }
  }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "a" && S.route.view === "instance" && S.route.tab === "mods" && !/INPUT|TEXTAREA/.test(document.activeElement.tagName)) {
    e.preventDefault(); $$("#mod-groups .mod").forEach(r => S.selected.add(r.dataset.key)); refreshSelection();
  }
  if (e.key === "F5" || ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "r")) { e.preventDefault(); refreshState().then(() => { renderSidebar(); renderMain(false); }); }
});
let dragDepth = 0, dz = null;
const hasFiles = e => e.dataTransfer && [...e.dataTransfer.types].includes("Files");
window.addEventListener("dragenter", e => {
  if (!hasFiles(e)) return;
  dragDepth++;
  if (S.route.view === "instance" && !dz) {
    dz = document.createElement("div"); dz.className = "dropzone";
    dz.innerHTML = `<div>${svg("upload")}<h3>Solte para adicionar à ${esc(inst(S.route.id)?.name || "")}</h3><p class="muted">Arquivos .jar vão para a pasta mods</p></div>`;
    document.body.appendChild(dz);
  }
});
window.addEventListener("dragleave", e => { if (!hasFiles(e)) return; if (--dragDepth <= 0) { dragDepth = 0; dz && dz.remove(); dz = null; } });
window.addEventListener("dragover", e => { if (hasFiles(e)) e.preventDefault(); });
window.addEventListener("drop", e => {
  if (!hasFiles(e)) return;
  e.preventDefault(); dragDepth = 0; dz && dz.remove(); dz = null;
  if (S.route.view === "instance") uploadJars([...e.dataTransfer.files]);
  else toast("Abra uma instância para adicionar mods", "info");
});
document.addEventListener("contextmenu", e => { if (!e.target.closest("input,textarea,.console")) e.preventDefault(); });
window.addEventListener("resize", debounce(() => placePills(), 100));

(async () => {
  try {
    await refreshState();
  } catch (e) {
    document.body.innerHTML = `<div class="empty" style="padding-top:30vh"><h3>Hypercube não está respondendo</h3><p>${esc(e.message)}</p></div>`;
    return;
  }
  renderSidebar();
  renderMain(true);
  connectEvents();
  if (!S.profile.name) showOnboarding();
  loadVersions().catch(() => { });
})();
