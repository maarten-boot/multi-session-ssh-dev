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

test('back to the session directory: quoted for the shell', async () => {
    const {cdCommand, homeItem} = await import('./cmdmenu.js');
    assert.equal(cdCommand('/home/ann/my proj'), `cd '/home/ann/my proj'`);
    assert.equal(cdCommand(`/tmp/it's $HOME`), `cd '/tmp/it'\\''s $HOME'`);
    assert.equal(cdCommand('C:\\Users\\ann\\proj', true), 'cd "C:\\Users\\ann\\proj"');
    const sent = [];
    const it = homeItem('/srv/p', t => sent.push(t));
    assert.equal(it.title, '/srv/p');
    it.action();
    assert.deepEqual(sent, [`cd '/srv/p'\r`]);
    assert.equal(homeItem('', () => {}), null);
    assert.equal(homeItem('/a\nrm -rf ~', () => {}), null);
    assert.equal(homeItem('/srv/p', () => {}, 'not connected').disabled, true);
});

test('local and remote commands: the other tab\'s in their own section, sent there', () => {
    const here = [], there = [];
    const cmds = [{name: 'status', command: 'git status'}, {name: 'push', command: 'git push', where: 'local'},
        {name: 'top', command: 'top', where: 'remote'}, {name: 'fetch', command: 'git fetch', where: 'local', group: 'git'},
        {name: 'bad', command: 'x', where: 'mars'}];
    const remote = commandItems(null, cmds, t => here.push(t), null,
        {tab: 'remote', sendOther: (t, c) => there.push([t, c.name])});
    assert.deepEqual(remote.map(i => i === '-' ? '-' : i.info ?? i.label), ['status  ⏎', 'top  ⏎', '-', 'Local tab', 'push  ⏎', '-', 'git']);
    remote[4].action();
    remote[6].submenu[0].action();
    remote[0].action();
    assert.deepEqual(there, [['git push\r', 'push'], ['git fetch\r', 'fetch']]);
    assert.deepEqual(here, ['git status\r']);
    // On the Local tab: local and unplaced ones here, the remote one in its section.
    const local = commandItems(null, cmds, () => {}, null, {tab: 'local', sendOther: () => {}});
    assert.deepEqual(local.map(i => i === '-' ? '-' : i.info ?? i.label), ['status  ⏎', 'push  ⏎', '-', 'git', '-', 'Remote tab', 'top  ⏎']);
    // Disabled (this tab not connected) does not disable the other tab's: it is started.
    const off = commandItems(null, cmds, () => {}, 'not connected', {tab: 'remote', sendOther: () => {}});
    assert.equal(off[0].disabled, true);
    assert.equal(off[4].disabled, false);
    // Without a tab: everything as before, in one list.
    assert.deepEqual(commandItems(null, cmds, () => {}).map(i => i === '-' ? '-' : i.label),
        ['status  ⏎', 'push  ⏎', 'top  ⏎', '-', 'git']);
});

test('example commands: only those the user lacks', async () => {
    const {missingCommands, commandLabel} = await import('./cmdmenu.js');
    const have = [{name: 'git-push', group: 'Git', command: 'git push --force'}, {name: 'Status?', group: 'text', command: 'x'}];
    const ex = [{name: 'git-push', group: 'Git', command: 'git push'}, {name: 'status?', group: 'Text', command: 'status?'},
        {name: 'git-push', group: '', command: 'git push'}, {name: 'continue', group: 'Text', command: 'you can continue'}];
    // Same group and name (ignoring case) is the user's own: kept, not added.
    assert.deepEqual(missingCommands(have, ex).map(commandLabel), ['git-push', 'Text › continue']);
    assert.deepEqual(missingCommands([], ex), ex);
    assert.deepEqual(missingCommands(ex, ex), []);
});
