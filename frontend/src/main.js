import '@xterm/xterm/css/xterm.css';
import './style.css';

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebLinksAddon} from '@xterm/addon-web-links';

import {Session, StartLocal, StartRemote, TermResize, TermWrite} from '../wailsjs/go/main/App';
import {BrowserOpenURL, ClipboardGetText, ClipboardSetText, EventsOff, EventsOn} from '../wailsjs/runtime/runtime';

const isMac = navigator.platform.toUpperCase().includes('MAC');

function decode(b64) {
    const s = atob(b64);
    const bytes = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) bytes[i] = s.charCodeAt(i);
    return bytes;
}

// App-level shortcuts: Ctrl+Shift+<digit> (Cmd+Shift+<digit> on macOS)
// selects a tab. Returns the tab index or -1.
function tabShortcut(e) {
    const mod = isMac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey;
    if (!mod || !e.shiftKey || e.altKey) return -1;
    const m = /^Digit([1-9])$/.exec(e.code);
    return m ? Number(m[1]) - 1 : -1;
}

// TermView binds one xterm.js instance to a backend pty. start is a
// function (cols, rows) => Promise<id>, so the same view hosts local shells
// and ssh connections.
class TermView {
    constructor(container, start, exitHint) {
        this.start = start;
        this.exitHint = exitHint;
        this.id = null;
        this.term = new Terminal({
            fontFamily: '"DejaVu Sans Mono", "Cascadia Mono", Menlo, monospace',
            fontSize: 13,
            scrollback: 10000,
            cursorBlink: true,
            theme: {background: '#1e1e1e'},
        });
        this.fit = new FitAddon();
        this.term.loadAddon(this.fit);
        this.term.loadAddon(new WebLinksAddon((e, uri) => BrowserOpenURL(uri)));
        this.term.open(container);
        this.term.attachCustomKeyEventHandler(e => this.handleKey(e));
        this.term.onData(d => this.id && TermWrite(this.id, d));
        this.term.onResize(({cols, rows}) => this.id && TermResize(this.id, cols, rows));
        new ResizeObserver(() => this.fit.fit()).observe(container);
        this.fit.fit();
    }

    async spawn() {
        this.exited = false;
        try {
            const id = await this.start(this.term.cols, this.term.rows);
            this.id = id;
            EventsOn(`term:data:${id}`, b64 => this.term.write(decode(b64)));
            EventsOn(`term:exit:${id}`, info => this.onExit(info));
        } catch (err) {
            this.term.write(`\x1b[31m${err}\x1b[0m\r\n`);
            this.onExit(null);
        }
    }

    onExit(info) {
        if (this.id) EventsOff(`term:data:${this.id}`, `term:exit:${this.id}`);
        this.id = null;
        this.exited = true;
        const why = !info ? '' : info.error ? `: ${info.error}` : ` with code ${info.code}`;
        this.term.write(`\r\n\x1b[2m[${this.exitHint.what} exited${why} — press Enter to ${this.exitHint.again}]\x1b[0m\r\n`);
    }

    // Returns false for keys we consume so xterm does not also handle them.
    handleKey(e) {
        if (e.type !== 'keydown') return true;
        if (tabShortcut(e) >= 0) return false; // handled by the window listener
        if (this.exited && e.key === 'Enter') {
            this.term.reset();
            this.spawn();
            return false;
        }
        const mod = isMac ? e.metaKey : (e.ctrlKey && e.shiftKey);
        if (mod && e.code === 'KeyC' && this.term.hasSelection()) {
            ClipboardSetText(this.term.getSelection());
            return false;
        }
        if (mod && e.code === 'KeyV') {
            e.preventDefault();
            ClipboardGetText().then(t => t && this.term.paste(t));
            return false;
        }
        return true;
    }

    focus() {
        this.fit.fit();
        this.term.focus();
    }
}

// Tab is a lazily created terminal pane.
class Tab {
    constructor(label, start, exitHint) {
        this.label = label;
        this.start = start;
        this.exitHint = exitHint;
        this.pane = document.createElement('div');
        this.pane.className = 'pane';
        this.pane.hidden = true;
        document.getElementById('panes').append(this.pane);
        this.view = null;
    }

    show() {
        this.pane.hidden = false;
        if (!this.view) {
            this.view = new TermView(this.pane, this.start, this.exitHint);
            this.view.spawn();
        }
        this.view.focus();
    }

    hide() {
        this.pane.hidden = true;
    }
}

async function init() {
    const info = await Session();
    document.getElementById('session-name').textContent = info.name;
    const pathEl = document.getElementById('session-path');
    if (info.error) {
        pathEl.textContent = info.error;
        pathEl.classList.add('error');
    } else if (info.hasRemote) {
        pathEl.textContent = `${info.target}:${info.remotePath}  ·  tmux ${info.tmuxName}`;
        pathEl.title = `local: ${info.localPath}`;
    } else {
        pathEl.textContent = info.localPath;
    }

    const tabs = [];
    if (info.hasRemote) tabs.push(new Tab('Remote', StartRemote, {what: 'ssh', again: 'reconnect'}));
    tabs.push(new Tab('Local', StartLocal, {what: 'shell', again: 'restart'}));

    const nav = document.getElementById('tabs');
    const keyHint = isMac ? '⇧⌘' : 'Ctrl+Shift+';
    let active = null;
    const select = i => {
        if (!tabs[i]) return;
        if (active && active !== tabs[i]) active.hide();
        active = tabs[i];
        nav.querySelectorAll('button').forEach((b, j) => b.classList.toggle('active', j === i));
        active.show();
    };
    tabs.forEach((t, i) => {
        const b = document.createElement('button');
        b.textContent = t.label;
        const k = document.createElement('kbd');
        k.textContent = keyHint + (i + 1);
        b.append(k);
        b.addEventListener('click', () => select(i));
        nav.append(b);
    });
    window.addEventListener('keydown', e => {
        const i = tabShortcut(e);
        if (i >= 0) {
            e.preventDefault();
            select(i);
        }
    });
    select(0);
}

init();
