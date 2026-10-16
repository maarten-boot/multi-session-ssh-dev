import './style.css';

import {isMac, TermView} from './terminal';
import {effectiveColorMode} from './appearance';
import {rank, score} from './fuzzy';
import {ACTIONS, actionFor, buildKeymap, format} from './keys';
import {FilesView} from './files';
import {$, el, hideMenu, showMenu, store} from './ui';
import {WindowSetTitle} from '../wailsjs/runtime/runtime';
import {
    BrowseDirectory, CreateSession, DeleteHost, DeleteSession, KillRemote, MoveSession,
    PreviewSession, SaveHost, SaveUI, StartLocal, StartRemote, State, UpdateSession, Version,
    SaveGroup, DeleteGroup, MoveGroup, SetSessionGroup, RepoCheck, TrustRepo,
} from '../wailsjs/go/main/App';

// ---------------------------------------------------------------- keyboard

// keymap is rebuilt from the config on every reload (see keys.js).
let keymap = buildKeymap({}, isMac);

// appKey returns the app action for a keydown, or null if the key belongs
// to the terminal.
const appKey = e => actionFor(e, keymap.map);
const TAB_ACTIONS = ['tab_remote', 'tab_local', 'tab_files'];
const keyHint = action => format(keymap.map[action], isMac);

// ---------------------------------------------------------------- sessions

// Activity detection (SPEC F4). A hidden terminal gets a "bell" badge when
// it rings the bell (claude waiting for input, with Claude Code's
// preferredNotifChannel set to terminal_bell), and an "activity" badge when
// it starts producing real output after being quiet. tmux redraws its
// status line (~150-300 bytes) when the clock changes; the burst threshold
// keeps that from counting.
const IDLE_MS = 10000;
const BURST_MS = 2000;
const BURST_MIN = 512;

class Tab {
    constructor(session, label, start, exitHint, onState) {
        this.session = session;
        this.label = label;
        this.start = start;
        this.exitHint = exitHint;
        this.onState = onState;
        this.badge = null; // null | 'activity' | 'bell'
        this.lastOutput = 0;
        this.burst = null;
        this.pane = el('div', {className: 'pane', hidden: true});
        session.container.append(this.pane);
        this.view = null;
    }

    ensureView() {
        if (!this.view) {
            this.view = new TermView(this.pane, {
                start: this.start, exitHint: this.exitHint, ui: app.state.ui,
                colorMode: effectiveColorMode(app.state.ui, this.session.data),
                isAppKey: e => appKey(e) !== null, onState: this.onState,
                onActivity: (kind, n) => this.activity(kind, n),
            });
            this.view.spawn();
        }
        return this.view;
    }

    show() {
        this.pane.hidden = false;
        this.ensureView().focus();
        this.setBadge(null);
    }

    visible() {
        return !this.pane.hidden && app.active === this.session && document.hasFocus();
    }

    activity(kind, n) {
        const now = Date.now();
        if (this.visible() || !app.state.ui.activity_badges) {
            this.lastOutput = now;
            this.burst = null;
            return;
        }
        if (kind === 'bell') {
            this.setBadge('bell');
            return;
        }
        if (now - this.lastOutput >= IDLE_MS) this.burst = {start: now, bytes: 0};
        this.lastOutput = now;
        if (!this.burst) return;
        if (now - this.burst.start > BURST_MS) {
            this.burst = null;
            return;
        }
        this.burst.bytes += n;
        const min = Math.max(BURST_MIN, 2 * (this.view?.term.cols ?? 80));
        if (this.burst.bytes >= min) {
            this.burst = null;
            if (this.badge !== 'bell') this.setBadge('activity');
        }
    }

    setBadge(b) {
        if (this.badge === b) return;
        this.badge = b;
        app.renderBadges(this.session);
    }

    hide() {
        this.pane.hidden = true;
    }

    dispose() {
        this.view?.dispose();
        this.pane.remove();
    }
}

// FilesTab hosts the session's file tree; it refreshes whenever shown.
class FilesTab {
    constructor(session) {
        this.session = session;
        this.label = 'Files';
        this.badge = null;
        this.pane = el('div', {className: 'pane files-pane', hidden: true});
        session.container.append(this.pane);
        this.view = null;
    }

    show() {
        this.pane.hidden = false;
        if (!this.view) {
            this.view = new FilesView(this.pane, this.session);
            this.view.renderRepoBanner();
        }
        else this.view.refresh();
        this.view.focus();
    }

    hide() {
        this.pane.hidden = true;
    }

    dispose() {
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
            new Tab(this, 'Remote', (c, r) => StartRemote(this.data.id, c, r), {
                what: 'ssh', again: 'reconnect', remote: true,
                place: () => this.data.host,
                autoReconnect: () => app.state.ui.auto_reconnect,
            }, s => this.setRemoteState(s)),
            new Tab(this, 'Local', (c, r) => StartLocal(this.data.id, c, r),
                {what: 'shell', again: 'restart', remote: false}),
            new FilesTab(this),
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

    // repoWarn: the repository has hooks or risky config the user has not
    // reviewed (or that changed since). See docs/CONNECTIVITY.md.
    repoWarn() {
        return !!(this.audit?.findings?.length && this.audit.fingerprint !== this.data.trusted_git);
    }

    checkRepo() {
        return app.checkRepo(this);
    }

    showRepoDetails() {
        return repoDialog(this);
    }

    badge() {
        const b = this.tabs.map(t => t.badge);
        return b.includes('bell') ? 'bell' : b.includes('activity') ? 'activity' : null;
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

    applyAppearance(ui) {
        const mode = effectiveColorMode(ui, this.data);
        this.tabs.forEach(t => t.view?.setAppearance?.(ui, mode));
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

// applyTheme switches the app chrome; index.html applies the cached value
// before first paint to avoid a flash of the wrong theme.
function applyTheme(theme) {
    document.documentElement.dataset.theme = theme;
    store.set('theme', theme);
}

const app = {
    state: null,
    sessions: new Map(), // id -> SessionUI
    order: [],           // SessionUI in sidebar order
    active: null,
    collapsed: new Set(store.get('collapsed', [])),
    mru: store.get('mru', []), // session ids, most recent first

    async init() {
        await this.reload();
        const last = this.sessions.get(store.get('active', ''));
        if (this.state.ui.connect_on_start === 'eager') this.order.forEach(s => s.connectRemote());
        this.checkAll();
        setInterval(() => this.checkAll(), 120000);
        this.select(last || this.order[0] || null);

        window.addEventListener('keydown', e => this.onKey(e));
        window.addEventListener('mousedown', e => {
            if (!$('#menu').contains(e.target)) hideMenu();
        });
        window.addEventListener('blur', hideMenu);
        window.addEventListener('focus', () => {
            if (this.active) this.checkRepo(this.active);
            const t = this.active?.tabs[this.active.activeTab];
            t?.setBadge?.(null);
        });
        $('#btn-new').onclick = e => {
            const r = e.currentTarget.getBoundingClientRect();
            newMenu(r.left, r.bottom + 2);
        };
        $('#btn-add-host').onclick = () => hostDialog();
        $('#btn-settings').onclick = () => settingsDialog();
    },

    // reload fetches state from the backend and re-renders, keeping
    // terminals of sessions that still exist.
    async reload() {
        this.state = await State();
        applyTheme(this.state.ui.theme);
        // The Files tree uses the terminal font too.
        const root = document.documentElement.style;
        root.setProperty('--mono-size', `${this.state.ui.font_size || 13}px`);
        if (this.state.ui.font_family) root.setProperty('--mono-family', this.state.ui.font_family);
        else root.removeProperty('--mono-family');
        keymap = buildKeymap(this.state.keys, isMac);
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
        for (const s of this.sessions.values()) s.applyAppearance(this.state.ui);
    },

    // sections returns the sidebar structure: ungrouped sessions first, then
    // one section per folder, each split by host in host order. Hosts appear
    // where they have sessions; a host without any session is listed in the
    // ungrouped section so its menu stays reachable. Unknown
    // hosts or folders referenced by sessions are shown as "(missing)".
    sections() {
        const folders = [...this.state.groups];
        for (const d of this.state.sessions) {
            if (d.group && !folders.includes(d.group)) folders.push(d.group);
        }
        const hostNames = this.state.hosts.map(h => h.name);
        for (const d of this.state.sessions) if (!hostNames.includes(d.host)) hostNames.push(d.host);
        return ['', ...folders].map(group => {
            const hosts = hostNames.map(name => ({
                name, group,
                host: this.state.hosts.find(h => h.name === name) ?? null,
                sessions: this.state.sessions.filter(d => d.host === name && d.group === group).map(d => this.sessions.get(d.id)),
            })).filter(h => h.sessions.length ||
                // A host without any session still needs a header (and menu).
                (group === '' && h.host && !this.state.sessions.some(d => d.host === h.name)));
            return {group, known: group === '' || this.state.groups.includes(group), hosts};
        });
    },

    collapsible(el, key) {
        if (this.collapsed.has(key)) el.classList.add('collapsed');
        return () => {
            el.classList.toggle('collapsed');
            el.classList.contains('collapsed') ? this.collapsed.add(key) : this.collapsed.delete(key);
            store.set('collapsed', [...this.collapsed]);
        };
    },

    render() {
        const err = $('#config-error');
        const problems = [];
        if (this.state.config_error) problems.push(`Config not loaded — changes will not be saved:\n${this.state.config_error}`);
        problems.push(...keymap.errors);
        err.hidden = !problems.length;
        err.textContent = problems.join('\n');
        $('#btn-new').title = `New session (${keyHint('new_session')}), folder or host`;

        const list = $('#session-list');
        list.replaceChildren();
        this.order = [];
        for (const sec of this.sections()) {
            let parent = list;
            if (sec.group) {
                const folder = el('div', {className: 'folder'});
                const count = sec.hosts.reduce((n, h) => n + h.sessions.length, 0);
                const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Folder actions'});
                const head = el('div', {className: 'folder-head'},
                    el('span', {className: 'caret', textContent: '▾'}),
                    el('span', {className: 'label', textContent: sec.group + (sec.known ? '' : ' (missing)')}),
                    el('span', {className: 'count', textContent: count}), more);
                const toggle = this.collapsible(folder, 'g:' + sec.group);
                head.onclick = e => !more.contains(e.target) && toggle();
                more.onclick = e => {
                    e.stopPropagation();
                    folderMenu(sec, e.clientX, e.clientY);
                };
                head.oncontextmenu = e => {
                    e.preventDefault();
                    folderMenu(sec, e.clientX, e.clientY);
                };
                enableFolderDrop(head, sec.group);
                const body = el('div', {className: 'folder-body'});
                if (!count) body.append(el('div', {className: 'folder-empty', textContent: 'Empty — drag sessions here'}));
                folder.append(head, body);
                list.append(folder);
                parent = body;
            }
            for (const g of sec.hosts) {
                const group = el('div', {className: 'host-group'});
                const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Host actions'});
                const head = el('div', {className: 'host-head', title: g.host ? g.host.ssh_target : 'host not configured'},
                    el('span', {className: 'caret', textContent: '▾'}),
                    el('span', {className: 'label', textContent: g.name + (g.host ? '' : ' (missing)')}), more);
                const toggle = this.collapsible(group, `h:${g.group}/${g.name}`);
                head.onclick = e => !more.contains(e.target) && toggle();
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
                enableDrop(items, g.group, g.name);
                group.append(head, items);
                parent.append(group);
            }
        }
        if (this.active && !this.sessions.has(this.active.id)) this.active = null;
        this.renderMain();
    },

    sessionItem(s, n) {
        const d = s.data;
        const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Session actions'});
        const item = el('div', {className: 'session', draggable: true},
            el('span', {className: 'dot ' + s.dotClass()}),
            el('div', {className: 'text'},
                el('div', {className: 'name', textContent: d.name}),
                el('div', {className: 'sub', textContent: '‎' + d.local_path})),
            el('span', {className: 'repo-warn', textContent: '⚠', hidden: !s.repoWarn(),
                title: 'Repository hooks or settings to review (see the Files tab)'}),
            el('span', {className: 'badge'}),
            n <= 9 ? el('span', {className: 'num', textContent: n}) : '',
            more);
        item.dataset.id = d.id;
        item.dataset.host = d.host;
        item.dataset.group = d.group;
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
        this.renderBadges(s);
        return item;
    },

    // checkRepo runs the repository check for a session (one at a time per
    // session) and updates the sidebar and the Files tab.
    async checkRepo(s) {
        if (s.checking) return;
        s.checking = true;
        try {
            s.audit = await RepoCheck(s.id);
        } catch {
            s.audit = null;
        } finally {
            s.checking = false;
        }
        this.renderRepoWarn(s);
    },

    renderRepoWarn(s) {
        const w = s.item?.querySelector('.repo-warn');
        if (w) w.hidden = !s.repoWarn();
        s.tabs[2]?.view?.renderRepoBanner?.();
    },

    // checkAll checks every session's repository, one after another.
    async checkAll() {
        for (const s of [...this.sessions.values()]) await this.checkRepo(s);
    },

    // renderBadges updates a session's sidebar badge, its tab badges when it
    // is the active session, and the window title.
    renderBadges(s) {
        const b = s.badge();
        const badgeEl = s.item?.querySelector(".badge");
        if (badgeEl) badgeEl.className = 'badge' + (b ? ' ' + b : '');
        if (s === this.active) {
            $('#tabs').querySelectorAll('button').forEach((btn, i) => {
                btn.classList.toggle('bell', s.tabs[i].badge === 'bell');
                btn.classList.toggle('activity', s.tabs[i].badge === 'activity');
            });
        }
        const n = [...this.sessions.values()].filter(x => x.badge()).length;
        const title = n ? `(${n}) Session Workbench` : 'Session Workbench';
        if (title !== this.title) {
            this.title = title;
            WindowSetTitle(title);
        }
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
            const b = el('button', {textContent: t.label}, el('kbd', {textContent: keyHint(TAB_ACTIONS[i])}));
            if (i === s.activeTab) b.classList.add('active');
            b.onclick = () => this.selectTab(i);
            return b;
        }));
        this.renderBadges(s);
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
            this.checkRepo(s);
            store.set('active', s.id);
            this.mru = [s.id, ...this.mru.filter(id => id !== s.id)].slice(0, 50);
            store.set('mru', this.mru);
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
        const n = /^session_(\d)$/.exec(a)?.[1];
        if (n) this.order[n - 1] && this.select(this.order[n - 1]);
        else if (TAB_ACTIONS.includes(a)) this.selectTab(TAB_ACTIONS.indexOf(a));
        else if (a === 'next_session') this.step(1);
        else if (a === 'prev_session') this.step(-1);
        else if (a === 'new_session') sessionDialog();
        else if (a === 'quick_switcher') quickSwitcher();
        else if (a === 'settings') settingsDialog();
        else if (a === 'find') this.active?.tabs[this.active.activeTab].view?.openFind?.();
    },

    focusActive() {
        this.active?.show();
    },
};

// ---------------------------------------------------------------- quick switcher

// quickSwitcher lists sessions, most recently used first (the current one
// last, so Enter on an empty query goes back to the previous session), and
// fuzzy-filters them by name, host and path as you type.
function quickSwitcher() {
    const dlg = $('#dlg-switch');
    if (dlg.open || !app.order.length) return;
    const input = dlg.querySelector('input');
    const list = dlg.querySelector('.switch-list');
    const recent = app.mru.map(id => app.sessions.get(id)).filter(s => s && s !== app.active);
    const rest = app.order.filter(s => !recent.includes(s) && s !== app.active);
    const items = [...recent, ...rest, ...(app.active ? [app.active] : [])];
    let shown = [];
    let sel = 0;

    const highlight = (text, q) => {
        const pos = [];
        if (q && score(q, text, pos) < 0) pos.length = 0;
        const span = el('span', {className: 'name'});
        [...text].forEach((ch, i) => span.append(pos.includes(i) ? el('b', {textContent: ch}) : ch));
        return span;
    };
    const render = () => {
        const q = input.value.trim();
        shown = rank(items, q, s => [[s.data.name, 2], [s.data.group, 1], [s.data.host, 1], [s.data.local_path, 1]]);
        sel = Math.min(sel, Math.max(0, shown.length - 1));
        list.replaceChildren(...shown.map((s, i) => {
            const row = el('div', {className: 'switch-item' + (i === sel ? ' selected' : '')},
                el('span', {className: 'dot ' + s.dotClass()}),
                highlight(s.data.name, q),
                el('span', {className: 'where', textContent: `${s.data.group ? s.data.group + ' · ' : ''}${s.data.host} · ${s.data.local_path}`}),
                s === app.active ? el('span', {className: 'current', textContent: 'current'}) : '');
            row.onmousedown = e => {
                e.preventDefault();
                choose(i);
            };
            return row;
        }));
        if (!shown.length) list.append(el('div', {className: 'switch-empty', textContent: 'No matching session'}));
        list.children[sel]?.scrollIntoView({block: 'nearest'});
    };
    const choose = i => {
        const s = shown[i];
        dlg.close();
        if (s) app.select(s);
    };
    input.value = '';
    input.oninput = () => {
        sel = 0;
        render();
    };
    input.onkeydown = e => {
        if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) sel = (sel + 1) % Math.max(1, shown.length);
        else if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)) sel = (sel - 1 + shown.length) % Math.max(1, shown.length);
        else if (e.key === 'Enter') return choose(sel);
        else return;
        e.preventDefault();
        render();
    };
    dlg.addEventListener('close', () => app.focusActive(), {once: true});
    render();
    dlg.showModal();
    input.focus();
}

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

// Sessions can be reordered within their folder and host only.
function enableDrop(container, group, host) {
    const target = e => {
        if (!dragged || dragged.dataset.host !== host || dragged.dataset.group !== group) return null;
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
        await moveSession(id, t === 'end' ? nextOutside(group, host) : t.dataset.id);
    });
}

// Dropping a session on a folder header moves it into that folder.
function enableFolderDrop(head, group) {
    head.addEventListener('dragover', e => {
        if (!dragged || dragged.dataset.group === group) return;
        e.preventDefault();
        clearDropMarks();
        head.classList.add('drop-into');
    });
    head.addEventListener('dragleave', () => head.classList.remove('drop-into'));
    head.addEventListener('drop', async e => {
        if (!dragged || dragged.dataset.group === group) return;
        e.preventDefault();
        head.classList.remove('drop-into');
        await setGroup(dragged.dataset.id, group);
    });
}

// The backend order is one flat list; "end of a folder/host block" means
// "before the first later session outside the block", or the very end.
function nextOutside(group, host) {
    const all = app.state.sessions;
    const inBlock = d => d.host === host && d.group === group;
    let last = -1;
    all.forEach((d, i) => inBlock(d) && (last = i));
    return all.slice(last + 1).find(d => !inBlock(d))?.id ?? '';
}

async function moveSession(id, beforeID) {
    try {
        await MoveSession(id, beforeID);
    } catch (err) {
        await confirmDialog({title: 'Could not reorder', text: String(err), ok: null});
    }
    await app.reload();
}

// Move up/down within the folder and host, for keyboard users.
function moveBy(s, delta) {
    const block = app.order.filter(o => o.data.host === s.data.host && o.data.group === s.data.group);
    const i = block.indexOf(s);
    const j = i + delta;
    if (j < 0 || j >= block.length) return;
    // Moving down by one == moving before the item two places down.
    const before = delta < 0 ? block[j].id : (block[j + 1]?.id ?? nextOutside(s.data.group, s.data.host));
    moveSession(s.id, before);
}

async function setGroup(id, group) {
    try {
        await SetSessionGroup(id, group);
    } catch (err) {
        await confirmDialog({title: 'Could not move session', text: err?.message ?? String(err), ok: null});
    }
    await app.reload();
}

async function groupAction(fn, title) {
    try {
        await fn();
    } catch (err) {
        await confirmDialog({title, text: err?.message ?? String(err), ok: null});
    }
    await app.reload();
}

// ---------------------------------------------------------------- menus

function sessionMenu(s, x, y) {
    showMenu(x, y, [
        {label: 'Edit…', action: () => sessionDialog(s)},
        {label: 'Move up', action: () => moveBy(s, -1)},
        {label: 'Move down', action: () => moveBy(s, 1)},
        {label: 'Move to folder…', disabled: !app.state.groups.length && !s.data.group, action: () => folderPicker(s, x, y)},
        '-',
        {
            label: effectiveColorMode(app.state.ui, s.data) === 'mono' ? 'Use full colors' : 'Use mono colors (no colors)',
            action: () => setSessionColors(s, effectiveColorMode(app.state.ui, s.data) === 'mono' ? 'full' : 'mono'),
        },
        '-',
        {label: 'Disconnect', action: () => s.disconnect()},
        {label: 'End remote session…', danger: true, action: () => endRemote(s)},
        {label: 'Delete session…', danger: true, action: () => deleteSession(s)},
    ]);
}

// folderPicker is the second step of "Move to folder…".
function folderPicker(s, x, y) {
    showMenu(x, y, [
        {label: s.data.group ? 'No folder' : '✓ No folder', disabled: !s.data.group, action: () => setGroup(s.id, '')},
        ...app.state.groups.map(g => ({
            label: (g === s.data.group ? '✓ ' : '') + g, disabled: g === s.data.group, action: () => setGroup(s.id, g),
        })),
    ]);
}

function folderMenu(sec, x, y) {
    const i = app.state.groups.indexOf(sec.group);
    const groups = app.state.groups;
    showMenu(x, y, [
        {label: 'New session in this folder…', disabled: !sec.known, action: () => sessionDialog(null, null, sec.group)},
        {label: sec.known ? 'Rename folder…' : 'Create this folder', action: () => sec.known ? folderDialog(sec.group) :
                groupAction(() => SaveGroup('', sec.group), 'Could not create folder')},
        {label: 'Move folder up', disabled: i <= 0, action: () => groupAction(() => MoveGroup(sec.group, groups[i - 1]), 'Could not move folder')},
        {label: 'Move folder down', disabled: i < 0 || i >= groups.length - 1,
            action: () => groupAction(() => MoveGroup(sec.group, groups[i + 2] ?? ''), 'Could not move folder')},
        '-',
        {label: 'Delete folder…', danger: true, disabled: !sec.known, action: () => deleteFolder(sec)},
    ]);
}

function newMenu(x, y) {
    showMenu(x, y, [
        {label: `New session…   ${keyHint('new_session')}`, action: () => sessionDialog()},
        {label: 'New folder…', action: () => folderDialog()},
        {label: 'Add host…', action: () => hostDialog()},
    ]);
}

function hostMenu(g, x, y) {
    showMenu(x, y, [
        {label: 'New session on this host…', disabled: !g.host, action: () => sessionDialog(null, g.name, g.group)},
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

async function sessionDialog(s = null, presetHost = null, presetGroup = null) {
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
    f.elements.colors.value = s?.data.color_mode ?? '';
    f.elements.group.replaceChildren(el('option', {value: '', textContent: '(no folder)'}),
        ...app.state.groups.map(g => el('option', {value: g, textContent: g})));
    f.elements.group.value = s?.data.group ?? presetGroup ?? '';
    f.elements.colors.options[0].textContent = `default (${app.state.ui.color_mode})`;
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
        const p = await PreviewSession({host: f.elements.host.value, local_path: f.elements.dir.value, name: '', start_mode: f.elements.mode.value, color_mode: '', group: ''});
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
            await UpdateSession(s.id, {
                name: f.elements.name.value || s.data.name, start_mode: f.elements.mode.value,
                color_mode: f.elements.colors.value, group: f.elements.group.value,
            });
        } else {
            created = await CreateSession({
                host: f.elements.host.value, local_path: f.elements.dir.value, name: f.elements.name.value,
                start_mode: f.elements.mode.value, color_mode: f.elements.colors.value, group: f.elements.group.value,
            });
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

// setSessionColors stores a per-session override; choosing the global
// value clears the override so the session follows future global changes.
async function setSessionColors(s, mode) {
    const override = mode === app.state.ui.color_mode ? '' : mode;
    try {
        await UpdateSession(s.id, {name: s.data.name, start_mode: s.data.start_mode, color_mode: override, group: s.data.group});
    } catch (err) {
        await confirmDialog({title: 'Could not change colors', text: String(err), ok: null});
    }
    await app.reload();
}

const ACTION_LABELS = {
    next_session: 'Next session', prev_session: 'Previous session', tab_remote: 'Remote tab',
    tab_local: 'Local tab', tab_files: 'Files tab', new_session: 'New session',
    quick_switcher: 'Quick switcher', settings: 'Settings', find: 'Find in terminal',
};

async function settingsDialog() {
    const dlg = $('#dlg-settings');
    if (dlg.open) return;
    const f = dlg.querySelector('form').elements;
    const ui = app.state.ui;
    for (const k of ['theme', 'color_mode', 'min_contrast', 'scrollback', 'font_family', 'font_size', 'connect_on_start']) f[k].value = ui[k] ?? '';
    f.auto_reconnect.checked = !!ui.auto_reconnect;
    f.activity_badges.checked = !!ui.activity_badges;
    f.copy_on_select.checked = !!ui.copy_on_select;
    const rows = Object.keys(ACTIONS).filter(a => !a.startsWith('session_')).map(a =>
        el('tr', {}, el('td', {textContent: ACTION_LABELS[a] ?? a}), el('td', {}, el('kbd', {textContent: keyHint(a) || '—'})),
            el('td', {className: 'action', textContent: a})));
    rows.push(el('tr', {}, el('td', {textContent: 'Session 1…9'}), el('td', {}, el('kbd', {textContent: keyHint('session_1').replace(/1$/, '1…9')})),
        el('td', {className: 'action', textContent: 'session_1 … session_9'})));
    dlg.querySelector('.shortcuts table').replaceChildren(...rows);
    Version().then(v => dlg.querySelector('.about').textContent = `Session Workbench ${v}`);
    dlg.querySelector('.shortcuts .hint').textContent =
        `Change shortcuts in the [keys] section of ${app.state.config_path}, e.g. next_session = "Ctrl+Tab"; "none" disables one. Restart the app after editing.`;
    const ok = await runDialog(dlg, () => SaveUI({
        ...ui,
        theme: f.theme.value,
        color_mode: f.color_mode.value,
        min_contrast: Number(f.min_contrast.value),
        scrollback: Number(f.scrollback.value),
        font_family: f.font_family.value.trim(),
        font_size: Number(f.font_size.value),
        connect_on_start: f.connect_on_start.value,
        auto_reconnect: f.auto_reconnect.checked,
        activity_badges: f.activity_badges.checked,
        copy_on_select: f.copy_on_select.checked,
    }));
    if (ok) await app.reload();
}

// repoDialog lists what git on this machine would run from the session's
// repository, and lets the user mark the current state as reviewed.
async function repoDialog(s) {
    await app.checkRepo(s);
    const a = s.audit;
    if (!a?.findings?.length) return;
    const dlg = $('#dlg-repo');
    const reviewed = a.fingerprint === s.data.trusted_git;
    const hooks = a.findings.filter(f => f.kind === 'hook').length;
    const cfg = a.findings.length - hooks;
    dlg.querySelector('h2').textContent = `Repository check: ${s.data.name}`;
    dlg.querySelector('.text').textContent =
        `${hooks ? `${hooks} hook${hooks > 1 ? 's' : ''}` : ''}${hooks && cfg ? ' and ' : ''}` +
        `${cfg ? `${cfg} setting${cfg > 1 ? 's' : ''}` : ''} in this repository would make git on this computer ` +
        'run a program or send data elsewhere. If the agent host can write to this repository it may have put ' +
        'them there. Check each one before running git here. If you set them up yourself (for example ' +
        'pre-commit, husky or git-lfs), mark them as reviewed: the warning comes back when anything changes.' +
        (reviewed ? '\n\nYou have reviewed this exact state before.' : '');
    dlg.querySelector('.findings').replaceChildren(...a.findings.map(f => el('div', {className: 'finding'},
        el('div', {className: 'what'}, el('b', {textContent: f.kind === 'hook' ? `hook ${f.name}` : f.name}),
            el('span', {textContent: ` — ${f.why}`})),
        el('code', {className: 'detail', textContent: f.detail}),
        el('code', {className: 'fix', textContent: f.fix, title: 'Run in the Local tab, in the repository'}))));
    const ok = await runDialog(dlg, () => TrustRepo(s.id, a.fingerprint));
    if (ok) {
        await app.reload();
        app.renderRepoWarn(s);
    }
}

async function folderDialog(oldName = '') {
    const dlg = $('#dlg-prompt');
    const f = dlg.querySelector('form').elements;
    dlg.querySelector('h2').textContent = oldName ? `Rename folder “${oldName}”` : 'New folder';
    dlg.querySelector('.hint').textContent = 'Folders group sessions in the sidebar, e.g. “Work” and “Private”.';
    f.value.value = oldName;
    f.value.placeholder = 'Folder name';
    const ok = await runDialog(dlg, () => SaveGroup(oldName, f.value.value), f.value);
    if (ok) await app.reload();
}

async function deleteFolder(sec) {
    const n = sec.hosts.reduce((k, h) => k + h.sessions.length, 0);
    const ok = await confirmDialog({
        title: `Delete folder “${sec.group}”?`,
        text: n ? `Its ${n} session(s) stay, without a folder.` : 'The folder is empty.',
        ok: 'Delete folder',
        action: () => DeleteGroup(sec.group),
    });
    if (ok) await app.reload();
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
