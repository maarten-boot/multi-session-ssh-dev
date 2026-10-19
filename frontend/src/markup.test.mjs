import test from 'node:test';
import assert from 'node:assert/strict';
import {MARKUP, apply, toggleMarkup} from './markup.js';

// run toggles fmt on the selection marked with [ ] (or | for a cursor) and
// returns the result with the new selection marked the same way.
function run(marked, fmt) {
    const {open, close} = MARKUP[fmt];
    let text = marked, from, to;
    if (marked.includes('|')) {
        from = to = marked.indexOf('|');
        text = marked.replace('|', '');
    } else {
        from = marked.indexOf('[');
        to = marked.indexOf(']') - 1;
        text = marked.replace('[', '').replace(']', '');
    }
    const r = toggleMarkup(text, from, to, open, close);
    const out = apply(text, r.changes);
    if (r.anchor === r.head) return out.slice(0, r.anchor) + '|' + out.slice(r.anchor);
    return out.slice(0, r.anchor) + '[' + out.slice(r.anchor, r.head) + ']' + out.slice(r.head);
}

test('wrap and unwrap a word', () => {
    assert.equal(run('a [word] b', 'bold'), 'a **[word]** b');
    assert.equal(run('a **[word]** b', 'bold'), 'a [word] b');      // markers just outside
    assert.equal(run('a [**word**] b', 'bold'), 'a [word] b');      // markers selected too
    assert.equal(run('a [word] b', 'italic'), 'a _[word]_ b');
    assert.equal(run('a [word] b', 'underline'), 'a <u>[word]</u> b');
    assert.equal(run('a <u>[word]</u> b', 'underline'), 'a [word] b');
    assert.equal(run('a [word] b', 'strike'), 'a ~~[word]~~ b');
});

test('formats combine', () => {
    assert.equal(run('a **[word]** b', 'italic'), 'a **_[word]_** b');
    assert.equal(run('a **_[word]_** b', 'italic'), 'a **[word]** b');
});

test('whitespace at the ends stays outside', () => {
    assert.equal(run('a[ word ]b', 'bold'), 'a **[word]** b');
    assert.equal(run('[   ]', 'bold'), '[   ]');
});

test('cursor: insert a pair, remove an empty one', () => {
    assert.equal(run('a | b', 'bold'), 'a **|** b');
    assert.equal(run('a **|** b', 'bold'), 'a | b');
    assert.equal(run('|', 'underline'), '<u>|</u>');
});

test('several lines: each line, all or nothing', () => {
    assert.equal(run('[one\n\n  two]', 'bold'), '[**one**\n\n  **two**]');
    assert.equal(run('[**one**\n\n  **two**]', 'bold'), '[one\n\n  two]');
    assert.equal(run('[**one**\ntwo]', 'bold'), '[**one**\n**two**]');
});

test('MARKUP keys and markers', () => {
    assert.deepEqual(Object.keys(MARKUP), ['bold', 'italic', 'underline', 'strike']);
    for (const m of Object.values(MARKUP)) assert.ok(m.open && m.close && m.key && m.title);
});
