import './style.css';

import {isMac, TermView} from './terminal';
import {effectiveColorMode} from './appearance';
import {rank, score} from './fuzzy';
import {ACTIONS, actionFor, buildKeymap, format} from './keys';
import {FilesView} from './files';
import {NotesView} from './notes';
import {WebView} from './web';
import {$, el, hideMenu, showMenu, store, toast} from './ui';
import {EventsOn, WindowSetTitle} from '../wailsjs/runtime/runtime';
import {
    BackupNow, BackupStatus, OpenBackupFolder, RestoreBackup,
    BrowseDirectory, CreateSession, DeleteHost, DeleteSession, InstanceProblem, KillRemote, MoveSession,
    PreviewSession, SaveHost, SaveUI, StartLocal, StartRemote, State, UpdateSession, Version,
    SaveGroup, DeleteGroup, MoveGroup, SetSessionGroup, RepoCheck, TrustRepo,
    SyncPreview, SyncSend, SyncGet, SyncFastForward, ShareStatus, StopShare,
} from '../wailsjs/go/main/App';

// ---------------------------------------------------------------- keyboard

// keymap is rebuilt from the config on every reload (see keys.js).
let keymap = buildKeymap({}, isMac);

// appKey returns the app action for a keydown, or null if the key belongs
// to the terminal.
const appKey = e => actionFor(e, keymap.map);
// Tabs: their order and which are shown come from Settings (ui.tab_order,
// ui.hidden_tabs). Ctrl+Shift+N is the Nth visible tab (tab_1 … tab_9);
// tab_<name> actions can bind a key to one tab regardless of position.
const tabOrder = () => (app.state.ui.tab_order ?? ['local', 'files', 'remote', 'notes', 'web'])
    .filter(k => !(app.state.ui.hidden_tabs ?? []).includes(k));
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
// WebTab shows the repository's web links and build runs; it refreshes when
// shown and polls only while visible.
class WebTab {
    constructor(session) {
        this.session = session;
        this.key = 'web';
        this.label = 'Web';
        this.badge = null;
        this.pane = el('div', {className: 'pane files-pane', hidden: true});
        session.container.append(this.pane);
        this.view = null;
    }

    show() {
        this.pane.hidden = false;
        if (!this.view) this.view = new WebView(this.pane, this.session);
        else this.view.refresh();
        this.view.visible = true;
    }

    hide() {
        this.pane.hidden = true;
        if (this.view) this.view.visible = false;
    }

    dispose() {
        this.view?.dispose();
        this.pane.remove();
    }
}

// NotesTab hosts the session's notes editor; it reloads when shown.
class NotesTab {
    constructor(session) {
        this.session = session;
        this.key = 'notes';
        this.label = 'Notes';
        this.badge = null;
        this.pane = el('div', {className: 'pane files-pane', hidden: true});
        session.container.append(this.pane);
        this.view = null;
    }

    show() {
        this.pane.hidden = false;
        if (!this.view) this.view = new NotesView(this.pane, this.session);
        else this.view.refresh();
        this.view.focus();
    }

    hide() {
        this.pane.hidden = true;
        this.view?.save();
    }

    dispose() {
        this.view?.dispose();
        this.pane.remove();
    }
}

class FilesTab {
    constructor(session) {
        this.session = session;
        this.key = 'files';
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
            new Tab(this, 'Local', (c, r) => StartLocal(this.data.id, c, r),
                {what: 'shell', again: 'restart', remote: false}),
            new FilesTab(this),
            new Tab(this, 'Remote', (c, r) => StartRemote(this.data.id, c, r), {
                what: 'ssh', again: 'reconnect', remote: true,
                place: () => this.data.host,
                autoReconnect: () => app.state.ui.auto_reconnect,
                missingDir: () => this.data.files === 'sync'
                    ? {text: 'There is no working copy on the agent host yet.',
                        actions: [{label: 'Send to agent…', primary: true, action: () => syncSendDialog(this)}]}
                    : this.data.files === 'sshfs'
                        ? {text: `The live share is not mounted at ${this.data.remote_path}.`, actions: []}
                        : {text: `${this.data.remote_path} does not exist on ${this.data.host}. Check the share or the path mapping.`, actions: []},
            }, s => this.setRemoteState(s)),
            new NotesTab(this),
            new WebTab(this),
        ];
        const keys = ['local', 'files', 'remote', 'notes', 'web'];
        this.tabs.forEach((t, i) => t.key = keys[i]);
        // Each session reopens on the tab last used (Local for new ones).
        this.activeKey = store.get(`tab:${data.id}`, 'local');
        this.share = null; // live share status, for sshfs sessions
        EventsOn(`share:${data.id}`, st => {
            this.share = st;
            if (app.active === this) app.renderMain();
        });
        if (data.files === 'sshfs') ShareStatus(data.id).then(st => this.share ??= st);
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

    // visibleTabs returns the shown tabs in the order set in Settings.
    visibleTabs() {
        return tabOrder().map(k => this.tab(k)).filter(Boolean);
    }

    activeTabObj() {
        const vis = this.visibleTabs();
        return vis.find(t => t.key === this.activeKey) ?? vis[0];
    }

    show() {
        this.container.hidden = false;
        const active = this.activeTabObj();
        this.activeKey = active?.key;
        this.tabs.forEach(t => t === active ? t.show() : t.hide());
    }

    hide() {
        this.container.hidden = true;
    }

    tab(key) {
        return this.tabs.find(t => t.key === key);
    }

    selectTab(key) {
        const t = this.visibleTabs().find(t => t.key === key);
        if (!t) return;
        this.activeTabObj()?.hide();
        this.activeKey = key;
        store.set(`tab:${this.id}`, key);
        this.show();
    }

    connectRemote() {
        this.tab('remote').ensureView();
    }

    applyAppearance(ui) {
        const mode = effectiveColorMode(ui, this.data);
        this.tabs.forEach(t => t.view?.setAppearance?.(ui, mode));
    }

    disconnect() {
        this.tabs.forEach(t => t.view?.disconnect?.());
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

// applyFont makes the whole app follow the font settings: the terminals,
// Files tree and Notes use the font as set (--mono-*), the rest of the app
// its size and, when one is set, its family (--ui-*). Cached for index.html,
// like the theme, so the first paint has the right size.
function applyFont(ui) {
    const size = `${ui.font_size || 13}px`;
    const root = document.documentElement.style;
    root.setProperty('--mono-size', size);
    root.setProperty('--ui-size', size);
    for (const v of ['--mono-family', '--ui-family']) {
        if (ui.font_family) root.setProperty(v, ui.font_family);
        else root.removeProperty(v);
    }
    store.set('font', {size, family: ui.font_family || ''});
}

// initials abbreviates a session name for the narrow sidebar: the first
// letters of its first two words ("multi-session ssh" -> "MS"), else its
// first two characters.
function initials(name) {
    const words = name.split(/[\s._\/-]+/).filter(Boolean);
    const s = words.length > 1 ? words[0][0] + words[1][0] : (words[0] || name).slice(0, 2);
    return s.toUpperCase();
}

const app = {
    state: null,
    sessions: new Map(), // id -> SessionUI
    order: [],           // SessionUI in sidebar order
    active: null,
    collapsed: new Set(store.get('collapsed', [])),
    mru: store.get('mru', []), // session ids, most recent first

    async init() {
        // Another instance uses this config: say so and start nothing.
        const problem = await InstanceProblem();
        if (problem) {
            $('#sidebar').hidden = true;
            $('#bar').hidden = true;
            $('#panes').hidden = true;
            $('#empty').hidden = false;
            $('#empty-text').textContent = problem;
            $('#empty-action').hidden = true;
            return;
        }
        this.backups = await BackupStatus().catch(() => ({}));
        EventsOn('backup', (host, st) => {
            this.backups[host] = st;
            this.renderBackup(host);
        });
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
            const t = this.active?.activeTabObj();
            t?.setBadge?.(null);
        });
        $('#btn-new').onclick = e => {
            const r = e.currentTarget.getBoundingClientRect();
            newMenu(r.left, r.bottom + 2);
        };
        $('#btn-add-host').onclick = () => hostDialog();
        $('#btn-settings').onclick = () => settingsDialog();
        $('#btn-narrow').onclick = () => this.setNarrow(!this.narrow);
        this.setNarrow(store.get('narrow', false));
        this.initSidebarResize();
    },

    // initSidebarResize lets the sidebar be resized by dragging its right
    // edge (or with ←/→ on the focused handle); double-click resets it. The
    // width is kept in rem, so it scales with the font size like the rest.
    initSidebarResize() {
        const handle = $('#sidebar-resizer');
        const root = document.documentElement;
        const remPx = () => parseFloat(getComputedStyle(root).fontSize) || 13;
        const set = rem => rem ? root.style.setProperty('--sidebar-width', `${rem}rem`) : root.style.removeProperty('--sidebar-width');
        const clampRem = px => Math.max(12, Math.min(px, window.innerWidth * 0.6) / remPx());
        const width = () => $('#sidebar').getBoundingClientRect().width;
        const save = () => store.set('sidebarWidth', Math.round(width() / remPx() * 100) / 100);
        set(store.get('sidebarWidth', null));
        handle.onpointerdown = e => {
            if (e.button !== 0) return;
            e.preventDefault();
            handle.setPointerCapture(e.pointerId);
            document.body.classList.add('resizing');
            const left = $('#sidebar').getBoundingClientRect().left;
            handle.onpointermove = ev => set(clampRem(ev.clientX - left));
            handle.onpointerup = handle.onpointercancel = () => {
                handle.onpointermove = handle.onpointerup = handle.onpointercancel = null;
                document.body.classList.remove('resizing');
                save();
            };
        };
        handle.ondblclick = () => {
            set(null);
            store.set('sidebarWidth', null);
        };
        handle.onkeydown = e => {
            const d = {ArrowLeft: -1, ArrowRight: 1}[e.key];
            if (!d) return;
            e.preventDefault();
            set(clampRem(width() + d * remPx()));
            save();
        };
    },

    // setNarrow shrinks the sidebar to a rail of session markers (number or
    // initials, status, badges; the name in the tooltip) or widens it again.
    setNarrow(on) {
        this.narrow = on;
        document.body.classList.toggle('sidebar-narrow', on);
        store.set('narrow', on);
        this.narrowButton();
        this.active?.show(); // terminals refit to the new width
    },

    narrowButton() {
        const b = $('#btn-narrow');
        b.textContent = this.narrow ? '»' : '«';
        b.title = `${this.narrow ? 'Widen' : 'Narrow'} the sidebar (${keyHint('toggle_sidebar') || 'no key'})`;
    },

    // reload fetches state from the backend and re-renders, keeping
    // terminals of sessions that still exist.
    async reload() {
        this.state = await State();
        applyTheme(this.state.ui.theme);
        applyFont(this.state.ui);
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
        this.active?.show(); // the tab order or visibility may have changed
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
        problems.push(...(this.state.problems ?? []).map(p => `Config: ${p}`));
        problems.push(...keymap.errors);
        err.hidden = !problems.length;
        err.textContent = problems.join('\n\n');
        $('#btn-new').title = `New session (${keyHint('new_session')}), folder or host`;
        this.narrowButton();

        const list = $('#session-list');
        list.replaceChildren();
        this.order = [];
        this.hostHeads = [];
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
                    el('span', {className: 'label', textContent: g.name + (g.host ? '' : ' (missing)')}),
                    el('span', {className: 'backup-warn', textContent: '⚠', hidden: true}), more);
                if (g.host) this.hostHeads.push({name: g.name, target: g.host.ssh_target, head});
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
        for (const h of this.hostHeads) this.renderBackup(h.name);
        this.renderMain();
    },

    // renderBackup shows a host's Claude backup state in its header tooltip,
    // with a ⚠ when the last backup failed.
    renderBackup(host) {
        const st = this.backups?.[host];
        for (const h of this.hostHeads.filter(h => h.name === host)) {
            h.head.title = `${h.target}\n${backupText(st, this.state.ui.claude_backup)}`;
            h.head.querySelector('.backup-warn').hidden = !st?.error;
        }
    },

    sessionItem(s, n) {
        const d = s.data;
        const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Session actions'});
        const item = el('div', {className: 'session', draggable: true},
            el('span', {className: 'dot ' + s.dotClass()}),
            el('span', {className: 'abbr', textContent: initials(d.name)}),
            el('div', {className: 'text'},
                el('div', {className: 'name', textContent: d.name}),
                el('div', {className: 'sub', textContent: '‎' + d.local_path})),
            el('span', {className: 'repo-warn', textContent: '⚠', hidden: !s.repoWarn(),
                title: 'Repository hooks or settings to review (see the Files tab)'}),
            el('span', {className: 'badge'}),
            n <= 9 ? el('span', {className: 'num', textContent: n}) : '',
            more);
        item.title = `${d.name}\n${d.local_path}`;
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
        s.tab('files')?.view?.renderRepoBanner?.();
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
            $('#tabs').querySelectorAll('button').forEach(btn => {
                const t = s.tab(btn.dataset.key);
                btn.classList.toggle('bell', t?.badge === 'bell');
                btn.classList.toggle('activity', t?.badge === 'activity');
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
        const active = s.activeTabObj();
        nav.replaceChildren(...s.visibleTabs().map((t, i) => {
            const label = el('span', {className: 'label', textContent: t.label});
            label.dataset.label = t.label;
            // The shortcut is in the tooltip, not on the tab.
            const key = keyHint(`tab_${t.key}`) || keyHint(`tab_${i + 1}`);
            const b = el('button', {title: key ? `${t.label} (${key})` : t.label}, label);
            b.dataset.key = t.key;
            if (t === active) b.classList.add('active');
            b.onclick = () => this.selectTab(t.key);
            return b;
        }));
        this.renderBadges(s);
        const sync = $('#sync-actions');
        sync.replaceChildren();
        if (d.files === 'sshfs') {
            const st = s.share?.state ?? 'stopped';
            const label = {mounted: '● shared', starting: '◌ connecting share…', retrying: '◌ share reconnecting…',
                failed: '⚠ share failed', stopped: '○ not shared'}[st] ?? st;
            sync.append(el('span', {className: `share-state ${st}`, textContent: label,
                title: s.share?.error || (st === 'mounted' ? `${d.local_path} is mounted at ${d.host}:${d.remote_path}` :
                    'The share starts with the Remote tab')}));
        }
        if (d.files === 'sync') {
            const send = el('button', {textContent: '⇡ Send', title: 'Send this directory to the agent copy'});
            const get = el('button', {textContent: '⇣ Get', title: 'Get commits from the agent copy'});
            send.onclick = () => syncSendDialog(s);
            get.onclick = () => syncGetDialog(s);
            sync.append(send, get);
        }
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

    selectTab(key) {
        if (!this.active) return;
        this.active.selectTab(key);
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
        else if (/^tab_\d$/.test(a)) this.active && this.selectTab(this.active.visibleTabs()[+a.slice(4) - 1]?.key);
        else if (a.startsWith('tab_')) this.selectTab(a.slice(4));
        else if (a === 'next_session') this.step(1);
        else if (a === 'prev_session') this.step(-1);
        else if (a === 'new_session') sessionDialog();
        else if (a === 'quick_switcher') quickSwitcher();
        else if (a === 'settings') settingsDialog();
        else if (a === 'find') this.active?.activeTabObj()?.view?.openFind?.();
        else if (a === 'toggle_sidebar') this.setNarrow(!this.narrow);
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
    const sync = s.data.files === 'sync' ? [
        {label: 'Send to agent…', action: () => syncSendDialog(s)},
        {label: 'Get commits from agent…', action: () => syncGetDialog(s)},
        '-',
    ] : [];
    showMenu(x, y, [
        ...sync,
        ...(s.data.files === 'sshfs' ? [{label: 'Stop sharing', disabled: s.share?.state !== 'mounted',
            action: () => StopShare(s.id)}, '-'] : []),
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

// when formats a backup time: the time today, else date and time; '' for
// Go's zero time (never).
function when(t) {
    const d = new Date(t);
    if (!t || d.getFullYear() < 2000) return '';
    const today = new Date().toDateString() === d.toDateString();
    return today ? d.toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'}) : d.toLocaleString([], {dateStyle: 'medium', timeStyle: 'short'});
}

function backupText(st, on) {
    if (st?.busy) return 'Claude data: backing up…';
    if (st?.error) return `Claude backup failed${when(st.last) ? ` (${when(st.last)})` : ''}: ${st.error}` +
        (when(st.last_ok) ? `\nLast good backup: ${when(st.last_ok)}` : '');
    if (when(st?.last_ok)) return `Claude data backed up ${when(st.last_ok)}`;
    return on ? 'Claude data not backed up yet' : 'Claude backups are off (Settings)';
}

async function backupNow(name) {
    toast(`Backing up the Claude data of ${name}…`);
    try {
        await BackupNow(name);
        toast(`Claude data of ${name} backed up.`);
    } catch (err) {
        toast(`Backup of ${name} failed: ${err?.message ?? err}`, true);
    }
}

async function restoreBackup(h) {
    const st = app.backups?.[h.name];
    const ok = await confirmDialog({
        title: `Restore Claude data to “${h.name}”?`,
        text: `Copies the backup from ${when(st?.last_ok)} to ~/.claude and ~/.claude.json on ${h.ssh_target}. ` +
            'Files that already exist there are kept, so this is meant for a new or reinstalled agent host. ' +
            'claude\'s login is not in the backup: start claude there and log in once.',
        ok: 'Restore',
        action: () => RestoreBackup(h.name),
    });
    if (ok) toast(`Claude data restored to ${h.name}.`);
}

function hostMenu(g, x, y) {
    const st = app.backups?.[g.name];
    const backedUp = !!when(st?.last_ok);
    showMenu(x, y, [
        {label: 'New session on this host…', disabled: !g.host, action: () => sessionDialog(null, g.name, g.group)},
        {label: g.host ? 'Edit host…' : 'Configure host…', action: () => hostDialog(g.host, g.host ? null : g.name)},
        '-',
        {label: 'Back up Claude data now', disabled: !g.host || st?.busy, action: () => backupNow(g.name)},
        {label: 'Restore Claude data to this host…', disabled: !g.host || !backedUp, action: () => restoreBackup(g.host)},
        {label: 'Open Claude backup folder', disabled: !backedUp,
            action: () => OpenBackupFolder(g.name).catch(err => toast(String(err), true))},
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

async function confirmDialog({title, text, ok = 'OK', cancel = 'Cancel', check, action}) {
    const dlg = $('#dlg-confirm');
    dlg.querySelector('h2').textContent = title;
    dlg.querySelector('.text').textContent = text;
    const okBtn = dlg.querySelector('button[value=ok]');
    okBtn.hidden = !ok;
    okBtn.textContent = ok || '';
    dlg.querySelector('button[value=cancel]').textContent = ok ? cancel : 'Close';
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
    // New sessions look as set in Settings; an override is for Edit only.
    f.elements.colors.closest('label').hidden = !editing;
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
    const oldMode = s?.data.start_mode;
    const ok = await runDialog(dlg, async () => {
        if (editing) {
            await UpdateSession(s.id, {
                name: f.elements.name.value || s.data.name, start_mode: f.elements.mode.value,
                color_mode: f.elements.colors.value, group: f.elements.group.value,
            });
        } else {
            created = await CreateSession({
                host: f.elements.host.value, local_path: f.elements.dir.value, name: f.elements.name.value,
                start_mode: f.elements.mode.value, color_mode: '', group: f.elements.group.value,
            });
            store.set('lastHost', f.elements.host.value);
            store.set('lastMode', f.elements.mode.value);
        }
    }, editing ? f.elements.name : f.elements.dir);
    if (!ok) return;
    await app.reload();
    if (created) app.select(app.sessions.get(created.id));
    if (editing && s.data.start_mode !== oldMode) await offerRestart(s, oldMode);
}

const MODE_LABELS = {claude: 'claude', 'claude-continue': 'claude --continue', shell: 'a shell'};

// offerRestart: a new start mode only applies when the remote tmux session is
// created; one that is already running keeps what it started with. Offer to
// end it, and reconnect an open Remote tab so it starts in the new mode.
async function offerRestart(s, oldMode) {
    await confirmDialog({
        title: 'Restart the remote session?',
        text: `New sessions on the agent host now start with ${MODE_LABELS[s.data.start_mode]}. ` +
            `If tmux session ${s.data.tmux_name} on ${s.data.host} is already running, it keeps ` +
            `running what it started with (${MODE_LABELS[oldMode]}) until it ends. Restarting ends it now, ` +
            'including claude or anything else running in it.',
        ok: 'Restart now',
        cancel: 'Later',
        action: async () => {
            await KillRemote(s.id);
            const v = s.tab('remote').view;
            if (!v) return; // not open in this app run: starts in the new mode when shown
            for (let i = 0; i < 50 && !v.exited; i++) await new Promise(r => setTimeout(r, 100));
            if (v.exited) v.restart();
        },
    });
}

async function hostDialog(h = null, presetName = null) {
    const dlg = $('#dlg-host');
    const f = dlg.querySelector('form');
    dlg.querySelector('h2').textContent = h ? `Edit host “${h.name}”` : 'Add agent host';
    for (const k of ['name', 'ssh_target', 'local_prefix', 'remote_prefix', 'remote_workdir']) f.elements[k].value = h?.[k] ?? '';
    f.elements.files.value = h?.files || 'shared';
    const showMode = () => {
        const shared = f.elements.files.value === 'shared';
        dlg.querySelector('.files-shared').hidden = !shared;
        dlg.querySelector('.files-workdir').hidden = shared;
    };
    f.elements.files.onchange = showMode;
    showMode();
    if (presetName) f.elements.name.value = presetName;
    f.elements.browse.onclick = async () => {
        const dir = await BrowseDirectory(f.elements.local_prefix.value || '~');
        if (dir) f.elements.local_prefix.value = dir;
    };
    const ok = await runDialog(dlg, () => SaveHost(h?.name ?? '', {
        name: f.elements.name.value, ssh_target: f.elements.ssh_target.value, files: f.elements.files.value,
        ...(f.elements.files.value === 'shared'
            ? {local_prefix: f.elements.local_prefix.value, remote_prefix: f.elements.remote_prefix.value, remote_workdir: ''}
            : {local_prefix: '', remote_prefix: '', remote_workdir: f.elements.remote_workdir.value}),
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
    next_session: 'Next session', prev_session: 'Previous session', new_session: 'New session',
    quick_switcher: 'Quick switcher', settings: 'Settings', find: 'Find in terminal',
    toggle_sidebar: 'Narrow / wide sidebar',
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
    f.claude_backup.checked = ui.claude_backup !== false;
    const every = f.claude_backup_every;
    if (![...every.options].some(o => o.value === ui.claude_backup_every)) {
        every.append(el('option', {value: ui.claude_backup_every, textContent: ui.claude_backup_every}));
    }
    every.value = ui.claude_backup_every;
    // Tabs: order with ↑/↓, shown or not with the checkbox.
    const TAB_LABELS = {local: 'Local', files: 'Files', remote: 'Remote', notes: 'Notes', web: 'Web'};
    let order = [...(ui.tab_order ?? Object.keys(TAB_LABELS))];
    const hidden = new Set(ui.hidden_tabs ?? []);
    const tabList = dlg.querySelector('.tab-list');
    const renderTabs = () => tabList.replaceChildren(...order.map((k, i) => {
        const box = el('input', {type: 'checkbox', checked: !hidden.has(k), title: 'Show this tab'});
        box.onchange = () => box.checked ? hidden.delete(k) : hidden.add(k);
        const mv = (d, label) => {
            const b = el('button', {type: 'button', className: 'icon', textContent: label, disabled: !order[i + d]});
            b.onclick = () => {
                [order[i], order[i + d]] = [order[i + d], order[i]];
                renderTabs();
                tabList.querySelectorAll('.tab-row')[i + d]?.querySelector(`button[data-d="${d}"]`)?.focus();
            };
            b.dataset.d = d;
            return b;
        };
        return el('div', {className: 'tab-row'}, el('label', {className: 'check'}, box, ' ' + TAB_LABELS[k]),
            mv(-1, '↑'), mv(1, '↓'));
    }));
    renderTabs();
    const rows = Object.keys(ACTIONS).filter(a => !a.startsWith('session_') && !/^tab_/.test(a)).map(a =>
        el('tr', {}, el('td', {textContent: ACTION_LABELS[a] ?? a}), el('td', {}, el('kbd', {textContent: keyHint(a) || '—'})),
            el('td', {className: 'action', textContent: a})));
    rows.push(el('tr', {}, el('td', {textContent: 'Tab 1…9 (by position)'}), el('td', {}, el('kbd', {textContent: keyHint('tab_1').replace(/1$/, '1…9')})),
        el('td', {className: 'action', textContent: 'tab_1 … tab_9; tab_local, tab_remote … per tab'})));
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
        claude_backup: f.claude_backup.checked,
        claude_backup_every: f.claude_backup_every.value,
        tab_order: order,
        hidden_tabs: order.filter(k => hidden.has(k)),
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

// ---------------------------------------------------------------- working copies (sync)

// syncSendDialog previews "Send to agent" and sends after confirmation.
async function syncSendDialog(s) {
    const dlg = $('#dlg-sync');
    if (dlg.open) return;
    const body = dlg.querySelector('.body');
    const okBtn = dlg.querySelector('button[value=ok]');
    dlg.querySelector('h2').textContent = `Send to agent: ${s.data.name}`;
    okBtn.textContent = 'Send';
    okBtn.hidden = false;
    okBtn.disabled = true;
    body.replaceChildren(el('p', {textContent: `Comparing with ${s.data.host}:${s.data.remote_path} …`}));
    let overwrite = null;
    const done = runDialog(dlg, async () => {
        const p = await SyncSend(s.id, !!overwrite?.checked);
        if (p.blocked) throw new Error(p.blocked);
        toast(`Sent to ${s.data.host}: ${p.new} new, ${p.changed} changed, ${p.deleted} deleted`);
    });
    let p;
    try {
        p = await SyncPreview(s.id);
    } catch (err) {
        body.replaceChildren(el('p', {className: 'bad', textContent: err?.message ?? String(err)}));
        return done;
    }
    const parts = [];
    if (p.blocked) {
        const get = el('button', {type: 'button', textContent: 'Get commits…'});
        get.onclick = () => {
            dlg.close();
            syncGetDialog(s);
        };
        parts.push(el('p', {className: 'bad', textContent: `Not now: ${p.blocked}.`}), get);
    }
    if (p.note) parts.push(el('p', {className: 'bad', textContent: `Note: ${p.note}.`}));
    const total = p.new + p.changed + p.deleted;
    parts.push(el('p', {textContent: !p.agent.exists ? `First copy: ${p.new} file(s) will be sent.` :
        total ? `On the agent host: ${p.new} new, ${p.changed} changed, ${p.deleted} deleted.` :
            'The agent copy is already up to date.'}));
    parts.push(el('p', {className: 'hint', textContent:
        'Your copy here is the source: the agent copy becomes identical to it (including .git). ' +
        'Files ignored by .gitignore are not sent and not deleted there.'}));
    if (p.agent.dirty && !p.blocked) {
        overwrite = el('input', {type: 'checkbox'});
        overwrite.onchange = () => okBtn.disabled = !overwrite.checked;
        parts.push(el('label', {className: 'check bad'}, overwrite,
            ` Overwrite uncommitted changes in ${p.agent.dirty} file(s) on the agent host`));
    }
    if (p.changes.length) {
        // git's own files are summarized: the working files are what matter here.
        const sign = {new: '+', changed: '~', deleted: '−'};
        const isGit = c => c.path === '.git' || c.path.startsWith('.git/');
        const git = p.changes.filter(isGit).length;
        const lines = p.changes.filter(c => !isGit(c)).map(c => `${sign[c.op]} ${c.path}`);
        if (git) lines.push(`  .git/ (${git} file${git > 1 ? 's' : ''} of git's own data)`);
        parts.push(el('pre', {className: 'changes', textContent: lines.join('\n') + (p.truncated ? '\n…' : '')}));
    }
    body.replaceChildren(...parts);
    okBtn.disabled = !!p.blocked || (!total && p.agent.exists) || !!(p.agent.dirty && !overwrite?.checked);
    (okBtn.disabled ? overwrite : okBtn)?.focus();
    const ok = await done;
    if (ok) s.tab('remote').view?.exited && s.tab('remote').view.restart();
    return ok;
}

// syncGetDialog fetches the agent copy's commits and offers a fast-forward.
async function syncGetDialog(s) {
    const dlg = $('#dlg-sync');
    if (dlg.open) return;
    const body = dlg.querySelector('.body');
    const okBtn = dlg.querySelector('button[value=ok]');
    dlg.querySelector('h2').textContent = `Get commits: ${s.data.name}`;
    okBtn.hidden = true;
    body.replaceChildren(el('p', {textContent: `Fetching from ${s.data.host}:${s.data.remote_path} …`}));
    let f = null;
    const done = runDialog(dlg, async () => {
        await SyncFastForward(s.id, f.agent_ref);
        toast(`${f.branch}: fast-forwarded by ${f.new.length} commit(s) from the agent`);
    });
    try {
        f = await SyncGet(s.id);
    } catch (err) {
        body.replaceChildren(el('p', {className: 'bad', textContent: err?.message ?? String(err)}));
        return done;
    }
    const parts = [el('p', {className: 'hint', textContent:
        `Fetched ${f.branches?.length ?? 0} branch(es) as ${f.branches?.[0]?.replace(/\/[^/]*$/, '') ?? 'agent-…'}/… ` +
        '(commits only: hooks and config of the agent copy are never copied here).'})];
    if (!f.branch) parts.push(el('p', {textContent: 'This repository is on a detached HEAD; nothing to compare.'}));
    else if (!f.agent_ref) parts.push(el('p', {textContent: `The agent copy has no branch “${f.branch}”.`}));
    else if (!f.new?.length) parts.push(el('p', {textContent: `No new commits on ${f.branch} from the agent.`}));
    else {
        parts.push(el('p', {textContent: `${f.new.length} new commit(s) from the agent on ${f.branch}:`}),
            el('pre', {className: 'changes', textContent: f.new.map(c => `${c.hash} ${c.subject}`).join('\n')}));
        if (f.can_ff) {
            okBtn.textContent = `Fast-forward ${f.branch}`;
            okBtn.hidden = false;
            okBtn.focus();
        } else {
            parts.push(el('p', {textContent: `Your ${f.branch} also has ${f.host_ahead} commit(s) the agent does not. ` +
                'Merge or rebase in the Local tab, for example:'}),
                el('pre', {className: 'changes', textContent: `git merge ${f.agent_ref}`}));
        }
    }
    if (f.agent_dirty) parts.push(el('p', {className: 'bad', textContent:
        `${f.agent_dirty} file(s) on the agent host have uncommitted changes; they are not included. ` +
        'Commit them there first if you want them.'}));
    body.replaceChildren(...parts);
    const ok = await done;
    if (ok) s.tab('files')?.view?.refresh();
    return ok;
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
        text: 'Removes it from the list. Its files are not touched, and its notes are kept.',
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
