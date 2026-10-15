import '@xterm/xterm/css/xterm.css';

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebLinksAddon} from '@xterm/addon-web-links';
import {SearchAddon} from '@xterm/addon-search';
import {Unicode11Addon} from '@xterm/addon-unicode11';

import {TermKill, TermResize, TermWrite} from '../wailsjs/go/main/App';
import {SgrFilter, termOptions} from './appearance';
import {BrowserOpenURL, ClipboardGetText, ClipboardSetText, EventsOff, EventsOn} from '../wailsjs/runtime/runtime';

export const isMac = navigator.platform.toUpperCase().includes('MAC');

const cap = s => s[0].toUpperCase() + s.slice(1);

function decode(b64) {
    const s = atob(b64);
    const bytes = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) bytes[i] = s.charCodeAt(i);
    return bytes;
}

// TermView binds one xterm.js instance to a backend pty.
//   start(cols, rows) => Promise<id> launches the process,
//   isAppKey(e) tells which keys belong to the app rather than the terminal,
//   onState('connected' | 'exited' | 'reconnecting') reports the process state.
//   exitHint: {what, again, remote, place(), autoReconnect()} decides what
//   happens when the process exits (overlay, automatic reconnect).
//   colorMode is 'full' or 'mono' (colors stripped from the output).
//   onActivity('data', nbytes) / onActivity('bell') reports output.
//   ui.copy_on_select copies the selection to the clipboard as it is made.
export class TermView {
    constructor(container, {start, exitHint, ui, colorMode, isAppKey, onState, onActivity}) {
        this.start = start;
        this.exitHint = exitHint;
        this.isAppKey = isAppKey;
        this.onState = onState || (() => {});
        this.onActivity = onActivity || (() => {});
        this.container = container;
        this.quietUntil = 0;
        this.id = null;
        // allowProposedApi: the unicode version switch is a proposed API.
        this.term = new Terminal({cursorBlink: true, allowProposedApi: true, ...termOptions(ui, colorMode)});
        this.setColorFilter(colorMode);
        this.copyOnSelect = !!ui.copy_on_select;
        this.fit = new FitAddon();
        this.term.loadAddon(this.fit);
        this.term.loadAddon(new WebLinksAddon((e, uri) => BrowserOpenURL(uri)));
        // Unicode 11 widths: emoji and other wide characters take two cells,
        // as in claude's output, so the cursor does not drift.
        this.term.loadAddon(new Unicode11Addon());
        this.term.unicode.activeVersion = '11';
        this.search = new SearchAddon();
        this.term.loadAddon(this.search);
        this.term.open(container);
        this.term.onSelectionChange(() => {
            if (this.copyOnSelect && this.term.hasSelection()) ClipboardSetText(this.term.getSelection());
        });
        this.term.attachCustomKeyEventHandler(e => this.handleKey(e));
        this.term.onBell(() => this.onActivity('bell'));
        this.term.onData(d => this.id && TermWrite(this.id, d));
        this.term.onResize(({cols, rows}) => this.id && this.resizePty(cols, rows));
        this.observer = new ResizeObserver(() => this.fit.fit());
        this.observer.observe(container);
        this.fit.fit();
    }

    async spawn() {
        this.exited = false;
        this.hideOverlay();
        this.connectedAt = Date.now();
        try {
            const id = await this.start(this.term.cols, this.term.rows);
            this.id = id;
            // Resizes while start() was in flight had no id to go to.
            this.fit.fit();
            this.resizePty(this.term.cols, this.term.rows);
            EventsOn(`term:data:${id}`, b64 => {
                const bytes = decode(b64);
                this.term.write(this.filter ? this.filter.filter(bytes) : bytes);
                if (Date.now() >= this.quietUntil) this.onActivity('data', bytes.length);
            });
            EventsOn(`term:exit:${id}`, info => this.onExit(info));
            this.onState('connected');
        } catch (err) {
            this.term.write(`\x1b[31m${err}\x1b[0m\r\n`);
            this.onExit(null);
        }
    }

    onExit(info) {
        if (this.id) EventsOff(`term:data:${this.id}`, `term:exit:${this.id}`);
        this.id = null;
        this.exited = true;
        if (this.disposed) return;
        const h = this.exitHint;
        const why = !info ? '' : info.error ? `: ${info.error}` : ` with code ${info.code}`;
        this.term.write(`\r\n\x1b[2m[${h.what} exited${why}]\x1b[0m\r\n`);

        const intentional = this.intentional;
        this.intentional = false;
        // ssh exits 255 for its own errors (connection refused or lost,
        // authentication); other codes come from the remote side, and 0 means
        // tmux detached or the session ended - nothing to retry.
        const lost = !info || info.code === 255 || info.code === -1 && !intentional;
        if (h.remote && lost && !intentional && h.autoReconnect()) {
            this.scheduleReconnect();
            return;
        }
        this.onState('exited');
        let msg;
        if (intentional) msg = 'Disconnected.';
        else if (!h.remote) msg = `Shell exited${why}.`;
        else if (info?.code === 0) msg = 'Remote session ended or detached.';
        else msg = `Connection to ${h.place()} ${lost ? 'failed or was lost' : 'closed'}${why}.`;
        this.showOverlay(msg, [{label: `${cap(h.again)} (Enter)`, primary: true, action: () => this.restart()}]);
    }

    // scheduleReconnect retries with backoff: 1, 2, 4, ... 30 s. A connection
    // that lasted a while starts over at 1 s.
    scheduleReconnect() {
        const lasted = Date.now() - this.connectedAt;
        this.attempt = lasted > 30000 ? 1 : (this.attempt || 0) + 1;
        let left = Math.min(30, 2 ** (this.attempt - 1));
        this.onState('reconnecting');
        // Quick failures (host unreachable, auth refused) never really connected.
        const what = lasted < 5000 ? `Could not connect to ${this.exitHint.place()}` : `Connection to ${this.exitHint.place()} lost`;
        const tick = () => {
            if (left <= 0) return this.restart();
            this.showOverlay(`${what} — retrying in ${left} s (attempt ${this.attempt}).`, [
                {label: 'Reconnect now (Enter)', primary: true, action: () => this.restart()},
                {label: 'Stop (Esc)', action: () => this.cancelReconnect()},
            ]);
            left--;
            this.reconnectTimer = setTimeout(tick, 1000);
        };
        tick();
    }

    cancelReconnect() {
        clearTimeout(this.reconnectTimer);
        this.reconnectTimer = null;
        this.attempt = 0;
        this.onState('exited');
        this.showOverlay(`Not connected to ${this.exitHint.place()}.`, [
            {label: 'Reconnect (Enter)', primary: true, action: () => this.restart()},
        ]);
    }

    restart() {
        clearTimeout(this.reconnectTimer);
        this.reconnectTimer = null;
        this.term.reset();
        this.spawn();
        this.term.focus();
    }

    showOverlay(text, buttons) {
        if (!this.overlay) {
            this.overlay = document.createElement('div');
            this.overlay.className = 'term-overlay';
            this.container.append(this.overlay);
        }
        const msg = document.createElement('span');
        msg.textContent = text;
        this.overlay.replaceChildren(msg, ...buttons.map(b => {
            const btn = document.createElement('button');
            btn.textContent = b.label;
            if (b.primary) btn.className = 'primary';
            btn.onmousedown = e => e.preventDefault(); // keep focus in the terminal
            btn.onclick = () => b.action();
            return btn;
        }));
        this.overlay.hidden = false;
    }

    hideOverlay() {
        if (this.overlay) this.overlay.hidden = true;
    }

    // openFind shows the search bar: Enter / Shift+Enter for next /
    // previous match, Esc closes it.
    openFind() {
        if (!this.findBar) {
            const input = document.createElement('input');
            input.placeholder = 'Find';
            input.spellcheck = false;
            const status = document.createElement('span');
            status.className = 'find-status';
            const btn = (label, title, fn) => {
                const b = document.createElement('button');
                b.textContent = label;
                b.title = title;
                b.onmousedown = e => e.preventDefault();
                b.onclick = fn;
                return b;
            };
            const find = (dir, incremental = false) => {
                const q = input.value;
                if (!q) {
                    this.search.clearDecorations();
                    status.textContent = '';
                    return;
                }
                const found = dir < 0 ? this.search.findPrevious(q) : this.search.findNext(q, {incremental});
                status.textContent = found ? '' : 'no match';
            };
            this.findBar = document.createElement('div');
            this.findBar.className = 'find-bar';
            this.findBar.append(input, status,
                btn('↑', 'Previous (Shift+Enter)', () => find(-1)),
                btn('↓', 'Next (Enter)', () => find(1)),
                btn('✕', 'Close (Esc)', () => this.closeFind()));
            input.addEventListener('input', () => find(1, true));
            input.addEventListener('keydown', e => {
                if (e.key === 'Enter') find(e.shiftKey ? -1 : 1);
                else if (e.key === 'Escape') this.closeFind();
                else return;
                e.preventDefault();
            });
            this.findInput = input;
            this.container.append(this.findBar);
        }
        this.findBar.hidden = false;
        if (this.term.hasSelection()) this.findInput.value = this.term.getSelection().split('\n')[0];
        this.findInput.select();
        this.findInput.focus();
    }

    closeFind() {
        if (!this.findBar) return;
        this.findBar.hidden = true;
        this.search.clearDecorations();
        this.term.clearSelection();
        this.term.focus();
    }

    // Returns false for keys we consume so xterm does not also handle them.
    handleKey(e) {
        if (e.type !== 'keydown') return true;
        if (this.isAppKey(e)) return false; // the window listener acts on it
        if (this.exited && e.key === 'Enter') {
            this.restart();
            return false;
        }
        if (this.exited && e.key === 'Escape' && this.reconnectTimer) {
            this.cancelReconnect();
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

    setColorFilter(colorMode) {
        this.filter = colorMode === 'mono' ? (this.filter ?? new SgrFilter()) : null;
    }

    // setAppearance applies theme, color mode and font settings live.
    // Switching to mono also neutralizes colored text already on screen
    // (via the contrast ratio); new output arrives without colors.
    setAppearance(ui, colorMode) {
        this.copyOnSelect = !!ui.copy_on_select;
        const changed = (colorMode === 'mono') !== !!this.filter;
        Object.assign(this.term.options, termOptions(ui, colorMode));
        this.setColorFilter(colorMode);
        this.fit.fit();
        if (changed && this.id) this.nudgeRedraw();
    }

    // nudgeRedraw makes full-screen programs (tmux, vim, claude) repaint
    // everything, so colors drawn before a color-mode switch disappear too:
    // a one-row resize and back delivers two SIGWINCHs.
    nudgeRedraw() {
        const {cols, rows} = this.term;
        if (rows < 2) return;
        this.resizePty(cols, rows - 1);
        setTimeout(() => this.id && this.resizePty(this.term.cols, this.term.rows), 80);
    }

    // resizePty tells the pty the new size. The redraw that programs answer
    // with is not activity, so output is ignored for activity for a moment.
    resizePty(cols, rows) {
        this.quietUntil = Date.now() + 1500;
        TermResize(this.id, cols, rows);
    }

    // disconnect kills the process; the view stays and offers to restart.
    disconnect() {
        if (this.reconnectTimer) return this.cancelReconnect();
        if (!this.id) return;
        this.intentional = true;
        TermKill(this.id);
    }

    focus() {
        this.fit.fit();
        this.term.focus();
    }

    dispose() {
        this.disposed = true;
        clearTimeout(this.reconnectTimer);
        this.disconnect();
        this.observer.disconnect();
        this.term.dispose();
    }
}
