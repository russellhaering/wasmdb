// chat.js — the chat client. On message completion,
// ```surface-ref {"page":"<name>"} fences are replaced with a live embed of the
// stored page via SurfaceUI.mount; all other fenced blocks render as ordinary
// code blocks. Auth is cookie-based through auth.js.
(function () {
  'use strict';

  var chat = document.getElementById('chat');
  var msgInput = document.getElementById('msg');
  var sendBtn = document.getElementById('send');
  var sessionId = null;
  var inflight = false;
  var queuedMessages = [];

  document.getElementById('sidebar-toggle').onclick = toggleSidebar;
  document.getElementById('sidebar-overlay').onclick = closeSidebar;
  document.getElementById('new-chat-btn').onclick = startNewSession;
  sendBtn.onclick = send;

  function toggleSidebar() {
    document.getElementById('sidebar').classList.toggle('open');
    document.getElementById('sidebar-overlay').classList.toggle('open');
  }
  function closeSidebar() {
    document.getElementById('sidebar').classList.remove('open');
    document.getElementById('sidebar-overlay').classList.remove('open');
  }
  function isMobile() { return window.innerWidth <= 640; }

  function showChat() {
    document.getElementById('main-container').style.display = 'flex';
    loadSessions();
    msgInput.focus();
  }

  function onUnauthorized() {
    WasmdbAuth.showLogin(function () { showChat(); });
  }

  function loadSessions() {
    fetch('/v1/chat/sessions').then(function (resp) {
      if (!resp.ok) return null;
      return resp.json();
    }).then(function (data) {
      if (data) renderSessionList(data.sessions || []);
    }).catch(function (e) { console.error('Failed to load sessions:', e); });
  }

  function renderSessionList(sessions) {
    var list = document.getElementById('session-list');
    list.innerHTML = '';
    for (var i = 0; i < sessions.length; i++) {
      (function (s) {
        var div = document.createElement('div');
        div.className = 'session-item' + (s.id === sessionId ? ' active' : '');
        div.dataset.id = s.id;
        var titleSpan = document.createElement('span');
        titleSpan.className = 'session-title';
        titleSpan.textContent = s.title || 'New Chat';
        titleSpan.title = s.title || 'New Chat';
        div.appendChild(titleSpan);
        var delBtn = document.createElement('span');
        delBtn.className = 'session-delete';
        delBtn.textContent = '×';
        delBtn.onclick = function (e) { e.stopPropagation(); deleteSession(s.id); };
        div.appendChild(delBtn);
        div.onclick = function () { switchToSession(s.id); };
        list.appendChild(div);
      })(sessions[i]);
    }
  }

  function startNewSession() {
    if (inflight) {
      addMessage('system', 'agent is still running — queued messages will continue in this session first');
      return;
    }
    sessionId = null;
    queuedMessages = [];
    updateSendButtonState();
    chat.innerHTML = '';
    if (isMobile()) closeSidebar();
    document.querySelectorAll('.session-item').forEach(function (el) { el.classList.remove('active'); });
    msgInput.focus();
  }

  function switchToSession(id) {
    if (inflight) {
      addMessage('system', 'agent is still running — wait for turn completion before switching sessions');
      return;
    }
    sessionId = id;
    queuedMessages = [];
    updateSendButtonState();
    chat.innerHTML = '';
    if (isMobile()) closeSidebar();
    document.querySelectorAll('.session-item').forEach(function (el) {
      el.classList.toggle('active', el.dataset.id === id);
    });
    var placeholder = document.createElement('div');
    placeholder.className = 'msg system';
    placeholder.textContent = 'session restored — type to continue the conversation';
    chat.appendChild(placeholder);
    msgInput.focus();
  }

  function deleteSession(id) {
    fetch('/v1/chat/sessions/' + encodeURIComponent(id), { method: 'DELETE' })
      .then(function () { if (id === sessionId) startNewSession(); loadSessions(); })
      .catch(function (e) { console.error('Failed to delete session:', e); });
  }

  msgInput.addEventListener('input', function () {
    msgInput.style.height = 'auto';
    msgInput.style.height = Math.min(msgInput.scrollHeight, 120) + 'px';
  });
  msgInput.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
  });

  function scrollToBottom() { chat.scrollTop = chat.scrollHeight; }

  function addMessage(role, content) {
    var div = document.createElement('div');
    div.className = 'msg ' + role;
    div.textContent = content;
    chat.appendChild(div);
    scrollToBottom();
    return div;
  }

  function escapeHtml(s) { var d = document.createElement('div'); d.textContent = s; return d.innerHTML; }

  // --- Markdown Renderer ---

  function renderMarkdown(text) {
    var lines = text.split('\n');
    var html = '';
    var inList = false;
    var inCodeBlock = false;
    var codeBlockContent = '';

    for (var i = 0; i < lines.length; i++) {
      var line = lines[i];

      if (line.trimStart().startsWith('```') && !inCodeBlock) {
        if (inList) { html += '</ul>'; inList = false; }
        inCodeBlock = true;
        codeBlockContent = '';
        continue;
      }
      if (inCodeBlock) {
        if (line.trimStart() === '```') {
          inCodeBlock = false;
          html += '<pre class="md-codeblock"><code>' + escapeHtml(codeBlockContent.replace(/\n$/, '')) + '</code></pre>';
          codeBlockContent = '';
          continue;
        }
        codeBlockContent += line + '\n';
        continue;
      }

      if (line.trim() === '') {
        if (inList) {
          var nextContentLine = '';
          for (var j = i + 1; j < lines.length; j++) {
            if (lines[j].trim() !== '') { nextContentLine = lines[j]; break; }
          }
          if (!nextContentLine.match(/^\s*[-*]\s+/) && !nextContentLine.match(/^\s*\d+[.)\s]\s*/)) {
            html += '</ul>'; inList = false;
          }
        }
        if (!inList) html += '\n';
        continue;
      }

      var hMatch = line.match(/^(#{1,3})\s+(.*)$/);
      if (hMatch) {
        if (inList) { html += '</ul>'; inList = false; }
        var level = hMatch[1].length;
        html += '<span class="md-h' + level + '">' + inlineMarkdown(escapeHtml(hMatch[2])) + '</span>\n';
        continue;
      }

      var ulMatch = line.match(/^(\s*)[-*]\s+(.*)$/);
      if (ulMatch) {
        if (!inList) { html += '<ul class="md-list">'; inList = true; }
        html += '<li>' + inlineMarkdown(escapeHtml(ulMatch[2])) + '</li>';
        continue;
      }

      var olMatch = line.match(/^(\s*)\d+[.)\s]\s*(.*)$/);
      if (olMatch) {
        if (!inList) { html += '<ul class="md-list md-ol">'; inList = true; }
        html += '<li>' + inlineMarkdown(escapeHtml(olMatch[2])) + '</li>';
        continue;
      }

      if (inList) { html += '</ul>'; inList = false; }
      html += inlineMarkdown(escapeHtml(line)) + '\n';
    }
    if (inList) html += '</ul>';
    if (inCodeBlock) {
      html += '<pre class="md-codeblock"><code>' + escapeHtml(codeBlockContent.replace(/\n$/, '')) + '</code></pre>';
    }
    return html;
  }

  function inlineMarkdown(escaped) {
    escaped = escaped.replace(/\*\*(.+?)\*\*/g, '<strong class="md-bold">$1</strong>');
    escaped = escaped.replace(/__(.+?)__/g, '<strong class="md-bold">$1</strong>');
    escaped = escaped.replace(/\*([^*]+?)\*/g, '<em class="md-italic">$1</em>');
    escaped = escaped.replace(/(^|\s)_([^_]+?)_($|\s)/g, '$1<em class="md-italic">$2</em>$3');
    escaped = escaped.replace(/`([^`]+?)`/g, '<code class="md-code">$1</code>');
    return escaped;
  }

  // --- Assistant content finalization (markdown + surface-ref embeds) ---

  // buildAssistantContent turns the completed assistant markdown into a
  // DocumentFragment, replacing ```surface-ref {"page":"..."} fences with live
  // page embeds.
  function buildAssistantContent(raw) {
    var frag = document.createDocumentFragment();
    var re = /```surface-ref[ \t]*\r?\n([\s\S]*?)\r?\n?```/g;
    var lastIndex = 0;
    var m;
    while ((m = re.exec(raw)) !== null) {
      var before = raw.slice(lastIndex, m.index);
      if (before.trim()) {
        var md = document.createElement('div');
        md.className = 'text-content md-rendered';
        md.innerHTML = renderMarkdown(before);
        frag.appendChild(md);
      }
      var pageName = null;
      try {
        var parsed = JSON.parse(m[1].trim());
        if (parsed && typeof parsed.page === 'string') pageName = parsed.page;
      } catch (e) { /* leave pageName null */ }
      if (pageName) {
        frag.appendChild(buildEmbed(pageName));
      } else {
        // Malformed surface-ref: fall back to showing the raw block.
        var fb = document.createElement('div');
        fb.className = 'text-content md-rendered';
        fb.innerHTML = renderMarkdown('```\n' + m[1] + '\n```');
        frag.appendChild(fb);
      }
      lastIndex = re.lastIndex;
    }
    var tail = raw.slice(lastIndex);
    if (tail.trim()) {
      var mdTail = document.createElement('div');
      mdTail.className = 'text-content md-rendered';
      mdTail.innerHTML = renderMarkdown(tail);
      frag.appendChild(mdTail);
    }
    return frag;
  }

  function buildEmbed(pageName) {
    var box = document.createElement('div');
    box.className = 'chat-embed';
    var label = document.createElement('div');
    label.className = 'chat-embed-label';
    label.textContent = 'page: ' + pageName;
    box.appendChild(label);
    var host = document.createElement('div');
    box.appendChild(host);
    // Mount asynchronously; the host is in the DOM by the time render resolves.
    SurfaceUI.mount(pageName, host, { onUnauthorized: onUnauthorized });
    return box;
  }

  // --- Streaming ---

  var activeTextSpan = null;
  var activeTextRaw = '';

  function finalizeText() {
    if (activeTextSpan && activeTextRaw.trim()) {
      var frag = buildAssistantContent(activeTextRaw);
      activeTextSpan.parentNode.insertBefore(frag, activeTextSpan);
      activeTextSpan.remove();
    } else if (activeTextSpan) {
      activeTextSpan.remove();
    }
    activeTextSpan = null;
    activeTextRaw = '';
  }

  function parseSubagentMeta(input) {
    if (!input || typeof input !== 'object') return null;
    var task = typeof input.task === 'string' ? input.task.trim() : '';
    var model = typeof input.model === 'string' ? input.model.trim() : '';
    return {
      taskPreview: task ? (task.length > 90 ? task.slice(0, 90) + '…' : task) : '',
      model: model || '',
    };
  }

  function handleEvent(type, data, container, toolCalls) {
    var th = container.querySelector('.thinking');
    if (th) th.remove();

    switch (type) {
      case 'text': {
        activeTextRaw += data.text;
        if (!activeTextSpan || activeTextSpan.parentNode !== container ||
            activeTextSpan !== container.lastElementChild) {
          activeTextSpan = document.createElement('span');
          activeTextSpan.className = 'text-content';
          container.appendChild(activeTextSpan);
        }
        activeTextSpan.textContent = activeTextRaw;
        break;
      }

      case 'tool_start': {
        finalizeText();
        var toolDiv = document.createElement('div');
        var isSubagent = data.tool === 'delegate_subagent';
        toolDiv.className = 'tool-call pending' + (isSubagent ? ' subagent' : '');
        toolDiv.id = 'tool-' + data.id;
        var label = escapeHtml(data.tool);
        if (isSubagent) label = 'sub-agent';
        var metaHtml = '';
        if (isSubagent) {
          var meta = parseSubagentMeta(data.input);
          if (meta) {
            var bits = [];
            if (meta.model) bits.push('model=' + meta.model);
            if (meta.taskPreview) bits.push('task="' + meta.taskPreview + '"');
            if (bits.length) metaHtml = ' <span class="tool-meta">' + escapeHtml(bits.join(' · ')) + '</span>';
          }
        }
        toolDiv.innerHTML = '<span class="tool-name">' + label + '</span>' + metaHtml + ' <span class="tool-dots"></span>';
        container.appendChild(toolDiv);
        toolCalls[data.id] = isSubagent ? 'sub-agent' : data.tool;
        break;
      }

      case 'tool_result': {
        var elx = document.getElementById('tool-' + data.id);
        var incomingTool = typeof data.tool === 'string' ? data.tool : '';
        if (incomingTool && !toolCalls[data.id]) {
          toolCalls[data.id] = incomingTool === 'delegate_subagent' ? 'sub-agent' : incomingTool;
        }
        if (elx) {
          var name = toolCalls[data.id] || (incomingTool ? incomingTool : 'tool');
          var isSub = name === 'sub-agent' || incomingTool === 'delegate_subagent';
          elx.className = 'tool-call' + (isSub ? ' subagent' : '') + (data.error ? ' error' : '');
          elx.innerHTML = '<span class="tool-name">' + escapeHtml(name) + '</span> ' + (data.error ? 'error' : 'done');
        } else if (incomingTool) {
          var fallback = document.createElement('div');
          var isSub2 = incomingTool === 'delegate_subagent';
          fallback.className = 'tool-call' + (isSub2 ? ' subagent' : '') + (data.error ? ' error' : '');
          var lbl = isSub2 ? 'sub-agent' : incomingTool;
          fallback.innerHTML = '<span class="tool-name">' + escapeHtml(lbl) + '</span> ' + (data.error ? 'error' : 'done');
          container.appendChild(fallback);
        }
        break;
      }

      case 'error': {
        var errSpan = document.createElement('span');
        errSpan.style.color = '#f07070';
        errSpan.textContent = '\nerror: ' + data.error;
        container.appendChild(errSpan);
        break;
      }

      case 'done':
        finalizeText();
        break;
    }
    scrollToBottom();
  }

  function updateSendButtonState() {
    if (inflight) {
      sendBtn.textContent = queuedMessages.length > 0 ? ('queue (' + queuedMessages.length + ')') : 'queue';
    } else {
      sendBtn.textContent = 'send';
    }
  }

  function runTurn(entry) {
    inflight = true;
    if (entry.userDiv) entry.userDiv.classList.remove('queued');
    updateSendButtonState();

    var assistantDiv = document.createElement('div');
    assistantDiv.className = 'msg assistant';
    chat.appendChild(assistantDiv);

    activeTextRaw = '';
    activeTextSpan = null;
    var toolCalls = {};

    var thinkingEl = document.createElement('span');
    thinkingEl.className = 'thinking';
    thinkingEl.textContent = 'thinking';
    assistantDiv.appendChild(thinkingEl);
    scrollToBottom();

    var body = { message: entry.text };
    if (sessionId) body.session_id = sessionId;

    fetch('/v1/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).then(function (resp) {
      if (!resp.ok) {
        return resp.json().catch(function () { return {}; }).then(function (err) {
          if (resp.status === 401) {
            assistantDiv.textContent = 'session expired — sign in again';
            onUnauthorized();
          } else {
            assistantDiv.textContent = 'error: ' + (err.message || resp.statusText);
          }
          return null;
        });
      }
      return consumeStream(resp, assistantDiv, toolCalls);
    }).catch(function (err) {
      if (!assistantDiv.textContent) assistantDiv.textContent = 'connection error: ' + err.message;
    }).then(function () {
      inflight = false;
      updateSendButtonState();
      setTimeout(loadSessions, 500);
      if (queuedMessages.length > 0) {
        var next = queuedMessages.shift();
        updateSendButtonState();
        runTurn(next);
      } else {
        msgInput.focus();
      }
    });
  }

  function consumeStream(resp, assistantDiv, toolCalls) {
    var reader = resp.body.getReader();
    var decoder = new TextDecoder();
    var buffer = '';
    var eventType = '';
    function pump() {
      return reader.read().then(function (res) {
        if (res.done) return;
        buffer += decoder.decode(res.value, { stream: true });
        var lines = buffer.split('\n');
        buffer = lines.pop();
        for (var i = 0; i < lines.length; i++) {
          var line = lines[i];
          if (line.startsWith('event: ')) {
            eventType = line.slice(7);
          } else if (line.startsWith('data: ') && eventType) {
            var data = JSON.parse(line.slice(6));
            if (eventType === 'session' && data.session_id) {
              sessionId = data.session_id;
            } else {
              handleEvent(eventType, data, assistantDiv, toolCalls);
            }
            eventType = '';
          }
        }
        return pump();
      });
    }
    return pump();
  }

  function send() {
    var text = msgInput.value.trim();
    if (!text) return;
    msgInput.value = '';
    msgInput.style.height = 'auto';
    var userDiv = addMessage('user', text);
    if (inflight) {
      userDiv.classList.add('queued');
      queuedMessages.push({ text: text, userDiv: userDiv });
      updateSendButtonState();
      return;
    }
    runTurn({ text: text, userDiv: userDiv });
  }

  updateSendButtonState();
  WasmdbAuth.require(function () { showChat(); });
})();
