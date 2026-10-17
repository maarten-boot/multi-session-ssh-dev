import {ClipboardSetText} from '../wailsjs/runtime/runtime';
import {NotesView} from './notes';
import {parsePrompts} from './prompts';
import {el, toast} from './ui';

const PLACEHOLDER = 'Notes for all sessions, and prompts you use often.\n\n' +
    'Every "## name" heading starts a prompt: it shows up on the right with Copy and\n' +
    'Paste into Remote. For example:\n\n' +
    '## Review\n' +
    'Review the changes since the last commit for bugs and missing tests. Be brief.\n\n' +
    'Saved automatically, with the app settings (notes/_app.md next to config.toml).';

// AppNotesPage is the notes page for the whole app: a notes editor and the
// prompts found in it. paste(text) types a prompt into the current
// session's Remote tab; label() names that session (null: none);
// canPaste() returns why a paste is not possible there (null: it is).
export class AppNotesPage {
    constructor(host, {paste, label, canPaste, keyHint}) {
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
        this.prompts = [];
        this.view = new NotesView(editor, {id: '_app'}, {
            title: 'Notes', placeholder: PLACEHOLDER,
            onChange: text => {
                this.prompts = parsePrompts(text);
                this.render();
            },
        });
        this.el.addEventListener('keydown', e => {
            if (e.key === 'Escape') {
                e.preventDefault();
                this.hide();
            }
        });
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
        this.list.replaceChildren(...this.prompts.map(p => {
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
        if (!this.prompts.length) {
            this.list.append(el('p', {className: 'prompt-empty', textContent: 'No prompts yet: start one with a "## name" line.'}));
        }
    }
}
