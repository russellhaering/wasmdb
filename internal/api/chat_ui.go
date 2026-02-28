package api

import "net/http"

func (s *Server) handleChatUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(chatHTML))
}

const chatHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>WasmDB</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html { height: 100%; }
  body {
    font-family: "JetBrains Mono", "Fira Code", "SF Mono", "Cascadia Code", monospace;
    background: #0a0a0a;
    color: #b0b0b0;
    height: 100dvh;
    height: 100vh;
    display: flex;
    flex-direction: column;
    font-size: 14px;
    line-height: 1.5;
    overflow: hidden;
  }
  header {
    padding: 6px 16px;
    border-bottom: 1px solid #333;
    display: flex;
    align-items: center;
    gap: 0;
    background: #0a0a0a;
    color: #606060;
    font-size: 13px;
  }
  header .title {
    color: #4ec94e;
    font-weight: bold;
  }
  header .sep { color: #333; margin: 0 8px; }
  header .label { color: #5ccfe6; }
  header::before {
    content: '─── ';
    color: #333;
  }
  header::after {
    content: '';
    flex: 1;
    border-bottom: 1px solid #333;
    margin-left: 8px;
  }
  #sidebar-toggle {
    display: none;
    background: transparent;
    border: none;
    color: #606060;
    font-family: inherit;
    font-size: 16px;
    cursor: pointer;
    padding: 0 8px 0 0;
    flex-shrink: 0;
  }
  #sidebar-toggle:hover { color: #4ec94e; }
  /* Main layout with sidebar */
  #main-container {
    flex: 1;
    display: none;
    flex-direction: row;
    overflow: hidden;
    position: relative;
  }
  #sidebar {
    width: 240px;
    border-right: 1px solid #333;
    display: flex;
    flex-direction: column;
    background: #0d0d0d;
    flex-shrink: 0;
  }
  #sidebar-overlay {
    display: none;
    position: absolute;
    inset: 0;
    background: rgba(0,0,0,0.6);
    z-index: 9;
  }
  #sidebar-header {
    padding: 8px 12px;
    border-bottom: 1px solid #222;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  #sidebar-header span {
    color: #606060;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 1px;
  }
  #new-chat-btn {
    background: transparent;
    border: 1px solid #333;
    color: #4ec94e;
    font-family: inherit;
    font-size: 12px;
    padding: 2px 8px;
    cursor: pointer;
  }
  #new-chat-btn:hover { background: #1a1a1a; }
  #session-list {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0;
  }
  #session-list::-webkit-scrollbar { width: 4px; }
  #session-list::-webkit-scrollbar-track { background: transparent; }
  #session-list::-webkit-scrollbar-thumb { background: #333; border-radius: 2px; }
  .session-item {
    padding: 6px 12px;
    cursor: pointer;
    font-size: 13px;
    color: #808080;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .session-item:hover { background: #1a1a1a; color: #b0b0b0; }
  .session-item.active { color: #4ec94e; background: #111; }
  .session-item .session-title {
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
  }
  .session-item .session-delete {
    display: none;
    color: #606060;
    font-size: 11px;
    padding: 0 4px;
    margin-left: 4px;
    flex-shrink: 0;
  }
  .session-item:hover .session-delete { display: inline; }
  .session-item .session-delete:hover { color: #f07070; }
  /* A2UI table horizontal scroll */
  .a2ui-table-wrap {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
    max-width: 100%;
  }
  #chat-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  #chat {
    flex: 1;
    overflow-y: auto;
    padding: 12px 16px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  #chat::-webkit-scrollbar { width: 6px; }
  #chat::-webkit-scrollbar-track { background: transparent; }
  #chat::-webkit-scrollbar-thumb { background: #333; border-radius: 3px; }
  .msg {
    max-width: 100%;
    padding: 2px 0;
    line-height: 1.5;
    font-size: 14px;
    white-space: pre-wrap;
    word-wrap: break-word;
  }
  .msg.user {
    color: #e0e0e0;
  }
  .msg.user::before {
    content: '> ';
    color: #4ec94e;
    font-weight: bold;
  }
  .msg.assistant {
    color: #b0b0b0;
    padding-left: 2px;
  }
  .msg.assistant .tool-call {
    display: block;
    margin: 2px 0;
    padding: 1px 0;
    font-size: 13px;
    color: #606060;
  }
  .msg.assistant .tool-call .tool-name {
    color: #5ccfe6;
  }
  .msg.assistant .tool-call.error .tool-name {
    color: #f07070;
  }
  .msg.assistant .tool-call::before {
    content: '  [';
    color: #606060;
  }
  .msg.assistant .tool-call::after {
    content: ']';
    color: #606060;
  }
  .msg.system {
    color: #606060;
    font-size: 13px;
    padding: 2px 0;
  }
  /* A2UI DataTable */
  .a2ui-datatable {
    border-collapse: collapse;
    margin: 6px 0;
    font-size: 13px;
    font-family: inherit;
  }
  .a2ui-datatable th {
    color: #e0e0e0;
    font-weight: bold;
    text-align: left;
    padding: 2px 12px 2px 0;
    border-bottom: 1px solid #4ec94e;
  }
  .a2ui-datatable td {
    padding: 2px 12px 2px 0;
    color: #b0b0b0;
    border-bottom: 1px solid #1a1a1a;
  }
  .a2ui-datatable caption {
    text-align: left;
    color: #606060;
    font-size: 12px;
    padding-bottom: 4px;
  }
  /* A2UI Card */
  .a2ui-card {
    border: 1px solid #333;
    margin: 6px 0;
    padding: 8px 12px;
    max-width: 500px;
  }
  .a2ui-card-title {
    color: #5ccfe6;
    font-weight: bold;
    margin-bottom: 4px;
    font-size: 13px;
  }
  /* A2UI Text */
  .a2ui-text { margin: 1px 0; }
  .a2ui-text .a2ui-label {
    color: #606060;
    margin-right: 4px;
  }
  .a2ui-text .a2ui-label::after { content: ':'; }
  .a2ui-text-bold { color: #e0e0e0; font-weight: bold; }
  .a2ui-text-dim { color: #606060; }
  .a2ui-text-code {
    background: #1a1a1a;
    padding: 1px 4px;
    color: #e6b450;
  }
  /* A2UI layout */
  .a2ui-column { display: flex; flex-direction: column; }
  .a2ui-row { display: flex; flex-direction: row; gap: 16px; }
  .a2ui-divider {
    border: none;
    border-top: 1px solid #333;
    margin: 4px 0;
  }
  /* Thinking / activity indicator */
  .thinking {
    color: #606060;
    font-size: 13px;
  }
  .thinking::after {
    content: '';
    animation: dots 1.2s steps(4, end) infinite;
  }
  @keyframes dots {
    0%  { content: ''; }
    25% { content: '.'; }
    50% { content: '..'; }
    75% { content: '...'; }
  }
  /* Tool call pending animation */
  .tool-call.pending .tool-dots::after {
    content: '';
    animation: dots 1.2s steps(4, end) infinite;
  }
  /* Markdown styles */
  .md-rendered {
    white-space: normal;
  }
  .md-bold { color: #e0e0e0; font-weight: bold; }
  .md-italic { font-style: italic; color: #c0c0c0; }
  .md-code {
    background: #1a1a1a;
    padding: 1px 5px;
    color: #e6b450;
    border-radius: 2px;
    font-size: 13px;
  }
  .md-codeblock {
    background: #111;
    border: 1px solid #222;
    padding: 8px 12px;
    margin: 6px 0;
    overflow-x: auto;
    font-size: 13px;
    line-height: 1.4;
    color: #b0b0b0;
    white-space: pre;
  }
  .md-codeblock code {
    font-family: inherit;
    background: none;
    padding: 0;
  }
  .md-h1 {
    display: block;
    color: #e0e0e0;
    font-weight: bold;
    font-size: 18px;
    margin: 8px 0 4px;
  }
  .md-h2 {
    display: block;
    color: #e0e0e0;
    font-weight: bold;
    font-size: 16px;
    margin: 6px 0 3px;
  }
  .md-h3 {
    display: block;
    color: #e0e0e0;
    font-weight: bold;
    font-size: 14px;
    margin: 4px 0 2px;
  }
  .md-list {
    margin: 4px 0;
    padding-left: 20px;
    list-style: disc;
  }
  .md-list li {
    margin: 2px 0;
    line-height: 1.5;
  }
  .md-list.md-ol {
    list-style: decimal;
  }
  #input-area {
    padding: 8px 16px 12px;
    border-top: 1px solid #333;
    background: #0a0a0a;
    display: flex;
    gap: 8px;
    align-items: flex-end;
  }
  #input-area .prompt-char {
    color: #4ec94e;
    font-weight: bold;
    font-size: 14px;
    padding: 6px 0;
    flex-shrink: 0;
  }
  #input-area textarea {
    flex: 1;
    padding: 6px 0;
    border: none;
    background: transparent;
    color: #e0e0e0;
    font-family: inherit;
    font-size: 14px;
    resize: none;
    outline: none;
    min-height: 28px;
    max-height: 120px;
    line-height: 1.5;
  }
  #input-area textarea::placeholder {
    color: #444;
  }
  #input-area button {
    padding: 4px 12px;
    border: 1px solid #333;
    background: transparent;
    color: #4ec94e;
    font-family: inherit;
    font-size: 13px;
    cursor: pointer;
    white-space: nowrap;
    flex-shrink: 0;
  }
  #input-area button:hover { background: #1a1a1a; }
  #input-area button:disabled {
    color: #333;
    border-color: #222;
    cursor: not-allowed;
  }
  /* Auth screen - terminal style */
  #auth-screen {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  #auth-box {
    width: 340px;
    border: 1px solid #333;
    padding: 16px;
  }
  #auth-box .auth-title {
    color: #4ec94e;
    font-weight: bold;
    margin-bottom: 12px;
    text-align: center;
    font-size: 14px;
  }
  #auth-screen label {
    display: block;
    font-size: 13px;
    color: #606060;
    margin-bottom: 2px;
  }
  #auth-screen input {
    width: 100%;
    padding: 6px 8px;
    border: 1px solid #333;
    background: #111;
    color: #e0e0e0;
    font-family: inherit;
    font-size: 14px;
    outline: none;
    margin-bottom: 10px;
  }
  #auth-screen input:focus { border-color: #4ec94e; }
  #auth-screen .auth-btn {
    width: 100%;
    padding: 6px;
    border: 1px solid #4ec94e;
    background: transparent;
    color: #4ec94e;
    font-family: inherit;
    font-size: 14px;
    cursor: pointer;
    font-weight: bold;
  }
  #auth-screen .auth-btn:hover { background: #112211; }
  #auth-error {
    margin-top: 8px;
    color: #f07070;
    font-size: 13px;
    display: none;
    text-align: center;
  }

  /* ── Mobile ── */
  @media (max-width: 640px) {
    body { font-size: 13px; }
    header { padding: 6px 12px; font-size: 12px; }
    header::before { content: '── '; }
    #sidebar-toggle { display: block; }
    #sidebar {
      position: absolute;
      top: 0; left: 0; bottom: 0;
      z-index: 10;
      width: 260px;
      transform: translateX(-100%);
      transition: transform 0.2s ease;
      border-right: 1px solid #333;
    }
    #sidebar.open {
      transform: translateX(0);
    }
    #sidebar-overlay.open {
      display: block;
    }
    .session-item {
      padding: 10px 12px;
      font-size: 13px;
    }
    .session-item .session-delete {
      display: inline;
      padding: 4px 8px;
      font-size: 14px;
    }
    #chat { padding: 10px 12px; }
    .msg { font-size: 13px; }
    #input-area {
      padding: 8px 10px 12px;
      padding-bottom: max(12px, env(safe-area-inset-bottom));
    }
    #input-area textarea { font-size: 16px; }
    #input-area button { padding: 6px 14px; font-size: 14px; }
    #auth-box {
      width: 90vw;
      max-width: 340px;
      padding: 14px;
    }
    #auth-screen input { font-size: 16px; }
    .a2ui-datatable { font-size: 12px; }
    .a2ui-datatable th,
    .a2ui-datatable td { padding: 4px 8px 4px 0; }
    .a2ui-card { max-width: 100%; }
    .a2ui-row { flex-direction: column; gap: 4px; }
  }
</style>
</head>
<body>
<header>
  <button id="sidebar-toggle" onclick="toggleSidebar()">≡</button>
  <span class="title">wasmdb</span>
  <span class="sep">|</span>
  <span class="label">chat</span>
</header>

<div id="auth-screen">
  <div id="auth-box">
    <div class="auth-title">wasmdb login</div>
    <label for="email-input">email</label>
    <input id="email-input" type="email" placeholder="you@example.com" autofocus>
    <label for="password-input">password</label>
    <input id="password-input" type="password" placeholder="********">
    <button class="auth-btn" onclick="authenticate()">connect</button>
    <p id="auth-error"></p>
  </div>
</div>

<div id="main-container">
  <div id="sidebar-overlay" onclick="closeSidebar()"></div>
  <div id="sidebar">
    <div id="sidebar-header">
      <span>sessions</span>
      <button id="new-chat-btn" onclick="startNewSession()">+ new</button>
    </div>
    <div id="session-list"></div>
  </div>
  <div id="chat-area">
    <div id="chat"></div>
    <div id="input-area">
      <span class="prompt-char">$</span>
      <textarea id="msg" placeholder="ask about your databases..." rows="1"></textarea>
      <button id="send" onclick="send()">send</button>
    </div>
  </div>
</div>
<script>
const chat = document.getElementById('chat');
const msgInput = document.getElementById('msg');
const sendBtn = document.getElementById('send');
let sessionId = null; // Will be set by server or when loading a session

function toggleSidebar() {
  document.getElementById('sidebar').classList.toggle('open');
  document.getElementById('sidebar-overlay').classList.toggle('open');
}
function closeSidebar() {
  document.getElementById('sidebar').classList.remove('open');
  document.getElementById('sidebar-overlay').classList.remove('open');
}
function isMobile() { return window.innerWidth <= 640; }

(async function checkSession() {
  try {
    const resp = await fetch('/v1/auth/me');
    if (resp.ok) showChat();
  } catch (e) {}
})();

function showChat() {
  document.getElementById('auth-screen').style.display = 'none';
  document.getElementById('main-container').style.display = 'flex';
  loadSessions();
  msgInput.focus();
}

async function loadSessions() {
  try {
    const resp = await fetch('/v1/chat/sessions');
    if (!resp.ok) return;
    const data = await resp.json();
    renderSessionList(data.sessions || []);
  } catch (e) {
    console.error('Failed to load sessions:', e);
  }
}

function renderSessionList(sessions) {
  const list = document.getElementById('session-list');
  list.innerHTML = '';
  for (const s of sessions) {
    const div = document.createElement('div');
    div.className = 'session-item' + (s.id === sessionId ? ' active' : '');
    div.dataset.id = s.id;

    const titleSpan = document.createElement('span');
    titleSpan.className = 'session-title';
    titleSpan.textContent = s.title || 'New Chat';
    titleSpan.title = s.title || 'New Chat';
    div.appendChild(titleSpan);

    const delBtn = document.createElement('span');
    delBtn.className = 'session-delete';
    delBtn.textContent = '×';
    delBtn.onclick = (e) => { e.stopPropagation(); deleteSession(s.id); };
    div.appendChild(delBtn);

    div.onclick = () => switchToSession(s.id);
    list.appendChild(div);
  }
}

function startNewSession() {
  sessionId = null;
  chat.innerHTML = '';
  if (isMobile()) closeSidebar();
  // Deselect all sidebar items.
  document.querySelectorAll('.session-item').forEach(el => el.classList.remove('active'));
  msgInput.focus();
}

async function switchToSession(id) {
  sessionId = id;
  chat.innerHTML = '';
  if (isMobile()) closeSidebar();
  // Update active state in sidebar.
  document.querySelectorAll('.session-item').forEach(el => {
    el.classList.toggle('active', el.dataset.id === id);
  });
  // TODO: load and display session history from server.
  // For now, new messages in the session will continue from where the server left off.
  const placeholder = document.createElement('div');
  placeholder.className = 'msg system';
  placeholder.textContent = 'session restored — type to continue the conversation';
  chat.appendChild(placeholder);
  msgInput.focus();
}

async function deleteSession(id) {
  try {
    await fetch('/v1/chat/sessions/' + encodeURIComponent(id), { method: 'DELETE' });
    if (id === sessionId) startNewSession();
    loadSessions();
  } catch (e) {
    console.error('Failed to delete session:', e);
  }
}

async function authenticate() {
  const email = document.getElementById('email-input').value.trim();
  const password = document.getElementById('password-input').value;
  const errEl = document.getElementById('auth-error');
  if (!email || !password) {
    errEl.textContent = 'email and password required';
    errEl.style.display = 'block';
    return;
  }
  errEl.style.display = 'none';
  try {
    const resp = await fetch('/v1/auth/login', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({email, password})
    });
    if (!resp.ok) {
      const data = await resp.json().catch(() => ({}));
      errEl.textContent = data.message || 'login failed';
      errEl.style.display = 'block';
      return;
    }
    showChat();
  } catch (e) {
    errEl.textContent = 'connection error: ' + e.message;
    errEl.style.display = 'block';
  }
}

document.getElementById('password-input').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') authenticate();
});

msgInput.addEventListener('input', () => {
  msgInput.style.height = 'auto';
  msgInput.style.height = Math.min(msgInput.scrollHeight, 120) + 'px';
});

msgInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
});

function scrollToBottom() { chat.scrollTop = chat.scrollHeight; }

function addMessage(role, content) {
  const div = document.createElement('div');
  div.className = 'msg ' + role;
  div.textContent = content;
  chat.appendChild(div);
  scrollToBottom();
  return div;
}

function escapeHtml(s) {
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}

// --- A2UI Renderer ---

function renderA2UI(jsonStr, container) {
  try {
    const surface = JSON.parse(jsonStr);
    const index = {};
    for (const c of surface.components) index[c.id] = c;
    const root = index['root'];
    if (!root) return false;
    const el = renderComponent(root, index);
    if (el) { container.appendChild(el); return true; }
  } catch (e) {
    console.error('A2UI parse error:', e);
  }
  return false;
}

function renderComponent(comp, index) {
  switch (comp.type) {
    case 'Column': return renderColumn(comp, index);
    case 'Row': return renderRow(comp, index);
    case 'DataTable': return renderDataTable(comp);
    case 'Card': return renderCard(comp, index);
    case 'Text': return renderText(comp);
    case 'Divider': return renderDivider();
    default: return null;
  }
}

function renderChildren(comp, index, parent) {
  if (!comp.children) return;
  for (const cid of comp.children) {
    const child = index[cid];
    if (child) {
      const el = renderComponent(child, index);
      if (el) parent.appendChild(el);
    }
  }
}

function renderColumn(comp, index) {
  const div = document.createElement('div');
  div.className = 'a2ui-column';
  renderChildren(comp, index, div);
  return div;
}

function renderRow(comp, index) {
  const div = document.createElement('div');
  div.className = 'a2ui-row';
  renderChildren(comp, index, div);
  return div;
}

function renderDataTable(comp) {
  const p = comp.properties || {};
  const cols = p.columns || [];
  const rows = p.rows || [];
  const wrap = document.createElement('div');
  wrap.className = 'a2ui-table-wrap';
  const table = document.createElement('table');
  table.className = 'a2ui-datatable';
  if (p.caption) {
    const cap = document.createElement('caption');
    cap.textContent = p.caption;
    table.appendChild(cap);
  }
  const thead = document.createElement('thead');
  const hr = document.createElement('tr');
  for (const col of cols) {
    const th = document.createElement('th');
    th.textContent = col.label || col.key;
    hr.appendChild(th);
  }
  thead.appendChild(hr);
  table.appendChild(thead);
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

function renderCard(comp, index) {
  const div = document.createElement('div');
  div.className = 'a2ui-card';
  const p = comp.properties || {};
  if (p.title) {
    const t = document.createElement('div');
    t.className = 'a2ui-card-title';
    t.textContent = p.title;
    div.appendChild(t);
  }
  renderChildren(comp, index, div);
  return div;
}

function renderText(comp) {
  const p = comp.properties || {};
  const span = document.createElement('div');
  span.className = 'a2ui-text';
  if (p.label) {
    const lbl = document.createElement('span');
    lbl.className = 'a2ui-label';
    lbl.textContent = p.label;
    span.appendChild(lbl);
  }
  const txt = document.createElement('span');
  if (p.style === 'bold') txt.className = 'a2ui-text-bold';
  else if (p.style === 'dim') txt.className = 'a2ui-text-dim';
  else if (p.style === 'code') txt.className = 'a2ui-text-code';
  txt.textContent = p.text || '';
  span.appendChild(txt);
  return span;
}

function renderDivider() {
  const hr = document.createElement('hr');
  hr.className = 'a2ui-divider';
  return hr;
}

// --- Markdown Renderer ---

function renderMarkdown(text) {
  // Process text line-by-line for block-level elements,
  // then apply inline formatting.
  const lines = text.split('\n');
  let html = '';
  let inList = false;
  let inCodeBlock = false;
  let codeBlockContent = '';
  let codeBlockLang = '';

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Code blocks (triple backtick)
    if (line.trimStart().startsWith('` + "`" + "`" + "`" + `') && !inCodeBlock) {
      if (inList) { html += '</ul>'; inList = false; }
      inCodeBlock = true;
      codeBlockLang = line.trimStart().slice(3).trim();
      codeBlockContent = '';
      continue;
    }
    if (inCodeBlock) {
      if (line.trimStart() === '` + "`" + "`" + "`" + `') {
        inCodeBlock = false;
        html += '<pre class="md-codeblock"><code>' + escapeHtml(codeBlockContent.replace(/\n$/, '')) + '</code></pre>';
        codeBlockContent = '';
        continue;
      }
      codeBlockContent += line + '\n';
      continue;
    }

    // Blank lines
    if (line.trim() === '') {
      // Don't close a list if the next non-blank line is also a list item.
      if (inList) {
        let nextContentLine = '';
        for (let j = i + 1; j < lines.length; j++) {
          if (lines[j].trim() !== '') { nextContentLine = lines[j]; break; }
        }
        if (!nextContentLine.match(/^\s*[-*]\s+/) && !nextContentLine.match(/^\s*\d+[.)\s]\s*/)) {
          html += '</ul>'; inList = false;
        }
      }
      if (!inList) html += '\n';
      continue;
    }

    // Headers
    const hMatch = line.match(/^(#{1,3})\s+(.*)$/);
    if (hMatch) {
      if (inList) { html += '</ul>'; inList = false; }
      const level = hMatch[1].length;
      html += '<span class="md-h' + level + '">' + inlineMarkdown(escapeHtml(hMatch[2])) + '</span>\n';
      continue;
    }

    // Unordered list items
    const ulMatch = line.match(/^(\s*)[-*]\s+(.*)$/);
    if (ulMatch) {
      if (!inList) { html += '<ul class="md-list">'; inList = true; }
      html += '<li>' + inlineMarkdown(escapeHtml(ulMatch[2])) + '</li>';
      continue;
    }

    // Ordered list items
    const olMatch = line.match(/^(\s*)\d+[.)\s]\s*(.*)$/);
    if (olMatch) {
      if (!inList) { html += '<ul class="md-list md-ol">'; inList = true; }
      html += '<li>' + inlineMarkdown(escapeHtml(olMatch[2])) + '</li>';
      continue;
    }

    // Regular paragraph line
    if (inList) { html += '</ul>'; inList = false; }
    html += inlineMarkdown(escapeHtml(line)) + '\n';
  }
  if (inList) html += '</ul>';
  if (inCodeBlock) {
    // Unclosed code block — render what we have
    html += '<pre class="md-codeblock"><code>' + escapeHtml(codeBlockContent.replace(/\n$/, '')) + '</code></pre>';
  }
  return html;
}

function inlineMarkdown(escaped) {
  // Bold: **text** or __text__
  escaped = escaped.replace(/\*\*(.+?)\*\*/g, '<strong class="md-bold">$1</strong>');
  escaped = escaped.replace(/__(.+?)__/g, '<strong class="md-bold">$1</strong>');
  // Italic: *text* or _text_ (but not inside words for _)
  escaped = escaped.replace(/\*([^*]+?)\*/g, '<em class="md-italic">$1</em>');
  escaped = escaped.replace(/(^|\s)_([^_]+?)_($|\s)/g, '$1<em class="md-italic">$2</em>$3');
  // Inline code: ` + "`" + `text` + "`" + `
  escaped = escaped.replace(/` + "`" + `([^` + "`" + `]+?)` + "`" + `/g, '<code class="md-code">$1</code>');
  return escaped;
}

// --- Streaming with A2UI detection ---

let activeTextSpan = null;
// Raw text for the active streaming text span.
let activeTextRaw = '';

function handleEvent(type, data, container, toolCalls) {
  // Remove thinking indicator on first real event.
  const th = container.querySelector('.thinking');
  if (th) th.remove();

  switch (type) {
    case 'text': {
      activeTextRaw += data.text;
      // Render as streaming plain text.
      if (!activeTextSpan || activeTextSpan.parentNode !== container ||
          activeTextSpan !== container.lastElementChild) {
        activeTextSpan = document.createElement('span');
        activeTextSpan.className = 'text-content';
        container.appendChild(activeTextSpan);
      }
      activeTextSpan.textContent = activeTextRaw;
      break;
    }

    case 'artifact': {
      // Finalize any pending streaming text as markdown.
      if (activeTextSpan && activeTextRaw.trim()) {
        const md = document.createElement('div');
        md.className = 'text-content md-rendered';
        md.innerHTML = renderMarkdown(activeTextRaw);
        activeTextSpan.parentNode.replaceChild(md, activeTextSpan);
      } else if (activeTextSpan) {
        activeTextSpan.remove();
      }
      activeTextSpan = null;
      activeTextRaw = '';
      // Render the A2UI surface directly.
      renderA2UI(data.json, container);
      break;
    }

    case 'tool_start':
      // Finalize any pending streaming text as markdown before the tool call.
      if (activeTextSpan && activeTextRaw.trim()) {
        const md = document.createElement('div');
        md.className = 'text-content md-rendered';
        md.innerHTML = renderMarkdown(activeTextRaw);
        activeTextSpan.parentNode.replaceChild(md, activeTextSpan);
      } else if (activeTextSpan) {
        activeTextSpan.remove();
      }
      activeTextSpan = null;
      activeTextRaw = '';
      const toolDiv = document.createElement('div');
      toolDiv.className = 'tool-call pending';
      toolDiv.id = 'tool-' + data.id;
      toolDiv.innerHTML = '<span class="tool-name">' + escapeHtml(data.tool) + '</span> <span class="tool-dots"></span>';
      container.appendChild(toolDiv);
      toolCalls[data.id] = data.tool;
      break;

    case 'tool_result': {
      const el = document.getElementById('tool-' + data.id);
      if (el) {
        const name = toolCalls[data.id] || 'tool';
        el.className = 'tool-call' + (data.error ? ' error' : '');
        el.innerHTML = '<span class="tool-name">' + escapeHtml(name) + '</span> ' + (data.error ? 'error' : 'done');
      }
      break;
    }

    case 'error': {
      const errSpan = document.createElement('span');
      errSpan.style.color = '#f07070';
      errSpan.textContent = '\nerror: ' + data.error;
      container.appendChild(errSpan);
      break;
    }

    case 'done':
      // Finalize any trailing streaming text as markdown.
      if (activeTextSpan && activeTextRaw.trim()) {
        const md = document.createElement('div');
        md.className = 'text-content md-rendered';
        md.innerHTML = renderMarkdown(activeTextRaw);
        activeTextSpan.parentNode.replaceChild(md, activeTextSpan);
      } else if (activeTextSpan) {
        activeTextSpan.remove();
      }
      activeTextSpan = null;
      activeTextRaw = '';
      break;
  }
  scrollToBottom();
}

// Wrap a DocumentFragment's children into a single container div for replaceChild.
function appendMarkdownSegment(text, container) {
  if (!text) return null;
  const div = document.createElement('div');
  div.className = 'text-content md-rendered';
  div.innerHTML = renderMarkdown(text);
  container.appendChild(div);
  return div;
}

async function send() {
  const text = msgInput.value.trim();
  if (!text) return;
  msgInput.value = '';
  msgInput.style.height = 'auto';
  sendBtn.disabled = true;
  addMessage('user', text);

  const assistantDiv = document.createElement('div');
  assistantDiv.className = 'msg assistant';
  chat.appendChild(assistantDiv);

  activeTextRaw = '';
  activeTextSpan = null;
  let toolCalls = {};

  const thinkingEl = document.createElement('span');
  thinkingEl.className = 'thinking';
  thinkingEl.textContent = 'thinking';
  assistantDiv.appendChild(thinkingEl);
  scrollToBottom();

  try {
    const body = {message: text};
    if (sessionId) body.session_id = sessionId;
    const resp = await fetch('/v1/chat', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(body)
    });
    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}));
      if (resp.status === 401) {
        assistantDiv.textContent = 'session expired — reload to sign in again';
      } else {
        assistantDiv.textContent = 'error: ' + (err.message || resp.statusText);
      }
      sendBtn.disabled = false;
      return;
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let eventType = '';
    while (true) {
      const {done, value} = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, {stream: true});
      const lines = buffer.split('\n');
      buffer = lines.pop();
      for (const line of lines) {
        if (line.startsWith('event: ')) {
          eventType = line.slice(7);
        } else if (line.startsWith('data: ') && eventType) {
          const data = JSON.parse(line.slice(6));
          if (eventType === 'session' && data.session_id) {
            sessionId = data.session_id;
            // Refresh sidebar to show the new session once done.
          } else {
            handleEvent(eventType, data, assistantDiv, toolCalls);
          }
          eventType = '';
        }
      }
    }
  } catch (err) {
    if (!assistantDiv.textContent) {
      assistantDiv.textContent = 'connection error: ' + err.message;
    }
  }
  sendBtn.disabled = false;
  msgInput.focus();
  // Refresh session list after message completes (persistence is async).
  setTimeout(loadSessions, 500);
}

msgInput.focus();
</script>
</body>
</html>
`
