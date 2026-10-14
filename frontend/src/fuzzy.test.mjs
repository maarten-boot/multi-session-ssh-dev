import {test} from 'node:test';
import assert from 'node:assert/strict';

import {rank, score} from './fuzzy.js';

test('score: subsequence, case-insensitive', () => {
    assert.ok(score('wb', 'workbench') >= 0);
    assert.ok(score('WB', 'workbench') >= 0);
    assert.equal(score('xyz', 'workbench'), -1);
    assert.equal(score('bw', 'workbench'), -1); // order matters
    assert.equal(score('', 'anything'), 0);
});

test('score: consecutive and word starts beat scattered', () => {
    assert.ok(score('work', 'workbench') > score('work', 'w-o-r-k'));
    assert.ok(score('msd', 'multi-session-dev') > score('msd', 'mismatched'));
    assert.ok(score('bench', 'workbench') > score('bench', 'b-e-n-c-h'));
});

test('score: positions', () => {
    const pos = [];
    score('sw', 'session-workbench', pos);
    assert.deepEqual(pos, [0, 8]);
});

test('rank: best weighted field first, unmatched dropped, stable', () => {
    const items = [
        {name: 'terminal-app', path: '/share/workspace/terminal-app'},
        {name: 'workbench', path: '/share/DEV/multi-session-ssh-dev'},
        {name: 'DEV', path: '/share/DEV'},
        {name: 'notes', path: '/share/notes'},
    ];
    const fields = it => [[it.name, 2], [it.path, 1]];
    assert.deepEqual(rank(items, 'wb', fields).map(i => i.name), ['workbench']);
    assert.deepEqual(rank(items, 'dev', fields).map(i => i.name)[0], 'DEV');
    assert.deepEqual(rank(items, 'ssh', fields).map(i => i.name), ['workbench']);
    assert.deepEqual(rank(items, '', fields).map(i => i.name), items.map(i => i.name));
    assert.deepEqual(rank(items, 'zzz', fields), []);
});
