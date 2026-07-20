// auth.js — the shared, cookie-session login flow used by both the dashboard
// and chat pages. Authentication is entirely cookie-based (wasmdb_session set by
// POST /v1/auth/login); same-origin fetches send it automatically. There is no
// localStorage bearer token.
(function () {
  'use strict';

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  var WasmdbAuth = {
    // isAuthed resolves true when the current session cookie is valid.
    isAuthed: function () {
      return fetch('/v1/auth/me').then(function (r) { return r.ok; }).catch(function () { return false; });
    },

    // showLogin renders a full-screen login overlay. onSuccess runs once the
    // user authenticates. If an overlay is already present it is reused.
    showLogin: function (onSuccess) {
      var existing = document.getElementById('auth-overlay');
      if (existing) { existing.classList.remove('hidden'); return; }

      var overlay = el('div');
      overlay.id = 'auth-overlay';
      var box = el('div', 'login-box');
      box.appendChild(el('h2', null, '─── WasmDB'));
      var err = el('div', 'error');
      err.style.display = 'none';
      var email = el('input');
      email.type = 'email'; email.placeholder = 'email'; email.autocomplete = 'email';
      var pass = el('input');
      pass.type = 'password'; pass.placeholder = 'password'; pass.autocomplete = 'current-password';
      var btn = el('button', null, 'login');

      function submit() {
        err.style.display = 'none';
        fetch('/v1/auth/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: email.value.trim(), password: pass.value }),
        }).then(function (resp) {
          return resp.json().catch(function () { return {}; }).then(function (data) {
            if (!resp.ok) throw new Error(data.error || data.message || 'login failed');
            overlay.remove();
            if (onSuccess) onSuccess();
          });
        }).catch(function (e) {
          err.textContent = e.message;
          err.style.display = 'block';
        });
      }

      btn.onclick = submit;
      pass.addEventListener('keydown', function (e) { if (e.key === 'Enter') submit(); });

      box.appendChild(err);
      box.appendChild(email);
      box.appendChild(pass);
      box.appendChild(btn);
      overlay.appendChild(box);
      document.body.appendChild(overlay);
      email.focus();
    },

    // require checks the session and either runs onReady() or shows the login
    // overlay, running onReady() after a successful login.
    require: function (onReady) {
      WasmdbAuth.isAuthed().then(function (ok) {
        if (ok) onReady();
        else WasmdbAuth.showLogin(onReady);
      });
    },
  };

  if (typeof window !== 'undefined') window.WasmdbAuth = WasmdbAuth;
})();
