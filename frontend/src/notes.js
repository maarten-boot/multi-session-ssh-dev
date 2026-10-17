import {LoadNotes, SaveNotes} from '../wailsjs/go/main/App';
import {el} from './ui';

const SAVE_DELAY = 600; // ms after the last keystroke

// NotesView is the Notes tab: a plain-text editor for the session's notes,
// saved automatically. The file lives next to the app's config, not in the
// project directory.
export class NotesView {
    constructor(container, session) {
        this.session = session;
        this.dirty = false;
        this.saving = null;
        this.text = el('textarea', {className: 'notes-text', spellcheck: true,
            placeholder: 'Notes about this project: plans, open questions, what claude is working on…\n\n' +
                'Saved automatically. Stored with the app settings, not in the project, so they are not in git ' +
                'and not visible on the agent host.'});
        this.status = el('span', {className: 'notes-status'});
        this.path = el('span', {className: 'notes-path'});
        container.append(el('div', {className: 'notes'},
            el('div', {className: 'notes-bar'}, el('span', {className: 'notes-title', textContent: 'Notes'}), this.path, this.status),
            this.text));
        this.text.addEventListener('input', () => {
            this.dirty = true;
            this.say('Unsaved changes…');
            clearTimeout(this.timer);
            this.timer = setTimeout(() => this.save(), SAVE_DELAY);
        });
        this.text.addEventListener('blur', () => this.save());
        this.text.addEventListener('keydown', e => {
            // Tab indents instead of leaving the editor.
            if (e.key === 'Tab' && !e.ctrlKey && !e.altKey && !e.metaKey && !e.shiftKey) {
                e.preventDefault();
                this.text.setRangeText('  ', this.text.selectionStart, this.text.selectionEnd, 'end');
                this.text.dispatchEvent(new Event('input'));
            }
        });
        window.addEventListener('blur', () => this.save());
        this.loaded = this.load();
    }

    async load() {
        try {
            const n = await LoadNotes(this.session.id);
            this.text.value = n.text;
            this.path.textContent = '\u200e' + n.path; // keeps the path intact in the rtl-ellipsis box
            this.path.title = n.path;
            this.mtime = n.mtime;
            this.say(n.mtime ? `Saved ${when(n.mtime)}` : '');
        } catch (err) {
            this.say(err?.message ?? String(err), true);
        }
    }

    // refresh picks up edits made outside the app, unless we have unsaved ones.
    async refresh() {
        await this.loaded;
        if (this.dirty || this.saving) return;
        try {
            const n = await LoadNotes(this.session.id);
            if (n.mtime !== this.mtime) {
                this.text.value = n.text;
                this.mtime = n.mtime;
                this.say(`Reloaded (changed outside the app ${when(n.mtime)})`);
            }
        } catch {
        }
    }

    async save() {
        clearTimeout(this.timer);
        if (!this.dirty) return this.saving;
        this.dirty = false;
        const text = this.text.value;
        this.saving = SaveNotes(this.session.id, text).then(n => {
            this.mtime = n.mtime;
            if (!this.dirty) this.say(text ? `Saved ${when(n.mtime || Date.now())}` : 'Empty (no notes file)');
        }).catch(err => {
            this.dirty = true;
            this.say(`Not saved: ${err?.message ?? err}`, true);
        }).finally(() => this.saving = null);
        return this.saving;
    }

    say(msg, bad = false) {
        this.status.textContent = msg;
        this.status.classList.toggle('bad', bad);
    }

    focus() {
        this.text.focus();
    }

    disconnect() {
    }

    dispose() {
        return this.save();
    }
}

function when(ms) {
    const d = new Date(ms);
    return d.toDateString() === new Date().toDateString() ? d.toLocaleTimeString() : d.toLocaleString();
}
