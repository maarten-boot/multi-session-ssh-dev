import {CreatePage, LoadPage, NotesTree} from '../wailsjs/go/main/App';
import {ClipboardSetText} from '../wailsjs/runtime/runtime';
import {NotesView, PAGE} from './notes';
import {parsePrompts} from './prompts';
import {el, toast} from './ui';

export const PROMPTS = 'Prompts';
const isPrompts = page => page === PROMPTS || page?.startsWith(PROMPTS + '/');

// promptPages reads the Prompts page of book and its subpages (tree order)
// into a Map path → text. view is the book's NotesView, if any: its open
// page's text is the editor's, which may not be saved yet.
export async function promptPages(book, pages, view) {
    const paths = [];
    const walk = ps => ps.filter(p => isPrompts(p.path)).forEach(p => {
        paths.push(p.path);
        walk(p.children ?? []);
    });
    walk(pages ?? []);
    const texts = new Map();
    for (const p of paths) {
        const known = view && p === view.page ? view.editor.text : null;
        texts.set(p, known ?? (await LoadPage(book, p).catch(() => ({text: ''}))).text);
    }
    return texts;
}

// sessionPrompts returns the prompts of a session's own notes.
export async function sessionPrompts(book, view) {
    const pages = await NotesTree(book).catch(() => []);
    return [...(await promptPages(book, pages, view)).values()].flatMap(parsePrompts);
}

const PLACEHOLDERS = {
    Home: 'Notes for all sessions: things that belong to no single project.\n\n' +
        'Prompts you use often go on the Prompts page. Right-click a page on the left for new pages ' +
        'and subpages. Saved automatically, with the app settings (notes/_app/ next to config.toml).',
    [PROMPTS]: 'Prompts you use often. Every "## name" heading starts a prompt: it shows up on the\n' +
        'right with Copy and Paste into Remote. For example:\n\n' +
        '## Review\n' +
        'Review the changes since the last commit for bugs and missing tests. Be brief.\n\n' +
        'Subpages of this page (right-click it) hold more prompts. They are also in Prompts ▾\n' +
        'in the Local and Remote tabs, after the prompts of the session\'s own Prompts page.',
};

// AppNotesPage is the notes page for the whole app: a book of notes pages
// and the prompts on its Prompts page and that page's subpages. paste(text)
// types a prompt into the current session's Remote tab; label() names that
// session (null: none); canPaste() returns why a paste is not possible
// there (null: it is); dialogs as for NotesView.
export class AppNotesPage {
    constructor(host, {paste, label, canPaste, keyHint, dialogs}) {
        this.paste = paste;
        this.label = label;
        this.canPaste = canPaste ?? (() => null);
        this.list = el('div', {className: 'prompt-list'});
        const close = el('button', {className: 'icon', textContent: '✕', title: `Close (Esc, ${keyHint})`});
        close.onclick = () => this.hide();
        const editor = el('div', {className: 'app-notes-editor'});
        this.el = el('div', {id: 'app-notes', hidden: true},
            el('div', {className: 'app-notes-bar'},
                el('span', {className: 'notes-title', textContent: 'Notes for all sessions'}),
                el('span', {className: 'app-notes-hint', textContent: `${keyHint} opens and closes this page`}), close),
            el('div', {className: 'app-notes-body'}, editor,
                el('div', {className: 'prompts'}, el('div', {className: 'prompts-title', textContent: 'Prompts'}), this.list)));
        host.append(this.el);
        this.texts = new Map(); // Prompts page path → its text, in tree order
        this.hasPromptsPage = false;
        this.promptsSeq = 0; // counts loadPrompts runs: only the last one counts
        this.view = new NotesView(editor, '_app', {
            title: 'Notes', dialogs,
            placeholder: page => PLACEHOLDERS[page] ?? PAGE,
            onChange: (page, text) => {
                if (!isPrompts(page)) return;
                this.texts.set(page, text);
                this.render();
            },
            onTree: pages => this.loadPrompts(pages),
        });
        this.el.addEventListener('keydown', e => {
            if (e.key === 'Escape' && !e.defaultPrevented) {
                e.preventDefault();
                this.hide();
            }
        });
    }

    // loadPrompts reads the Prompts page and its subpages (tree order).
    async loadPrompts(pages) {
        const seq = ++this.promptsSeq;
        const texts = await promptPages('_app', pages, this.view);
        if (seq !== this.promptsSeq) return;
        this.hasPromptsPage = texts.size > 0;
        this.texts = texts;
        this.render();
    }

    get prompts() {
        return [...this.texts.values()].flatMap(parsePrompts);
    }

    get visible() {
        return !this.el.hidden;
    }

    show() {
        this.el.hidden = false;
        this.render();
        this.view.refresh();
        this.view.focus();
    }

    hide() {
        if (!this.visible) return;
        this.el.hidden = true;
        this.view.save();
        this.onHide?.();
    }

    toggle() {
        this.visible ? this.hide() : this.show();
    }

    render() {
        const target = this.label();
        const problem = target ? this.canPaste() : 'No session';
        const prompts = this.prompts;
        this.list.replaceChildren(...prompts.map(p => {
            const copy = el('button', {type: 'button', textContent: 'Copy'});
            copy.onclick = () => ClipboardSetText(p.text).then(() => toast(`Copied “${p.name}”`));
            const paste = el('button', {type: 'button', textContent: 'Paste into Remote', disabled: !!problem,
                title: problem || `Into the Remote tab of ${target}, without Enter`});
            paste.onclick = () => {
                this.hide();
                this.paste(p.text);
            };
            return el('div', {className: 'prompt'},
                el('div', {className: 'prompt-name', textContent: p.name}),
                el('div', {className: 'prompt-text', textContent: p.text}),
                el('div', {className: 'prompt-actions'}, copy, paste));
        }));
        if (prompts.length) return;
        if (this.hasPromptsPage) {
            this.list.append(el('p', {className: 'prompt-empty', textContent: 'No prompts yet: start one with a "## name" line on the Prompts page.'}));
            return;
        }
        const make = el('button', {type: 'button', textContent: 'Create the Prompts page'});
        make.onclick = async () => {
            await CreatePage('_app', PROMPTS).catch(err => toast(err?.message ?? String(err), true));
            await this.view.loadTree();
            await this.view.open(PROMPTS);
        };
        this.list.append(el('p', {className: 'prompt-empty', textContent: 'Prompts come from a page named “Prompts” and its subpages.'}),
            el('div', {className: 'prompt-actions'}, make));
    }
}
