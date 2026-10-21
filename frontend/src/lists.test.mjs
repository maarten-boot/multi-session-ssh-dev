import test from 'node:test';
import assert from 'node:assert/strict';
import {LISTS, apply, continueList, listItem, toggleList, toggleTasks} from './markup.js';

// mark/unmark: | is the cursor, [ ] a selection (as in markup.test.mjs).
function parse(marked) {
    if (marked.includes('|')) {
        const at = marked.indexOf('|');
        return {text: marked.replace('|', ''), from: at, to: at};
    }
    const from = marked.indexOf('{'), to = marked.indexOf('}') - 1;
    return {text: marked.replace('{', '').replace('}', ''), from, to};
}
function show(text, r) {
    const out = apply(text, r.changes);
    const a = r.anchor, h = r.head ?? r.anchor;
    if (a === h) return out.slice(0, a) + '|' + out.slice(a);
    return out.slice(0, a) + '{' + out.slice(a, h) + '}' + out.slice(h);
}
const list = (marked, kind) => {
    const {text, from, to} = parse(marked);
    return show(text, toggleList(text, from, to, kind));
};
const enter = marked => {
    const {text, from} = parse(marked);
    const r = continueList(text, from);
    return r && show(text, r);
};
const tick = marked => {
    const {text, from, to} = parse(marked);
    const r = toggleTasks(text, from, to);
    return r && show(text, r);
};

test('list items', () => {
    assert.deepEqual(listItem('  - [x] done'), {indent: '  ', marker: '-', box: '[x]', end: 8});
    assert.deepEqual(listItem('12. twelve'), {indent: '', marker: '12.', box: '', end: 4});
    assert.equal(listItem('-no space'), null);
    assert.equal(listItem('plain'), null);
});

test('bullets on and off', () => {
    assert.equal(list('one|', 'bullet'), '- one|');
    assert.equal(list('- one|', 'bullet'), 'one|');
    assert.equal(list('|', 'bullet'), '- |');                        // an empty line too
    assert.equal(list('{one\n\n  two}', 'bullet'), '{- one\n\n  - two}');   // indent kept, blank line stays
    assert.equal(list('{- one\ntwo}', 'bullet'), '{- one\n- two}');  // added where missing
    assert.equal(list('{- one\n* two}', 'bullet'), '{one\ntwo}');
    assert.equal(list('- [ ] task|', 'bullet'), '- task|');          // a check list item becomes a bullet
    assert.equal(list('1. first|', 'bullet'), '- first|');
});

test('check lists on and off', () => {
    assert.equal(list('buy milk|', 'task'), '- [ ] buy milk|');
    assert.equal(list('- buy milk|', 'task'), '- [ ] buy milk|');
    assert.equal(list('{- [x] a\n- [ ] b}', 'task'), '{a\nb}');
    assert.equal(list('{- [x] a\nb}', 'task'), '{- [x] a\n- [ ] b}');
});

test('Enter continues a list', () => {
    assert.equal(enter('- one|'), '- one\n- |');
    assert.equal(enter('  * one|'), '  * one\n  * |');
    assert.equal(enter('- [x] done|'), '- [x] done\n- [ ] |');      // a new box is empty
    assert.equal(enter('9. nine|'), '9. nine\n10. |');
    assert.equal(enter('- spl|it'), '- spl\n- |it');
    assert.equal(enter('- one\n- |'), '- one\n|');                   // empty item: the list ends
    assert.equal(enter('- [ ] |'), '|');
    assert.equal(enter('    - |'), '  - |');                        // nested: outdents first
    assert.equal(enter('plain|'), null);
    assert.equal(enter('|- one'), null);                            // before the marker
});

test('tick check boxes', () => {
    assert.equal(tick('- [ ] a|'), '- [x] a|');
    assert.equal(tick('  - [X] a|'), '  - [ ] a|');
    assert.equal(tick('{- [x] a\n- [ ] b}'), '{- [x] a\n- [x] b}');  // any unticked: all ticked
    assert.equal(tick('{- [x] a\n- [x] b}'), '{- [ ] a\n- [ ] b}');
    assert.equal(tick('- a|'), null);
});

test('LISTS keys', () => {
    for (const m of Object.values(LISTS)) assert.ok(m.label && m.title && m.key);
});
