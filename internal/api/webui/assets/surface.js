// surface.js — the single component renderer shared by the dashboard and chat.
//
// Public contract:
//   SurfaceUI.mount(pageName, containerEl, opts?) -> { destroy(), refresh() }
//     opts.onUnauthorized  callback invoked on any HTTP 401 (login redirect).
//     opts.params          initial render params (default {}).
//
// It POSTs /v1/ui/pages/{name}/render {params}, renders the returned surface v2
// component tree (resolving {"$data": "path"} refs against the data object),
// and wires Form/Button/Input/DataTable interactions to
// /v1/ui/pages/{name}/actions/{action}. All data text goes through textContent
// / createElement — never innerHTML.
(function () {
  'use strict';

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  // isDataRef reports whether v is {"$data": "path"} and returns the path.
  function dataRefPath(v) {
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      var keys = Object.keys(v);
      if (keys.length === 1 && keys[0] === '$data' && typeof v['$data'] === 'string') {
        return v['$data'];
      }
    }
    return null;
  }

  // resolvePath walks a dot-separated path against a nested object.
  function resolvePath(data, path) {
    if (!path) return undefined;
    var segs = path.split('.');
    var cur = data;
    for (var i = 0; i < segs.length; i++) {
      if (cur == null || typeof cur !== 'object') return undefined;
      cur = cur[segs[i]];
    }
    return cur;
  }

  // resolve turns a property value into a concrete value, following a $data ref
  // when present.
  function resolve(v, data) {
    var path = dataRefPath(v);
    if (path !== null) return resolvePath(data, path);
    return v;
  }

  // formatCell renders a value for display in a DataTable cell / metric.
  function formatCell(val, type) {
    if (val == null) return '';
    if (type === 'bool' || typeof val === 'boolean') return val ? '✓' : '—';
    if (type === 'datetime' && val) {
      var d = new Date(val);
      if (!isNaN(d.getTime())) return d.toLocaleString();
      return String(val);
    }
    if (typeof val === 'object') return JSON.stringify(val);
    return String(val);
  }

  // coerce turns a raw string input value into the typed value for a field.
  function coerce(raw, type) {
    switch (type) {
      case 'int':
      case 'float':
        if (raw === '' || raw == null) return null;
        var n = Number(raw);
        return isNaN(n) ? raw : n;
      case 'bool':
        return !!raw;
      case 'datetime':
        // datetime-local -> RFC3339. Empty stays empty.
        if (!raw) return null;
        var d = new Date(raw);
        return isNaN(d.getTime()) ? raw : d.toISOString();
      default:
        return raw;
    }
  }

  function Session(pageName, container, opts) {
    this.pageName = pageName;
    this.container = container;
    this.opts = opts || {};
    this.params = Object.assign({}, this.opts.params || {});
    this.refreshTimer = null;
    this.debounceTimer = null;
    this.last = null; // { surface, data, actions }
    this.modalOpen = false;
  }

  Session.prototype.api = function (path, body) {
    var self = this;
    return fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {}),
    }).then(function (resp) {
      if (resp.status === 401) {
        if (self.opts.onUnauthorized) self.opts.onUnauthorized();
        var e = new Error('unauthorized');
        e.unauthorized = true;
        throw e;
      }
      return resp;
    });
  };

  Session.prototype.destroy = function () {
    if (this.refreshTimer) { clearInterval(this.refreshTimer); this.refreshTimer = null; }
    if (this.debounceTimer) { clearTimeout(this.debounceTimer); this.debounceTimer = null; }
    this.container.innerHTML = '';
  };

  Session.prototype.showError = function (msg, phase, logs) {
    this.container.innerHTML = '';
    var banner = el('div', 'error-banner');
    if (phase) {
      var ph = el('span', 'err-phase', '[' + phase + ']');
      banner.appendChild(ph);
    }
    banner.appendChild(document.createTextNode(msg || 'unknown error'));
    if (logs && logs.length) {
      banner.appendChild(el('div', 'err-logs', logs.join('\n')));
    }
    this.container.appendChild(banner);
  };

  // refresh fetches a fresh render with the current params and rebuilds the tree.
  Session.prototype.refresh = function () {
    var self = this;
    return this.api('/v1/ui/pages/' + encodeURIComponent(this.pageName) + '/render', { params: this.params })
      .then(function (resp) {
        if (resp.status === 404) { self.showError('page not found'); return; }
        return resp.json().then(function (result) {
          if (result && result.error) {
            self.showError(result.error, result.error_phase, result.logs);
            return;
          }
          self.last = { surface: result.surface, data: result.data || {}, actions: result.actions || {} };
          self.build();
          self.setupRefresh(result.auto_refresh_seconds || 0);
        });
      })
      .catch(function (e) {
        if (e && e.unauthorized) return;
        self.showError(e && e.message ? e.message : String(e));
      });
  };

  Session.prototype.setupRefresh = function (seconds) {
    var self = this;
    if (this.refreshTimer) { clearInterval(this.refreshTimer); this.refreshTimer = null; }
    if (seconds && seconds > 0) {
      this.refreshTimer = setInterval(function () {
        // Pause while a modal or a focused form/input inside the container is active.
        if (self.modalOpen) return;
        var ae = document.activeElement;
        if (ae && self.container.contains(ae) &&
            (ae.tagName === 'INPUT' || ae.tagName === 'SELECT' || ae.tagName === 'TEXTAREA')) {
          return;
        }
        self.refresh();
      }, seconds * 1000);
    }
  };

  // build renders the current surface tree into the container.
  Session.prototype.build = function () {
    this.container.innerHTML = '';
    if (!this.last || !this.last.surface) { this.showError('empty surface'); return; }
    var comps = this.last.surface.components || [];
    var idx = {};
    for (var i = 0; i < comps.length; i++) idx[comps[i].id] = comps[i];
    var root = idx['root'];
    if (!root) { this.showError('no root component'); return; }
    var node = this.renderComponent(root, idx);
    if (node) this.container.appendChild(node);
  };

  Session.prototype.renderChildren = function (comp, idx, parent) {
    var kids = comp.children || [];
    for (var i = 0; i < kids.length; i++) {
      var child = idx[kids[i]];
      if (!child) continue;
      var node = this.renderComponent(child, idx);
      if (node) parent.appendChild(node);
    }
  };

  Session.prototype.renderComponent = function (comp, idx) {
    if (!comp) return null;
    var p = comp.properties || {};
    var data = this.last.data;
    switch (comp.type) {
      case 'Column':
      case 'Row': {
        var div = el('div', comp.type === 'Column' ? 'sf-column' : 'sf-row');
        if (typeof p.gap === 'number') div.style.gap = p.gap + 'px';
        if (p.align) div.classList.add('sf-align-' + p.align);
        this.renderChildren(comp, idx, div);
        return div;
      }
      case 'Card': {
        var card = el('div', 'sf-card');
        var title = resolve(p.title, data);
        if (title != null && title !== '') card.appendChild(el('div', 'sf-card-title', String(title)));
        this.renderChildren(comp, idx, card);
        return card;
      }
      case 'Divider':
        return el('hr', 'sf-divider');
      case 'Text': {
        var variant = p.variant || 'body';
        return el('div', 'sf-text sf-text-' + variant, String(resolve(p.value, data) != null ? resolve(p.value, data) : ''));
      }
      case 'Metric': {
        var m = el('div', 'sf-metric');
        m.appendChild(el('div', 'sf-metric-label', String(p.label != null ? p.label : '')));
        var valLine = el('div', 'sf-metric-value');
        valLine.appendChild(document.createTextNode(formatCell(resolve(p.value, data))));
        if (p.unit) valLine.appendChild(el('span', 'sf-metric-unit', String(p.unit)));
        m.appendChild(valLine);
        return m;
      }
      case 'DataTable':
        return this.renderDataTable(comp);
      case 'Form':
        return this.renderForm(comp);
      case 'Input':
        return this.renderInput(comp);
      case 'Button':
        return this.renderButton(comp);
      default:
        return el('div', 'error-banner', 'unknown component type: ' + comp.type);
    }
  };

  Session.prototype.renderDataTable = function (comp) {
    var self = this;
    var p = comp.properties || {};
    var data = this.last.data;
    var cols = p.columns || [];
    var rows = resolve(p.rows, data);
    if (!Array.isArray(rows)) rows = [];
    var rowActions = p.row_actions || [];

    var wrap = el('div', 'sf-table-wrap');
    var table = el('table', 'sf-datatable');

    var thead = el('thead');
    var htr = el('tr');
    for (var c = 0; c < cols.length; c++) {
      htr.appendChild(el('th', null, cols[c].label != null ? cols[c].label : cols[c].key));
    }
    if (rowActions.length) htr.appendChild(el('th', null, ''));
    thead.appendChild(htr);
    table.appendChild(thead);

    var tbody = el('tbody');
    if (rows.length === 0) {
      var etr = el('tr');
      var etd = el('td');
      etd.colSpan = cols.length + (rowActions.length ? 1 : 0);
      etd.appendChild(el('div', 'sf-empty', p.empty_text || 'No data.'));
      etr.appendChild(etd);
      tbody.appendChild(etr);
    } else {
      for (var r = 0; r < rows.length; r++) {
        var row = rows[r];
        var tr = el('tr');
        for (var ci = 0; ci < cols.length; ci++) {
          var col = cols[ci];
          tr.appendChild(el('td', null, formatCell(row ? row[col.key] : null, col.type)));
        }
        if (rowActions.length) {
          var atd = el('td');
          var actWrap = el('div', 'sf-row-actions');
          this.buildRowActions(actWrap, rowActions, row, cols);
          atd.appendChild(actWrap);
          tr.appendChild(atd);
        }
        tbody.appendChild(tr);
      }
    }
    table.appendChild(tbody);
    wrap.appendChild(table);
    return wrap;
  };

  Session.prototype.buildRowActions = function (wrap, rowActions, row, cols) {
    var self = this;
    var hasID = row && typeof row.id === 'string' && row.id !== '';
    for (var i = 0; i < rowActions.length; i++) {
      (function (ra) {
        var actionDecl = (self.last.actions && self.last.actions[ra.action]) || {};
        var isDanger = actionDecl.type === 'delete';
        var btn = el('button', 'sf-button sf-button-sm' + (isDanger ? ' sf-button-danger' : ''), ra.label || ra.action);
        if (!hasID) {
          btn.disabled = true;
          btn.title = 'row has no id';
          console.warn('surface: row action "' + ra.action + '" disabled — row has no id key', row);
        } else {
          btn.onclick = function () {
            var confirmNeeded = ra.confirm || actionDecl.confirm;
            if (actionDecl.type === 'update') {
              self.openEditModal(ra, row, cols);
              return;
            }
            if (confirmNeeded && !window.confirm('Confirm: ' + (ra.label || ra.action) + '?')) return;
            self.runAction(ra.action, { id: row.id }, btn);
          };
        }
        wrap.appendChild(btn);
      })(rowActions[i]);
    }
  };

  Session.prototype.openEditModal = function (ra, row, cols) {
    var self = this;
    this.modalOpen = true;
    var overlay = el('div', 'sf-modal-overlay');
    var modal = el('div', 'sf-modal');
    modal.appendChild(el('h3', null, ra.label || ('Edit ' + ra.action)));

    var inputs = {}; // key -> {input, type}
    for (var i = 0; i < cols.length; i++) {
      var col = cols[i];
      if (col.key === 'id') continue;
      var field = el('div', 'sf-field');
      field.appendChild(el('label', null, col.label || col.key));
      var ctrl = self.buildTypedControl(col.type, row ? row[col.key] : null);
      field.appendChild(ctrl.node);
      inputs[col.key] = ctrl;
      modal.appendChild(field);
    }

    var errBox = el('div', 'sf-inline-error');
    errBox.style.display = 'none';
    modal.appendChild(errBox);

    var actions = el('div', 'sf-modal-actions');
    var save = el('button', 'sf-button', 'Save');
    var cancel = el('button', 'sf-button sf-button-danger', 'Cancel');
    function close() { self.modalOpen = false; overlay.remove(); }
    cancel.onclick = close;
    save.onclick = function () {
      var params = { id: row.id };
      for (var k in inputs) { if (inputs.hasOwnProperty(k)) params[k] = inputs[k].value(); }
      save.disabled = true;
      self.runAction(ra.action, params, null, function (res) {
        if (res && res.ok === false) {
          save.disabled = false;
          errBox.textContent = res.error || 'action failed';
          errBox.style.display = 'block';
        } else {
          close();
        }
      });
    };
    actions.appendChild(save);
    actions.appendChild(cancel);
    modal.appendChild(actions);
    overlay.appendChild(modal);
    overlay.onclick = function (e) { if (e.target === overlay) close(); };
    document.body.appendChild(overlay);
  };

  // buildTypedControl returns { node, value() } for a typed value.
  Session.prototype.buildTypedControl = function (type, current) {
    if (type === 'bool') {
      var cb = el('input', 'sf-checkbox');
      cb.type = 'checkbox';
      cb.checked = !!current;
      return { node: cb, value: function () { return cb.checked; } };
    }
    if (type === 'datetime') {
      var dt = el('input', 'sf-input');
      dt.type = 'datetime-local';
      if (current) {
        var d = new Date(current);
        if (!isNaN(d.getTime())) {
          // to local datetime-local value
          var pad = function (n) { return (n < 10 ? '0' : '') + n; };
          dt.value = d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) +
            'T' + pad(d.getHours()) + ':' + pad(d.getMinutes());
        }
      }
      return { node: dt, value: function () { return coerce(dt.value, 'datetime'); } };
    }
    var inp = el('input', 'sf-input');
    inp.type = (type === 'int' || type === 'float') ? 'number' : 'text';
    if (type === 'float') inp.step = 'any';
    if (current != null) inp.value = String(current);
    return { node: inp, value: function () { return coerce(inp.value, type); } };
  };

  Session.prototype.renderForm = function (comp) {
    var self = this;
    var p = comp.properties || {};
    var fields = p.fields || [];
    var submit = p.submit || {};
    var form = el('form', 'sf-form');
    var controls = []; // {name, type, required, value()}

    for (var i = 0; i < fields.length; i++) {
      var f = fields[i];
      var wrap = el('div', 'sf-field');
      var lbl = el('label', null, f.label || f.name);
      if (f.required) { var star = el('span', 'req', '*'); lbl.appendChild(star); }
      wrap.appendChild(lbl);

      var ctrl;
      if (f.type === 'select') {
        var sel = el('select', 'sf-select');
        var opts = f.options || [];
        for (var o = 0; o < opts.length; o++) {
          var opt = el('option', null, String(opts[o]));
          opt.value = String(opts[o]);
          sel.appendChild(opt);
        }
        if (f['default'] != null) sel.value = String(f['default']);
        ctrl = { node: sel, value: function (s) { return function () { return s.value; }; }(sel) };
      } else {
        ctrl = this.buildTypedControl(f.type, f['default']);
      }
      wrap.appendChild(ctrl.node);
      controls.push({ name: f.name, type: f.type, required: !!f.required, value: ctrl.value, node: ctrl.node });
      form.appendChild(wrap);
    }

    var errBox = el('div', 'sf-inline-error');
    errBox.style.display = 'none';

    var btn = el('button', 'sf-button', submit.label || 'Submit');
    btn.type = 'submit';
    form.appendChild(btn);
    form.appendChild(errBox);

    form.onsubmit = function (ev) {
      ev.preventDefault();
      errBox.style.display = 'none';
      var params = {};
      for (var j = 0; j < controls.length; j++) {
        var c = controls[j];
        var v = c.value();
        if (c.required && (v == null || v === '')) {
          errBox.textContent = (c.name) + ' is required';
          errBox.style.display = 'block';
          return;
        }
        params[c.name] = v;
      }
      btn.disabled = true;
      self.runAction(submit.action, params, null, function (res) {
        btn.disabled = false;
        if (res && res.ok === false) {
          errBox.textContent = res.error || 'action failed';
          errBox.style.display = 'block';
        } else {
          form.reset();
        }
      });
    };
    return form;
  };

  Session.prototype.renderInput = function (comp) {
    var self = this;
    var p = comp.properties || {};
    var bound = p.bind !== false; // defaults to true
    var wrap = el('div', 'sf-field sf-standalone-input');
    if (p.label) wrap.appendChild(el('label', null, p.label));

    var ctrl;
    if (p.type === 'select') {
      var sel = el('select', 'sf-select');
      var opts = p.options || [];
      for (var o = 0; o < opts.length; o++) {
        var opt = el('option', null, String(opts[o]));
        opt.value = String(opts[o]);
        sel.appendChild(opt);
      }
      if (this.params[p.name] != null) sel.value = String(this.params[p.name]);
      ctrl = sel;
    } else {
      ctrl = el('input', 'sf-input');
      ctrl.type = (p.type === 'int' || p.type === 'float') ? 'number' : (p.type === 'datetime' ? 'datetime-local' : 'text');
      if (p.placeholder) ctrl.placeholder = p.placeholder;
      if (this.params[p.name] != null) ctrl.value = String(this.params[p.name]);
    }

    if (bound) {
      var handler = function () {
        var raw = (ctrl.type === 'checkbox') ? ctrl.checked : ctrl.value;
        self.params[p.name] = coerce(raw, p.type);
        if (self.debounceTimer) clearTimeout(self.debounceTimer);
        self.debounceTimer = setTimeout(function () { self.refresh(); }, 300);
      };
      ctrl.addEventListener('input', handler);
      ctrl.addEventListener('change', handler);
    }
    wrap.appendChild(ctrl);
    return wrap;
  };

  Session.prototype.renderButton = function (comp) {
    var self = this;
    var p = comp.properties || {};
    var data = this.last.data;
    var btn = el('button', 'sf-button', p.label || 'Button');
    var actionDecl = (this.last.actions && this.last.actions[p.action]) || {};
    var errBox = el('div', 'sf-inline-error');
    errBox.style.display = 'none';
    btn.onclick = function () {
      var params = {};
      if (p.params) {
        for (var k in p.params) {
          if (p.params.hasOwnProperty(k)) params[k] = resolve(p.params[k], data);
        }
      }
      if ((p.confirm || actionDecl.confirm) && !window.confirm('Confirm: ' + (p.label || p.action) + '?')) return;
      errBox.style.display = 'none';
      btn.disabled = true;
      self.runAction(p.action, params, null, function (res) {
        btn.disabled = false;
        if (res && res.ok === false) {
          errBox.textContent = res.error || 'action failed';
          errBox.style.display = 'block';
        }
      });
    };
    var wrap = el('div', 'sf-column');
    wrap.appendChild(btn);
    wrap.appendChild(errBox);
    return wrap;
  };

  // runAction POSTs an action. onDone(result) is called with the parsed body
  // (or undefined on transport error). Default success behavior: query actions
  // returning data update in place; everything else triggers a full re-render.
  Session.prototype.runAction = function (actionName, params, srcEl, onDone) {
    var self = this;
    var actionDecl = (this.last.actions && this.last.actions[actionName]) || {};
    this.api('/v1/ui/pages/' + encodeURIComponent(this.pageName) + '/actions/' + encodeURIComponent(actionName), { params: params })
      .then(function (resp) { return resp.json(); })
      .then(function (res) {
        if (res && res.ok === false) {
          if (onDone) onDone(res);
          else if (srcEl) self.showInlineNear(srcEl, res.error || 'action failed');
          return;
        }
        // Success.
        if (actionDecl.type === 'query' && res && res.data) {
          // Update data in place; keep the same surface + actions.
          self.last.data = res.data;
          self.build();
          if (onDone) onDone(res);
        } else {
          if (onDone) onDone(res);
          self.refresh();
        }
      })
      .catch(function (e) {
        if (e && e.unauthorized) return;
        if (onDone) onDone({ ok: false, error: e.message || String(e) });
        else if (srcEl) self.showInlineNear(srcEl, e.message || String(e));
      });
  };

  Session.prototype.showInlineNear = function (srcEl, msg) {
    var errBox = el('div', 'sf-inline-error', msg);
    if (srcEl && srcEl.parentNode) srcEl.parentNode.appendChild(errBox);
    else this.showError(msg);
  };

  var SurfaceUI = {
    mount: function (pageName, container, opts) {
      var session = new Session(pageName, container, opts);
      session.refresh();
      return {
        destroy: function () { session.destroy(); },
        refresh: function () { return session.refresh(); },
      };
    },
  };

  if (typeof window !== 'undefined') window.SurfaceUI = SurfaceUI;
})();
