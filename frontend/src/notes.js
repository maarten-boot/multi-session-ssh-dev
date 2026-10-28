import {CreatePage, DeletePage, LoadPage, NotesTodos, NotesTree, RenamePage, SavePage} from '../wailsjs/go/main/App';
import {Editor, format, listFormat} from './editor';
import {LISTS, MARKUP} from './markup';
import {el, showMenu, store} from './ui';

const SAVE_DELAY = 600; // ms after the last keystroke
const HOME = 'Home';
const isMac = navigator.platform.startsWith('Mac');

const SESSION_HOME = 'Notes about this project: plans, open questions, what claude is working on…\n\n' +
    'Saved automatically. Stored with the app settings, not in the project, so they are not in git ' +
    'and not visible on the agent host. Right-click a page on the left for new pages and subpages.';
export const PAGE = 'An empty page. Type here; it is saved automatically.';
const SESSION_PROMPTS = 'Prompts for this session. Every "## name" heading starts a prompt: Prompts ▾ in the\n' +
    'Local and Remote tabs pastes it there (without Enter), before the prompts for all sessions. For example:\n\n' +
    '## Tests\n' +
    'Run the tests of this project and fix what fails.\n\n' +
    'Subpages of this page (right-click it) hold more prompts.';

const errText = err => (err?.message ?? String(err)).replace(/^notes: /, '');
// tidy makes backend errors read well in a dialog.
const tidy = fn => async (...a) => {
    try {
        return await fn(...a);
    } catch (err) {
        throw new Error(errText(err));
    }
};
const parentOf = p => p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '';
const within = (p, root) => p === root || p.startsWith(root + '/');

// NotesView is a book of notes pages: a page tree on the left and an editor
// for the open page, saved automatically. The files live next to the app's
// config, not in the project directory. book is a session id, or "_app"
// for the notes for all sessions.
// opts: title; placeholder(page) → text for an empty page; onChange(page,
// text) on every edit and load; onTree(pages) when the tree is (re)loaded;
// onTodos(todos) with the pages that have TODO lines (page → count), after
// the tree loads and after every save;
// dialogs: {prompt({title, hint, value, ok, submit(value)}),
// confirm({title, text, ok, action})} from the app.
export class NotesView {
    constructor(container, book, opts = {}) {
        this.book = book;
        this.opts = opts;
        this.placeholder = opts.placeholder ?? (p => p === HOME ? SESSION_HOME : p === 'Prompts' ? SESSION_PROMPTS : PAGE);
        this.onChange = opts.onChange ?? (() => {});
        this.pages = []; // the tree from NotesTree
        this.todos = {}; // page → its number of TODO lines (NotesTodos)
        this.expanded = new Set(store.get(`notesopen:${book}`, []));
        this.page = null;
        this.seq = 0; // counts opens, see open
        this.dirty = false;
        this.saving = null; // the last save, until it is done

        this.treeEl = el('div', {className: 'notes-tree', tabIndex: 0});
        const newBtn = el('button', {className: 'icon', textContent: '+', title: 'New page or subpage'});
        newBtn.onclick = () => {
            const r = newBtn.getBoundingClientRect();
            this.menu(this.page && this.find(this.page) ? this.page : null, r.left, r.bottom + 2, true);
        };
        this.pageTitle = el('span', {className: 'notes-page-title'});
        this.status = el('span', {className: 'notes-status'});
        this.path = el('span', {className: 'notes-path'});
        const key = k => k.replace('Mod', isMac ? 'Cmd' : 'Ctrl').replace(/-/g, '+').replace(/\+([a-z])$/, (_, c) => '+' + c.toUpperCase());
        const tool = (name, m, run) => {
            const b = el('button', {type: 'button', className: 'notes-tool notes-tool-' + name, textContent: m.label,
                title: `${m.title} (${key(m.key)})`});
            b.onmousedown = e => e.preventDefault(); // keep the selection
            b.onclick = () => run(this.editor.view, name);
            return b;
        };
        const tools = [...Object.entries(MARKUP).map(([name, m]) => tool(name, m, format)),
            el('span', {className: 'notes-tool-gap'}),
            ...Object.entries(LISTS).map(([name, m]) => tool(name, m, listFormat))];
        const editorEl = el('div', {className: 'notes-editor'});
        container.append(el('div', {className: 'notes'},
            el('div', {className: 'notes-bar'}, el('span', {className: 'notes-title', textContent: opts.title ?? 'Notes'}),
                this.pageTitle, this.path, this.status),
            el('div', {className: 'notes-body'},
                el('div', {className: 'notes-side'},
                    el('div', {className: 'notes-side-bar'}, el('span', {textContent: 'Pages'}), newBtn),
                    this.treeEl),
                el('div', {className: 'notes-page'},
                    el('div', {className: 'notes-toolbar'}, ...tools),
                    editorEl))));
        this.editor = new Editor(editorEl, {
            onChange: text => {
                this.onChange(this.page, text);
                this.dirty = true;
                this.say('Unsaved changes…');
                clearTimeout(this.timer);
                this.timer = setTimeout(() => this.save(), SAVE_DELAY);
            },
            onBlur: () => this.save(),
        });
        this.treeEl.addEventListener('click', e => {
            const row = e.target.closest('.notes-row');
            if (!row) return;
            if (e.target.closest('.caret')) this.toggle(row.dataset.path);
            else this.open(row.dataset.path);
        });
        this.treeEl.addEventListener('contextmenu', e => {
            e.preventDefault();
            this.menu(e.target.closest('.notes-row')?.dataset.path ?? null, e.clientX, e.clientY);
        });
        this.treeEl.addEventListener('keydown', e => this.onKey(e));
        this.onWindowBlur = () => this.save();
        window.addEventListener('blur', this.onWindowBlur);
        this.loaded = this.init();
    }

    async init() {
        await this.loadTree();
        const want = store.get(`notespage:${this.book}`, null);
        await this.open(want && this.find(want) ? want : this.defaultPage(), false);
    }

    // defaultPage: Home if it has text, else the first page that has.
    defaultPage() {
        const first = ps => {
            for (const p of ps) {
                if (p.has_text) return p.path;
                const c = first(p.children);
                if (c) return c;
            }
            return null;
        };
        return this.find(HOME)?.has_text ? HOME : first(this.pages) ?? HOME;
    }

    find(path, ps = this.pages) {
        for (const p of ps) {
            if (p.path === path) return p;
            if (within(path, p.path)) return this.find(path, p.children);
        }
        return null;
    }

    async loadTree() {
        try {
            this.pages = await NotesTree(this.book);
        } catch (err) {
            this.pages = [];
            this.say(errText(err), true);
        }
        // Home is always there, even before it has text.
        if (!this.find(HOME)) this.pages.unshift({path: HOME, name: HOME, has_text: false, children: []});
        this.renderTree();
        this.opts.onTree?.(this.pages);
        this.loadTodos();
    }

    // loadTodos finds the pages with TODO lines and marks them in the tree.
    async loadTodos() {
        const seq = this.todoSeq = (this.todoSeq ?? 0) + 1;
        const todos = await NotesTodos(this.book).catch(() => null);
        if (!todos || seq !== this.todoSeq) return;
        this.todos = todos;
        this.renderTree();
        this.opts.onTodos?.(todos);
    }

    // todoIn: the TODO lines of a page, with those of its subpages when they
    // are hidden (the page is collapsed).
    todoIn(p, open) {
        const sub = ps => ps.reduce((n, c) => n + (this.todos[c.path] ?? 0) + sub(c.children), 0);
        return (this.todos[p.path] ?? 0) + (open ? 0 : sub(p.children));
    }

    renderTree() {
        const rows = [];
        const walk = (ps, depth) => {
            for (const p of ps) {
                const open = this.expanded.has(p.path);
                const todo = this.todoIn(p, open);
                const row = el('div', {className: 'notes-row' + (p.path === this.page ? ' selected' : '') + (p.has_text ? '' : ' empty'),
                    title: p.path + (p.has_text ? '' : ' (no text)') + (todo ? `\n${todo} TODO line(s)` + (this.todos[p.path] === todo ? '' : ', with its subpages') : '')},
                    el('span', {className: 'caret', textContent: p.children.length ? (open ? '▾' : '▸') : ''}),
                    el('span', {className: 'name', textContent: p.name}),
                    todo ? el('span', {className: 'todo-mark', textContent: '▲'}) : '');
                row.dataset.path = p.path;
                row.style.paddingLeft = (6 + depth * 14) + 'px';
                rows.push(row);
                if (open) walk(p.children, depth + 1);
            }
        };
        walk(this.pages, 0);
        this.treeEl.replaceChildren(...rows);
        this.treeEl.querySelector('.selected')?.scrollIntoView({block: 'nearest'});
    }

    toggle(path) {
        this.expanded.has(path) ? this.expanded.delete(path) : this.expanded.add(path);
        store.set(`notesopen:${this.book}`, [...this.expanded]);
        this.renderTree();
    }

    // open saves the current page and shows another one. Of several opens
    // in a row the last one wins.
    async open(path, focus = true) {
        if (path !== this.page) {
            const seq = ++this.seq;
            await this.save();
            if (this.dirty || seq !== this.seq) return; // not saved: stay; or a later open
            try {
                const n = await LoadPage(this.book, path);
                if (seq !== this.seq) return;
                this.show(path, n);
            } catch (err) {
                this.say(errText(err), true);
                return;
            }
            store.set(`notespage:${this.book}`, path);
            for (let p = parentOf(path); p; p = parentOf(p)) this.expanded.add(p);
            this.renderTree();
        }
        if (focus) this.focus();
    }

    show(path, n) {
        this.page = path;
        this.editor.setText(n.text, this.placeholder(path));
        this.onChange(path, n.text);
        this.mtime = n.mtime;
        this.pageTitle.textContent = path.replaceAll('/', ' › ');
        this.path.textContent = '‎' + n.path; // keeps the path intact in the rtl-ellipsis box
        this.path.title = n.path;
        this.say(n.mtime ? `Saved ${when(n.mtime)}` : '');
    }

    // refresh picks up edits made outside the app, unless we have unsaved
    // ones. The page may change while it loads (another page opened, an
    // edit): then the result is old.
    async refresh() {
        await this.loaded;
        await this.loadTree();
        if (this.dirty || this.saving) return;
        const page = this.page, seq = this.seq;
        try {
            const n = await LoadPage(this.book, page);
            if (page !== this.page || seq !== this.seq || this.dirty || this.saving) return;
            if (n.mtime !== this.mtime) {
                this.show(this.page, n);
                this.say(`Reloaded (changed outside the app ${when(n.mtime)})`);
            }
        } catch {
        }
    }

    // save writes the page if it has unsaved changes. Saves run one after
    // the other (each SavePage runs on its own in the backend, so two at once
    // could land in the wrong order); the promise resolves when the latest
    // text is on disk, or its save failed.
    async save() {
        clearTimeout(this.timer);
        if (!this.dirty) return this.saving;
        this.dirty = false;
        const text = this.editor.text, page = this.page, prev = this.saving;
        const saving = (async () => {
            await prev; // never rejects
            const had = !!this.mtime;
            const n = await SavePage(this.book, page, text);
            this.loadTodos(); // also when another page is open by now
            if (page !== this.page) return;
            this.mtime = n.mtime;
            if (!this.dirty && this.saving === saving) this.say(text ? `Saved ${when(n.mtime || Date.now())}` : 'Empty (no file)');
            if (had !== !!n.mtime) this.loadTree(); // the page appeared or went
        })().catch(err => {
            if (this.saving === saving) this.dirty = true; // else a later save has newer text
            this.say(`Not saved: ${errText(err)}`, true);
        }).finally(() => {
            if (this.saving === saving) this.saving = null;
        });
        this.saving = saving;
        return saving;
    }

    // --- pages ---

    // menu is a page's right-click menu (path null: the empty tree); onlyNew:
    // the + button's (new page, new subpage of the open page).
    menu(path, x, y, onlyNew = false) {
        const items = [{label: 'New page…', action: () => this.newPage('')}];
        if (path && onlyNew) items.push({label: `New subpage of “${this.find(path)?.name ?? path}”…`, action: () => this.newPage(path)});
        else if (path) {
            items.push({label: `New subpage of “${this.find(path)?.name ?? path}”…`, action: () => this.newPage(path)}, '-',
                {label: 'Rename or move…', action: () => this.rename(path)},
                {label: 'Delete…', danger: true, action: () => this.remove(path)});
        }
        showMenu(x, y, items);
    }

    async newPage(parent) {
        let created = null;
        const ok = await this.opts.dialogs.prompt({
            title: parent ? `New subpage of “${parent}”` : 'New page',
            hint: 'A name like “Design”; “Design/API” makes a subpage. Not allowed: / \\ : * ? " < > | and a dot at the start.',
            value: '', ok: 'Create',
            submit: tidy(async name => {
                const path = parent ? `${parent}/${name.trim()}` : name.trim();
                await CreatePage(this.book, path);
                created = path;
            }),
        });
        if (!ok || !created) return;
        await this.loadTree();
        await this.open(created);
    }

    async rename(path) {
        await this.save();
        let to = null;
        const ok = await this.opts.dialogs.prompt({
            title: `Rename “${path}”`,
            hint: 'Subpages move along. Use / to put it under another page, e.g. “Design/API”.',
            value: path, ok: 'Rename',
            submit: tidy(async name => {
                await RenamePage(this.book, path, name.trim());
                to = name.trim();
            }),
        });
        if (!ok || !to) return;
        if (within(this.page, path)) {
            this.page = to + this.page.slice(path.length);
            store.set(`notespage:${this.book}`, this.page);
        }
        // Open folders move along; the open page stays in view.
        this.expanded = new Set([...this.expanded].map(p => within(p, path) ? to + p.slice(path.length) : p));
        for (let p = parentOf(to); p; p = parentOf(p)) this.expanded.add(p);
        for (let p = parentOf(this.page); p; p = parentOf(p)) this.expanded.add(p);
        store.set(`notesopen:${this.book}`, [...this.expanded]);
        await this.loadTree();
        const page = this.page, seq = ++this.seq;
        const n = await LoadPage(this.book, page).catch(() => null);
        // Edits the save before the rename could not store stay in the editor.
        if (n && seq === this.seq && !this.dirty) this.show(page, n);
    }

    async remove(path) {
        const count = ps => ps.reduce((k, p) => k + 1 + count(p.children), 0);
        const subs = count(this.find(path)?.children ?? []);
        const ok = await this.opts.dialogs.confirm({
            title: `Delete page “${path}”?`,
            text: (subs ? `Its ${subs} subpage(s) are deleted too. ` : '') + 'This cannot be undone.',
            ok: 'Delete',
            action: tidy(async () => {
                // Unsaved edits of a page that goes are dropped, but only
                // once it is gone: if the delete fails they are kept.
                let dirty = false;
                if (within(this.page, path)) {
                    clearTimeout(this.timer);
                    dirty = this.dirty;
                    this.dirty = false;
                    await this.saving;
                    dirty ||= this.dirty; // that save failed
                    this.dirty = false;
                }
                try {
                    await DeletePage(this.book, path);
                } catch (err) {
                    if (dirty) this.dirty = true;
                    throw err;
                }
            }),
        });
        if (!ok) return;
        await this.loadTree();
        if (within(this.page, path)) {
            this.page = null;
            await this.open(this.defaultPage());
        }
    }

    onKey(e) {
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        const rows = [...this.treeEl.querySelectorAll('.notes-row')].map(r => r.dataset.path);
        const i = rows.indexOf(this.page);
        const p = this.find(this.page);
        if (e.key === 'ArrowDown' && i < rows.length - 1) this.open(rows[i + 1], false);
        else if (e.key === 'ArrowUp' && i > 0) this.open(rows[i - 1], false);
        else if (e.key === 'ArrowRight' && p?.children.length && !this.expanded.has(p.path)) this.toggle(p.path);
        else if (e.key === 'ArrowLeft' && p && this.expanded.has(p.path)) this.toggle(p.path);
        else if (e.key === 'ArrowLeft' && parentOf(this.page)) this.open(parentOf(this.page), false);
        else if (e.key === 'Enter') this.focus();
        else return;
        e.preventDefault();
    }

    say(msg, bad = false) {
        this.status.textContent = msg;
        this.status.classList.toggle('bad', bad);
    }

    focus() {
        this.editor.focus();
    }

    disconnect() {
    }

    dispose() {
        window.removeEventListener('blur', this.onWindowBlur);
        const saved = this.save(); // takes the text now
        this.editor.destroy();
        return saved;
    }
}

function when(ms) {
    const d = new Date(ms);
    return d.toDateString() === new Date().toDateString() ? d.toLocaleTimeString() : d.toLocaleString();
}
