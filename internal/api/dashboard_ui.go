package api

import "net/http"

func (s *Server) handleDashboardUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>WasmDB Dashboard</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html { height: 100%; }
  body {
    font-family: "JetBrains Mono", "Fira Code", "SF Mono", "Cascadia Code", monospace;
    background: #0a0a0a;
    color: #b0b0b0;
    min-height: 100vh;
    font-size: 14px;
    line-height: 1.5;
  }

  /* Header */
  header {
    padding: 10px 24px;
    border-bottom: 1px solid #333;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: #0a0a0a;
  }
  header .left { display: flex; align-items: center; gap: 12px; }
  header .title { color: #4ec94e; font-weight: bold; font-size: 15px; }
  header .sep { color: #333; }
  header .subtitle { color: #5ccfe6; font-size: 13px; }
  header nav { display: flex; gap: 16px; }
  header nav a {
    color: #808080;
    text-decoration: none;
    font-size: 13px;
    transition: color 0.2s;
  }
  header nav a:hover { color: #4ec94e; }
  header nav a.active { color: #4ec94e; }

  /* Auth overlay */
  #auth-overlay {
    position: fixed; inset: 0;
    background: #0a0a0a;
    display: flex; align-items: center; justify-content: center;
    z-index: 1000;
  }
  #auth-overlay.hidden { display: none; }
  .login-box {
    border: 1px solid #333;
    padding: 32px;
    max-width: 360px;
    width: 100%;
  }
  .login-box h2 { color: #4ec94e; margin-bottom: 16px; font-size: 16px; }
  .login-box input {
    width: 100%;
    padding: 8px 12px;
    margin-bottom: 12px;
    background: #111;
    border: 1px solid #333;
    color: #e0e0e0;
    font-family: inherit;
    font-size: 14px;
    outline: none;
  }
  .login-box input:focus { border-color: #4ec94e; }
  .login-box button {
    width: 100%;
    padding: 8px;
    background: #1a3a1a;
    border: 1px solid #4ec94e;
    color: #4ec94e;
    font-family: inherit;
    font-size: 14px;
    cursor: pointer;
  }
  .login-box button:hover { background: #2a4a2a; }
  .login-box .error { color: #ff6b6b; font-size: 12px; margin-bottom: 8px; }

  /* Main content */
  main {
    max-width: 1200px;
    margin: 0 auto;
    padding: 24px;
  }

  /* Page list (sidebar-style) */
  .page-tabs {
    display: flex;
    gap: 8px;
    margin-bottom: 24px;
    flex-wrap: wrap;
    border-bottom: 1px solid #222;
    padding-bottom: 12px;
  }
  .page-tab {
    padding: 6px 14px;
    background: #111;
    border: 1px solid #222;
    color: #808080;
    cursor: pointer;
    font-family: inherit;
    font-size: 13px;
    transition: all 0.2s;
  }
  .page-tab:hover { border-color: #444; color: #b0b0b0; }
  .page-tab.active {
    border-color: #4ec94e;
    color: #4ec94e;
    background: #0d1a0d;
  }

  /* Page container */
  .page-content {
    min-height: 300px;
  }
  .page-header {
    margin-bottom: 16px;
  }
  .page-header h2 {
    color: #e0e0e0;
    font-size: 16px;
    font-weight: bold;
  }
  .page-header p {
    color: #606060;
    font-size: 13px;
    margin-top: 4px;
  }

  /* Empty state */
  .empty-state {
    text-align: center;
    padding: 80px 24px;
    color: #444;
  }
  .empty-state h2 { color: #555; font-size: 18px; margin-bottom: 8px; }
  .empty-state p { font-size: 13px; max-width: 500px; margin: 0 auto; }
  .empty-state code {
    background: #111;
    padding: 2px 6px;
    border: 1px solid #222;
    color: #4ec94e;
    font-size: 12px;
  }

  /* Loading */
  .loading {
    text-align: center;
    padding: 40px;
    color: #444;
  }
  .loading::after {
    content: '';
    animation: dots 1.5s steps(3, end) infinite;
  }
  @keyframes dots {
    0% { content: '.'; }
    33% { content: '..'; }
    66% { content: '...'; }
  }

  /* Error banner */
  .error-banner {
    background: #1a0a0a;
    border: 1px solid #ff6b6b;
    color: #ff6b6b;
    padding: 8px 16px;
    margin-bottom: 16px;
    font-size: 13px;
  }

  /* A2UI components (shared with chat) */
  .a2ui-table-wrap { overflow-x: auto; margin: 8px 0; }
  .a2ui-datatable {
    border-collapse: collapse;
    width: 100%;
    font-size: 13px;
  }
  .a2ui-datatable th {
    text-align: left;
    padding: 6px 12px 6px 0;
    border-bottom: 1px solid #4ec94e;
    color: #4ec94e;
    font-weight: bold;
    white-space: nowrap;
  }
  .a2ui-datatable td {
    padding: 5px 12px 5px 0;
    border-bottom: 1px solid #1a1a1a;
    color: #b0b0b0;
  }
  .a2ui-datatable caption {
    text-align: left;
    color: #808080;
    font-style: italic;
    padding-bottom: 6px;
    font-size: 12px;
  }
  .a2ui-card {
    border: 1px solid #333;
    padding: 12px 16px;
    margin: 8px 0;
    max-width: 600px;
  }
  .a2ui-card-title {
    color: #5ccfe6;
    font-weight: bold;
    margin-bottom: 8px;
    border-bottom: 1px solid #222;
    padding-bottom: 4px;
  }
  .a2ui-text { margin: 1px 0; }
  .a2ui-text .a2ui-label {
    color: #808080;
    margin-right: 6px;
  }
  .a2ui-text .a2ui-label::after { content: ':'; }
  .a2ui-text-bold { color: #e0e0e0; font-weight: bold; }
  .a2ui-text-dim { color: #606060; }
  .a2ui-text-code {
    background: #111;
    padding: 1px 5px;
    border: 1px solid #222;
    color: #ffa657;
    font-size: 13px;
  }
  .a2ui-column { display: flex; flex-direction: column; }
  .a2ui-row { display: flex; flex-direction: row; gap: 16px; flex-wrap: wrap; }
  .a2ui-divider {
    border: none;
    border-top: 1px solid #222;
    margin: 12px 0;
  }

  /* Refresh indicator */
  .refresh-info {
    font-size: 11px;
    color: #444;
    text-align: right;
    margin-top: 8px;
  }

  @media (max-width: 768px) {
    main { padding: 12px; }
    .a2ui-datatable { font-size: 12px; }
    .a2ui-datatable th,
    .a2ui-datatable td { padding: 4px 8px 4px 0; }
    .a2ui-card { max-width: 100%; }
    .a2ui-row { flex-direction: column; gap: 4px; }
  }
</style>
</head>
<body>

<div id="auth-overlay">
  <div class="login-box">
    <h2>─── WasmDB Dashboard</h2>
    <div id="login-error" class="error" style="display:none"></div>
    <input type="email" id="login-email" placeholder="email" autocomplete="email">
    <input type="password" id="login-password" placeholder="password" autocomplete="current-password">
    <button onclick="doLogin()">login</button>
  </div>
</div>

<header>
  <div class="left">
    <span class="title">WasmDB</span>
    <span class="sep">│</span>
    <span class="subtitle">Dashboard</span>
  </div>
  <nav>
    <a href="/ui" class="active">dashboard</a>
    <a href="/chat">chat</a>
  </nav>
</header>

<main id="main-content">
  <div class="loading" id="loading">loading</div>
</main>

<script>
let token = localStorage.getItem('wasmdb_token');
let pages = [];
let activePage = null;
let refreshTimer = null;

async function apiFetch(path, opts = {}) {
  const headers = opts.headers || {};
  if (token) headers['Authorization'] = 'Bearer ' + token;
  headers['Content-Type'] = headers['Content-Type'] || 'application/json';
  const resp = await fetch(path, { ...opts, headers });
  if (resp.status === 401) {
    localStorage.removeItem('wasmdb_token');
    document.getElementById('auth-overlay').classList.remove('hidden');
    throw new Error('unauthorized');
  }
  return resp;
}

async function doLogin() {
  const email = document.getElementById('login-email').value;
  const password = document.getElementById('login-password').value;
  const errEl = document.getElementById('login-error');
  errEl.style.display = 'none';
  try {
    const resp = await fetch('/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    });
    if (!resp.ok) {
      const data = await resp.json().catch(() => ({}));
      throw new Error(data.error || 'login failed');
    }
    const data = await resp.json();
    token = data.token;
    localStorage.setItem('wasmdb_token', token);
    document.getElementById('auth-overlay').classList.add('hidden');
    loadDashboard();
  } catch (e) {
    errEl.textContent = e.message;
    errEl.style.display = 'block';
  }
}

document.getElementById('login-password').addEventListener('keydown', e => {
  if (e.key === 'Enter') doLogin();
});

async function loadDashboard() {
  const main = document.getElementById('main-content');
  main.innerHTML = '<div class="loading" id="loading">loading</div>';
  try {
    const resp = await apiFetch('/v1/ui-configs');
    if (!resp.ok) throw new Error('failed to load ui configs');
    const all = await resp.json();
    pages = all.filter(p => p.enabled).sort((a, b) => a.sort_order - b.sort_order);
    if (pages.length === 0) {
      main.innerHTML = '<div class="empty-state">' +
        '<h2>No dashboard pages configured</h2>' +
        '<p>Use the chat agent to create UI pages, or create them via the API. Try asking:<br><br>' +
        '<code>Create a dashboard page that shows a summary of my data</code></p>' +
        '</div>';
      return;
    }
    renderPageList();
    selectPage(pages[0].name);
  } catch (e) {
    if (e.message !== 'unauthorized') {
      main.innerHTML = '<div class="error-banner">Error: ' + escapeHtml(e.message) + '</div>';
    }
  }
}

function renderPageList() {
  const main = document.getElementById('main-content');
  let html = '<div class="page-tabs" id="page-tabs">';
  for (const p of pages) {
    html += '<button class="page-tab" data-name="' + escapeHtml(p.name) + '" onclick="selectPage(\'' + escapeHtml(p.name) + '\')">' + escapeHtml(p.title || p.name) + '</button>';
  }
  html += '</div><div class="page-content" id="page-content"><div class="loading">loading</div></div>';
  main.innerHTML = html;
}

async function selectPage(name) {
  activePage = name;
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }

  // Update tab highlight.
  document.querySelectorAll('.page-tab').forEach(t => {
    t.classList.toggle('active', t.dataset.name === name);
  });

  const container = document.getElementById('page-content');
  container.innerHTML = '<div class="loading">loading</div>';

  await renderPage(name, container);

  // Set up auto-refresh if configured.
  const page = pages.find(p => p.name === name);
  if (page && page.auto_refresh_seconds > 0) {
    refreshTimer = setInterval(() => {
      if (activePage === name) renderPage(name, document.getElementById('page-content'));
    }, page.auto_refresh_seconds * 1000);
  }
}

async function renderPage(name, container) {
  try {
    const resp = await apiFetch('/v1/ui-configs/' + encodeURIComponent(name) + '/render', { method: 'POST' });
    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}));
      throw new Error(err.error || 'render failed');
    }
    const result = await resp.json();

    let html = '<div class="page-header">';
    if (result.title) html += '<h2>' + escapeHtml(result.title) + '</h2>';
    if (result.description) html += '<p>' + escapeHtml(result.description) + '</p>';
    html += '</div><div id="a2ui-root"></div>';

    container.innerHTML = html;

    // Parse the surface JSON — it may contain template variables like {{data.field}}.
    let surfaceStr = result.surface_json;
    if (result.data) {
      surfaceStr = templateReplace(surfaceStr, result.data);
    }

    try {
      const surface = JSON.parse(surfaceStr);
      renderA2UI(surface, document.getElementById('a2ui-root'));
    } catch (parseErr) {
      container.innerHTML += '<div class="error-banner">Invalid A2UI surface JSON: ' + escapeHtml(parseErr.message) + '</div>';
    }

    // Add refresh timestamp.
    const page = pages.find(p => p.name === name);
    if (page && page.auto_refresh_seconds > 0) {
      container.innerHTML += '<div class="refresh-info">auto-refresh: ' + page.auto_refresh_seconds + 's │ last: ' + new Date().toLocaleTimeString() + '</div>';
    }
  } catch (e) {
    container.innerHTML = '<div class="error-banner">Error: ' + escapeHtml(e.message) + '</div>';
  }
}

// Template replacement: {{key}} or {{key.subkey}} patterns.
function templateReplace(str, data) {
  return str.replace(/\{\{([^}]+)\}\}/g, (match, path) => {
    const keys = path.trim().split('.');
    let val = data;
    for (const k of keys) {
      if (val == null) return match;
      val = val[k];
    }
    if (val == null) return match;
    if (typeof val === 'object') return JSON.stringify(val);
    return String(val);
  });
}

// A2UI renderer (same as chat UI).
function renderA2UI(surface, container) {
  const comps = surface.components || [];
  const idx = {};
  for (const c of comps) idx[c.id] = c;
  const root = idx['root'];
  if (!root) { container.innerHTML = '<div class="error-banner">No root component</div>'; return; }
  const el = renderComponent(root, idx);
  if (el) container.appendChild(el);
}

function renderComponent(comp, idx) {
  if (!comp) return null;
  const p = comp.properties || {};

  // Render children helper.
  function renderChildren(parent) {
    for (const cid of (comp.children || [])) {
      const child = idx[cid];
      if (child) {
        const el = renderComponent(child, idx);
        if (el) parent.appendChild(el);
      }
    }
  }

  switch (comp.type) {
    case 'Column': {
      const div = document.createElement('div');
      div.className = 'a2ui-column';
      renderChildren(div);
      return div;
    }
    case 'Row': {
      const div = document.createElement('div');
      div.className = 'a2ui-row';
      renderChildren(div);
      return div;
    }
    case 'DataTable': {
      const wrap = document.createElement('div');
      wrap.className = 'a2ui-table-wrap';
      const table = document.createElement('table');
      table.className = 'a2ui-datatable';
      if (p.caption) {
        const cap = document.createElement('caption');
        cap.textContent = p.caption;
        table.appendChild(cap);
      }
      const cols = p.columns || [];
      const rows = p.rows || [];
      if (cols.length > 0) {
        const thead = document.createElement('thead');
        const tr = document.createElement('tr');
        for (const col of cols) {
          const th = document.createElement('th');
          th.textContent = col.label || col.key;
          tr.appendChild(th);
        }
        thead.appendChild(tr);
        table.appendChild(thead);
      }
      const tbody = document.createElement('tbody');
      for (const row of rows) {
        const tr = document.createElement('tr');
        for (const col of cols) {
          const td = document.createElement('td');
          const val = row[col.key];
          td.textContent = val != null ? String(val) : '';
          tr.appendChild(td);
        }
        tbody.appendChild(tr);
      }
      table.appendChild(tbody);
      wrap.appendChild(table);
      return wrap;
    }
    case 'Card': {
      const div = document.createElement('div');
      div.className = 'a2ui-card';
      if (p.title) {
        const t = document.createElement('div');
        t.className = 'a2ui-card-title';
        t.textContent = p.title;
        div.appendChild(t);
      }
      renderChildren(div);
      return div;
    }
    case 'Text': {
      const span = document.createElement('div');
      span.className = 'a2ui-text';
      if (p.label) {
        const lbl = document.createElement('span');
        lbl.className = 'a2ui-label';
        lbl.textContent = p.label;
        span.appendChild(lbl);
      }
      const txt = document.createElement('span');
      txt.textContent = p.value || '';
      if (p.style === 'bold') txt.className = 'a2ui-text-bold';
      else if (p.style === 'dim') txt.className = 'a2ui-text-dim';
      else if (p.style === 'code') txt.className = 'a2ui-text-code';
      span.appendChild(txt);
      return span;
    }
    case 'Divider': {
      const hr = document.createElement('hr');
      hr.className = 'a2ui-divider';
      return hr;
    }
    default:
      return null;
  }
}

function escapeHtml(s) {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

// Init.
async function init() {
  if (token) {
    try {
      const resp = await apiFetch('/v1/auth/me');
      if (resp.ok) {
        document.getElementById('auth-overlay').classList.add('hidden');
        loadDashboard();
        return;
      }
    } catch(e) {}
    localStorage.removeItem('wasmdb_token');
    token = null;
  }
  document.getElementById('loading').style.display = 'none';
}
init();
</script>
</body>
</html>
`
