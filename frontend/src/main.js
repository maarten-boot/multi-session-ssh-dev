import './style.css';

import {isMac, TermView} from './terminal';
import {
    BrowseDirectory, CreateSession, DeleteHost, DeleteSession, KillRemote, MoveSession,
    PreviewSession, SaveHost, StartLocal, StartRemote, State, UpdateSession,
} from '../wailsjs/go/main/App';

const $ = sel => document.querySelector(sel);
const el = (tag, props = {}, ...children) => {
    const e = Object.assign(document.createElement(tag), props);
    e.append(...children);
    return e;
};

// Small per-machine UI memory (last session, collapsed groups). Optional.
const store = {
    get(k, d) {
        try {
            return JSON.parse(localStorage.getItem('sw:' + k)) ?? d;
        } catch {
            return d;
        }
    },
    set(k, v) {
        try {
            localStorage.setItem('sw:' + k, JSON.stringify(v));
        } catch {
        }
    },
};

// ---------------------------------------------------------------- keyboard

// appKey maps a keydown to an app action, or null if it belongs to the
// terminal. Defaults per SPEC 4.6 (configurable in a later milestone).
function appKey(e) {
    if (e.type !== 'keydown') return null;
    const digit = /^Digit([1-9])$/.exec(e.code)?.[1];
    if (isMac) {
        if (!e.metaKey || e.ctrlKey || e.altKey) return null;
        if (e.shiftKey && digit) return {tab: +digit - 1};
        if (e.shiftKey && e.code === 'BracketRight') return {step: 1};
        if (e.shiftKey && e.code === 'BracketLeft') return {step: -1};
        if (e.shiftKey && e.code === 'KeyN') return {newSession: true};
        if (!e.shiftKey && digit) return {session: +digit - 1};
        return null;
    }
    if (!e.ctrlKey || e.metaKey) return null;
    if (e.altKey && !e.shiftKey && digit) return {session: +digit - 1};
    if (e.altKey || !e.shiftKey) return null;
    if (digit) return {tab: +digit - 1};
    if (e.code === 'PageDown') return {step: 1};
    if (e.code === 'PageUp') return {step: -1};
    if (e.code === 'KeyN') return {newSession: true};
    return null;
}

const keyHint = isMac ? '⇧⌘' : 'Ctrl+Shift+';

// ---------------------------------------------------------------- sessions

class Tab {
    constructor(session, label, start, exitHint, onState) {
        this.session = session;
        this.label = label;
        this.start = start;
        this.exitHint = exitHint;
        this.onState = onState;
        this.pane = el('div', {className: 'pane', hidden: true});
        session.container.append(this.pane);
        this.view = null;
    }

    ensureView() {
        if (!this.view) {
            this.view = new TermView(this.pane, {
                start: this.start, exitHint: this.exitHint, ui: app.state.ui,
                isAppKey: e => appKey(e) !== null, onState: this.onState,
            });
            this.view.spawn();
        }
        return this.view;
    }

    show() {
        this.pane.hidden = false;
        this.ensureView().focus();
    }

    hide() {
        this.pane.hidden = true;
    }

    dispose() {
        this.view?.dispose();
        this.pane.remove();
    }
}

class SessionUI {
    constructor(data) {
        this.data = data;
        this.remoteState = 'idle';
        this.container = el('div', {className: 'session-panes', hidden: true});
        $('#panes').append(this.container);
        this.tabs = [
            new Tab(this, 'Remote', (c, r) => StartRemote(this.data.id, c, r),
                {what: 'ssh', again: 'reconnect'}, s => this.setRemoteState(s)),
            new Tab(this, 'Local', (c, r) => StartLocal(this.data.id, c, r),
                {what: 'shell', again: 'restart'}),
        ];
        this.activeTab = 0;
    }

    get id() {
        return this.data.id;
    }

    setRemoteState(s) {
        this.remoteState = s;
        app.renderDot(this);
    }

    dotClass() {
        return this.data.error ? 'error' : this.remoteState;
    }

    show() {
        this.container.hidden = false;
        this.tabs.forEach((t, i) => i === this.activeTab ? t.show() : t.hide());
    }

    hide() {
        this.container.hidden = true;
    }

    selectTab(i) {
        if (!this.tabs[i]) return;
        this.activeTab = i;
        this.show();
    }

    connectRemote() {
        this.tabs[0].ensureView();
    }

    disconnect() {
        this.tabs.forEach(t => t.view?.disconnect());
    }

    dispose() {
        this.tabs.forEach(t => t.dispose());
        this.container.remove();
    }
}

// ---------------------------------------------------------------- app

const app = {
    state: null,
    sessions: new Map(), // id -> SessionUI
    order: [],           // SessionUI in sidebar order
    active: null,
    collapsed: new Set(store.get('collapsed', [])),

    async init() {
        await this.reload();
        const last = this.sessions.get(store.get('active', ''));
        if (this.state.ui.connect_on_start === 'eager') this.order.forEach(s => s.connectRemote());
        this.select(last || this.order[0] || null);

        window.addEventListener('keydown', e => this.onKey(e));
        window.addEventListener('mousedown', e => {
            if (!$('#menu').contains(e.target)) hideMenu();
        });
        window.addEventListener('blur', hideMenu);
        $('#btn-new').onclick = () => sessionDialog();
        $('#btn-add-host').onclick = () => hostDialog();
    },

    // reload fetches state from the backend and re-renders, keeping
    // terminals of sessions that still exist.
    async reload() {
        this.state = await State();
        const seen = new Set();
        for (const d of this.state.sessions) {
            seen.add(d.id);
            const s = this.sessions.get(d.id);
            if (s) s.data = d;
            else this.sessions.set(d.id, new SessionUI(d));
        }
        for (const [id, s] of this.sessions) {
            if (!seen.has(id)) {
                s.dispose();
                this.sessions.delete(id);
                if (this.active === s) this.active = null;
            }
        }
        this.render();
    },

    // groups returns [{host, sessions}] in host order; sessions whose host is
    // missing from the config are collected under their host name.
    groups() {
        const groups = this.state.hosts.map(h => ({name: h.name, host: h, sessions: []}));
        const byName = new Map(groups.map(g => [g.name, g]));
        for (const d of this.state.sessions) {
            let g = byName.get(d.host);
            if (!g) {
                g = {name: d.host, host: null, sessions: []};
                byName.set(d.host, g);
                groups.push(g);
            }
            g.sessions.push(this.sessions.get(d.id));
        }
        return groups;
    },

    render() {
        const err = $('#config-error');
        err.hidden = !this.state.config_error;
        err.textContent = this.state.config_error ? `Config not loaded — changes will not be saved:\n${this.state.config_error}` : '';

        const list = $('#session-list');
        list.replaceChildren();
        this.order = [];
        for (const g of this.groups()) {
            const group = el('div', {className: 'host-group'});
            if (this.collapsed.has(g.name)) group.classList.add('collapsed');
            const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Host actions'});
            const head = el('div', {className: 'host-head', title: g.host ? g.host.ssh_target : 'host not configured'},
                el('span', {className: 'caret', textContent: '▾'}),
                el('span', {className: 'label', textContent: g.name + (g.host ? '' : ' (missing)')}), more);
            head.onclick = e => {
                if (more.contains(e.target)) return;
                group.classList.toggle('collapsed');
                group.classList.contains('collapsed') ? this.collapsed.add(g.name) : this.collapsed.delete(g.name);
                store.set('collapsed', [...this.collapsed]);
            };
            more.onclick = e => {
                e.stopPropagation();
                hostMenu(g, e.clientX, e.clientY);
            };
            head.oncontextmenu = e => {
                e.preventDefault();
                hostMenu(g, e.clientX, e.clientY);
            };
            const items = el('div', {className: 'sessions'});
            for (const s of g.sessions) {
                this.order.push(s);
                items.append(this.sessionItem(s, this.order.length));
            }
            enableDrop(items, g.name);
            group.append(head, items);
            list.append(group);
        }
        if (this.active && !this.sessions.has(this.active.id)) this.active = null;
        this.renderMain();
    },

    sessionItem(s, n) {
        const d = s.data;
        const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Session actions'});
        const item = el('div', {className: 'session', draggable: true, title: d.error || d.local_path},
            el('span', {className: 'dot ' + s.dotClass()}),
            el('div', {className: 'text'},
                el('div', {className: 'name', textContent: d.name}),
                el('div', {className: 'sub', textContent: '‎' + d.local_path})),
            n <= 9 ? el('span', {className: 'num', textContent: n}) : '',
            more);
        item.dataset.id = d.id;
        item.dataset.host = d.host;
        if (s === this.active) item.classList.add('active');
        item.onclick = e => !more.contains(e.target) && this.select(s);
        more.onclick = e => {
            e.stopPropagation();
            sessionMenu(s, e.clientX, e.clientY);
        };
        item.oncontextmenu = e => {
            e.preventDefault();
            sessionMenu(s, e.clientX, e.clientY);
        };
        enableDrag(item);
        s.item = item;
        return item;
    },

    renderDot(s) {
        const dot = s.item?.querySelector('.dot');
        if (dot) dot.className = 'dot ' + s.dotClass();
    },

    renderMain() {
        const s = this.active;
        const hasHosts = this.state.hosts.length > 0;
        $('#bar').hidden = !s;
        $('#panes').hidden = !s;
        $('#empty').hidden = !!s;
        if (!s) {
            $('#empty-text').textContent = hasHosts ? 'No sessions yet.' : 'Add an agent host to get started.';
            const b = $('#empty-action');
            b.textContent = hasHosts ? 'New session…' : 'Add host…';
            b.onclick = () => hasHosts ? sessionDialog() : hostDialog();
            return;
        }
        const d = s.data;
        $('#session-name').textContent = d.name;
        const nav = $('#tabs');
        nav.replaceChildren(...s.tabs.map((t, i) => {
            const b = el('button', {textContent: t.label}, el('kbd', {textContent: keyHint + (i + 1)}));
            if (i === s.activeTab) b.classList.add('active');
            b.onclick = () => this.selectTab(i);
            return b;
        }));
        const path = $('#session-path');
        path.classList.toggle('error', !!d.error);
        const host = this.state.hosts.find(h => h.name === d.host);
        path.textContent = d.error || `${host.ssh_target}:${d.remote_path}  ·  tmux ${d.tmux_name}`;
        path.title = `local: ${d.local_path}`;
    },

    select(s) {
        if (this.active && this.active !== s) this.active.hide();
        this.active = s;
        this.renderMain(); // un-hides the pane area before the terminal measures itself
        if (s) {
            store.set('active', s.id);
            s.show();
        }
        document.querySelectorAll('.session').forEach(i => i.classList.toggle('active', s && i.dataset.id === s.id));
    },

    selectTab(i) {
        if (!this.active) return;
        this.active.selectTab(i);
        this.renderMain();
    },

    step(delta) {
        if (!this.order.length) return;
        const i = this.order.indexOf(this.active);
        this.select(this.order[(i + delta + this.order.length) % this.order.length]);
    },

    onKey(e) {
        if (document.querySelector('dialog[open]')) return;
        const a = appKey(e);
        if (!a) {
            if (e.key === 'Escape') hideMenu();
            return;
        }
        e.preventDefault();
        hideMenu();
        if (a.tab !== undefined) this.selectTab(a.tab);
        else if (a.session !== undefined) this.order[a.session] && this.select(this.order[a.session]);
        else if (a.step) this.step(a.step);
        else if (a.newSession) sessionDialog();
    },

    focusActive() {
        this.active?.show();
    },
};

// ---------------------------------------------------------------- drag & drop

let dragged = null;

function enableDrag(item) {
    item.addEventListener('dragstart', e => {
        dragged = item;
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', item.dataset.id);
    });
    item.addEventListener('dragend', () => {
        dragged = null;
        clearDropMarks();
    });
}

function clearDropMarks() {
    document.querySelectorAll('.drop-before, .drop-end').forEach(x => x.classList.remove('drop-before', 'drop-end'));
}

// Sessions can be reordered within their host group only.
function enableDrop(container, host) {
    const target = e => {
        if (!dragged || dragged.dataset.host !== host) return null;
        const item = e.target.closest('.session');
        return item && item !== dragged ? item : 'end';
    };
    container.addEventListener('dragover', e => {
        const t = target(e);
        if (!t) return;
        e.preventDefault();
        clearDropMarks();
        t === 'end' ? container.classList.add('drop-end') : t.classList.add('drop-before');
    });
    container.addEventListener('dragleave', e => {
        if (!container.contains(e.relatedTarget)) clearDropMarks();
    });
    container.addEventListener('drop', async e => {
        const t = target(e);
        if (!t) return;
        e.preventDefault();
        const id = dragged.dataset.id;
        clearDropMarks();
        await moveSession(id, t === 'end' ? nextOutsideGroup(host) : t.dataset.id);
    });
}

// The backend order is one flat list; "end of a group" means "before the
// first session of the next group", or the very end.
function nextOutsideGroup(host) {
    const all = app.state.sessions;
    let last = -1;
    all.forEach((d, i) => d.host === host && (last = i));
    return all.slice(last + 1).find(d => d.host !== host)?.id ?? '';
}

async function moveSession(id, beforeID) {
    try {
        await MoveSession(id, beforeID);
    } catch (err) {
        await confirmDialog({title: 'Could not reorder', text: String(err), ok: null});
    }
    await app.reload();
}

// Move up/down within the host group, for keyboard users.
function moveBy(s, delta) {
    const group = app.order.filter(o => o.data.host === s.data.host);
    const i = group.indexOf(s);
    const j = i + delta;
    if (j < 0 || j >= group.length) return;
    // Moving down by one == moving before the item two places down.
    const before = delta < 0 ? group[j].id : (group[j + 1]?.id ?? nextOutsideGroup(s.data.host));
    moveSession(s.id, before);
}

// ---------------------------------------------------------------- menus

function showMenu(x, y, items) {
    const m = $('#menu');
    m.replaceChildren(...items.map(it => {
        if (it === '-') return el('hr');
        const b = el('button', {textContent: it.label, className: it.danger ? 'danger' : '', disabled: !!it.disabled});
        b.onclick = () => {
            hideMenu();
            it.action();
        };
        return b;
    }));
    m.hidden = false;
    const r = m.getBoundingClientRect();
    m.style.left = Math.min(x, innerWidth - r.width - 4) + 'px';
    m.style.top = Math.min(y, innerHeight - r.height - 4) + 'px';
}

function hideMenu() {
    $('#menu').hidden = true;
}

function sessionMenu(s, x, y) {
    showMenu(x, y, [
        {label: 'Edit…', action: () => sessionDialog(s)},
        {label: 'Move up', action: () => moveBy(s, -1)},
        {label: 'Move down', action: () => moveBy(s, 1)},
        '-',
        {label: 'Disconnect', action: () => s.disconnect()},
        {label: 'End remote session…', danger: true, action: () => endRemote(s)},
        {label: 'Delete session…', danger: true, action: () => deleteSession(s)},
    ]);
}

function hostMenu(g, x, y) {
    showMenu(x, y, [
        {label: 'New session on this host…', disabled: !g.host, action: () => sessionDialog(null, g.name)},
        {label: g.host ? 'Edit host…' : 'Configure host…', action: () => hostDialog(g.host, g.host ? null : g.name)},
        '-',
        {label: 'Delete host…', danger: true, disabled: !g.host, action: () => deleteHost(g.host)},
    ]);
}

// ---------------------------------------------------------------- dialogs

// runDialog opens dlg and calls submit(form) when OK is pressed; the dialog
// stays open and shows the error if submit throws. Resolves true on success.
function runDialog(dlg, submit, focus = null) {
    const form = dlg.querySelector('form');
    const errEl = dlg.querySelector('.error');
    errEl.textContent = '';
    return new Promise(resolve => {
        const onSubmit = async e => {
            if (e.submitter?.value !== 'ok') return; // Cancel closes normally
            e.preventDefault();
            e.submitter.disabled = true;
            try {
                await submit(form);
                dlg.close('ok');
            } catch (err) {
                errEl.textContent = err?.message ?? String(err);
            } finally {
                e.submitter.disabled = false;
            }
        };
        form.addEventListener('submit', onSubmit);
        dlg.addEventListener('close', () => {
            form.removeEventListener('submit', onSubmit);
            resolve(dlg.returnValue === 'ok');
            app.focusActive();
        }, {once: true});
        dlg.returnValue = '';
        dlg.showModal();
        focus?.focus();
    });
}

async function confirmDialog({title, text, ok = 'OK', check, action}) {
    const dlg = $('#dlg-confirm');
    dlg.querySelector('h2').textContent = title;
    dlg.querySelector('.text').textContent = text;
    const okBtn = dlg.querySelector('button[value=ok]');
    okBtn.hidden = !ok;
    okBtn.textContent = ok || '';
    dlg.querySelector('button[value=cancel]').textContent = ok ? 'Cancel' : 'Close';
    const checkLabel = dlg.querySelector('label.check');
    const box = dlg.querySelector('input[name=check]');
    checkLabel.hidden = !check;
    box.checked = false;
    if (check) checkLabel.querySelector('span').textContent = check;
    return runDialog(dlg, async () => action && action(box.checked));
}

async function sessionDialog(s = null, presetHost = null) {
    if (!s && app.state.hosts.length === 0) {
        if (!await hostDialog()) return;
    }
    const dlg = $('#dlg-session');
    const f = dlg.querySelector('form');
    const editing = !!s;
    dlg.querySelector('h2').textContent = editing ? `Edit “${s.data.name}”` : 'New session';
    f.querySelector('button[value=ok]').textContent = editing ? 'Save' : 'Create';
    f.elements.host.replaceChildren(...app.state.hosts.map(h => el('option', {value: h.name, textContent: `${h.name}  (${h.ssh_target})`})));
    const lastHost = store.get('lastHost', '');
    f.elements.host.value = s?.data.host ?? presetHost ?? (app.state.hosts.some(h => h.name === lastHost) ? lastHost : app.state.hosts[0].name);
    f.elements.dir.value = s?.data.local_path ?? '';
    f.elements.name.value = s?.data.name ?? '';
    f.elements.name.placeholder = '';
    f.elements.mode.value = s?.data.start_mode ?? store.get('lastMode', 'claude');
    f.elements.host.disabled = f.elements.dir.disabled = f.elements.browse.disabled = editing;
    const preview = dlg.querySelector('.preview');
    preview.textContent = editing ? `→ ${s.data.remote_path || s.data.error}` : '';
    preview.classList.toggle('bad', editing && !!s.data.error);

    let seq = 0;
    const update = async () => {
        if (editing) return;
        const n = ++seq;
        if (!f.elements.dir.value.trim()) {
            preview.textContent = '';
            f.elements.name.placeholder = '';
            return;
        }
        const p = await PreviewSession({host: f.elements.host.value, local_path: f.elements.dir.value, name: '', start_mode: f.elements.mode.value});
        if (n !== seq) return; // a newer request is in flight
        f.elements.name.placeholder = p.name;
        preview.classList.toggle('bad', !!p.error);
        preview.textContent = p.error || `→ ${f.elements.host.value}:${p.remote_path}`;
    };
    let timer;
    f.elements.dir.oninput = () => {
        clearTimeout(timer);
        timer = setTimeout(update, 150);
    };
    f.elements.host.onchange = update;
    f.elements.browse.onclick = async () => {
        const host = app.state.hosts.find(h => h.name === f.elements.host.value);
        const start = f.elements.dir.value || app.state.workspace_roots?.[0] || host?.local_prefix || '';
        const dir = await BrowseDirectory(start);
        if (dir) {
            f.elements.dir.value = dir;
            update();
        }
    };
    update();

    let created = null;
    const ok = await runDialog(dlg, async () => {
        if (editing) {
            await UpdateSession(s.id, f.elements.name.value || s.data.name, f.elements.mode.value);
        } else {
            created = await CreateSession({host: f.elements.host.value, local_path: f.elements.dir.value, name: f.elements.name.value, start_mode: f.elements.mode.value});
            store.set('lastHost', f.elements.host.value);
            store.set('lastMode', f.elements.mode.value);
        }
    }, editing ? f.elements.name : f.elements.dir);
    if (!ok) return;
    await app.reload();
    if (created) app.select(app.sessions.get(created.id));
}

async function hostDialog(h = null, presetName = null) {
    const dlg = $('#dlg-host');
    const f = dlg.querySelector('form');
    dlg.querySelector('h2').textContent = h ? `Edit host “${h.name}”` : 'Add agent host';
    for (const k of ['name', 'ssh_target', 'local_prefix', 'remote_prefix']) f.elements[k].value = h?.[k] ?? '';
    if (presetName) f.elements.name.value = presetName;
    f.elements.browse.onclick = async () => {
        const dir = await BrowseDirectory(f.elements.local_prefix.value || '~');
        if (dir) f.elements.local_prefix.value = dir;
    };
    const ok = await runDialog(dlg, () => SaveHost(h?.name ?? '', {
        name: f.elements.name.value, ssh_target: f.elements.ssh_target.value,
        local_prefix: f.elements.local_prefix.value, remote_prefix: f.elements.remote_prefix.value,
    }));
    if (ok) await app.reload();
    return ok;
}

async function endRemote(s) {
    await confirmDialog({
        title: `End remote session “${s.data.name}”?`,
        text: `This kills tmux session ${s.data.tmux_name} on ${s.data.host}, including claude or anything else running in it. The session stays in the list; connecting again starts fresh.`,
        ok: 'End session',
        action: () => KillRemote(s.id),
    });
}

async function deleteSession(s) {
    const ok = await confirmDialog({
        title: `Delete session “${s.data.name}”?`,
        text: 'Removes it from the list. Files are not touched.',
        ok: 'Delete',
        check: `Also end tmux session ${s.data.tmux_name} on ${s.data.host} (stops claude there)`,
        action: async kill => {
            if (kill) await KillRemote(s.id);
            await DeleteSession(s.id);
        },
    });
    if (!ok) return;
    const i = app.order.indexOf(s);
    await app.reload();
    if (!app.active) app.select(app.order[Math.min(i, app.order.length - 1)] || null);
}

async function deleteHost(h) {
    const ok = await confirmDialog({
        title: `Delete host “${h.name}”?`,
        text: 'Only hosts without sessions can be deleted.',
        ok: 'Delete',
        action: () => DeleteHost(h.name),
    });
    if (ok) await app.reload();
}

app.init();
