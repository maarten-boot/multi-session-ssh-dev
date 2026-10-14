import {ItemPaths, ListDir, OpenPath, RevealPath} from '../wailsjs/go/main/App';
import {ClipboardSetText} from '../wailsjs/runtime/runtime';
import {el, showMenu, store} from './ui';

const errText = err => err?.message ?? String(err);

// FilesView is the Files tab: a lazily loaded tree of the session's local
// directory. Directories load when expanded; showing the tab again
// refreshes everything that is expanded (no filesystem watching).
export class FilesView {
    constructor(container, session) {
        this.session = session;
        this.showHidden = store.get(`hidden:${session.id}`, false);
        this.children = new Map(); // rel -> {entries, truncated} | {error}
        this.expanded = new Set([""]);
        this.selected = null;
        this.rows = []; // visible rows in order: {entry, depth}

        this.hiddenBox = el('input', {type: 'checkbox', checked: this.showHidden});
        this.hiddenBox.onchange = () => {
            this.showHidden = this.hiddenBox.checked;
            store.set(`hidden:${session.id}`, this.showHidden);
            this.refresh();
        };
        const refreshBtn = el('button', {className: 'icon', textContent: '⟳', title: 'Refresh'});
        refreshBtn.onclick = () => this.refresh();
        this.rootLabel = el('span', {className: 'files-root'});
        this.tree = el('div', {className: 'tree', tabIndex: 0});
        this.status = el('div', {className: 'files-status'});
        container.append(el('div', {className: 'files'},
            el('div', {className: 'files-bar'}, this.rootLabel,
                el('label', {className: 'files-hidden'}, this.hiddenBox, ' Hidden files'), refreshBtn),
            this.tree, this.status));

        this.tree.addEventListener('keydown', e => this.onKey(e));
        this.tree.addEventListener('contextmenu', e => {
            e.preventDefault();
            const row = e.target.closest('.row');
            if (row?.dataset.rel !== undefined) this.select(row.dataset.rel);
            this.menu(row ? this.entry(row.dataset.rel) : null, e.clientX, e.clientY);
        });
        this.tree.addEventListener('click', e => {
            const row = e.target.closest('.row');
            if (!row?.dataset.rel) return;
            this.select(row.dataset.rel);
            if (e.target.closest('.caret')) this.toggle(row.dataset.rel);
        });
        this.tree.addEventListener('dblclick', e => {
            const row = e.target.closest('.row');
            if (row?.dataset.rel) this.activate(this.entry(row.dataset.rel));
        });
        this.refresh();
    }

    // --- data ---

    async load(rel) {
        try {
            this.children.set(rel, await ListDir(this.session.id, rel, this.showHidden));
        } catch (err) {
            this.children.set(rel, {error: errText(err)});
        }
    }

    // refresh reloads the root and every expanded directory, dropping
    // expansions whose directory is gone.
    async refresh() {
        this.rootLabel.textContent = this.session.data.local_path;
        this.rootLabel.title = this.session.data.local_path;
        const dirs = [...this.expanded];
        this.children.clear();
        await Promise.all(dirs.map(d => this.load(d)));
        for (const d of dirs) {
            if (d && this.children.get(d)?.error) this.expanded.delete(d);
        }
        this.render();
        if (this.selected === null && document.activeElement === this.tree) this.focus();
    }

    async toggle(rel) {
        const e = this.entry(rel);
        if (!e?.dir) return;
        if (this.expanded.has(rel)) {
            this.expanded.delete(rel);
        } else {
            this.expanded.add(rel);
            if (!this.children.has(rel)) await this.load(rel);
        }
        this.render();
    }

    entry(rel) {
        return this.rows.find(r => r.entry.rel === rel)?.entry ?? null;
    }

    // --- rendering ---

    render() {
        this.rows = [];
        const walk = (rel, depth) => {
            const c = this.children.get(rel);
            if (!c) return;
            if (c.error) {
                this.rows.push({entry: {rel: null, name: c.error, error: true}, depth});
                return;
            }
            for (const e of c.entries) {
                this.rows.push({entry: e, depth});
                if (e.dir && this.expanded.has(e.rel)) walk(e.rel, depth + 1);
            }
            if (c.truncated) this.rows.push({entry: {rel: null, name: `… more entries not shown`, note: true}, depth});
            if (!c.entries.length && rel) this.rows.push({entry: {rel: null, name: 'empty', note: true}, depth});
        };
        walk("", 0);
        if (this.selected !== null && !this.entry(this.selected)) this.selected = null;

        const scroll = this.tree.scrollTop;
        this.tree.replaceChildren(...this.rows.map(({entry: e, depth}) => {
            const row = el('div', {className: 'row'});
            row.style.paddingLeft = (8 + depth * 16) + 'px';
            if (e.rel === null) {
                row.classList.add(e.error ? 'error' : 'note');
                row.append(el('span', {className: 'caret'}), el('span', {className: 'name', textContent: e.name}));
                return row;
            }
            row.dataset.rel = e.rel;
            row.classList.add(e.dir ? 'dir' : 'file');
            if (e.hidden) row.classList.add('hidden-entry');
            if (e.symlink) row.classList.add('symlink');
            if (e.rel === this.selected) row.classList.add('selected');
            const caret = e.dir ? (this.expanded.has(e.rel) ? '▾' : '▸') : '';
            row.append(el('span', {className: 'caret', textContent: caret}),
                el('span', {className: 'name', textContent: e.name + (e.dir ? '/' : '')}));
            if (!e.dir) row.append(el('span', {className: 'size', textContent: fmtSize(e.size)}));
            return row;
        }));
        this.tree.scrollTop = scroll;
    }

    select(rel) {
        this.selected = rel;
        this.tree.querySelectorAll('.row.selected').forEach(r => r.classList.remove('selected'));
        const row = [...this.tree.querySelectorAll('.row')].find(r => r.dataset.rel === rel);
        row?.classList.add('selected');
        row?.scrollIntoView({block: 'nearest'});
        const e = this.entry(rel);
        if (e && !this.sayTimer) this.describe(e);
    }

    // describe shows the selected entry's details in the status line
    // (instead of a tooltip, which would cover the context menu).
    describe(e) {
        const parts = [e.rel + (e.dir ? '/' : '')];
        if (e.symlink) parts.push('symlink');
        if (!e.dir) parts.push(fmtSize(e.size));
        parts.push('modified ' + new Date(e.mtime * 1000).toLocaleString());
        this.status.textContent = parts.join('  ·  ');
        this.status.classList.remove('bad');
    }

    // --- actions ---

    activate(e) {
        if (!e) return;
        if (e.dir) this.toggle(e.rel);
        else this.run(() => OpenPath(this.session.id, e.rel));
    }

    async run(fn, ok = '') {
        try {
            await fn();
            if (ok) this.say(ok);
        } catch (err) {
            this.say(errText(err), true);
        }
    }

    say(msg, bad = false) {
        clearTimeout(this.sayTimer);
        this.status.textContent = msg;
        this.status.classList.toggle('bad', bad);
        this.sayTimer = msg ? setTimeout(() => {
            this.sayTimer = null;
            const e = this.entry(this.selected);
            e ? this.describe(e) : this.say('');
        }, bad ? 8000 : 3000) : null;
    }

    async copy(rel, which) {
        await this.run(async () => {
            const p = await ItemPaths(this.session.id, rel);
            const text = which === 'remote' ? p.remote : p.local;
            if (!text) throw new Error('this session has no remote path mapping');
            await ClipboardSetText(text);
            this.say(`Copied ${text}`);
        });
    }

    menu(e, x, y) {
        const rel = e?.rel ?? '';
        const items = [];
        if (e?.rel) {
            if (e.dir) {
                items.push({label: this.expanded.has(rel) ? 'Collapse' : 'Expand', action: () => this.toggle(rel)});
                items.push({label: 'Open in file manager', action: () => this.run(() => OpenPath(this.session.id, rel))});
            } else {
                items.push({label: 'Open', action: () => this.activate(e)});
                items.push({label: 'Open containing folder', action: () => this.run(() => RevealPath(this.session.id, rel))});
            }
            items.push('-');
        } else {
            items.push({label: 'Open in file manager', action: () => this.run(() => OpenPath(this.session.id, ''))}, '-');
        }
        items.push({label: 'Copy local path', action: () => this.copy(rel, 'local')});
        items.push({label: 'Copy remote path', disabled: !this.session.data.remote_path, action: () => this.copy(rel, 'remote')});
        items.push('-', {label: 'Refresh', action: () => this.refresh()});
        showMenu(x, y, items);
    }

    onKey(e) {
        if (e.ctrlKey || e.metaKey || e.altKey) return; // app shortcuts bubble on
        const rels = this.rows.filter(r => r.entry.rel !== null).map(r => r.entry.rel);
        if (!rels.length) return;
        let i = rels.indexOf(this.selected);
        const cur = this.entry(this.selected);
        const go = j => this.select(rels[Math.max(0, Math.min(rels.length - 1, j))]);
        switch (e.key) {
        case 'ArrowDown': go(i + 1); break;
        case 'ArrowUp': go(i < 0 ? 0 : i - 1); break;
        case 'Home': go(0); break;
        case 'End': go(rels.length - 1); break;
        case 'PageDown': go(i + 20); break;
        case 'PageUp': go(i - 20); break;
        case 'ArrowRight':
            if (cur?.dir && !this.expanded.has(cur.rel)) this.toggle(cur.rel);
            else if (cur?.dir) go(i + 1);
            break;
        case 'ArrowLeft':
            if (cur?.dir && this.expanded.has(cur.rel)) this.toggle(cur.rel);
            else if (cur) {
                const parent = cur.rel.includes('/') ? cur.rel.slice(0, cur.rel.lastIndexOf('/')) : null;
                if (parent) this.select(parent);
            }
            break;
        case 'Enter': this.activate(cur); break;
        case 'F5': this.refresh(); break;
        default: return;
        }
        e.preventDefault();
    }

    focus() {
        this.tree.focus();
        if (this.selected === null) {
            const first = this.rows.find(r => r.entry.rel !== null);
            if (first) this.select(first.entry.rel);
        }
    }

    disconnect() {
    }

    dispose() {
    }
}

function fmtSize(n) {
    if (n < 1024) return `${n} B`;
    const u = ['KB', 'MB', 'GB', 'TB'];
    let i = -1;
    do {
        n /= 1024;
        i++;
    } while (n >= 1024 && i < u.length - 1);
    return `${n < 10 ? n.toFixed(1) : Math.round(n)} ${u[i]}`;
}
