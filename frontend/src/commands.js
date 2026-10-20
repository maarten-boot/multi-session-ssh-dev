import {el} from './ui';

// The command menu of the Local and Remote tabs and the editors for its
// lists (Settings, Edit…) and for the start commands (Settings).

// commandItems returns the menu entries for a session's commands (its own
// first, then the global ones); send(text) types into the terminal. Each
// entry types the command, then Enter unless its run is false.
export function commandItems(session, global, send, disabled = null) {
    const own = session?.data?.commands ?? [];
    const all = [...own, ...(global ?? [])];
    return all.map(c => {
        const run = c.run !== false;
        return {
            label: `${c.name}${run ? '  ⏎' : '  …'}`, disabled: !!disabled,
            action: () => send(c.command + (run ? '\r' : '')),
        };
    });
}

// CommandList edits a list of {name, command, run?} in container: a row per
// entry (name, command, optional "run" tick, ↑ ↓ ✕) and an Add button.
// opts: run (show the tick), emptyCommand (an empty command is allowed),
// placeholder {name, command}.
export class CommandList {
    constructor(container, items, opts = {}) {
        this.container = container;
        this.opts = opts;
        this.items = (items ?? []).map(c => ({...c}));
        this.render();
    }

    // read takes the inputs' values into this.items.
    read() {
        this.container.querySelectorAll('.command-row').forEach((row, i) => {
            const it = this.items[i];
            it.name = row.querySelector('.cmd-name').value;
            it.command = row.querySelector('.cmd-command').value;
            if (this.opts.run) it.run = row.querySelector('.cmd-run').checked;
        });
    }

    // value is the list as edited, without rows left completely empty.
    get value() {
        this.read();
        return this.items.filter(c => c.name.trim() || c.command.trim())
            .map(c => this.opts.run ? {name: c.name.trim(), command: c.command, run: c.run !== false}
                : {name: c.name.trim(), command: c.command});
    }

    render(focus = -1) {
        const ph = this.opts.placeholder ?? {};
        const rows = this.items.map((c, i) => {
            const name = el('input', {className: 'cmd-name', value: c.name ?? '', placeholder: ph.name ?? 'Name',
                autocomplete: 'off', spellcheck: false, ariaLabel: 'Name'});
            const command = el('input', {className: 'cmd-command', value: c.command ?? '', autocomplete: 'off', spellcheck: false,
                placeholder: ph.command ?? (this.opts.emptyCommand ? '(empty: a login shell)' : 'Command'), ariaLabel: 'Command'});
            const cells = [name, command];
            if (this.opts.run) {
                cells.push(el('label', {className: 'check cmd-run-label', title: 'Press Enter after typing it'},
                    el('input', {type: 'checkbox', className: 'cmd-run', checked: c.run !== false}), ' run'));
            }
            const button = (text, title, action, disabled = false) => {
                const b = el('button', {type: 'button', className: 'icon', textContent: text, title, disabled});
                b.onclick = () => {
                    this.read();
                    action();
                };
                return b;
            };
            cells.push(
                button('↑', 'Move up', () => this.move(i, -1), i === 0),
                button('↓', 'Move down', () => this.move(i, 1), i === this.items.length - 1),
                button('✕', 'Remove', () => {
                    this.items.splice(i, 1);
                    this.render();
                }));
            const row = el('div', {className: 'command-row'}, ...cells);
            if (i === focus) queueMicrotask(() => name.focus());
            return row;
        });
        const add = el('button', {type: 'button', textContent: '+ Add'});
        add.onclick = () => {
            this.read();
            this.items.push({name: '', command: '', run: true});
            this.render(this.items.length - 1);
        };
        const empty = this.items.length ? [] : [el('p', {className: 'hint', textContent: 'None yet.'})];
        this.container.replaceChildren(...empty, ...rows, el('div', {className: 'command-add'}, add));
    }

    move(i, d) {
        [this.items[i], this.items[i + d]] = [this.items[i + d], this.items[i]];
        this.render();
    }
}
