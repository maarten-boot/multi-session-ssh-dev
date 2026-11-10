import './style.css';

import {isMac, TermView} from './terminal';
import {effectiveColorMode} from './appearance';
import {rank, score} from './fuzzy';
import {ACTIONS, actionFor, buildKeymap, format} from './keys';
import {FilesView} from './files';
import {NotesView} from './notes';
import {configureSpelling} from './editor';
import {AppNotesPage, PROMPTS, sessionPrompts} from './appnotes';
import {promptMenu} from './prompts';
import {WebView} from './web';
import {QAView} from './qa';
import {CommandList} from './commands';
import {commandItems, homeItem} from './cmdmenu';
import {$, balloon, el, hideBalloon, hideMenu, movable, showMenu, store, textMenu, toast} from './ui';
import {EventsOff, EventsOn, WindowSetTitle} from '../wailsjs/runtime/runtime';
import {TOOLCHAINS, addToolchain} from './dockerfile';
import {SERVICES, addService, hasService} from './services';
import {fmtSize} from './format';
import {idleLabel, idleOptions} from './idle';
import {costParts, costRows, dollars, sessionCost, usageText, warnings} from './cost';
import {
    BackupNow, BackupStatus, OpenBackupFolder, RestoreBackup, Notify, ClearNotification,
    BrowseDirectory, CreateSession, DeleteHost, DeleteSession, InstanceProblem, KillRemote, MoveSession,
    PreviewSession, SaveHost, SaveUI, StartLocal, StartRemote, State, UpdateSession, Version,
    SaveGroup, DeleteGroup, MoveGroup, SetSessionGroup, RepoCheck, TrustRepo,
    SyncPreview, SyncSend, SyncGet, SyncTake, ShareStatus, StopShare, RecordInfo, OpenLogFolder, SpellLanguages,
    SyncInfo, SyncSave, SyncTakeFiles, SyncSendAll, OpenSaved, SavedCopy, RemoveSaved,
    AutosaveStatus, QuitSaving, QuitNow, NotesTodos, QAModels, QAFindClaude,
    ContainerDockerfile, SaveContainerDockerfile, ContainerRebuild, ContainerStopBuild, ContainerStop, ContainerRemove,
    ContainerCompose, SaveContainerCompose, ContainerComposeApply, SetSessionActive,
    SwitchTargets, SwitchHost, Requirements, SaveRequirements, CheckRequirements, IdleStopped, SessionShown, StopClaude, ResumeClaude, CostSummary, ResetCost, CostNow,
    ContainerEngineInfo, ExportParts, ExportSession, ImportOpen, ImportSession, Profile,
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
const tabOrder = () => (app.state.ui.tab_order ?? ['local', 'files', 'remote', 'notes', 'web', 'qa'])
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
    // commandItems is this tab's command menu (see commands.js); disabled
    // while the terminal is not running.
    // The Local tab's menu starts with "Back to the session directory"
    // (it always runs a shell; the Remote tab may run claude).
    commandItems() {
        const send = d => {
            this.view?.send(d);
            this.view?.term.focus();
        };
        const disabled = this.view?.id ? null : 'not connected';
        const otherKey = this.key === 'local' ? 'remote' : 'local';
        const hasOther = this.session.visibleTabs().some(t => t.key === otherKey);
        const items = commandItems(this.session, app.state.ui.commands, send, disabled, {
            tab: this.key,
            sendOther: hasOther && ((d, c) => {
                this.view?.term.focus(); // keys keep going to the tab on screen
                return this.session.tab(otherKey)?.typeInBackground(d)
                    .then(() => toast(`“${c.name}” typed into the ${otherKey === 'local' ? 'Local' : 'Remote'} tab`))
                    .catch(err => toast(`“${c.name}”: ${err?.message ?? err}`, true))
                    .finally(() => {
                        if (app.active === this.session && !this.pane.hidden) this.view?.term.focus();
                    });
            }),
        });
        const home = this.key === 'local'
            ? homeItem(this.session.data.local_path, send, disabled, navigator.platform.startsWith('Win')) : null;
        return home ? [home, ...(items.length ? ['-', ...items] : [])] : items;
    }

    // typeInBackground types text into this tab without showing it: the
    // terminal is started first when it is not running (it stays hidden;
    // its activity badge shows what it prints). Waits up to 20 s for it.
    async typeInBackground(text) {
        const v = this.ensureView();
        if (!v) throw new Error('the session is inactive');
        if (v.exited) v.spawn(); // ended earlier: start it again
        for (let i = 0; i < 200 && !v.id; i++) {
            if (v.exited && i > 0) break;
            await new Promise(r => setTimeout(r, 100));
        }
        if (!v.id) throw new Error(`the ${this.label} tab is not connected`);
        v.send(text);
    }

    // promptItems is this tab's Prompts menu: the session's own prompts,
    // then those for all sessions, pasted here without Enter.
    // section: for the right-click menu (see promptMenu).
    async promptItems(section = false) {
        const own = await sessionPrompts(this.session.id, this.session.tab('notes')?.view);
        return promptMenu(own, app.appNotes?.prompts ?? [], text => app.pasteInto(this.key, text), section);
    }

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

    // ensureView starts the terminal (null for an inactive session: it
    // connects nothing).
    ensureView() {
        if (this.session.data.inactive) return null;
        if (!this.view) {
            this.view = new TermView(this.pane, {
                start: this.start, exitHint: this.exitHint, ui: app.state.ui,
                colorMode: effectiveColorMode(app.state.ui, this.session.data),
                isAppKey: e => appKey(e) !== null, onState: this.onState,
                onActivity: (kind, n) => this.activity(kind, n),
                commands: () => this.commandItems(),
                prompts: () => this.promptItems(true),
            });
            this.view.spawn();
        }
        return this.view;
    }

    show() {
        this.pane.hidden = false;
        if (this.session.data.inactive) return this.showInactive();
        this.inactiveNote?.remove();
        this.inactiveNote = null;
        this.ensureView().focus();
        this.setBadge(null);
        app.clearWaiting(this.session);
    }

    // showInactive puts a notice with an Activate button where the
    // terminal would be. Deactivating the session on screen redraws it
    // before dropTerminals runs: the terminal goes first, so the notice is
    // not put below it (out of the window).
    showInactive() {
        if (this.view) this.dropView();
        if (!this.inactiveNote) {
            const btn = el('button', {type: 'button', className: 'primary', textContent: 'Activate session'});
            btn.onclick = () => setSessionActive(this.session, true);
            this.inactiveNote = el('div', {className: 'inactive-note'},
                el('p', {textContent: `“${this.session.data.name}” is inactive: kept for later, so its ${this.label} tab ` +
                    'connects nothing — not when the app starts and not when you open it. Notes, Files, Web and Q&A work as usual.'}),
                btn);
            this.pane.append(this.inactiveNote);
        }
        // No scrolling: a notice placed out of view must not shift the window.
        this.inactiveNote.querySelector('button').focus({preventScroll: true});
    }

    // dropView ends and removes the terminal (deactivating the session).
    dropView() {
        this.view?.dispose();
        this.view = null;
        this.pane.replaceChildren();
        this.inactiveNote = null;
        this.setBadge(null);
    }

    // visible: on screen and looked at (the notes page covers the terminals).
    visible() {
        return !this.pane.hidden && app.active === this.session && document.hasFocus() && !app.appNotes?.visible;
    }

    activity(kind, n) {
        const now = Date.now();
        if (kind === 'bell' && !this.visible()) app.notifyWaiting(this.session, this);
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

// QATab asks Claude questions through the Claude API (see qa.js).
class QATab {
    constructor(session) {
        this.session = session;
        this.key = 'qa';
        this.label = 'Q&A';
        this.badge = null;
        this.pane = el('div', {className: 'pane files-pane', hidden: true});
        session.container.append(this.pane);
        this.view = null;
    }

    show() {
        this.pane.hidden = false;
        if (!this.view) this.view = new QAView(this.pane, this.session, pageDialogs);
        else this.view.refresh();
        this.view.focus();
    }

    hide() {
        this.pane.hidden = true;
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
        if (!this.view) {
            this.view = new NotesView(this.pane, this.session.id, {dialogs: pageDialogs,
                onTodos: todos => app.setTodos(this.session, todos)});
        }
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

// FilesTab hosts the session's file tree; it refreshes whenever shown.
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
                // A misconfigured session fails the same way every time.
                autoReconnect: () => app.state.ui.auto_reconnect && !this.data.error,
                problem: () => this.data.error,
                missingDir: () => this.data.files === 'sync'
                    ? {text: 'There is no working copy on the agent host yet.',
                        actions: [{label: 'Send to agent…', primary: true, action: () => syncSendDialog(this)}]}
                    : this.data.files === 'sshfs'
                        ? {text: `The live share is not mounted at ${this.data.remote_path}.`, actions: []}
                        : {text: `${this.data.remote_path} does not exist on ${this.data.host}. Check the share or the path mapping.`, actions: []},
            }, s => this.setRemoteState(s)),
            new NotesTab(this),
            new WebTab(this),
            new QATab(this),
        ];
        const keys = ['local', 'files', 'remote', 'notes', 'web', 'qa'];
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

    // todoCount: the TODO lines in the session's notes; todoText says where.
    todoCount() {
        return Object.values(this.todos ?? {}).reduce((a, b) => a + b, 0);
    }

    todoText() {
        const n = this.todoCount();
        return n ? `${n} TODO ${n === 1 ? 'line' : 'lines'} in the notes: ${Object.keys(this.todos).sort().join(', ')}` : '';
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

    // visibleTabs returns the shown tabs in the order set in Settings; a
    // plain login has no Local and Files tabs (no local directory).
    visibleTabs() {
        const none = this.data.plain ? ['local', 'files'] : [];
        return tabOrder().filter(k => !none.includes(k)).map(k => this.tab(k)).filter(Boolean);
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

    // dropTerminals ends and removes the Local and Remote terminals (the
    // session was deactivated); show() puts the inactive notice there.
    dropTerminals() {
        this.tabs.forEach(t => t.dropView?.());
        this.setRemoteState('idle');
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
        // A separate setup (-profile): its name follows the app's in the title.
        const prof = await Profile().catch(() => '');
        this.appName = prof ? `Session Workbench — ${prof}` : 'Session Workbench';
        if (prof) store.prefix = `sw:${prof}:`;
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
        // Quitting: notes typed in the last moments are saved first (the
        // backend waits up to 3 s, see beforeClose).
        EventsOn('app:closing', async () => {
            const views = [...this.sessions.values()].map(s => s.tab('notes')?.view);
            await Promise.allSettled([...views, this.appNotes?.view].filter(Boolean).map(v => v.save()));
            QuitSaving(); // quits, after saving the agent copies when that is on
        });
        EventsOn('autosave:quit', p => showQuitSaving(p, false));
        EventsOn('autosave:quit-done', p => showQuitSaving(p, true));
        $('#quit-saving .quit-now').onclick = () => QuitNow();
        // Agent copy saves of working-copy sessions (session id → status).
        this.autosave = await AutosaveStatus().catch(() => ({}));
        EventsOn('autosave', (id, st) => {
            this.autosave[id] = st;
            if (this.active?.id === id) this.renderMain();
        });
        // Remote tabs being recorded (session id → true), kept up to date by
        // the backend's "record" events.
        this.recording = (await RecordInfo().catch(() => null))?.recording ?? {};
        EventsOn('record', (id, on, err) => {
            this.recording[id] = on;
            if (err) toast(`Recording ${this.sessions.get(id)?.data.name ?? id}: ${err}`, true);
            if (this.active?.id === id) this.renderMain();
        });
        EventsOn('backup', (host, st) => {
            this.backups[host] = st;
            this.renderBackup(host);
        });
        // Sessions whose claude was stopped for being idle (SPEC F17).
        this.idleStopped = new Set(await IdleStopped().catch(() => []));
        EventsOn('idle', ids => {
            this.idleStopped = new Set(ids ?? []);
            this.render();
        });
        EventsOn('idle:resumed', id => {
            const s = this.sessions.get(id);
            if (s) toast(`claude resumes in “${s.data.name}”`);
        });
        // What claude cost (SPEC F18): updated every minute.
        this.cost = await CostSummary().catch(() => null);
        EventsOn('cost', c => {
            this.cost = c;
            this.renderCost();
        });
        $('#cost').addEventListener('click', () => settingsDialog('cost'));
        await this.reload();
        const last = this.sessions.get(store.get('active', ''));
        if (this.state.ui.connect_on_start === 'eager') this.order.forEach(s => s.connectRemote());
        this.checkAll();
        setInterval(() => this.checkAll(), 120000);
        this.checkTodos();
        this.select(last || this.order[0] || null);

        // An action that fails unexpectedly (a menu item, a button) says so
        // instead of doing nothing.
        window.addEventListener('unhandledrejection', e => toast(`Error: ${e.reason?.message ?? e.reason}`, true));
        window.addEventListener('error', e => toast(`Error: ${e.message}`, true));
        window.addEventListener('keydown', e => this.onKey(e));
        window.addEventListener('mousedown', e => {
            if (!$('#menu').contains(e.target)) hideMenu();
        });
        window.addEventListener('blur', hideMenu);
        window.addEventListener('focus', () => {
            if (this.active) this.checkRepo(this.active);
            this.checkTodos(); // the notes may have been edited outside the app
            if (this.appNotes?.visible) return; // the page covers the session; closing it clears
            const t = this.active?.activeTabObj();
            t?.setBadge?.(null);
            if (this.active) this.clearWaiting(this.active);
        });
        // A click on a desktop notification opens the session's tab that
        // rang (Remote when that is not known).
        EventsOn('notification:open', id => {
            if (id === 'cost') return settingsDialog('cost'); // a cost or usage warning
            const s = this.sessions.get(id);
            if (!s) return;
            const tab = s.notifiedTab || 'remote';
            this.select(s);
            this.selectTab(tab);
        });
        $('#btn-new').onclick = e => {
            const r = e.currentTarget.getBoundingClientRect();
            newMenu(r.left, r.bottom + 2);
        };
        $('#session-list').addEventListener('scroll', hideBalloon); // also with the scrollbar
        // Right-click the empty space below or between the hosts and folders
        // (sessions and headers have their own menus).
        $('#session-list').oncontextmenu = e => {
            if (e.target.closest('.session, .host-head, .folder-head')) return;
            e.preventDefault();
            listMenu(e.clientX, e.clientY);
        };
        $('#btn-add-host').onclick = () => hostDialog();
        $('#btn-settings').onclick = () => settingsDialog();
        $('#btn-narrow').onclick = () => this.setNarrow(!this.narrow);
        this.setNarrow(store.get('narrow', false));
        this.initSidebarResize();
        this.appNotes = new AppNotesPage($('#main'), {
            paste: text => this.pasteToRemote(text),
            label: () => this.active?.data.name ?? null,
            canPaste: () => this.pasteProblem(),
            keyHint: keyHint('app_notes') || 'no key',
            dialogs: pageDialogs,
        });
        this.appNotes.onHide = () => this.active?.show();
        $('#btn-notes').onclick = () => this.appNotes.toggle();
        $('#btn-notes').title = `Notes and prompts for all sessions (${keyHint('app_notes') || 'no key'})`;
    },

    // pasteProblem says why a prompt cannot be pasted into the current
    // session's Remote tab, or returns null when it can.
    pasteProblem() {
        const s = this.active;
        if (!s) return 'No session';
        if ((this.state.ui.hidden_tabs ?? []).includes('remote')) return 'The Remote tab is hidden (Settings → Appearance)';
        if (s.data.error) return `The session has a problem: ${s.data.error}`;
        if (s.data.inactive) return 'The session is inactive (session menu → Activate session)';
        return null;
    },

    // leaveNotesPage closes the notes page without its onHide (the caller
    // shows what comes next), saving the notes first.
    leaveNotesPage() {
        if (!this.appNotes?.visible) return;
        this.appNotes.el.hidden = true;
        this.appNotes.view.save();
    },

    // pasteToRemote puts text into the current session's Remote tab as a
    // paste (claude sees pasted text; nothing runs until Enter).
    async pasteToRemote(text) {
        if (!this.pasteProblem()) await this.pasteInto('remote', text);
    },

    // pasteInto pastes text into the current session's Local or Remote tab.
    // It waits until the tab is connected and the program in it has turned
    // on bracketed paste (claude and modern shells do): without it every
    // line would be submitted on its own. A single line can still go without
    // it, since it has no newline.
    async pasteInto(key, text) {
        const s = this.active;
        const tab = s?.tab(key);
        if (!tab) return;
        this.selectTab(key);
        const v = tab.ensureView();
        if (!v) return toast('The session is inactive; the prompt was not pasted.', true);
        if (v.exited && !v.reconnectTimer) v.restart();
        const wait = async (ok, ms) => {
            for (let t = 0; t < ms && !ok(); t += 100) await new Promise(r => setTimeout(r, 100));
            return ok();
        };
        if (!await wait(() => v.id, 15000)) return toast(`The ${tab.label} tab is not connected; the prompt was not pasted.`, true);
        if (!await wait(() => !v.id || v.term.modes.bracketedPasteMode, 20000) || !v.id) {
            const line = text.replace(/\r?\n+$/, '');
            if (!v.id || /[\r\n]/.test(line)) {
                return toast(`The ${tab.label} tab does not accept a safe paste yet; try again when claude is ready.`, true);
            }
            v.term.paste(line);
        } else {
            v.term.paste(text);
        }
        v.focus();
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
        // The saved width is in rem: a bigger font or a smaller window can
        // push it past the limit, so it is clamped on load and on resize.
        const fitSaved = () => {
            const rem = store.get('sidebarWidth', null);
            set(rem && clampRem(rem * remPx()));
        };
        fitSaved();
        this.fitSidebar = fitSaved; // after a font change (reload)
        window.addEventListener('resize', fitSaved);
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
                // Terminals were not refitted during the drag (terminal.js).
                this.active?.tabs.forEach(t => t.view?.term && !t.pane.hidden && t.view.fit.fit());
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
        configureSpelling(this.state.ui);
        applyTheme(this.state.ui.theme);
        applyFont(this.state.ui);
        this.fitSidebar?.();
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

    // folderTree returns the sidebar structure: the top (folder '') with
    // its sessions, then its folders, each again with sessions and then
    // subfolders. Folders nest by path ("Work/Client A" is in Work);
    // siblings keep the order of groups. Sessions are split by host in host
    // order; hosts appear where they have sessions, and a host without any
    // session is listed at the top so its menu stays reachable. Unknown
    // hosts or folders referenced by sessions (or subfolders) are shown as
    // "(missing)".
    folderTree() {
        const hostNames = this.state.hosts.map(h => h.name);
        for (const d of this.state.sessions) if (!hostNames.includes(d.host)) hostNames.push(d.host);
        const folders = new Map();
        const node = group => {
            if (folders.has(group)) return folders.get(group);
            const parent = group === '' ? null : node(group.includes('/') ? group.slice(0, group.lastIndexOf('/')) : '');
            const f = {
                group, name: group.slice(group.lastIndexOf('/') + 1), parent, children: [],
                known: group === '' || this.state.groups.includes(group),
                hosts: hostNames.map(name => ({
                    name, group,
                    host: this.state.hosts.find(h => h.name === name) ?? null,
                    sessions: this.state.sessions.filter(d => d.host === name && d.group === group).map(d => this.sessions.get(d.id)),
                })).filter(h => h.sessions.length ||
                    // A host without any session still needs a header (and menu).
                    (group === '' && h.host && !this.state.sessions.some(d => d.host === h.name))),
            };
            folders.set(group, f);
            parent?.children.push(f);
            return f;
        };
        const root = node('');
        this.state.groups.forEach(node);
        this.state.sessions.forEach(d => node(d.group));
        const count = f => f.count = f.hosts.reduce((n, h) => n + h.sessions.length, 0) + f.children.reduce((n, c) => n + count(c), 0);
        count(root);
        return root;
    },

    // collapsible remembers whether el is collapsed; one that starts
    // collapsed (closed) is remembered when opened ("+key").
    collapsible(el, key, closed = false) {
        const k = closed ? '+' + key : key;
        if (this.collapsed.has(k) !== closed) el.classList.add('collapsed');
        return () => {
            el.classList.toggle('collapsed');
            el.classList.contains('collapsed') !== closed ? this.collapsed.add(k) : this.collapsed.delete(k);
            store.set('collapsed', [...this.collapsed]);
        };
    },

    render() {
        this.renderCost();
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
        hideBalloon(); // its session item goes now
        list.replaceChildren();
        this.order = [];
        this.hostHeads = [];
        for (const s of this.sessions.values()) s.items = [];
        this.renderTop(this.folderTree(), list);
        if (this.active && !this.sessions.has(this.active.id)) this.active = null;
        for (const h of this.hostHeads) this.renderBackup(h.name);
        this.renderMain();
    },

    // heading adds a collapsible top heading (Hosts, Groups, Active,
    // Inactive) to parent and returns its body. Right-click: the list menu.
    heading(parent, key, label, count, title, closed = false) {
        const folder = el('div', {className: `folder top-folder ${key}-folder`, style: '--depth: 0'});
        const head = el('div', {className: 'folder-head', title},
            el('span', {className: 'caret', textContent: '▾'}),
            el('span', {className: 'label', textContent: label}),
            el('span', {className: 'count', textContent: count}));
        head.onclick = this.collapsible(folder, key, closed);
        head.oncontextmenu = e => {
            e.preventDefault();
            listMenu(e.clientX, e.clientY);
        };
        const body = el('div', {className: 'folder-body'});
        folder.append(head, body);
        parent.append(folder);
        return {head, body};
    },

    // renderTop fills the list (SPEC F5): the sessions in no folder by host
    // (under the heading Hosts when there are folders; dropping a session
    // there takes it out of its folder), the folders under Groups, then
    // every active and every inactive session by name (Active, Inactive:
    // collapsed at first, no numbers; their balloon adds host and folder).
    // Empty headings are left out.
    renderTop(root, list) {
        const top = {...root, children: []};
        if (!root.children.length) {
            this.renderFolder(top, list, 0); // no folders: the hosts without a heading, as before
        } else {
            if (root.hosts.length) {
                const n = root.hosts.reduce((a, g) => a + g.sessions.length, 0);
                const {head, body} = this.heading(list, 'hosts', 'Hosts', n, 'Sessions that are in no folder, by host');
                enableFolderDrop(head, '');
                body.style.setProperty('--indent', '14px');
                this.renderFolder(top, body, 1);
            }
            const n = root.count - root.hosts.reduce((a, g) => a + g.sessions.length, 0);
            const {body} = this.heading(list, 'groups', 'Groups', n, 'Your folders');
            this.renderFolder({...root, hosts: []}, body, 1);
        }
        const byName = (a, b) => a.data.name.localeCompare(b.data.name, undefined, {sensitivity: 'base', numeric: true});
        for (const [key, label, inactive, title] of [
            ['active', 'Active', false, 'Every active session, by name (also listed above)'],
            ['inactive', 'Inactive', true, 'Every inactive session, by name (also listed above)'],
        ]) {
            const list2 = this.state.sessions.filter(d => !!d.inactive === inactive)
                .map(d => this.sessions.get(d.id)).filter(Boolean).sort(byName);
            if (!list2.length) continue;
            const {body} = this.heading(list, key, label, list2.length, title, true);
            const items = el('div', {className: 'sessions'});
            for (const s of list2) items.append(this.sessionItem(s, 0, true));
            body.append(items);
        }
    },

    // renderFolder adds folder f's sessions (by host) and then its
    // subfolders to parent; depth indents them.
    renderFolder(f, parent, depth) {
        for (const g of f.hosts) {
            const group = el('div', {className: 'host-group'});
            const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Host actions'});
            const head = el('div', {className: 'host-head', title: g.host ? hostWhere(g.host) : 'host not configured'},
                el('span', {className: 'caret', textContent: '▾'}),
                el('span', {className: 'label', textContent: g.name + (g.host ? '' : ' (missing)')}),
                el('span', {className: 'backup-warn', textContent: '⚠', hidden: true}), more);
            if (g.host && g.host.kind !== 'container') this.hostHeads.push({name: g.name, target: g.host.ssh_target, head});
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
        for (const sub of f.children) {
            const folder = el('div', {className: 'folder', style: `--depth: ${depth}`});
            const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Folder actions'});
            const head = el('div', {className: 'folder-head', title: sub.group},
                el('span', {className: 'caret', textContent: '▾'}),
                el('span', {className: 'label', textContent: sub.name + (sub.known ? '' : ' (missing)')}),
                el('span', {className: 'count', textContent: sub.count}), more);
            const toggle = this.collapsible(folder, 'g:' + sub.group);
            head.onclick = e => !more.contains(e.target) && toggle();
            more.onclick = e => {
                e.stopPropagation();
                folderMenu(sub, e.clientX, e.clientY);
            };
            head.oncontextmenu = e => {
                e.preventDefault();
                folderMenu(sub, e.clientX, e.clientY);
            };
            enableFolderDrop(head, sub.group);
            const body = el('div', {className: 'folder-body'});
            if (!sub.count && !sub.children.length) body.append(el('div', {className: 'folder-empty', textContent: 'Empty — drag sessions here'}));
            folder.append(head, body);
            parent.append(folder);
            this.renderFolder(sub, body, depth + 1);
        }
    },

    // notifyWaiting shows a desktop notification for a bell in a session you
    // are not looking at (claude waiting, when it rings the bell): at most
    // one per session per minute; looking at the session withdraws it and
    // lets the next bell notify right away. tab is the Tab that rang.
    notifyWaiting(s, tab) {
        if (this.state.ui.notifications === false) return;
        const now = Date.now();
        if (s.notifiedAt && now - s.notifiedAt < 60000) return;
        s.notifiedAt = now;
        s.notifiedTab = tab.key;
        tab = tab.label;
        const where = `${s.data.name} · ${s.data.host}`;
        Notify(s.id, tab === 'Remote' ? 'Claude is waiting' : `Bell in ${tab}`, where).catch(() => {});
    },

    // clearWaiting withdraws a session's notification once it is looked at.
    clearWaiting(s) {
        if (!s.notifiedAt) return;
        s.notifiedAt = 0;
        ClearNotification(s.id).catch(() => {});
    },

    // renderCost shows today's and the total cost in the top bar (SPEC F18).
    // Parts at or above a limit (Settings → Cost) are marked ⚠, and a
    // desktop notification says so once (per day, per usage window).
    renderCost() {
        const b = $('#cost');
        b.hidden = this.state.ui.show_cost === false || !this.cost;
        if (b.hidden) return;
        const now = Date.now() / 1000;
        const {parts, title} = costParts(this.cost, this.state.ui, now);
        b.replaceChildren(...parts.flatMap((p, i) => [i ? ' · ' : '',
            p.warn ? el('span', {className: 'warn', textContent: '⚠ ' + p.text}) : p.text]));
        b.title = title;
        b.classList.toggle('warn', parts.some(p => p.warn));
        this.costWarn(now);
    },

    // costWarn notifies a limit reached: today's cost once a day, a usage
    // window once until it resets.
    costWarn(now) {
        const ui = this.state.ui, s = this.cost;
        const w = warnings(s, ui, now);
        const once = (key, title, body) => {
            if (store.get(key, false)) return;
            store.set(key, true);
            Notify('cost', title, body).catch(() => {});
        };
        if (w.daily) {
            once(`costWarn:${new Date().toDateString()}`, 'claude cost today',
                `${dollars(s.today)} at API prices, above your limit of ${dollars(ui.cost_daily_limit)}`);
        }
        for (const [on, key, name, limit] of [[w.five, 'five_hour', '5-hour', ui.usage_5h_limit], [w.seven, 'seven_day', '7-day', ui.usage_7d_limit]]) {
            const win = s.usage?.[key];
            if (on) once(`usageWarn:${key}:${win.resets_at}`, `claude ${name} usage`,
                `${Math.floor(win.used)}% used (your limit: ${limit}%)`);
        }
    },

    // renderBackup shows a host's Claude backup state in its header tooltip,
    // with a ⚠ when the last backup failed.
    renderBackup(host) {
        const st = this.backups?.[host];
        const cfg = this.state.hosts.find(x => x.name === host);
        const off = cfg && cfg.kind !== 'container' && !backsUp(cfg);
        for (const h of this.hostHeads.filter(h => h.name === host)) {
            h.head.title = `${h.target}\n${off ? 'Claude backups are off for this host (Edit host…)' : backupText(st, this.state.ui.claude_backup)}`;
            h.head.querySelector('.backup-warn').hidden = off || !st?.error;
        }
    },

    // sessionItem is a session's row: n is its number (Alt+1…9; 0 none);
    // flat: in Active or Inactive, where the balloon adds host and folder.
    sessionItem(s, n, flat = false) {
        const d = s.data;
        const more = el('button', {className: 'icon more', textContent: '⋯', title: 'Session actions'});
        const item = el('div', {className: 'session', draggable: true},
            el('span', {className: 'dot ' + s.dotClass()}),
            el('span', {className: 'abbr', textContent: initials(d.name)}),
            el('div', {className: 'text'},
                el('div', {className: 'name', textContent: d.name}),
                el('div', {className: 'sub', textContent: '‎' + where(d) + (d.inactive ? ' · inactive' : '') + '‎'})),
            el('span', {className: 'inactive-mark', textContent: '⏸', hidden: !d.inactive,
                title: 'Inactive: nothing connects (session menu → Activate session)'}),
            el('span', {className: 'idle-mark', textContent: '💤', hidden: !this.idleStopped?.has(d.id),
                title: 'claude was stopped after waiting for input; it resumes when you open the session'}),
            el('span', {className: 'todo-warn', textContent: '▲', hidden: !s.todoCount(), title: s.todoText()}),
            el('span', {className: 'repo-warn', textContent: '⚠', hidden: !s.repoWarn(),
                title: 'Repository hooks or settings to review (see the Files tab)'}),
            el('span', {className: 'badge'}),
            n >= 1 && n <= 9 ? el('span', {className: 'num', textContent: n}) : '',
            more);
        // Name, path and description, after Settings → Appearance's delay.
        balloon(item, () => [
            el('div', {className: 'balloon-name', textContent: d.name}),
            el('div', {className: 'balloon-path', textContent: where(d)}),
            flat ? el('div', {className: 'balloon-path', textContent: `Host ${d.host} · ${d.group ? 'folder ' + d.group : 'no folder'}`}) : '',
            d.description ? el('div', {className: 'balloon-text', textContent: d.description}) : '',
            d.inactive ? el('div', {className: 'balloon-text', textContent: '⏸ Inactive: nothing connects until you activate it.'}) : '',
            this.idleStopped?.has(d.id) ? el('div', {className: 'balloon-text', textContent: '💤 claude was stopped after waiting for input; it resumes when you open the session.'}) : '',
            this.state.ui.show_cost !== false && this.state.ui.cost_per_session && sessionCost(this.cost?.sessions?.[d.id])
                ? el('div', {className: 'balloon-text', textContent: sessionCost(this.cost.sessions[d.id])}) : '',
            s.todoCount() ? el('div', {className: 'balloon-todo'}, el('span', {className: 'todo-mark', textContent: '▲ '}), s.todoText()) : '',
        ], () => 1000 * (this.state.ui.description_delay ?? 1));
        item.dataset.id = d.id;
        item.classList.toggle('inactive', !!d.inactive);
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
        // A session can have a row in Hosts/Groups and one in Active/Inactive.
        s.items = [...(s.items ?? []), item];
        s.item = s.items[0];
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
        for (const it of s.items ?? []) it.querySelector('.repo-warn').hidden = !s.repoWarn();
        s.tab('files')?.view?.renderRepoBanner?.();
    },

    // checkTodos finds the TODO lines in every session's notes (the files,
    // also when edited outside the app).
    async checkTodos() {
        for (const s of [...this.sessions.values()]) {
            const todos = await NotesTodos(s.id).catch(() => null);
            if (todos && this.sessions.get(s.id) === s) this.setTodos(s, todos);
        }
    },

    setTodos(s, todos) {
        s.todos = todos;
        for (const it of s.items ?? []) {
            const w = it.querySelector('.todo-warn');
            w.hidden = !s.todoCount();
            w.title = s.todoText();
        }
        if (s === this.active) this.renderBadges(s);
    },

    // checkAll checks every session's repository, one after another.
    async checkAll() {
        for (const s of [...this.sessions.values()]) await this.checkRepo(s);
    },

    // renderBadges updates a session's sidebar badge, its tab badges when it
    // is the active session, and the window title.
    renderBadges(s) {
        const b = s.badge();
        for (const it of s.items ?? []) it.querySelector('.badge').className = 'badge' + (b ? ' ' + b : '');
        if (s === this.active) {
            $('#tabs').querySelectorAll('button').forEach(btn => {
                const t = s.tab(btn.dataset.key);
                btn.classList.toggle('bell', t?.badge === 'bell');
                btn.classList.toggle('activity', t?.badge === 'activity');
                const mark = btn.querySelector('.todo-mark');
                if (mark) mark.hidden = !s.todoCount();
                if (t?.key === 'notes') btn.title = btn.title.split('\n')[0] + (s.todoCount() ? '\n▲ ' + s.todoText() : '');
            });
        }
        this.renderTitle();
    },

    // renderTitle sets the window title: the number of sessions with a
    // badge, the app name and the active session's name.
    renderTitle() {
        const n = [...this.sessions.values()].filter(x => x.badge()).length;
        // The session first, so it shows where the title is cut short or
        // the desktop adds the program's name: "(2) alpha — Session Workbench".
        const name = this.appName ?? 'Session Workbench';
        const title = (n ? `(${n}) ` : '') + (this.active ? this.active.data.name + ' — ' : '') + name;
        if (title !== this.title) {
            this.title = title;
            WindowSetTitle(title);
        }
    },

    renderDot(s) {
        for (const it of s.items ?? []) it.querySelector('.dot').className = 'dot ' + s.dotClass();
    },

    renderMain() {
        const s = this.active;
        this.renderTitle();
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
            const b = el('button', {title: key ? `${t.label} (${key})` : t.label}, label,
                t.key === 'notes' ? el('span', {className: 'todo-mark', textContent: '▲', hidden: true}) : '');
            b.dataset.key = t.key;
            if (t === active) b.classList.add('active');
            b.onclick = () => this.selectTab(t.key);
            return b;
        }));
        this.renderBadges(s);
        const sync = $('#sync-actions');
        sync.replaceChildren();
        if (this.recording?.[s.id]) {
            sync.append(el('span', {className: 'rec-state', textContent: '● recording',
                title: 'The Remote tab is recorded (input and output as text). Session menu: Open recordings folder.'}));
        }
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
            const get = el('button', {textContent: '⇣ Get', title: 'Get commits from the agent copy (its files, when this folder has no git)'});
            const full = el('button', {textContent: 'Full ▾', title: 'Everything at once: save the agent copy, take all its files, send all'});
            send.onclick = () => syncSendDialog(s);
            get.onclick = () => getFromAgent(s);
            full.onclick = () => {
                const r = full.getBoundingClientRect();
                showMenu(r.left, r.bottom + 2, fullItems(s));
            };
            sync.append(send, get, full);
            const st = this.autosave?.[s.id];
            if (st) sync.append(saveState(st));
        }
        // The command menu, on the Local and Remote tabs.
        const term = ['local', 'remote'].includes(active?.key) ? active : null;
        if (term && (term.key === 'local' || (d.commands?.length ?? 0) + (this.state.ui.commands?.length ?? 0) > 0)) {
            const cmds = el('button', {textContent: 'Commands ▾', title: `Type a command into the ${term.label} tab`});
            cmds.onclick = () => {
                const r = cmds.getBoundingClientRect();
                showMenu(r.left, r.bottom + 2, term.commandItems());
            };
            sync.append(cmds);
        }
        if (term) {
            const prompts = el('button', {textContent: 'Prompts ▾', title: `Paste a prompt into the ${term.label} tab (without Enter)`});
            prompts.onclick = async () => {
                const items = await term.promptItems();
                const r = prompts.getBoundingClientRect();
                showMenu(r.left, r.bottom + 2, items);
            };
            sync.append(prompts);
        }
        // Only a problem of the session shows here; host, path and tmux
        // session are in Edit… and the list balloon.
        const path = $('#session-path');
        path.classList.toggle('error', !!d.error);
        path.textContent = d.error ?? '';
        path.hidden = !d.error;
    },

    select(s) {
        this.leaveNotesPage(); // picking a session leaves the notes page
        if (this.active && this.active !== s) this.active.hide();
        this.active = s;
        SessionShown(s?.id ?? ''); // never stopped for being idle; resumed if it was
        this.renderMain(); // un-hides the pane area before the terminal measures itself
        if (s) {
            this.checkRepo(s);
            store.set('active', s.id);
            this.mru = [s.id, ...this.mru.filter(id => id !== s.id)].slice(0, 50);
            store.set('mru', this.mru);
            s.show();
            this.clearWaiting(s); // also when it opens on Files, Notes or Web
        }
        document.querySelectorAll('.session').forEach(i => i.classList.toggle('active', s && i.dataset.id === s.id));
    },

    selectTab(key) {
        if (!this.active) return;
        this.leaveNotesPage();
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
        // A key the notes editor acted on (lists: Ctrl+Shift+8/9, also tab_8/9) is not also an app key.
        if (e.defaultPrevented && e.target.closest?.('.cm-editor')) return;
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
        // The notes page covers the terminals: no find bar behind it.
        else if (a === 'find') !this.appNotes?.visible && this.active?.activeTabObj()?.view?.openFind?.();
        else if (a === 'toggle_sidebar') this.setNarrow(!this.narrow);
        else if (a === 'app_notes') this.appNotes.toggle();
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
        shown = rank(items, q, s => [[s.data.name, 2], [s.data.group, 1], [s.data.host, 1], [s.data.local_path, 1],
            [s.data.description, 1, true]]); // the description: as typed
        sel = Math.min(sel, Math.max(0, shown.length - 1));
        list.replaceChildren(...shown.map((s, i) => {
            const where = `${s.data.group ? s.data.group + ' · ' : ''}${s.data.host} · ${s.data.local_path}`;
            const row = el('div', {className: 'switch-item' + (i === sel ? ' selected' : ''),
                    title: [s.data.name, where, s.data.description].filter(Boolean).join('\n')},
                el('span', {className: 'dot ' + s.dotClass()}),
                highlight(s.data.name, q),
                el('span', {className: 'where', textContent: where}),
                s === app.active ? el('span', {className: 'current', textContent: 'current'}) : '',
                s.data.description ? el('span', {className: 'desc', textContent: s.data.description.split('\n')[0]}) : '');
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
    // Resizable (drag its corner); the size is kept for next time.
    const size = store.get('switcherSize', null);
    dlg.style.width = size ? size.w + 'px' : '';
    dlg.style.height = size ? size.h + 'px' : '';
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
    dlg.addEventListener('close', () => {
        // Resizing sets the inline size (closed, it measures 0).
        if (dlg.style.height) store.set('switcherSize', {w: parseFloat(dlg.style.width), h: parseFloat(dlg.style.height)});
        app.focusActive();
    }, {once: true});
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

// hostWhere says where a host is: its ssh target, or for a container host
// that its sessions run in containers here.
const hostWhere = h => h.kind === 'container' ? 'containers on this computer' : h.ssh_target;

// FILE_MODES names a host's file access mode (docs/CONNECTIVITY.md).
// where is a session's place for the list: its local directory, or for a
// plain login the folder it starts in on the host.
const where = d => d.plain ? `ssh login: ${d.remote_dir || '~'}` : d.local_path;

const FILE_MODES = {
    login: ['none (a plain ssh login)', 'No local directory: the shell starts in the login\'s home or the remote folder set in Edit….'],
    container: ['mounted in its container', 'The session directory is mounted at the same path in the session\'s own container.'],
    shared: ['shared folder', 'The agent host sees this directory through a shared file system (NFS, SMB, VM share).'],
    sshfs: ['live share (sshfs)', 'The agent host mounts this directory over ssh while the app runs.'],
    sync: ['working copy (sync)', 'The agent host has its own copy: Send to agent copies files there, Get fetches its commits ' +
        '(its files, without git here); Full ▾ saves, takes, sends or restores everything.'],
};

function sessionMenu(s, x, y) {
    const [mode, about] = FILE_MODES[s.data.files] ?? [s.data.files || 'unknown', ''];
    const shareState = s.data.files === 'sshfs' ? (s.share?.state === 'mounted' ? ', mounted' : ', not mounted') : '';
    const info = s.data.error ? [] : [{info: `Files: ${mode}${shareState}`, title: about}, '-'];
    const sync = s.data.files === 'sync' ? [
        {label: 'Send to agent…', action: () => syncSendDialog(s)},
        {label: 'Get from agent…', action: () => getFromAgent(s)},
        ...fullItems(s),
        '-',
    ] : [];
    showMenu(x, y, [
        ...info,
        ...sync,
        ...(s.data.files === 'sshfs' ? [{label: 'Stop sharing', disabled: s.share?.state !== 'mounted',
            action: () => StopShare(s.id)}, '-'] : []),
        ...(s.data.files === 'container' ? [
            {label: 'Dockerfile…', action: () => dockerfileDialog(s)},
            {label: 'Services (compose)…', action: () => servicesDialog(s)},
            {label: 'Stop container…', action: () => stopContainer(s)},
            '-'] : []),
        {label: 'Edit…', action: () => sessionDialog(s)},
        {label: 'Move up', action: () => moveBy(s, -1)},
        {label: 'Move down', action: () => moveBy(s, 1)},
        {label: 'Move to folder…', disabled: !app.state.groups.length && !s.data.group, action: () => folderPicker(s, x, y)},
        '-',
        {
            label: effectiveColorMode(app.state.ui, s.data) === 'mono' ? 'Use full colors' : 'Use mono colors (no colors)',
            action: () => setSessionColors(s, effectiveColorMode(app.state.ui, s.data) === 'mono' ? 'full' : 'mono'),
        },
        {
            label: recordsRemote(app.state.ui, s.data) ? 'Stop recording the Remote tab' : 'Record the Remote tab',
            action: () => setSessionRecord(s, !recordsRemote(app.state.ui, s.data)),
        },
        {label: 'Open recordings folder', action: () => OpenLogFolder(s.id).catch(err => toast(err?.message ?? String(err), true))},
        {label: 'Export…', action: () => exportDialog(s)},
        ...(s.data.plain ? [] : [
            ...(s.data.files === 'container' || s.data.inactive ? [] : [{label: 'Switch host…', action: () => switchDialog(s)}]),
            {label: 'Requirements…', action: () => requirementsDialog(s)},
        ]),
        '-',
        ...(s.data.inactive ? [{label: 'Activate session', action: () => setSessionActive(s, true)}] : [
            {label: 'Disconnect', action: () => s.disconnect()},
            ...(!s.data.claude ? [] : app.idleStopped?.has(s.id)
                ? [{label: 'Resume claude', action: () => resumeClaude(s)}]
                : [{label: 'Stop claude now', action: () => stopClaude(s)}]),
            {label: 'Deactivate session…', action: () => deactivateSession(s)},
        ]),
        ...(s.data.tmux ? [{label: 'End remote session…', danger: true, action: () => endRemote(s)}] : []),
        {label: 'Delete session…', danger: true, action: () => deleteSession(s)},
    ]);
}

// folderChoices lists the folders in sidebar order, indented by depth,
// as [{group, label}]; skip(group) leaves a folder and its subfolders out.
function folderChoices(skip = () => false) {
    const out = [];
    const walk = (f, depth) => {
        for (const sub of f.children) {
            if (skip(sub.group)) continue;
            if (sub.known) out.push({group: sub.group, label: '\u00a0\u00a0\u00a0'.repeat(depth) + sub.name});
            walk(sub, depth + 1);
        }
    };
    walk(app.folderTree(), 0);
    return out;
}

// folderPicker is the second step of "Move to folder…".
function folderPicker(s, x, y) {
    showMenu(x, y, [
        {label: s.data.group ? 'No folder' : '✓ No folder', disabled: !s.data.group, action: () => setGroup(s.id, '')},
        ...folderChoices().map(({group, label}) => ({
            label: (group === s.data.group ? '✓ ' : '') + label, disabled: group === s.data.group, action: () => setGroup(s.id, group),
        })),
    ]);
}

function folderMenu(f, x, y) {
    // Up and down move it among its siblings (with its subfolders).
    const sibs = f.parent.children.filter(c => c.known);
    const i = sibs.indexOf(f);
    showMenu(x, y, [
        {label: 'New session in this folder…', disabled: !f.known, action: () => sessionDialog(null, null, f.group)},
        {label: 'New subfolder…', disabled: !f.known, action: () => folderDialog('', f.group)},
        '-',
        {label: f.known ? 'Rename folder…' : 'Create this folder', action: () => f.known ? folderDialog(f.group) :
                groupAction(() => SaveGroup('', f.group), 'Could not create folder')},
        {label: 'Move folder up', disabled: i <= 0, action: () => groupAction(() => MoveGroup(f.group, sibs[i - 1].group), 'Could not move folder')},
        {label: 'Move folder down', disabled: i < 0 || i >= sibs.length - 1,
            action: () => groupAction(() => MoveGroup(f.group, sibs[i + 2]?.group ?? ''), 'Could not move folder')},
        {label: 'Move folder to…', disabled: !f.known, action: () => folderMovePicker(f, x, y)},
        '-',
        {label: 'Delete folder…', danger: true, disabled: !f.known, action: () => deleteFolder(f)},
    ]);
}

// folderMovePicker is the second step of "Move folder to…": the top or
// another folder, not one inside it.
function folderMovePicker(f, x, y) {
    const into = parent => groupAction(() => SaveGroup(f.group, parent ? `${parent}/${f.name}` : f.name), 'Could not move folder');
    const here = f.parent.group;
    showMenu(x, y, [
        {label: (here ? '' : '✓ ') + 'Top level', disabled: !here, action: () => into('')},
        ...folderChoices(g => g === f.group).map(({group, label}) => ({
            label: (group === here ? '✓ ' : '') + label, disabled: group === here, action: () => into(group),
        })),
    ]);
}

const newItems = () => [
    {label: `New session…   ${keyHint('new_session')}`, action: () => sessionDialog()},
    {label: 'New folder…', action: () => folderDialog()},
    {label: 'Add host…', action: () => hostDialog()},
    {label: 'Import session…', action: () => importDialog()},
];

function newMenu(x, y) {
    showMenu(x, y, newItems());
}

// listMenu is the menu of the empty space in the session list: the + menu,
// and the notes and prompts for all sessions.
function listMenu(x, y) {
    showMenu(x, y, [...newItems(), '-',
        {label: `Notes for all sessions…   ${keyHint('app_notes')}`, action: () => app.appNotes.showPage('Home')},
        {label: 'Prompts for all sessions…', action: () => app.appNotes.showPage(PROMPTS)},
        '-',
        {label: `Quick switcher…   ${keyHint('quick_switcher')}`, disabled: !app.order.length, action: () => quickSwitcher()},
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
    if (when(st?.last_ok)) return `Claude data backed up ${when(st.last_ok)}` + (st.note ? ` (${st.note})` : '');
    return on ? 'Claude data not backed up yet' : 'Claude backups are off (Settings → Backups and recordings)';
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

// backsUp: the Claude backups include host h (config.BacksUp): its own
// backup setting, else yes unless its sessions are plain logins.
const backsUp = h => h?.backup ?? (h?.local_dir !== false);

function hostMenu(g, x, y) {
    const st = app.backups?.[g.name];
    const backedUp = !!when(st?.last_ok);
    // A container host's ~/.claude is a volume, kept across rebuilds; a host
    // can have backups switched off (plain logins): no backup items.
    const box = g.host?.kind === 'container' || (g.host && !backsUp(g.host));
    showMenu(x, y, [
        {label: 'New session on this host…', disabled: !g.host, action: () => sessionDialog(null, g.name, g.group)},
        {label: g.host ? 'Edit host…' : 'Configure host…', action: () => hostDialog(g.host, g.host ? null : g.name)},
        '-',
        ...(box ? [] : [{label: 'Back up Claude data now', disabled: !g.host || st?.busy, action: () => backupNow(g.name)},
        {label: 'Restore Claude data to this host…', disabled: !g.host || !backedUp, action: () => restoreBackup(g.host)},
        {label: 'Open Claude backup folder', disabled: !backedUp,
            action: () => OpenBackupFolder(g.name).catch(err => toast(String(err), true))},
        '-']),
        {label: 'Delete host…', danger: true, disabled: !g.host, action: () => deleteHost(g.host)},
    ]);
}

// ---------------------------------------------------------------- dialogs

// runDialog opens dlg and calls submit(form) when OK is pressed; the dialog
// stays open and shows the error if submit throws. Resolves true on success.
function runDialog(dlg, submit, focus = null, opened = null) {
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
        opened?.();
        focus?.focus();
    });
}

// confirmDialog asks before action. check: a checkbox label (action gets
// whether it is ticked) or a list of them (action gets a list of booleans);
// they start unticked.
async function confirmDialog({title, text, ok = 'OK', cancel = 'Cancel', check, checked = false, action}) {
    const dlg = $('#dlg-confirm');
    dlg.querySelector('h2').textContent = title;
    dlg.querySelector('.text').textContent = text;
    const okBtn = dlg.querySelector('button[value=ok]');
    okBtn.hidden = !ok;
    okBtn.textContent = ok || '';
    dlg.querySelector('button[value=cancel]').textContent = ok ? cancel : 'Close';
    const labels = check === undefined || check === null ? [] : Array.isArray(check) ? check : [check];
    const boxes = labels.map((_, i) => el('input', {type: 'checkbox', checked: Array.isArray(checked) ? !!checked[i] : checked}));
    dlg.querySelector('.checks').replaceChildren(...labels.map((l, i) => el('label', {className: 'check'}, boxes[i], ' ' + l)));
    const ticked = () => Array.isArray(check) ? boxes.map(b => b.checked) : !!boxes[0]?.checked;
    return runDialog(dlg, async () => action && action(ticked()));
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
    // The id names the session in config.toml and its folders; it cannot be changed.
    const idEl = dlg.querySelector('.session-id');
    idEl.hidden = !editing;
    idEl.replaceChildren(...(editing ? ['Id: ', el('code', {textContent: s.data.id}),
        ` — names it in config.toml, its folders (notes/${s.data.id}/, qa/${s.data.id}.json, ` +
        `working-copies/${s.data.id}/)${s.data.tmux ? ` and its tmux session ${s.data.tmux_name}` : ''}; it cannot be changed.`] : []));
    f.elements.host.replaceChildren(...app.state.hosts.map(h => el('option', {value: h.name, textContent: `${h.name}  (${hostWhere(h)})`})));
    const lastHost = store.get('lastHost', '');
    f.elements.host.value = s?.data.host ?? presetHost ?? (app.state.hosts.some(h => h.name === lastHost) ? lastHost : app.state.hosts[0].name);
    f.elements.dir.value = s?.data.local_path ?? '';
    f.elements.name.value = s?.data.name ?? '';
    f.elements.name.placeholder = '';
    f.elements.description.value = s?.data.description ?? '';
    const starts = app.state.ui.start_commands ?? [];
    f.elements.mode.replaceChildren(...starts.map(c => el('option', {value: c.name,
        textContent: c.command.trim() && c.command !== c.name ? `${c.name}  (${c.command})` : c.command.trim() ? c.name : `${c.name}  (a login shell)`})));
    if (s && !starts.some(c => c.name === s.data.start_mode)) {
        f.elements.mode.append(el('option', {value: s.data.start_mode, textContent: `${s.data.start_mode}  (not in Settings → Commands)`}));
    }
    const lastMode = store.get('lastMode', 'claude');
    f.elements.mode.value = s?.data.start_mode ?? (starts.some(c => c.name === lastMode) ? lastMode : starts[0]?.name ?? '');
    // The session's own commands (Edit only).
    const cmdBox = dlg.querySelector('.session-commands');
    cmdBox.hidden = !editing;
    const cmdList = new CommandList(cmdBox.querySelector('.command-list'), s?.data.commands ?? [],
        {run: true, group: true, where: true, groups: () => (app.state.ui.commands ?? []).map(c => c.group)});
    f.elements.colors.value = s?.data.color_mode ?? '';
    // New sessions look as set in Settings; an override is for Edit only.
    f.elements.colors.closest('label').hidden = !editing;
    f.elements.record.value = s?.data.record ?? '';
    f.elements.record.options[0].textContent = `default (${app.state.ui.record_remote ? 'on' : 'off'}, from Settings)`;
    f.elements.record.closest('label').hidden = !editing;
    f.elements.history.value = s?.data.history ?? '';
    f.elements.history.options[0].textContent = `default (${app.state.ui.private_history !== false ? 'its own' : 'shared'}, from Settings)`;
    f.elements.history.closest('label').hidden = !editing;
    // Stopping an idle claude (SPEC F17): sessions running claude in tmux.
    fillIdleChoices(f.elements.idle_stop, s?.data.idle_stop ?? '',
        `default (${idleLabel(app.state.ui.idle_stop)}, from Settings)`);
    f.elements.idle_stop.closest('label').hidden = !editing || !s.data.claude;
    // Container sessions: the ~/.claude volume (takes effect when the
    // container is made again: Dockerfile… → Rebuild).
    const inBox = editing && app.state.hosts.find(h => h.name === s.data.host)?.kind === 'container';
    f.elements.claude_volume.closest('label').hidden = !inBox;
    f.elements.claude_volume.value = s?.data.own_claude_volume ? 'own' : 'shared';
    f.elements.group.replaceChildren(el('option', {value: '', textContent: '(no folder)'}),
        // Full paths: the closed list shows only the chosen one, without the tree around it.
        ...folderChoices().map(({group}) => el('option', {value: group, textContent: group.replaceAll('/', ' / ')})));
    f.elements.group.value = s?.data.group ?? presetGroup ?? '';
    f.elements.colors.options[0].textContent = `default (${app.state.ui.color_mode})`;
    f.elements.host.disabled = f.elements.dir.disabled = f.elements.browse.disabled = editing;
    const preview = dlg.querySelector('.preview');
    preview.textContent = editing ? `→ ${s.data.remote_path || s.data.error}` : '';
    preview.classList.toggle('bad', editing && !!s.data.error);
    // With or without a local directory: the host's default, fixed once
    // made; a container host always mounts one.
    const hostOf = () => app.state.hosts.find(h => h.name === f.elements.host.value);
    const plain = () => f.elements.localdir.value === 'no';
    f.elements.remotedir.value = s?.data.remote_dir ?? '';
    // tmux: the host's setting unless the session has its own.
    const tmuxLabel = () => `default (${hostOf()?.tmux === false ? 'no: the host has no tmux' : 'yes'}, from the host)`;
    f.elements.tmux.value = s?.data.tmux_own == null ? 'host' : s.data.tmux_own ? 'yes' : 'no';
    f.elements.tmux.options[0].textContent = tmuxLabel();
    const showKind = () => {
        const box = hostOf()?.kind === 'container';
        if (box) f.elements.localdir.value = 'yes';
        f.elements.localdir.disabled = editing || box;
        dlg.querySelector('.dir-row').hidden = plain();
        dlg.querySelector('.remote-dir').hidden = !plain();
        // A new plain login starts a shell (a start command without a
        // command), not the last mode used for agent sessions.
        const shell = starts.find(c => !c.command.trim());
        if (!editing) f.elements.mode.value = plain() && shell ? shell.name
            : (starts.some(c => c.name === lastMode) ? lastMode : starts[0]?.name ?? '');
    };
    f.elements.localdir.value = editing ? (s.data.plain ? 'no' : 'yes') : (hostOf()?.local_dir === false ? 'no' : 'yes');
    showKind();

    let seq = 0;
    const update = async () => {
        if (editing) return;
        const n = ++seq;
        if (plain()) {
            const p = await PreviewSession({host: f.elements.host.value, local_path: '', name: '', start_mode: f.elements.mode.value,
                color_mode: '', group: '', local_dir: false, remote_dir: f.elements.remotedir.value});
            if (n !== seq) return;
            f.elements.name.placeholder = p.name;
            preview.classList.toggle('bad', !!p.error);
            preview.textContent = p.error || `→ ${f.elements.host.value}:${p.remote_path}  (a plain login)`;
            return;
        }
        if (!f.elements.dir.value.trim()) {
            preview.textContent = '';
            f.elements.name.placeholder = '';
            return;
        }
        const p = await PreviewSession({host: f.elements.host.value, local_path: f.elements.dir.value, name: '', start_mode: f.elements.mode.value,
            color_mode: '', group: '', local_dir: true, remote_dir: ''});
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
    f.elements.host.onchange = () => {
        f.elements.tmux.options[0].textContent = tmuxLabel();
        f.elements.localdir.value = hostOf()?.local_dir === false ? 'no' : 'yes';
        showKind();
        update();
    };
    f.elements.localdir.onchange = () => {
        showKind();
        update();
    };
    f.elements.remotedir.oninput = () => {
        clearTimeout(timer);
        timer = setTimeout(update, 150);
    };
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
                color_mode: f.elements.colors.value, group: f.elements.group.value, record: f.elements.record.value,
                history: f.elements.history.value, commands: cmdList.value, description: f.elements.description.value,
                idle_stop: f.elements.idle_stop.value,
                ...(inBox ? {own_claude_volume: f.elements.claude_volume.value === 'own'} : {}),
                ...(s.data.plain ? {remote_dir: f.elements.remotedir.value} : {}),
                tmux: f.elements.tmux.value,
            });
        } else {
            created = await CreateSession({
                host: f.elements.host.value, local_path: f.elements.dir.value, name: f.elements.name.value,
                start_mode: f.elements.mode.value, color_mode: '', group: f.elements.group.value,
                description: f.elements.description.value,
                local_dir: !plain(), remote_dir: plain() ? f.elements.remotedir.value : '',
                tmux: {host: null, yes: true, no: false}[f.elements.tmux.value],
            });
            store.set('lastHost', f.elements.host.value);
            if (!plain()) store.set('lastMode', f.elements.mode.value); // a login's shell is not the agents' default
        }
    }, editing || plain() ? f.elements.name : f.elements.dir);
    if (!ok) return;
    await app.reload();
    if (created) app.select(app.sessions.get(created.id));
    // Without tmux the next connection starts the new mode anyway.
    if (editing && s.data.start_mode !== oldMode && s.data.tmux) await offerRestart(s, oldMode);
}

// startLabel describes a start mode: its name and command.
function startLabel(name) {
    const c = app.state.ui.start_commands?.find(c => c.name === name);
    if (!c) return `“${name}”`;
    return c.command.trim() ? `“${c.name}” (${c.command})` : `“${c.name}” (a login shell)`;
}

// offerRestart: a new start mode only applies when the remote tmux session is
// created; one that is already running keeps what it started with. Offer to
// end it, and reconnect an open Remote tab so it starts in the new mode.
async function offerRestart(s, oldMode) {
    await confirmDialog({
        title: 'Restart the remote session?',
        text: `New sessions on the agent host now start with ${startLabel(s.data.start_mode)}. ` +
            `If tmux session ${s.data.tmux_name} on ${s.data.host} is already running, it keeps ` +
            `running what it started with (${startLabel(oldMode)}) until it ends. Restarting ends it now, ` +
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
    f.elements.kind.value = h?.kind ?? '';
    f.elements.local_dir.value = h?.local_dir === false ? 'no' : 'yes';
    f.elements.tmux.value = h?.tmux === false ? 'no' : 'yes';
    // Claude backups: the host's own choice, else follow Sessions on this
    // host (plain logins: no claude there, so no backups) until changed.
    let backupSet = h?.backup != null;
    f.elements.backup.value = backsUp(h ?? {}) ? 'yes' : 'no';
    f.elements.backup.onchange = () => backupSet = true;
    const showMode = () => {
        const shared = f.elements.files.value === 'shared';
        const box = f.elements.kind.value === 'container';
        dlg.querySelector('.host-ssh').hidden = box;
        dlg.querySelector('.host-container').hidden = !box;
        dlg.querySelector('.files-shared').hidden = !shared;
        dlg.querySelector('.files-workdir').hidden = shared;
        // Plain logins share no directory: the file fields are shown, not
        // used (a session can still choose a directory, then they apply).
        const logins = f.elements.local_dir.value === 'no';
        if (!backupSet) f.elements.backup.value = logins ? 'no' : 'yes';
        for (const k of ['files', 'local_prefix', 'remote_prefix', 'browse', 'remote_workdir']) f.elements[k].disabled = logins;
    };
    f.elements.files.onchange = showMode;
    f.elements.kind.onchange = showMode;
    f.elements.local_dir.onchange = showMode;
    showMode();
    if (presetName) f.elements.name.value = presetName;
    f.elements.browse.onclick = async () => {
        const dir = await BrowseDirectory(f.elements.local_prefix.value || '~');
        if (dir) f.elements.local_prefix.value = dir;
    };
    const ok = await runDialog(dlg, () => SaveHost(h?.name ?? '', f.elements.kind.value === 'container' ? {
        name: f.elements.name.value, kind: 'container', ssh_target: '', files: '', local_prefix: '', remote_prefix: '', remote_workdir: '',
    } : {
        name: f.elements.name.value, kind: '', ssh_target: f.elements.ssh_target.value, files: f.elements.files.value,
        local_dir: f.elements.local_dir.value === 'no' ? false : null,
        tmux: f.elements.tmux.value === 'no' ? false : null,
        // Stored only when it differs from what Sessions on this host implies.
        backup: (f.elements.backup.value === 'yes') === (f.elements.local_dir.value !== 'no') ? null : f.elements.backup.value === 'yes',
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
        await UpdateSession(s.id, {name: s.data.name, start_mode: s.data.start_mode, color_mode: override, group: s.data.group,
            record: s.data.record, history: s.data.history});
    } catch (err) {
        await confirmDialog({title: 'Could not change colors', text: String(err), ok: null});
    }
    await app.reload();
}

// recordsRemote: does the session's Remote tab get recorded (its own
// setting, else the global one)?
const recordsRemote = (ui, d) => d.record === 'on' || (d.record !== 'off' && !!ui.record_remote);

// setSessionRecord turns recording on or off for one session; choosing what
// the global setting says clears the override.
async function setSessionRecord(s, on) {
    const override = on === !!app.state.ui.record_remote ? '' : on ? 'on' : 'off';
    try {
        await UpdateSession(s.id, {name: s.data.name, start_mode: s.data.start_mode, color_mode: s.data.color_mode,
            group: s.data.group, record: override, history: s.data.history});
    } catch (err) {
        await confirmDialog({title: 'Could not change recording', text: String(err), ok: null});
    }
    await app.reload();
}

const ACTION_LABELS = {
    next_session: 'Next session', prev_session: 'Previous session', new_session: 'New session',
    quick_switcher: 'Quick switcher', settings: 'Settings', find: 'Find in terminal',
    toggle_sidebar: 'Narrow / wide sidebar', app_notes: 'Notes for all sessions',
};

async function settingsDialog(page) {
    const dlg = $('#dlg-settings');
    if (dlg.open) return;
    const f = dlg.querySelector('form').elements;
    const ui = app.state.ui;
    // Pages behind a tab bar; the last one used opens again.
    const pages = [...dlg.querySelectorAll('.settings-page')];
    const tabs = [...dlg.querySelectorAll('.settings-tabs button')];
    const showPage = name => {
        if (!pages.some(p => p.dataset.page === name)) name = 'appearance';
        pages.forEach(p => p.hidden = p.dataset.page !== name);
        tabs.forEach(t => {
            t.classList.toggle('active', t.dataset.page === name);
            t.ariaSelected = String(t.dataset.page === name);
        });
        store.set('settingsPage', name);
    };
    tabs.forEach(t => t.onclick = () => showPage(t.dataset.page));
    showPage(page ?? store.get('settingsPage', 'appearance'));
    costPage(dlg, f, ui);
    qaSettings(dlg, f, ui);
    f.container_engine.value = ui.container_engine ?? '';
    const found = dlg.querySelector('.container-found');
    const lookEngine = () => ContainerEngineInfo(f.container_engine.value)
        .then(v => found.textContent = `Found: ${v}`)
        .catch(err => found.textContent = err?.message ?? String(err));
    f.container_engine.onchange = lookEngine;
    lookEngine();
    const commands = new CommandList(dlg.querySelector('[data-list=commands]'), ui.commands ?? [], {run: true, group: true, where: true,
        groups: () => app.state.sessions.flatMap(d => (d.commands ?? []).map(c => c.group))});
    const starts = new CommandList(dlg.querySelector('[data-list=start_commands]'), ui.start_commands ?? [],
        {emptyCommand: true, placeholder: {name: 'Name (start mode)'}});
    for (const k of ['theme', 'color_mode', 'min_contrast', 'scrollback', 'font_family', 'font_size', 'connect_on_start', 'description_delay']) f[k].value = ui[k] ?? '';
    f.auto_reconnect.checked = !!ui.auto_reconnect;
    f.activity_badges.checked = !!ui.activity_badges;
    f.copy_on_select.checked = !!ui.copy_on_select;
    f.claude_backup.checked = ui.claude_backup !== false;
    f.notifications.checked = ui.notifications !== false;
    f.ctrl_copy_paste.checked = ui.ctrl_copy_paste !== false;
    f.private_history.checked = ui.private_history !== false;
    f.show_cost.checked = ui.show_cost !== false;
    f.cost_per_session.checked = !!ui.cost_per_session;
    fillIdleChoices(f.idle_stop, ui.idle_stop ?? '', 'never');
    f.ctrl_copy_paste.closest('label').hidden = isMac; // Cmd+C/V there
    f.private_history.closest('label').title = navigator.platform.startsWith('Win')
        ? 'On Windows only the Remote tab: PowerShell and cmd keep their own history' : '';
    const every = f.claude_backup_every;
    if (![...every.options].some(o => o.value === ui.claude_backup_every)) {
        every.append(el('option', {value: ui.claude_backup_every, textContent: ui.claude_backup_every}));
    }
    every.value = ui.claude_backup_every;
    f.record_remote.checked = !!ui.record_remote;
    f.save_agent_copies.checked = ui.save_agent_copies !== false;
    f.save_agent_copies_at_quit.checked = ui.save_agent_copies_at_quit !== false;
    const saveEvery = f.save_agent_copies_every;
    if (![...saveEvery.options].some(o => o.value === ui.save_agent_copies_every)) {
        saveEvery.append(el('option', {value: ui.save_agent_copies_every, textContent: ui.save_agent_copies_every}));
    }
    saveEvery.value = ui.save_agent_copies_every;
    f.saved_copies_hours.value = ui.saved_copies_hours ?? 24;
    f.saved_copies_days.value = ui.saved_copies_days ?? 14;
    const recEvery = f.record_every;
    if (![...recEvery.options].some(o => o.value === ui.record_every)) {
        recEvery.append(el('option', {value: ui.record_every, textContent: ui.record_every}));
    }
    recEvery.value = ui.record_every;
    f.record_dir.value = ui.record_dir ?? '';
    f.spell_check.checked = ui.spell_check !== false;
    const langs = await SpellLanguages().catch(() => ['en_US']);
    const lang = ui.spell_language || 'en_US';
    f.spell_language.replaceChildren(...[...new Set([...langs, lang])].map(l =>
        el('option', {value: l, textContent: l === 'en_US' ? 'en_US (English, built in)' : langs.includes(l) ? l : `${l} (not found)`})));
    f.spell_language.value = lang;
    f.spell_words.value = (ui.spell_words ?? []).join('\n');
    dlg.querySelector('.spell-hint').textContent = `More languages: Hunspell dictionaries (xx.aff and xx.dic) in ` +
        `${app.state.config_path.replace(/[^/\\]*$/, '')}dictionaries` +
        (navigator.platform.startsWith('Linux') ? ', or installed ones (hunspell-* packages)' : '') + '.';
    RecordInfo().then(r => dlg.querySelector('.record-default').textContent =
        `Default: ${r.default_dir || '(no data folder)'}. Files: <folder>/<host>/<session path>/YYYYMMDD-HHMM.log`).catch(() => {});
    // Tabs: order with ↑/↓, shown or not with the checkbox.
    const TAB_LABELS = {local: 'Local', files: 'Files', remote: 'Remote', notes: 'Notes', web: 'Web', qa: 'Q&A'};
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
    dlg.querySelector('.tab-order .caption').textContent =
        `Tabs (order and which are shown; ${keyHint('tab_1') ? `${keyHint('tab_1')}… follow` : 'tab_1 … tab_9 follow'} this order)`;
    const rows = Object.keys(ACTIONS).filter(a => !a.startsWith('session_') && !/^tab_/.test(a)).map(a =>
        el('tr', {}, el('td', {textContent: ACTION_LABELS[a] ?? a}), el('td', {}, el('kbd', {textContent: keyHint(a) || '—'})),
            el('td', {className: 'action', textContent: a})));
    rows.push(el('tr', {}, el('td', {textContent: 'Tab 1…9 (by position)'}), el('td', {}, el('kbd', {textContent: keyHint('tab_1').replace(/1$/, '1…9')})),
        el('td', {className: 'action', textContent: 'tab_1 … tab_9; tab_local, tab_remote … per tab'})));
    rows.push(el('tr', {}, el('td', {textContent: 'Session 1…9'}), el('td', {}, el('kbd', {textContent: keyHint('session_1').replace(/1$/, '1…9')})),
        el('td', {className: 'action', textContent: 'session_1 … session_9'})));
    dlg.querySelector('.shortcuts table').replaceChildren(...rows);
    Version().then(v => dlg.querySelector('.about').textContent = `${app.appName ?? 'Session Workbench'} ${v}`);
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
        description_delay: f.description_delay.value === '' ? 1 : Number(f.description_delay.value), // empty: the default
        connect_on_start: f.connect_on_start.value,
        idle_stop: f.idle_stop.value,
        show_cost: f.show_cost.checked,
        cost_per_session: f.cost_per_session.checked,
        cost_daily_limit: f.cost_daily_limit.value === '' ? 0 : Number(f.cost_daily_limit.value),
        usage_5h_limit: f.usage_5h_limit.value === '' ? 0 : Number(f.usage_5h_limit.value),
        usage_7d_limit: f.usage_7d_limit.value === '' ? 0 : Number(f.usage_7d_limit.value),
        auto_reconnect: f.auto_reconnect.checked,
        activity_badges: f.activity_badges.checked,
        copy_on_select: f.copy_on_select.checked,
        claude_backup: f.claude_backup.checked,
        notifications: f.notifications.checked,
        ctrl_copy_paste: f.ctrl_copy_paste.checked,
        private_history: f.private_history.checked,
        claude_backup_every: f.claude_backup_every.value,
        record_remote: f.record_remote.checked,
        save_agent_copies: f.save_agent_copies.checked,
        save_agent_copies_every: f.save_agent_copies_every.value,
        save_agent_copies_at_quit: f.save_agent_copies_at_quit.checked,
        saved_copies_hours: Number(f.saved_copies_hours.value),
        saved_copies_days: Number(f.saved_copies_days.value),
        record_every: f.record_every.value,
        record_dir: f.record_dir.value.trim(),
        spell_check: f.spell_check.checked,
        spell_language: f.spell_language.value,
        spell_words: f.spell_words.value.split('\n'),
        qa_engine: f.qa_engine.value,
        qa_model: f.qa_model.value.trim(),
        qa_claude_model: f.qa_claude_model.value,
        qa_claude: f.qa_claude.value.trim(),
        container_engine: f.container_engine.value,
        commands: commands.value,
        start_commands: starts.value,
        tab_order: order,
        hidden_tabs: order.filter(k => hidden.has(k)),
    }), pages.find(p => !p.hidden)?.querySelector('input, select')); // the open page's first field
    if (ok) await app.reload();
    if (ok && f.show_cost.checked && ui.show_cost === false) CostNow(); // turned on: count at once
}

// qaSettings fills the Q&A page of Settings: who answers by default, the
// models, the Claude Code program (with where it is found).
function qaSettings(dlg, f, ui) {
    const option = (value, text) => el('option', {value, textContent: text});
    const cmds = (ui.qa_commands ?? []).map(c => c.name).filter(Boolean);
    f.qa_engine.replaceChildren(
        option('', 'automatic (see below)'),
        option('api', 'Claude API (needs an API key)'),
        option('claude', 'Claude Code (its own login, no API key)'),
        ...cmds.map(n => option(n, `${n} (own command)`)));
    if (ui.qa_engine && ![...f.qa_engine.options].some(o => o.value === ui.qa_engine)) {
        f.qa_engine.append(option(ui.qa_engine, `${ui.qa_engine} (unknown: automatic is used)`));
    }
    f.qa_engine.value = ui.qa_engine ?? '';
    f.qa_model.value = ui.qa_model ?? '';
    // The API's models as suggestions (none without an API key).
    const list = dlg.querySelector('#qa-api-models');
    list.replaceChildren();
    QAModels('api').then(ms => list.replaceChildren(...(ms ?? []).map(m => option(m.id, m.name)))).catch(() => {});
    f.qa_claude_model.replaceChildren(
        option('', "Claude Code's default"), option('fable', 'Fable (latest)'), option('opus', 'Opus (latest)'),
        option('sonnet', 'Sonnet (latest)'), option('haiku', 'Haiku (latest)'));
    if (ui.qa_claude_model && ![...f.qa_claude_model.options].some(o => o.value === ui.qa_claude_model)) {
        f.qa_claude_model.append(option(ui.qa_claude_model, ui.qa_claude_model));
    }
    f.qa_claude_model.value = ui.qa_claude_model ?? '';
    f.qa_claude.value = ui.qa_claude ?? '';
    const found = dlg.querySelector('.qa-claude-found');
    const look = () => QAFindClaude(f.qa_claude.value)
        .then(p => found.textContent = `Claude Code found: ${p}`)
        .catch(err => found.textContent = err?.message ?? String(err));
    f.qa_claude.oninput = look;
    look();
    dlg.querySelector('.qa-hint').textContent = 'Automatic: the Claude API when ANTHROPIC_API_KEY is set, else Claude Code ' +
        'when it is installed. A session keeps the engine and model chosen in its Q&A tab; these are ' +
        'the defaults for sessions that chose none. Own commands are [[ui.qa_commands]] entries in ' +
        `${app.state.config_path}` + (cmds.length ? ` (now: ${cmds.join(', ')}).` : ' (none yet).');
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
    if (p.changes.length) parts.push(changeList(p)); // git's own files summarized
    body.replaceChildren(...parts);
    okBtn.disabled = !!p.blocked || (!total && p.agent.exists) || !!(p.agent.dirty && !overwrite?.checked);
    (okBtn.disabled ? overwrite : okBtn)?.focus();
    const ok = await done;
    if (ok) s.tab('remote').view?.exited && s.tab('remote').view.restart();
    return ok;
}

// saveState shows when a session's agent copy was last saved (autosave).
function saveState(st) {
    const known = t => t && !t.startsWith('0001-');
    if (st.busy) {
        return el('span', {className: 'save-state', textContent: `⟳ saving${st.percent >= 0 ? ` ${st.percent}%` : '…'}`,
            title: 'The agent copy is being saved (Full ▾ → Save agent copy)'});
    }
    if (st.error) {
        return el('span', {className: 'save-state failed', textContent: '⚠ not saved',
            title: `Saving the agent copy failed: ${st.error}` + (known(st.last_ok) ? `\nLast saved ${new Date(st.last_ok).toLocaleString()}` : '')});
    }
    if (!known(st.last_ok)) return el('span');
    const t = new Date(st.last_ok);
    const today = t.toDateString() === new Date().toDateString();
    return el('span', {className: 'save-state', textContent: `saved ${today ? t.toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'}) : t.toLocaleDateString()}`,
        title: `Agent copy saved ${t.toLocaleString()}${st.note ? ` (${st.note})` : ''}`});
}

// showQuitSaving shows the progress of saving the agent copies at quit;
// done: finished with failures (the app waits for Quit).
function showQuitSaving(p, done) {
    const box = $('#quit-saving');
    box.hidden = false;
    hideMenu();
    const failed = p.failed ?? [], skipped = p.skipped ?? [];
    box.querySelector('h2').textContent = done ? 'Not every agent copy could be saved' : 'Saving agent copies before quitting…';
    box.querySelector('.quit-text').textContent = done
        ? `${p.done - failed.length - skipped.length} of ${p.total} saved` +
            (skipped.length ? ` (${skipped.length} never sent, nothing to save)` : '') +
            '. For these the copy saved earlier (if any) is kept:'
        : p.current ? `${p.done + 1} of ${p.total}: ${p.current}${p.percent >= 0 ? ` — ${p.percent}%` : ' …'}` : `${p.done} of ${p.total} saved`;
    const bar = box.querySelector('progress');
    const part = p.current && p.percent >= 0 ? p.percent / 100 : 0;
    bar.value = p.total ? Math.round((p.done + part) / p.total * 100) : 0;
    const list = box.querySelector('.quit-failed');
    list.hidden = !failed.length;
    list.textContent = failed.join('\n');
    const btn = box.querySelector('.quit-now');
    btn.textContent = done ? 'Quit' : 'Quit now (stop saving)';
    btn.focus();
}

// getFromAgent is Get: commits when the session directory is a git
// repository, else (no commits to fetch) the agent copy's files.
async function getFromAgent(s) {
    const info = await SyncInfo(s.id).catch(() => ({git: true}));
    return info.git ? syncGetDialog(s) : takeFilesDialog(s, true);
}

// fullItems are the full transfers of a working copy (Full ▾, session menu).
const fullItems = s => [
    {label: 'Save agent copy (everything, with .git)…', action: () => saveCopyDialog(s)},
    {label: 'Take all files from the agent…', action: () => takeFilesDialog(s, false)},
    {label: 'Send all (ignored files too)…', action: () => sendAllDialog(s, false)},
    {label: 'Restore saved copy to the agent…', action: () => sendAllDialog(s, true)},
    {label: 'Open saved copy folder', action: () => OpenSaved(s.id).catch(err => toast(err?.message ?? String(err), true))},
];

// changeList shows a plan's changes, git's own files summarized.
function changeList(p) {
    const sign = {new: '+', changed: '~', deleted: '−'};
    const isGit = c => /(^|\/)\.git(\/|$)/.test(c.path);
    const git = p.changes.filter(isGit).length;
    const lines = p.changes.filter(c => !isGit(c)).map(c => `${sign[c.op]} ${c.path}`);
    if (git) lines.push(`  .git/ (${git} file${git > 1 ? 's' : ''} of git's own data)`);
    return el('pre', {className: 'changes', textContent: lines.join('\n') + (p.truncated ? '\n…' : '')});
}

const counts = p => `${p.new} new, ${p.changed} changed, ${p.deleted} deleted`;
const savedWhen = iso => iso ? new Date(iso).toLocaleString() : 'unknown';

// fullDialog runs one of the full transfers in #dlg-sync: preview() fills
// the body (returns false when there is nothing to do), run() does it.
async function fullDialog(s, {title, ok, preview, run}) {
    const dlg = $('#dlg-sync');
    if (dlg.open) return false;
    const body = dlg.querySelector('.body');
    const okBtn = dlg.querySelector('button[value=ok]');
    dlg.querySelector('h2').textContent = `${title}: ${s.data.name}`;
    okBtn.textContent = ok;
    okBtn.hidden = false;
    okBtn.disabled = true;
    body.replaceChildren(el('p', {textContent: `Looking at ${s.data.host}:${s.data.remote_path} …`}));
    const done = runDialog(dlg, async () => {
        body.append(el('p', {textContent: 'Working… (large copies take a while)'}));
        await run();
    });
    try {
        okBtn.disabled = !await preview(body, okBtn);
    } catch (err) {
        body.replaceChildren(el('p', {className: 'bad', textContent: err?.message ?? String(err)}));
    }
    if (!okBtn.disabled) okBtn.focus();
    return done;
}

// saveCopyDialog saves the whole agent copy here (SyncSave).
function saveCopyDialog(s) {
    return fullDialog(s, {
        title: 'Save agent copy', ok: 'Save',
        preview: async body => {
            const info = (await SyncInfo(s.id)).saved;
            const ui = app.state.ui;
            body.replaceChildren(
                el('p', {textContent: `Copies everything in ${s.data.host}:${s.data.remote_path} — committed or not, ` +
                    'files .gitignore ignores, and .git — to a dated folder here, so nothing is lost if the agent host goes away.'}),
                el('p', {textContent: info.exists ? `${info.copies.length} saved cop${info.copies.length > 1 ? 'ies' : 'y'}; ` +
                    `the newest from ${savedWhen(info.latest.when)} (${info.latest.files} files).` : 'No copy saved yet.'}),
                el('p', {className: 'hint', textContent: `Kept: every copy of the last ${ui.saved_copies_hours} hours and the last of each day ` +
                    `for ${ui.saved_copies_days} days (Settings → Backups and recordings). Folder: ${info.base}. Your session ` +
                    'directory is not changed; Full ▾ → Restore saved copy sends one to an agent host again.'}));
            return true;
        },
        run: async () => {
            const info = await SyncSave(s.id);
            const c = info.latest;
            toast(`Saved the agent copy of ${s.data.name}: ${c.files} files${c.note ? ` (${c.note})` : ''}`, !!c.note);
        },
    });
}

// takeFilesDialog brings the agent copy's files into the session directory
// (SyncTakeFiles); asGet: it is Get for a session without git.
function takeFilesDialog(s, asGet) {
    const deletes = el('input', {type: 'checkbox', checked: asGet}); // before the first preview
    let overwrite = null, p = null;
    return fullDialog(s, {
        title: asGet ? 'Get from agent (no git here)' : 'Take all files from the agent', ok: 'Take files',
        preview: async (body, okBtn) => {
            const show = async () => {
                p = await SyncTakeFiles(s.id, true, deletes.checked);
                const total = p.new + p.changed + p.deleted;
                const parts = [el('p', {textContent: total ? `Here: ${counts(p)}.` : 'Your files already match the agent copy.'})];
                parts.push(el('p', {className: 'hint', textContent: (asGet ? 'This directory is not a git repository, so there are no commits to fetch: ' +
                    'Get copies the agent copy\'s files here instead. ' : '') +
                    'New and changed files come here as they are on the agent host, unreviewed (not as commits); .git is never copied. ' +
                    'Check them before running anything here (Makefile, scripts, .envrc, editor settings).'}));
                deletes.onchange = () => show().catch(err => body.replaceChildren(el('p', {className: 'bad', textContent: err?.message ?? String(err)})));
                parts.push(el('label', {className: 'check'}, deletes, ' Also delete files here that the agent copy does not have (a full copy of the other side)'));
                if (p.overwrites) {
                    overwrite = el('input', {type: 'checkbox'});
                    overwrite.onchange = () => okBtn.disabled = !overwrite.checked;
                    parts.push(el('label', {className: 'check bad'}, overwrite,
                        ` Overwrite or delete ${p.overwrites} file(s) with uncommitted changes here`));
                } else {
                    overwrite = null;
                }
                if (p.changes.length) parts.push(changeList(p));
                body.replaceChildren(...parts);
                okBtn.disabled = !total || !!(overwrite && !overwrite.checked);
                return !okBtn.disabled;
            };
            return show();
        },
        run: async () => {
            const r = await SyncTakeFiles(s.id, false, deletes.checked);
            toast(`Took the agent's files: ${counts(r)}`);
        },
    });
}

// sendAllDialog sends in full (SyncSendAll): the session directory with
// the files .gitignore ignores, or (restore) a saved copy, picked from a list.
function sendAllDialog(s, restore) {
    let sure = null, pick = null;
    const from = () => restore ? pick?.value ?? '' : '';
    return fullDialog(s, {
        title: restore ? 'Restore saved copy to the agent' : 'Send all to the agent', ok: restore ? 'Restore' : 'Send all',
        preview: async (body, okBtn) => {
            const head = [];
            if (restore) {
                const info = (await SyncInfo(s.id)).saved;
                if (!info.exists) throw new Error('There is no saved copy of this session yet (Full ▾ → Save agent copy).');
                pick = el('select', {}, ...info.copies.map(c => el('option', {value: c.stamp,
                    textContent: `${savedWhen(c.when)} — ${c.files} files${c.from ? `, from ${c.from}` : ''}`})));
                head.push(el('label', {className: 'restore-pick'}, 'Saved copy ', pick),
                    el('p', {textContent: `It goes to ${s.data.host}:${s.data.remote_path}.`}));
            }
            const show = async () => {
                const p = await SyncSendAll(s.id, from(), true);
                const total = p.new + p.changed + p.deleted;
                const parts = [...head];
                parts.push(el('p', {textContent: !p.agent.exists ? `New agent copy: ${p.new} file(s).` :
                    total ? `On the agent host: ${counts(p)}.` : 'The agent copy is already identical.'}));
                parts.push(el('p', {className: 'hint', textContent: (restore ? 'Everything in that copy is sent, with .git and ignored files. ' :
                    'Everything here is sent, also the files .gitignore ignores (build output, .env…). ') +
                    'The agent copy becomes identical: what only it has is deleted there, ignored files too.'}));
                const risks = [];
                if (p.agent.unfetched) risks.push(`${p.agent.unfetched} branch(es) with commits you have not fetched`);
                if (p.agent.dirty) risks.push(`uncommitted changes in ${p.agent.dirty} file(s)`);
                sure = null;
                if (risks.length) {
                    sure = el('input', {type: 'checkbox'});
                    sure.onchange = () => okBtn.disabled = !sure.checked;
                    parts.push(el('label', {className: 'check bad'}, sure, ` Overwrite the agent copy's ${risks.join(' and ')}`));
                }
                if (p.changes.length) parts.push(changeList(p));
                body.replaceChildren(...parts);
                okBtn.disabled = !(total > 0 || !p.agent.exists) || !!sure;
                return !okBtn.disabled;
            };
            if (pick) pick.onchange = () => show().catch(err => body.replaceChildren(el('p', {className: 'bad', textContent: err?.message ?? String(err)})));
            return show();
        },
        run: async () => {
            const p = await SyncSendAll(s.id, from(), false);
            toast(`${restore ? 'Restored' : 'Sent all'} to ${s.data.host}: ${counts(p)}`);
            const v = s.tab('remote').view;
            if (v?.exited) v.restart();
        },
    });
}

// AGENT_STATES describes an agent branch next to the host's (syncer.AgentBranch).
const AGENT_STATES = {
    new: b => `new on the agent${b.ahead > 0 ? `, ${b.ahead} new commit(s)` : ''}`,
    ahead: b => `the agent has ${b.ahead} more commit(s)`,
    same: () => 'up to date',
    behind: b => `you have ${b.behind} more commit(s); not changed`,
    diverged: b => `both have commits (${b.ahead} / ${b.behind}): merge or rebase in the Local tab`,
    unsafe: () => 'the name has characters a shell would act on: not taken over, check it on the agent host',
};

// shq quotes s for a POSIX shell (suggested commands are pasted there).
const shq = s => /^[A-Za-z0-9._/+@-]+$/.test(s) ? s : `'${s.replaceAll("'", `'\\''`)}'`;

// syncGetDialog fetches the agent copy's branches, lists each next to the
// host's and takes the ticked ones over (SyncTake).
async function syncGetDialog(s) {
    const dlg = $('#dlg-sync');
    if (dlg.open) return;
    const body = dlg.querySelector('.body');
    const okBtn = dlg.querySelector('button[value=ok]');
    dlg.querySelector('h2').textContent = `Get commits: ${s.data.name}`;
    okBtn.hidden = true;
    body.replaceChildren(el('p', {textContent: `Fetching from ${s.data.host}:${s.data.remote_path} …`}));
    let boxes = [];
    const done = runDialog(dlg, async () => {
        const names = boxes.filter(b => b.checked).map(b => b.value);
        if (!names.length) throw new Error('Nothing is ticked.');
        const r = await SyncTake(s.id, names);
        // Nothing done: the dialog stays open with the reason.
        if (r.failed && !r.done?.length) throw new Error(r.failed);
        if (r.done?.length) toast(r.done.join('; '));
        if (r.failed) toast(`Not taken over: ${r.failed}`, true);
    });
    let f;
    try {
        f = await SyncGet(s.id);
    } catch (err) {
        body.replaceChildren(el('p', {className: 'bad', textContent: err?.message ?? String(err)}));
        return done;
    }
    const parts = [el('p', {className: 'hint', textContent:
        `Fetched ${f.branches?.length ?? 0} branch(es) as ${f.branches?.[0]?.replace(/\/[^/]*$/, '') ?? 'agent-…'}/… ` +
        '(commits only: hooks and config of the agent copy are never copied here).'})];
    // Every agent branch: new ones and ones you can fast-forward are ticked;
    // under each, the commits it brings and the files they change (as in
    // the Send preview).
    const sign = {new: '+', changed: '~', deleted: '−'};
    const rows = (f.agent ?? []).flatMap(b => {
        const takes = b.state === 'new' || b.state === 'ahead';
        const box = el('input', {type: 'checkbox', value: b.name, checked: takes, disabled: !takes});
        if (takes) boxes.push(box);
        const where = [b.current ? 'checked out here' : '', b.name === f.agent_branch ? 'the agent is on it' : ''].filter(Boolean).join(', ');
        const row = el('label', {className: 'check agent-branch'}, box,
            el('span', {className: 'name', textContent: b.name}),
            el('span', {textContent: ` — ${AGENT_STATES[b.state]?.(b) ?? b.state}${where ? ` (${where})` : ''}`}));
        if (!b.commits?.length && !b.changes?.length) return [row];
        const lines = [...(b.commits ?? []).map(c => `${c.hash} ${c.subject}`), '',
            ...(b.changes ?? []).map(c => `${sign[c.op] ?? '~'} ${c.path}`)];
        return [row, el('pre', {className: 'changes branch-changes', textContent: lines.join('\n') + (b.more ? '\n…' : '')})];
    });
    if (rows.length) parts.push(el('p', {textContent: 'Agent branches:'}), el('div', {className: 'agent-branches'}, ...rows));
    else parts.push(el('p', {textContent: 'The agent copy has no branches.'}));
    if (f.more_branches) parts.push(el('p', {className: 'hint', textContent:
        `Only the first ${f.agent.length} branches are compared; the others are fetched as ${f.branches[0].replace(/\/[^/]*$/, '')}/… too.`}));
    if (boxes.length) {
        parts.push(el('p', {className: 'hint', textContent:
            'Ticked branches are created here or fast-forwarded; you stay on the branch you are on.'}));
        okBtn.textContent = 'Take over ticked branches';
        okBtn.hidden = false;
        okBtn.focus();
    } else if (rows.length) {
        parts.push(el('p', {textContent: 'Nothing to take over.'}));
    }
    const diverged = (f.agent ?? []).filter(b => b.state === 'diverged');
    if (diverged.length) {
        parts.push(el('p', {textContent: 'For a branch where both sides have commits, for example:'}),
            el('pre', {className: 'changes', textContent: diverged.map(b => `git checkout ${shq(b.name)} && git merge ${shq(b.agent_ref)}`).join('\n')}));
    }
    if (f.agent_dirty) parts.push(el('p', {className: 'bad', textContent:
        `${f.agent_dirty} file(s) on the agent host have uncommitted changes; they are not included. ` +
        'Commit them there first if you want them.'}));
    body.replaceChildren(...parts);
    const ok = await done;
    if (ok) s.tab('files')?.view?.refresh();
    return ok;
}

// folderDialog adds a folder (in parent, '' = the top) or renames oldName
// (its last part; it stays where it is).
async function folderDialog(oldName = '', parent = '') {
    const dlg = $('#dlg-prompt');
    const f = dlg.querySelector('form').elements;
    if (oldName) parent = oldName.includes('/') ? oldName.slice(0, oldName.lastIndexOf('/')) : '';
    const name = oldName.slice(oldName.lastIndexOf('/') + 1);
    dlg.querySelector('h2').textContent = oldName ? `Rename folder “${name}”` : parent ? `New folder in “${parent}”` : 'New folder';
    dlg.querySelector('.hint').textContent = 'Folders group sessions in the sidebar, e.g. “Work” and “Private”. ' +
        'A / makes subfolders: “Clients/Acme”.';
    f.value.value = name;
    f.value.placeholder = 'Folder name';
    const path = () => parent ? `${parent}/${f.value.value}` : f.value.value;
    const ok = await runDialog(dlg, () => SaveGroup(oldName, path()), f.value);
    if (ok) await app.reload();
}

// pageDialogs are the notes pages' dialogs (new, rename, delete).
const pageDialogs = {
    async prompt({title, hint, value, ok, submit}) {
        const dlg = $('#dlg-prompt');
        const f = dlg.querySelector('form').elements;
        const okBtn = dlg.querySelector('button[value=ok]');
        dlg.querySelector('h2').textContent = title;
        dlg.querySelector('.hint').textContent = hint;
        f.value.value = value;
        f.value.placeholder = 'Page name';
        okBtn.textContent = ok;
        try {
            return await runDialog(dlg, () => submit(f.value.value), f.value);
        } finally {
            okBtn.textContent = 'Save';
        }
    },
    confirm: confirmDialog,
};

async function deleteFolder(f) {
    const n = f.count;
    const up = f.parent.group ? `in “${f.parent.group}”` : 'without a folder';
    const subs = f.children.length ? ' Its subfolders go too.' : '';
    const ok = await confirmDialog({
        title: `Delete folder “${f.group}”?`,
        text: n ? `Its ${n} session(s) stay, ${up}.${subs}` : `The folder has no sessions.${subs}`,
        ok: 'Delete folder',
        action: () => DeleteGroup(f.group),
    });
    if (ok) await app.reload();
}

// renderRequirements shows a requirements check (CheckRequirements) in box.
function renderRequirements(box, c) {
    if (!c.exists) {
        box.replaceChildren(el('p', {className: 'hint', textContent:
            'The project has no requirements list (session menu → Requirements…).'}));
        return;
    }
    const rows = (c.results ?? []).map(r => el('li', {className: r.ok ? 'req-ok' : 'req-bad'},
        el('span', {className: 'req-mark', textContent: r.ok ? '✓ ' : '✗ '}),
        `${r.name}${r.op ? ` ${r.op} ${r.version}` : ''}: ` +
        (r.ok ? (r.have ? `has ${r.have}` : 'installed') : r.problem)));
    const head = c.results?.length
        ? (c.missing ? `On ${c.host}, ${c.missing} of ${c.results.length} missing or too old:` : `On ${c.host}, all ${c.results.length} present:`)
        : 'The list has no requirements.';
    box.replaceChildren(el('p', {textContent: head}), el('ul', {className: 'req-list'}, ...rows),
        ...(c.errors ?? []).map(e => el('p', {className: 'error-text', textContent: e})));
}

// requirementsDialog edits the project's .session-workbench/requirements
// (SPEC F16) and checks it on the session's agent host.
async function requirementsDialog(s) {
    const dlg = $('#dlg-reqs');
    if (dlg.open) return;
    let file;
    try {
        file = await Requirements(s.id);
    } catch (err) {
        return confirmDialog({title: 'Requirements', text: err?.message ?? String(err), ok: null});
    }
    const f = dlg.querySelector('form').elements;
    const results = dlg.querySelector('.req-results');
    dlg.querySelector('h2').textContent = `Requirements of “${s.data.name}”`;
    dlg.querySelector('.reqs-where').textContent = `${file.path}${file.exists ? '' : ' (not made yet: saving makes it)'} — ` +
        'one command per line, optionally with a version (go >= 1.26). It goes with the project (git), and Switch host… checks it on the new host.';
    f.text.value = file.text;
    results.replaceChildren();
    const check = dlg.querySelector('button[name=check]');
    check.textContent = `Check on ${s.data.host}`;
    check.onclick = async () => {
        if (s.data.inactive) return results.replaceChildren(el('p', {textContent: 'The session is inactive: nothing is checked on its host.'}));
        check.disabled = true;
        results.replaceChildren(el('p', {textContent: `Checking on ${s.data.host}…`}));
        try {
            renderRequirements(results, await CheckRequirements(s.id, '', f.text.value));
        } catch (err) {
            results.replaceChildren(el('p', {className: 'error-text', textContent: err?.message ?? String(err)}));
        } finally {
            check.disabled = false;
        }
    };
    await runDialog(dlg, () => SaveRequirements(s.id, f.text.value).then(p => toast(`Saved ${p}`)), f.text);
}

// switchDialog moves a session to another agent host (SPEC F16): its
// Claude Code conversations and memory go along (newer wins, nothing is
// deleted); the requirements are checked there first. A working copy is
// fetched from the old host first (Get from agent); the new host's Remote
// tab then offers Send to agent.
async function switchDialog(s) {
    const dlg = $('#dlg-hostswitch');
    if (dlg.open) return;
    let targets;
    try {
        targets = await SwitchTargets(s.id);
    } catch (err) {
        return confirmDialog({title: 'Switch host', text: err?.message ?? String(err), ok: null});
    }
    const f = dlg.querySelector('form').elements;
    const results = dlg.querySelector('.req-results');
    const out = dlg.querySelector('.build-output');
    const old = s.data.host;
    dlg.querySelector('h2').textContent = `Switch “${s.data.name}” to another host`;
    dlg.querySelector('.switch-intro').textContent = `It is on ${old} now. Its Claude Code conversations and memory for this ` +
        'project go along (files the new host lacks or has older; nothing is deleted there), and so do its trust and allowed ' +
        'tools. Notes, Q&A and recordings stay as they are (they are on this computer).';
    const usable = targets.filter(t => !t.problem);
    f.host.replaceChildren(...targets.map(t => el('option', {value: t.name, disabled: !!t.problem,
        textContent: t.name + (t.previous ? '  (worked there before)' : '') + (t.problem ? `  — ${t.problem}` : '')})));
    f.host.value = usable[0]?.name ?? '';
    dlg.querySelector('.endold-text').textContent = s.data.tmux
        ? `End tmux session ${s.data.tmux_name} on ${old} first (stops claude there, which would go on writing)` : '';
    f.endold.closest('label').hidden = !s.data.tmux;
    f.endold.checked = !!s.data.tmux;
    const sync = s.data.files === 'sync';
    dlg.querySelector('.getfirst').hidden = !sync;
    dlg.querySelector('.getfirst-text').textContent = `Get the working copy's changes from ${old} first (Get from agent)`;
    f.getfirst.checked = sync;
    out.hidden = true;
    out.textContent = '';
    const where = dlg.querySelector('.switch-where');
    let seq = 0;
    const show = async () => {
        const t = targets.find(t => t.name === f.host.value);
        if (!t) {
            where.textContent = 'No other host can take this session (Add host… first).';
            return results.replaceChildren();
        }
        const mode = {shared: 'shared folder', sshfs: 'live share', sync: 'working copy'}[t.files] ?? t.files;
        where.textContent = `There: ${t.remote_path} (${mode})` +
            (t.files === 'sync' ? ' — the Remote tab offers Send to agent when the copy is not there yet.' : '.');
        const n = ++seq;
        results.replaceChildren(el('p', {textContent: `Checking the requirements on ${t.name}…`}));
        const c = await CheckRequirements(s.id, t.name, '').catch(err => ({error: err?.message ?? String(err)}));
        if (n !== seq) return;
        if (c.error) results.replaceChildren(el('p', {className: 'error-text', textContent: `Requirements not checked: ${c.error}`}));
        else renderRequirements(results, c);
    };
    f.host.onchange = show;
    show();
    const ok = await runDialog(dlg, async () => {
        const host = f.host.value;
        if (!host) throw new Error('There is no host to switch to.');
        if (sync && f.getfirst.checked) {
            await getFromAgent(s); // its own window, on top of this one
            if (!await confirmDialog({title: `Switch to ${host} now?`, text: `The working copy's changes from ${old} are taken (or ` +
                'you chose not to). Switch the session now?', ok: 'Switch'})) throw new Error('Not switched.');
        }
        s.tab('remote')?.dropView(); // its terminal is on the old host
        out.hidden = false;
        out.textContent = '';
        EventsOn(`switch:${s.id}`, chunk => {
            out.textContent += chunk.replaceAll('\r\n', '\n');
            out.scrollTop = out.scrollHeight;
        });
        try {
            const rep = await SwitchHost(s.id, host, f.endold.checked);
            const what = rep.found ? `${rep.conversations} conversations, ${rep.files} files (${rep.memory} memory)` +
                (rep.config ? ', settings merged' : '') : 'no Claude state to copy';
            toast(`“${s.data.name}” is on ${host} now: ${what}` + (rep.notes?.length ? ` — ${rep.notes.join('; ')}` : ''));
        } finally {
            EventsOff(`switch:${s.id}`);
        }
    }, f.host);
    if (!ok) return;
    await app.reload();
    if (app.active === s) s.show();
}

// stopClaude stops the session's claude now (SPEC F17), after asking when
// it is busy; it resumes when the session is opened again, or with Resume
// claude.
async function stopClaude(s) {
    let res = await StopClaude(s.id, false).catch(err => toast(err?.message ?? String(err), true));
    if (!res) return;
    if (res.busy) {
        if (!await confirmDialog({title: `Stop claude in “${s.data.name}”?`,
            text: `${res.busy}. Stopping ends that; what it was doing is lost (the conversation so far is kept).`,
            ok: 'Stop claude'})) return;
        res = await StopClaude(s.id, true).catch(err => toast(err?.message ?? String(err), true));
    }
    if (res?.stopped) toast(`claude stopped in “${s.data.name}” (Resume claude, or open the session again)`);
}

// resumeClaude resumes a stopped claude (SPEC F17).
async function resumeClaude(s) {
    await ResumeClaude(s.id).catch(err => toast(err?.message ?? String(err), true));
}

// costPage fills Settings → Cost (SPEC F18): the settings, the warning
// limits (empty = none), the subscription usage and a table of each
// session's cost with their sum and the top bar's figures.
function costPage(dlg, f, ui) {
    const page = dlg.querySelector('.settings-page[data-page="cost"]');
    f.cost_daily_limit.value = ui.cost_daily_limit || '';
    f.usage_5h_limit.value = ui.usage_5h_limit || '';
    f.usage_7d_limit.value = ui.usage_7d_limit || '';
    const fill = () => {
        const s = app.cost;
        const now = Date.now() / 1000;
        page.querySelector('.usage-text').textContent = usageText(s?.usage, now);
        const {rows, sum} = costRows(app.state.sessions, s);
        const td = (text, className = '') => el('td', {textContent: text, className});
        const row = (cells, className = '') => el('tr', {className}, ...cells);
        page.querySelector('.cost-table tbody').replaceChildren(...rows.map(r =>
            row([td(r.name), td(r.host), td(dollars(r.today), 'num'), td(dollars(r.total), 'num')])));
        page.querySelector('.cost-table tfoot').replaceChildren(
            row([td('Sum'), td(''), td(dollars(sum.today), 'num'), td(dollars(sum.total), 'num')], 'sum'),
            row([td('Top bar'), td(s?.since ? `total since ${s.since}` : ''), td(dollars(s?.today), 'num'), td(dollars(s?.total), 'num')], 'top'));
    };
    fill();
    page.querySelector('button[name=reset_cost]').onclick = () => confirmDialog({
        title: 'Reset the total cost',
        text: 'The top bar\'s total counts again from now. Today\'s cost and each session\'s stay as they are.',
        ok: 'Reset',
        action: async () => {
            await ResetCost();
            app.cost = await CostSummary().catch(() => app.cost);
            fill();
        },
    });
}

// fillIdleChoices fills an idle time select (SPEC F17; see idle.js).
function fillIdleChoices(sel, value, first) {
    sel.replaceChildren(...idleOptions(value, first, first !== 'never').map(o => el('option', {value: o.value, textContent: o.text})));
    sel.value = value;
}

// setSessionActive activates or deactivates a session (no questions).
async function setSessionActive(s, active) {
    try {
        await SetSessionActive(s.id, active);
    } catch (err) {
        return toast(err?.message ?? String(err), true);
    }
    if (!active) s.dropTerminals();
    await app.reload();
    if (active) toast(`“${s.data.name}” is active again`);
}

// deactivateSession keeps a session for later: its tabs disconnect and it
// connects nothing until it is activated. It asks whether to end what runs
// for it (tmux and claude on the agent host, or its container).
async function deactivateSession(s) {
    const box = s.data.files === 'container';
    const tm = !box && !!s.data.tmux;
    const check = box ? [`Also stop its container sw-${s.id} and its services (its tmux session and claude end)`]
        : tm ? [`Also end tmux session ${s.data.tmux_name} on ${s.data.host} (stops claude there)`] : [];
    const ok = await confirmDialog({
        title: `Deactivate session “${s.data.name}”?`,
        text: 'It stays in the list, marked inactive, and connects nothing — not when the app starts and not when you ' +
            'open it — until you activate it again (session menu → Activate session). Its tabs disconnect now; ' +
            'Notes, Files, Web and Q&A keep working, but no live share is mounted and no notifications come.' +
            (check.length ? ' What runs for it keeps running unless you tick below.' : ''),
        ok: 'Deactivate',
        check,
        checked: true, // ending tmux (and claude) frees the agent host; untick to keep them
        action: async ticks => {
            await SetSessionActive(s.id, false);
            s.dropTerminals();
            if (tm && ticks[0]) await KillRemote(s.id);
            if (box && ticks[0]) await ContainerStop(s.id);
        },
    });
    if (!ok) return;
    await app.reload();
    toast(`“${s.data.name}” is inactive`);
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
    // A working copy's saved agent copy (Full ▾) can go with it; it is kept unless ticked.
    const saved = await SavedCopy(s.id).catch(() => ({exists: false}));
    // Without tmux nothing keeps running on the host: nothing to end.
    const tm = !!s.data.tmux;
    const check = tm ? [`Also end tmux session ${s.data.tmux_name} on ${s.data.host} (stops claude there)`] : [];
    const box = s.data.files === 'container';
    // A compose project: its services go with the container; their data
    // (volumes, e.g. a database) only when ticked.
    const compose = box ? await ContainerCompose(s.id).catch(() => null) : null;
    const services = !!(compose?.exists || compose?.services?.length);
    if (box) {
        check.push(`Also remove its container and image sw-${s.id}` + (services ? ' and its services\' containers' : '') +
            (s.data.own_claude_volume ? ', and its own Claude Code login volume' : ''));
        if (services) check.push('Also delete its services\' data volumes (databases and the like; needs the tick above)');
    }
    if (saved.exists) {
        const n = saved.copies.length;
        check.push(`Also delete its ${n > 1 ? `${n} saved agent copies` : 'saved agent copy'} (the newest from ${savedWhen(saved.latest.when)})`);
    }
    const ok = await confirmDialog({
        title: `Delete session “${s.data.name}”?`,
        text: 'Removes it from the list. Its files are not touched, and its notes are kept' +
            (saved.exists ? '; so are its saved agent copies unless you tick it below.' : '.'),
        ok: 'Delete',
        check,
        action: async ticks => {
            const o = tm ? 1 : 0;
            const kill = tm && ticks[0];
            const dropBox = box && ticks[o];
            const dropData = dropBox && services && ticks[o + 1];
            const dropSaved = saved.exists && ticks[o + (box ? 1 : 0) + (services ? 1 : 0)];
            // First the terminals, so that no auto-reconnect makes the
            // container (and its services) again while they are removed.
            if (kill || dropBox) s.disconnect();
            if (kill) await KillRemote(s.id);
            if (dropBox) await ContainerRemove(s.id, !!s.data.own_claude_volume, dropData);
            await DeleteSession(s.id);
            if (dropSaved) await RemoveSaved(s.id);
        },
    });
    if (!ok) return;
    const i = app.order.indexOf(s);
    await app.reload();
    if (!app.active) app.select(app.order[Math.min(i, app.order.length - 1)] || null);
}

// stopContainer stops a container session's container, after asking (what
// runs in its tmux ends; the Remote tab starts it again when it connects).
async function stopContainer(s) {
    const ok = await confirmDialog({
        title: `Stop the container of “${s.data.name}”?`,
        text: `Stops sw-${s.id} and its services (compose), if any: its tmux session and what runs there (claude) end. ` +
            'Its files, the services\' data and the Claude Code login stay; the Remote tab starts them again when it connects.',
        ok: 'Stop container',
        action: async () => {
            await ContainerStop(s.id);
            s.disconnect();
        },
    });
    if (ok) toast(`Container sw-${s.id} stopped`);
}

// dockerfileDialog edits a container session's Dockerfile: the add-on
// buttons, Save, and Save and rebuild with the build's output. The window
// can be moved (by its heading) and resized (its corner); the text and the
// output have the right-click Cut/Copy/Paste menu.
let dockerfileWindow;
async function dockerfileDialog(s) {
    const dlg = $('#dlg-dockerfile');
    if (dlg.open) return;
    if (!dockerfileWindow) {
        dockerfileWindow = movable(dlg);
        for (const field of [dlg.querySelector('.dockerfile-text'), dlg.querySelector('.build-output')]) {
            field.addEventListener('contextmenu', e => {
                e.preventDefault();
                e.stopPropagation();
                textMenu(e.clientX, e.clientY, field);
            });
        }
    }
    let file;
    try {
        file = await ContainerDockerfile(s.id);
    } catch (err) {
        return confirmDialog({title: 'Dockerfile', text: err?.message ?? String(err), ok: null});
    }
    const f = dlg.querySelector('form').elements;
    const out = dlg.querySelector('.build-output');
    const status = dlg.querySelector('.build-status');
    dlg.querySelector('h2').textContent = `Dockerfile of “${s.data.name}”`;
    f.text.value = file.text;
    f.where.value = file.in_repo ? 'repo' : 'app';
    const where = () => dlg.querySelector('.where-hint').textContent = f.where.value === 'repo'
        ? `Saved in the repository: ${s.data.local_path}/.session-workbench/Dockerfile (versioned with the project).`
        : file.in_repo
            ? 'Saved in the app\'s data folder — but the repository\'s .session-workbench/Dockerfile is used while it exists.'
            : `Saved in the app's data folder (${file.path}).`;
    f.where.onchange = where;
    where();
    status.textContent = `Container sw-${s.id}: ${file.state === 'none' ? 'not made yet' : file.state}.`;
    out.textContent = '';
    out.hidden = true;
    dlg.querySelector('.toolchains').replaceChildren(...Object.entries(TOOLCHAINS).map(([name, lines]) => {
        const b = el('button', {type: 'button', textContent: '+ ' + name, title: lines});
        b.onclick = () => {
            f.text.value = addToolchain(f.text.value, lines, name);
            f.text.focus();
        };
        return b;
    }));
    const save = () => SaveContainerDockerfile(s.id, f.text.value, f.where.value === 'repo');
    let building = false;
    const rebuild = dlg.querySelector('button[name=rebuild]');
    rebuild.onclick = async () => {
        if (building) return ContainerStopBuild(s.id);
        if (file.state === 'running' && !await confirmDialog({
            title: 'Rebuild and replace the container?',
            text: `Builds the image again, then replaces the running container sw-${s.id}: its tmux session and what ` +
                'runs there (claude) end. Your files and the Claude Code login stay.',
            ok: 'Rebuild',
        })) return;
        try {
            await save();
        } catch (err) {
            return status.textContent = err?.message ?? String(err);
        }
        building = true;
        rebuild.textContent = 'Stop build';
        out.hidden = false;
        out.textContent = '';
        status.textContent = 'Building…';
        EventsOn(`container:build:${s.id}`, chunk => {
            out.textContent += chunk.replaceAll('\r\n', '\n');
            out.scrollTop = out.scrollHeight;
        });
        try {
            await ContainerRebuild(s.id);
            status.textContent = 'Built. The Remote tab starts the new container when it connects.';
            file.state = 'none';
            s.disconnect();
        } catch (err) {
            const msg = err?.message ?? String(err);
            const line = /line (\d+)/i.exec(msg);
            status.textContent = `The build failed: ${msg}` + (line ? ` (Dockerfile line ${line[1]})` : '');
        } finally {
            EventsOff(`container:build:${s.id}`);
            building = false;
            rebuild.textContent = 'Save and rebuild';
        }
    };
    await runDialog(dlg, () => save().then(p => toast(`Saved ${p}`)), f.text, () => dockerfileWindow.pin());
    if (building) ContainerStopBuild(s.id);
}

// servicesDialog edits a container session's compose file (SPEC C7): the
// services next to its dev container. The add-on buttons, Save (compose
// checks the file first), and Save and restart services with compose's
// output. Moves and resizes like the Dockerfile window.
let servicesWindow;
async function servicesDialog(s) {
    const dlg = $('#dlg-services');
    if (dlg.open) return;
    if (!servicesWindow) {
        servicesWindow = movable(dlg);
        for (const field of [dlg.querySelector('.dockerfile-text'), dlg.querySelector('.build-output')]) {
            field.addEventListener('contextmenu', e => {
                e.preventDefault();
                e.stopPropagation();
                textMenu(e.clientX, e.clientY, field);
            });
        }
    }
    let file;
    try {
        file = await ContainerCompose(s.id);
    } catch (err) {
        return confirmDialog({title: 'Services', text: err?.message ?? String(err), ok: null});
    }
    const f = dlg.querySelector('form').elements;
    const out = dlg.querySelector('.build-output');
    const status = dlg.querySelector('.build-status');
    const errBox = dlg.querySelector('.error');
    dlg.querySelector('h2').textContent = `Services of “${s.data.name}”`;
    f.text.value = file.text;
    f.where.value = file.in_repo ? 'repo' : 'app';
    const repoPath = `${s.data.local_path}/.session-workbench/compose.yaml`;
    const where = () => dlg.querySelector('.where-hint').textContent = f.where.value === 'repo'
        ? `Saved in the repository: ${repoPath} (versioned with the project; its relative paths and a .env file there count from that folder).`
        : file.exists && file.in_repo
            ? 'Saved in the app\'s data folder — but the repository\'s .session-workbench/compose.yaml is used while it exists.'
            : 'Saved in the app\'s data folder (only for you).';
    f.where.onchange = where;
    where();
    const describe = () => {
        const svc = file.services?.length ? ` Service containers: ${file.services.join(', ')}.` : '';
        status.textContent = file.problem ? `Compose cannot run: ${file.problem}`
            : (file.exists ? `Container sw-${s.id}: ${file.state === 'none' ? 'not made yet' : file.state}.${svc}`
                : 'No services yet: the container runs on its own. Saving makes it a Compose project with the dev container as service "dev".') +
            ` (${file.compose})`;
    };
    describe();
    out.textContent = '';
    out.hidden = true;
    errBox.textContent = '';
    dlg.querySelector('.toolchains').replaceChildren(...Object.entries(SERVICES).map(([label, svc]) => {
        const b = el('button', {type: 'button', textContent: '+ ' + label, title: svc.lines});
        b.onclick = () => {
            const name = svc.lines.split(':')[0];
            if (hasService(f.text.value, name)) return toast(`There is a service “${name}” already`);
            f.text.value = addService(f.text.value, label);
            f.text.focus();
        };
        return b;
    }));
    const save = async () => {
        errBox.textContent = '';
        const p = await SaveContainerCompose(s.id, f.text.value, f.where.value === 'repo');
        file.exists = true;
        file.in_repo = f.where.value === 'repo';
        return p;
    };
    let applying = false;
    const apply = dlg.querySelector('button[name=apply]');
    apply.onclick = async () => {
        if (applying) return ContainerStopBuild(s.id);
        try {
            await save();
        } catch (err) {
            return errBox.textContent = err?.message ?? String(err);
        }
        if (file.state !== 'running') {
            return status.textContent = 'Saved. The services start with the container when the Remote tab connects.';
        }
        if (!await confirmDialog({
            title: 'Restart the services?',
            text: 'Services that changed are made again and removed ones stop. When you changed the "dev" part, the dev ' +
                `container sw-${s.id} is made again too: its tmux session and what runs there (claude) end.`,
            ok: 'Restart services',
        })) return status.textContent = 'Saved; the running services are unchanged until the container starts again.';
        applying = true;
        apply.textContent = 'Stop';
        out.hidden = false;
        out.textContent = '';
        status.textContent = 'Starting the services…';
        EventsOn(`container:compose:${s.id}`, chunk => {
            out.textContent += chunk.replaceAll('\r\n', '\n');
            out.scrollTop = out.scrollHeight;
        });
        try {
            await ContainerComposeApply(s.id);
            const now = await ContainerCompose(s.id).catch(() => null);
            if (now) Object.assign(file, {state: now.state, services: now.services});
            describe();
            status.textContent = 'Services restarted. ' + status.textContent;
        } catch (err) {
            status.textContent = `Starting the services failed: ${err?.message ?? String(err)}`;
        } finally {
            EventsOff(`container:compose:${s.id}`);
            applying = false;
            apply.textContent = 'Save and restart services';
        }
    };
    await runDialog(dlg, () => save().then(p => toast(`Saved ${p}`)), f.text, () => servicesWindow.pin());
    if (applying) ContainerStopBuild(s.id);
}

// exportDialog asks which parts of the session go into an archive (notes,
// Dockerfile and settings always), and its format; Export… then asks where.
async function exportDialog(s) {
    const dlg = $('#dlg-export');
    if (dlg.open) return;
    let parts;
    try {
        parts = await ExportParts(s.id);
    } catch (err) {
        return confirmDialog({title: 'Export', text: err?.message ?? String(err), ok: null});
    }
    const f = dlg.querySelector('form').elements;
    dlg.querySelector('h2').textContent = `Export “${s.data.name}”`;
    const status = dlg.querySelector('.export-status');
    status.textContent = '';
    const amount = p => p.files < 0 ? '' : p.files === 0 ? ' — nothing yet' : ` — ${p.files} file${p.files === 1 ? '' : 's'}, ${fmtSize(p.size)}`;
    dlg.querySelector('.export-parts').replaceChildren(...parts.map(p => {
        const box = el('input', {type: 'checkbox', name: 'part', value: p.id, checked: p.always || p.default, disabled: p.always});
        return el('label', {className: 'check', title: p.note},
            box, ` ${p.label}${p.always ? ' (always)' : ''}${amount(p)}`,
            ...(p.note ? [el('span', {className: 'part-note', textContent: p.note})] : []));
    }));
    f.kind.value = store.get('exportKind', 'zip');
    const ok = await runDialog(dlg, async () => {
        const chosen = [...dlg.querySelectorAll('input[name=part]:checked:not(:disabled)')].map(b => b.value);
        store.set('exportKind', f.kind.value);
        status.textContent = 'Exporting…';
        let res;
        try {
            res = await ExportSession(s.id, chosen, f.kind.value);
        } finally {
            status.textContent = '';
        }
        if (!res.file) throw new Error('Not exported: no file chosen.');
        const extra = [...(res.missing ?? []), ...(res.skipped ? [`${res.skipped} symlink(s) or special file(s) left out`] : [])];
        toast(`Exported to ${res.file}` + (extra.length ? ` — missing: ${extra.join('; ')}` : ''), extra.length > 0);
    }, dlg.querySelector('button[value=ok]'));
    return ok;
}

// importDialog opens an archive, then asks for the host (the archive's
// suggested, or added from it), the directory and the name of the new
// session.
async function importDialog() {
    const dlg = $('#dlg-import');
    if (dlg.open) return;
    let info;
    try {
        info = await ImportOpen();
    } catch (err) {
        return confirmDialog({title: 'Import session', text: err?.message ?? String(err), ok: null});
    }
    if (!info) return;
    const m = info.manifest;
    const f = dlg.querySelector('form').elements;
    const when = m.exported ? new Date(m.exported).toLocaleString() : '';
    const PARTS = {notes: 'notes', dockerfile: 'the Dockerfile', qa: 'Q&A', recordings: 'recordings',
        history: 'shell history', copies: 'saved agent copies', project: 'the project files'};
    dlg.querySelector('.import-about').textContent = `“${m.session.name}” from ${info.path.split(/[\\/]/).pop()}` +
        ` (exported ${when} by Session Workbench ${m.app}). In it: ${(m.parts ?? []).map(p => PARTS[p] ?? p).join(', ')}.` +
        (m.missing?.length ? ` Missing at export: ${m.missing.join('; ')}.` : '');
    const ADD = '\u0000add';
    f.host.replaceChildren(
        ...app.state.hosts.map(h => el('option', {value: h.name, textContent: h.name, selected: h.name === m.host.name})),
        ...(info.host_exists ? [] : [el('option', {value: ADD, selected: true,
            textContent: `Add “${m.host.name}” from the archive (${m.host.kind === 'container' ? 'containers' : m.host.ssh_target || 'ssh'})`})]));
    f.dir.value = m.session.local_path ?? '';
    f.name.value = m.session.name;
    f.dir.closest('label').hidden = info.plain; // a plain login has no directory
    const hasProject = (m.parts ?? []).includes('project');
    dlg.querySelector('.import-dir-hint').textContent = info.plain
        ? `A plain ssh login: no directory; its shell starts in ${m.session.remote_dir || 'the home folder'}.`
        : hasProject
        ? 'The project files are unpacked here: the directory must be empty or new.'
        : 'The archive has no project files: an existing directory is used as it is, a new one is created.';
    dlg.querySelector('.import-notes').textContent = info.not_imported?.length
        ? `Not imported (they would run commands on this computer): ${info.not_imported.join(', ')}.` : '';
    f.browse.onclick = async () => {
        const dir = await BrowseDirectory(f.dir.value);
        if (dir) f.dir.value = dir;
    };
    let result;
    const ok = await runDialog(dlg, async () => {
        const add = f.host.value === ADD;
        result = await ImportSession({path: info.path, host: add ? m.host.name : f.host.value, add_host: add,
            local_path: f.dir.value, name: f.name.value});
    }, info.plain ? f.name : f.dir);
    if (!ok) return;
    await app.reload();
    app.select(app.sessions.get(result.session.id));
    const notes = result.notes ?? [];
    toast(`Imported “${result.session.name}”` + (notes.length ? ` — ${notes.join('; ')}` : ''), notes.length > 0);
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
