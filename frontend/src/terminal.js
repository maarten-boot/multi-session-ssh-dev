import '@xterm/xterm/css/xterm.css';

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebLinksAddon} from '@xterm/addon-web-links';

import {TermKill, TermResize, TermWrite} from '../wailsjs/go/main/App';
import {SgrFilter, termOptions} from './appearance';
import {BrowserOpenURL, ClipboardGetText, ClipboardSetText, EventsOff, EventsOn} from '../wailsjs/runtime/runtime';

export const isMac = navigator.platform.toUpperCase().includes('MAC');

function decode(b64) {
    const s = atob(b64);
    const bytes = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) bytes[i] = s.charCodeAt(i);
    return bytes;
}

// TermView binds one xterm.js instance to a backend pty.
//   start(cols, rows) => Promise<id> launches the process,
//   isAppKey(e) tells which keys belong to the app rather than the terminal,
//   onState('connected' | 'exited') reports the process state.
//   colorMode is 'full' or 'mono' (colors stripped from the output).
export class TermView {
    constructor(container, {start, exitHint, ui, colorMode, isAppKey, onState}) {
        this.start = start;
        this.exitHint = exitHint;
        this.isAppKey = isAppKey;
        this.onState = onState || (() => {});
        this.id = null;
        this.term = new Terminal({cursorBlink: true, ...termOptions(ui, colorMode)});
        this.setColorFilter(colorMode);
        this.fit = new FitAddon();
        this.term.loadAddon(this.fit);
        this.term.loadAddon(new WebLinksAddon((e, uri) => BrowserOpenURL(uri)));
        this.term.open(container);
        this.term.attachCustomKeyEventHandler(e => this.handleKey(e));
        this.term.onData(d => this.id && TermWrite(this.id, d));
        this.term.onResize(({cols, rows}) => this.id && TermResize(this.id, cols, rows));
        this.observer = new ResizeObserver(() => this.fit.fit());
        this.observer.observe(container);
        this.fit.fit();
    }

    async spawn() {
        this.exited = false;
        try {
            const id = await this.start(this.term.cols, this.term.rows);
            this.id = id;
            // Resizes while start() was in flight had no id to go to.
            this.fit.fit();
            TermResize(id, this.term.cols, this.term.rows);
            EventsOn(`term:data:${id}`, b64 => {
                const bytes = decode(b64);
                this.term.write(this.filter ? this.filter.filter(bytes) : bytes);
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
        this.onState('exited');
        if (this.disposed) return;
        const why = !info ? '' : info.error ? `: ${info.error}` : ` with code ${info.code}`;
        this.term.write(`\r\n\x1b[2m[${this.exitHint.what} exited${why} — press Enter to ${this.exitHint.again}]\x1b[0m\r\n`);
    }

    // Returns false for keys we consume so xterm does not also handle them.
    handleKey(e) {
        if (e.type !== 'keydown') return true;
        if (this.isAppKey(e)) return false; // the window listener acts on it
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

    setColorFilter(colorMode) {
        this.filter = colorMode === 'mono' ? (this.filter ?? new SgrFilter()) : null;
    }

    // setAppearance applies theme, color mode and font settings live.
    // Switching to mono also neutralizes colored text already on screen
    // (via the contrast ratio); new output arrives without colors.
    setAppearance(ui, colorMode) {
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
        TermResize(this.id, cols, rows - 1);
        setTimeout(() => this.id && TermResize(this.id, this.term.cols, this.term.rows), 80);
    }

    // disconnect kills the process; the view stays and offers to restart.
    disconnect() {
        if (this.id) TermKill(this.id);
    }

    focus() {
        this.fit.fit();
        this.term.focus();
    }

    dispose() {
        this.disposed = true;
        this.disconnect();
        this.observer.disconnect();
        this.term.dispose();
    }
}
