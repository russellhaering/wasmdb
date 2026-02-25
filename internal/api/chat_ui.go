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
<title>WasmDB Chat</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #0f1117;
    color: #e4e4e7;
    height: 100vh;
    display: flex;
    flex-direction: column;
  }
  header {
    padding: 12px 20px;
    border-bottom: 1px solid #27272a;
    display: flex;
    align-items: center;
    gap: 10px;
    background: #18181b;
  }
  header h1 {
    font-size: 16px;
    font-weight: 600;
    color: #fafafa;
  }
  header .badge {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 10px;
    background: #3b82f6;
    color: white;
    font-weight: 500;
  }
  #chat {
    flex: 1;
    overflow-y: auto;
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .msg {
    max-width: 85%;
    padding: 12px 16px;
    border-radius: 12px;
    line-height: 1.5;
    font-size: 14px;
    white-space: pre-wrap;
    word-wrap: break-word;
  }
  .msg.user {
    align-self: flex-end;
    background: #3b82f6;
    color: white;
    border-bottom-right-radius: 4px;
  }
  .msg.assistant {
    align-self: flex-start;
    background: #27272a;
    color: #e4e4e7;
    border-bottom-left-radius: 4px;
  }
  .msg.assistant .tool-call {
    display: inline-block;
    margin: 4px 0;
    padding: 4px 8px;
    background: #1e1e2e;
    border: 1px solid #3f3f46;
    border-radius: 6px;
    font-family: "SF Mono", "Fira Code", monospace;
    font-size: 12px;
    color: #a1a1aa;
  }
  .msg.assistant .tool-call .tool-name {
    color: #60a5fa;
    font-weight: 600;
  }
  .msg.assistant .tool-call.error {
    border-color: #7f1d1d;
    color: #fca5a5;
  }
  .msg.system {
    align-self: center;
    background: transparent;
    color: #71717a;
    font-size: 12px;
    padding: 4px;
  }
  #input-area {
    padding: 16px 20px;
    border-top: 1px solid #27272a;
    background: #18181b;
    display: flex;
    gap: 10px;
  }
  #input-area textarea {
    flex: 1;
    padding: 10px 14px;
    border-radius: 10px;
    border: 1px solid #3f3f46;
    background: #27272a;
    color: #fafafa;
    font-family: inherit;
    font-size: 14px;
    resize: none;
    outline: none;
    min-height: 42px;
    max-height: 120px;
  }
  #input-area textarea:focus {
    border-color: #3b82f6;
  }
  #input-area textarea::placeholder {
    color: #71717a;
  }
  #input-area button {
    padding: 10px 20px;
    border-radius: 10px;
    border: none;
    background: #3b82f6;
    color: white;
    font-weight: 600;
    font-size: 14px;
    cursor: pointer;
    white-space: nowrap;
  }
  #input-area button:hover { background: #2563eb; }
  #input-area button:disabled {
    background: #3f3f46;
    color: #71717a;
    cursor: not-allowed;
  }
  .typing-indicator {
    display: inline-block;
    color: #71717a;
    font-size: 13px;
    padding: 8px 16px;
  }
  .typing-indicator::after {
    content: '';
    animation: dots 1.5s steps(4, end) infinite;
  }
  @keyframes dots {
    0%, 20% { content: ''; }
    40% { content: '.'; }
    60% { content: '..'; }
    80%, 100% { content: '...'; }
  }
</style>
</head>
<body>
<header>
  <h1>WasmDB</h1>
  <span class="badge">Chat</span>
</header>

<div id="auth-screen" style="flex:1;display:flex;align-items:center;justify-content:center;">
  <div style="text-align:center;">
    <p style="margin-bottom:12px;color:#a1a1aa;font-size:14px;">Enter your API token to start chatting.</p>
    <div style="display:flex;gap:10px;">
      <input id="token-input" type="password" placeholder="Bearer token"
        style="padding:10px 14px;border-radius:10px;border:1px solid #3f3f46;background:#27272a;color:#fafafa;font-size:14px;outline:none;width:280px;">
      <button onclick="authenticate()"
        style="padding:10px 20px;border-radius:10px;border:none;background:#3b82f6;color:white;font-weight:600;font-size:14px;cursor:pointer;">Connect</button>
    </div>
    <p id="auth-error" style="margin-top:10px;color:#fca5a5;font-size:13px;display:none;"></p>
  </div>
</div>

<div id="chat" style="display:none;"></div>
<div id="input-area" style="display:none;">
  <textarea id="msg" placeholder="Ask about your databases..." rows="1"></textarea>
  <button id="send" onclick="send()">Send</button>
</div>
<script>
const chat = document.getElementById('chat');
const msgInput = document.getElementById('msg');
const sendBtn = document.getElementById('send');
const sessionId = crypto.randomUUID();
let apiToken = '';

function authenticate() {
  const token = document.getElementById('token-input').value.trim();
  if (!token) return;
  apiToken = token;
  document.getElementById('auth-screen').style.display = 'none';
  chat.style.display = 'flex';
  document.getElementById('input-area').style.display = 'flex';
  msgInput.focus();
}

document.getElementById('token-input').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') authenticate();
});

// Auto-resize textarea
msgInput.addEventListener('input', () => {
  msgInput.style.height = 'auto';
  msgInput.style.height = Math.min(msgInput.scrollHeight, 120) + 'px';
});

msgInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    send();
  }
});

function scrollToBottom() {
  chat.scrollTop = chat.scrollHeight;
}

function addMessage(role, content) {
  const div = document.createElement('div');
  div.className = 'msg ' + role;
  div.textContent = content;
  chat.appendChild(div);
  scrollToBottom();
  return div;
}

function escapeHtml(s) {
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

async function send() {
  const text = msgInput.value.trim();
  if (!text) return;

  msgInput.value = '';
  msgInput.style.height = 'auto';
  sendBtn.disabled = true;

  addMessage('user', text);

  // Create assistant message container
  const assistantDiv = document.createElement('div');
  assistantDiv.className = 'msg assistant';
  chat.appendChild(assistantDiv);

  let currentText = '';
  let toolCalls = {};

  try {
    const resp = await fetch('/v1/chat', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer ' + apiToken
      },
      body: JSON.stringify({session_id: sessionId, message: text})
    });

    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}));
      if (resp.status === 401) {
        assistantDiv.textContent = 'Authentication failed. Please reload and try a different token.';
      } else {
        assistantDiv.textContent = 'Error: ' + (err.message || resp.statusText);
      }
      sendBtn.disabled = false;
      return;
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const {done, value} = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, {stream: true});
      const lines = buffer.split('\n');
      buffer = lines.pop(); // keep incomplete line

      let eventType = '';
      for (const line of lines) {
        if (line.startsWith('event: ')) {
          eventType = line.slice(7);
        } else if (line.startsWith('data: ') && eventType) {
          const data = JSON.parse(line.slice(6));
          handleEvent(eventType, data, assistantDiv, toolCalls);
          eventType = '';
        }
      }
    }
  } catch (err) {
    if (!assistantDiv.textContent) {
      assistantDiv.textContent = 'Connection error: ' + err.message;
    }
  }

  sendBtn.disabled = false;
  msgInput.focus();
}

// Track the current text span so new text after tool calls creates a new span
let activeTextSpan = null;

function handleEvent(type, data, container, toolCalls) {
  switch (type) {
    case 'text':
      // Append to current text span, or create a new one after tool calls
      if (!activeTextSpan || activeTextSpan.parentNode !== container ||
          activeTextSpan !== container.lastElementChild) {
        activeTextSpan = document.createElement('span');
        activeTextSpan.className = 'text-content';
        container.appendChild(activeTextSpan);
      }
      activeTextSpan.textContent += data.text;
      break;

    case 'tool_start':
      activeTextSpan = null; // next text gets a new span
      const toolDiv = document.createElement('div');
      toolDiv.className = 'tool-call';
      toolDiv.id = 'tool-' + data.id;
      toolDiv.innerHTML = '<span class="tool-name">' + escapeHtml(data.tool) + '</span> ...';
      container.appendChild(toolDiv);
      toolCalls[data.id] = data.tool;
      break;

    case 'tool_result':
      const el = document.getElementById('tool-' + data.id);
      if (el) {
        const name = toolCalls[data.id] || 'tool';
        if (data.error) {
          el.className = 'tool-call error';
          el.innerHTML = '<span class="tool-name">' + escapeHtml(name) + '</span> error';
        } else {
          el.innerHTML = '<span class="tool-name">' + escapeHtml(name) + '</span> done';
        }
      }
      break;

    case 'error':
      const errSpan = document.createElement('span');
      errSpan.style.color = '#fca5a5';
      errSpan.textContent = '\nError: ' + data.error;
      container.appendChild(errSpan);
      break;

    case 'done':
      break;
  }
  scrollToBottom();
}

// Focus input on load
msgInput.focus();
</script>
</body>
</html>
`
