// dashboard.js — the dashboard shell: a tab per enabled UI page, each mounted
// through the shared SurfaceUI renderer. Cookie-session auth via auth.js; no
// localStorage token.
(function () {
  'use strict';

  var pages = [];
  var activePage = null;
  var mounted = null; // current SurfaceUI controller

  function esc(s) { var d = document.createElement('div'); d.textContent = s; return d.innerHTML; }

  function onUnauthorized() {
    if (mounted) { mounted.destroy(); mounted = null; }
    WasmdbAuth.showLogin(function () { loadDashboard(); });
  }

  function loadDashboard() {
    var main = document.getElementById('main-content');
    main.innerHTML = '<div class="loading">loading</div>';
    fetch('/v1/ui/pages').then(function (resp) {
      if (resp.status === 401) { onUnauthorized(); throw new Error('unauthorized'); }
      if (!resp.ok) throw new Error('failed to load ui pages');
      return resp.json();
    }).then(function (all) {
      pages = (all || []).filter(function (p) { return p.enabled; }).sort(function (a, b) {
        if (a.sort_order !== b.sort_order) return a.sort_order - b.sort_order;
        return (a.name || '').localeCompare(b.name || '');
      });
      if (pages.length === 0) {
        main.innerHTML = '<div class="empty-state">' +
          '<h2>No dashboard pages configured</h2>' +
          '<p>Use the chat agent to create UI pages, or create them via the API. Try asking:<br><br>' +
          '<code>Create a dashboard page that shows a summary of my data</code></p>' +
          '</div>';
        return;
      }
      renderShell();
      selectPage(pages[0].name);
    }).catch(function (e) {
      if (e.message !== 'unauthorized') {
        main.innerHTML = '<div class="error-banner">Error: ' + esc(e.message) + '</div>';
      }
    });
  }

  function renderShell() {
    var main = document.getElementById('main-content');
    main.innerHTML = '';
    var tabs = document.createElement('div');
    tabs.className = 'page-tabs';
    tabs.id = 'page-tabs';
    for (var i = 0; i < pages.length; i++) {
      (function (p) {
        var btn = document.createElement('button');
        btn.className = 'page-tab';
        btn.dataset.name = p.name;
        btn.textContent = p.title || p.name;
        btn.onclick = function () { selectPage(p.name); };
        tabs.appendChild(btn);
      })(pages[i]);
    }
    main.appendChild(tabs);

    var content = document.createElement('div');
    content.className = 'page-content';
    content.id = 'page-content';
    main.appendChild(content);
  }

  function selectPage(name) {
    activePage = name;
    if (mounted) { mounted.destroy(); mounted = null; }

    var tabsEl = document.querySelectorAll('.page-tab');
    for (var i = 0; i < tabsEl.length; i++) {
      tabsEl[i].classList.toggle('active', tabsEl[i].dataset.name === name);
    }

    var content = document.getElementById('page-content');
    content.innerHTML = '';

    var page = null;
    for (var j = 0; j < pages.length; j++) { if (pages[j].name === name) { page = pages[j]; break; } }

    if (page) {
      var header = document.createElement('div');
      header.className = 'page-header';
      var h2 = document.createElement('h2');
      h2.textContent = page.title || page.name;
      header.appendChild(h2);
      if (page.description) {
        var pdesc = document.createElement('p');
        pdesc.textContent = page.description;
        header.appendChild(pdesc);
      }
      content.appendChild(header);
    }

    var surfaceHost = document.createElement('div');
    content.appendChild(surfaceHost);

    mounted = SurfaceUI.mount(name, surfaceHost, { onUnauthorized: onUnauthorized });
  }

  WasmdbAuth.require(function () {
    loadDashboard();
  });
})();
