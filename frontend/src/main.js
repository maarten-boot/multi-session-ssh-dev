import '@xterm/xterm/css/xterm.css';
import './style.css';

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebLinksAddon} from '@xterm/addon-web-links';

import {StartLocalShell, TermKill, TermResize, TermWrite} from '../wailsjs/go/main/App';
import {BrowserOpenURL, ClipboardGetText, ClipboardSetText, EventsOff, EventsOn} from '../wailsjs/runtime/runtime';

const isMac = navigator.platform.toUpperCase().includes('MAC');

function decode(b64) {
    const s = atob(b64);
    const bytes = new Uint8Array(s.length);
    for (let i = 0; i < s.length; i++) bytes[i] = s.charCodeAt(i);
    return bytes;
}

// TermView binds one xterm.js instance to a backend pty. start is a
// function (cols, rows) => Promise<id> so the same view can host local
// shells now and ssh sessions later.
class TermView {
    constructor(container, start) {
        this.start = start;
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
        const id = await this.start(this.term.cols, this.term.rows);
        this.id = id;
        EventsOn(`term:data:${id}`, b64 => this.term.write(decode(b64)));
        EventsOn(`term:exit:${id}`, info => this.onExit(info));
        this.term.focus();
    }

    onExit(info) {
        EventsOff(`term:data:${this.id}`, `term:exit:${this.id}`);
        this.id = null;
        this.exited = true;
        const why = info.error ? `: ${info.error}` : ` with code ${info.code}`;
        this.term.write(`\r\n\x1b[2m[process exited${why} — press Enter to restart]\x1b[0m\r\n`);
    }

    // Returns false for keys we consume so xterm does not also handle them.
    handleKey(e) {
        if (e.type !== 'keydown') return true;
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

    dispose() {
        if (this.id) TermKill(this.id);
        this.term.dispose();
    }
}

const view = new TermView(document.getElementById('terminal'), StartLocalShell);
view.spawn().catch(err => view.term.write(`\x1b[31mfailed to start shell: ${err}\x1b[0m\r\n`));
