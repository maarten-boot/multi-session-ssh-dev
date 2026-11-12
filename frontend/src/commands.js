import {el} from './ui';

// Editors for the command menu's lists (Settings, Edit…) and for the start
// commands (Settings). The menu entries are in cmdmenu.js.

let lists = 0; // numbers the group suggestion lists

// CommandList edits a list of {name, command, run?, group?} in container: a
// row per entry (group, name, command, optional "run" tick, ↑ ↓ ✕) and an
// Add button. opts: run (show the tick), group (show the group, suggesting
// the groups in the list and those of groups()), where (show which tab it
// goes to: the tab it is chosen in, Local or Remote), emptyCommand (an
// empty command is allowed), placeholder {name, command}, extra (returns an
// element shown next to Add, e.g. a button that calls add()).
export class CommandList {
    constructor(container, items, opts = {}) {
        this.container = container;
        this.opts = opts;
        this.items = (items ?? []).map(c => ({...c}));
        this.listId = `cmd-groups-${++lists}`;
        container.classList.toggle('grouped', !!opts.group);
        container.classList.toggle('placed', !!opts.where);
        this.render();
    }

    // read takes the inputs' values into this.items.
    read() {
        this.container.querySelectorAll('.command-row').forEach((row, i) => {
            const it = this.items[i];
            it.name = row.querySelector('.cmd-name').value;
            it.command = row.querySelector('.cmd-command').value;
            if (this.opts.group) it.group = row.querySelector('.cmd-group').value;
            if (this.opts.run) it.run = row.querySelector('.cmd-run').checked;
            if (this.opts.where) it.where = row.querySelector('.cmd-where').value;
        });
    }

    // add appends entries after the ones being edited.
    add(items) {
        this.read();
        this.items.push(...items.map(c => ({...c})));
        this.render();
    }

    // value is the list as edited, without rows left completely empty.
    get value() {
        this.read();
        return this.items.filter(c => c.name.trim() || c.command.trim())
            .map(c => ({name: c.name.trim(), command: c.command,
                ...(this.opts.run ? {run: c.run !== false} : {}),
                ...(this.opts.group ? {group: (c.group ?? '').trim()} : {}),
                ...(this.opts.where ? {where: c.where ?? ''} : {})}));
    }

    render(focus = -1) {
        const ph = this.opts.placeholder ?? {};
        const rows = this.items.map((c, i) => {
            const name = el('input', {className: 'cmd-name', value: c.name ?? '', placeholder: ph.name ?? 'Name',
                autocomplete: 'off', spellcheck: false, ariaLabel: 'Name'});
            const command = el('input', {className: 'cmd-command', value: c.command ?? '', autocomplete: 'off', spellcheck: false,
                placeholder: ph.command ?? (this.opts.emptyCommand ? '(empty: a login shell)' : 'Command'), ariaLabel: 'Command'});
            const cells = [name, command];
            if (this.opts.group) {
                cells.unshift(el('input', {className: 'cmd-group', value: c.group ?? '', placeholder: 'Group',
                    autocomplete: 'off', spellcheck: false, ariaLabel: 'Group (a submenu)',
                    title: 'Commands with the same group are a submenu; empty: in the menu itself'}));
                cells[0].setAttribute('list', this.listId);
            }
            if (this.opts.run) {
                cells.push(el('label', {className: 'check cmd-run-label', title: 'Press Enter after typing it'},
                    el('input', {type: 'checkbox', className: 'cmd-run', checked: c.run !== false}), ' run'));
            }
            if (this.opts.where) {
                const where = el('select', {className: 'cmd-where', ariaLabel: 'Typed into',
                    title: 'Which terminal it is typed into: the tab you choose it in, or always the Local or the Remote ' +
                        'tab (the one on screen stays; e.g. git push from this computer while you watch the Remote tab)'},
                    el('option', {value: '', textContent: 'this tab'}),
                    el('option', {value: 'local', textContent: 'Local'}),
                    el('option', {value: 'remote', textContent: 'Remote'}));
                where.value = c.where ?? '';
                cells.push(where);
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
        const groups = this.opts.group ? [el('datalist', {id: this.listId},
            ...[...new Set([...this.items.map(c => c.group?.trim()), ...(this.opts.groups?.() ?? [])].filter(Boolean))]
                .sort().map(g => el('option', {value: g})))] : [];
        const extra = this.opts.extra ? [this.opts.extra(this)] : [];
        this.container.replaceChildren(...empty, ...rows, el('div', {className: 'command-add'}, add, ...extra), ...groups);
    }

    // move swaps two rows; the same arrow of the moved row keeps the focus.
    move(i, d) {
        [this.items[i], this.items[i + d]] = [this.items[i + d], this.items[i]];
        this.render();
        const row = this.container.querySelectorAll('.command-row')[i + d];
        (row?.querySelector(`button[title="${d < 0 ? 'Move up' : 'Move down'}"]:not(:disabled)`) ?? row?.querySelector('.cmd-name'))?.focus();
    }
}
