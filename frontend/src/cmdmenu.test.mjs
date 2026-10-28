import test from 'node:test';
import assert from 'node:assert/strict';
import {commandItems} from './cmdmenu.js';

test('own commands first, Enter only when run', () => {
    const sent = [];
    const items = commandItems({data: {commands: [{name: 'own', command: 'make'}]}},
        [{name: 'fetch', command: 'git fetch --all'}, {name: 'rebase', command: 'git rebase main', run: false}],
        t => sent.push(t));
    assert.deepEqual(items.map(i => i.label), ['own  ⏎', 'fetch  ⏎', 'rebase  …']);
    items.forEach(i => i.action());
    assert.deepEqual(sent, ['make\r', 'git fetch --all\r', 'git rebase main']);
});

test('unusable entries are left out, disabled applies to all', () => {
    const items = commandItems(null, [{name: 'ok', command: 'ls'}, {name: 'nl', command: 'a\nb'}, {name: 'esc', command: 'a\u001b[2J'},
        {name: '', command: 'x'}, {name: 'empty', command: '  '}, {name: 'c1', command: 'a\u009bb'}], () => {}, 'not connected');
    assert.deepEqual(items.map(i => [i.label, i.disabled]), [['ok  ⏎', true]]);
});

test('groups are submenus after the other commands, merged across own and global', () => {
    const sent = [];
    const items = commandItems({data: {commands: [{name: 'st', command: 'git status -s', group: 'git'}, {name: 'own', command: 'make'}]}},
        [{name: 'compact', command: '/compact', group: 'claude'}, {name: 'fetch', command: 'git fetch', group: 'git'},
            {name: 'test', command: 'make test'}],
        t => sent.push(t));
    assert.deepEqual(items.map(i => i === '-' ? '-' : i.label), ['own  ⏎', 'test  ⏎', '-', 'git', 'claude']);
    assert.deepEqual(items[3].submenu.map(i => i.label), ['st  ⏎', 'fetch  ⏎']);
    items[4].submenu[0].action();
    assert.deepEqual(sent, ['/compact\r']);
    const only = commandItems(null, [{name: 'a', command: 'x', group: 'g'}, {name: 'b', command: 'y', group: ' '}], () => {});
    assert.deepEqual(only.map(i => i === '-' ? '-' : i.label), ['b  ⏎', '-', 'g']); // a blank group is none
    assert.deepEqual(commandItems(null, [{name: 'a', command: 'x', group: 'g'}], () => {}).map(i => i.label), ['g']);
});
