import {test} from 'node:test';
import assert from 'node:assert/strict';
import {engineName, isSVG, parseInline, parseMarkdown, toMarkdown, parseAnswer, svgURL, tokens} from './qaparse.js';

const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>';

test('text, code and svg parts', () => {
    const a = `Here:\n\n\`\`\`go\nfmt.Println("x")\n\`\`\`\n\nA picture:\n\`\`\`svg\n${svg}\n\`\`\`\nDone.`;
    assert.deepEqual(parseAnswer(a), [
        {type: 'text', text: 'Here:'},
        {type: 'code', lang: 'go', text: 'fmt.Println("x")', raw: '```go\nfmt.Println("x")\n```'},
        {type: 'text', text: 'A picture:'},
        {type: 'svg', text: svg, raw: '```svg\n' + svg + '\n```'},
        {type: 'text', text: 'Done.'},
    ]);
});

test('an open fence is code until it closes (streaming)', () => {
    assert.deepEqual(parseAnswer('Look:\n```svg\n<svg>'), [
        {type: 'text', text: 'Look:'},
        {type: 'code', lang: 'svg', text: '<svg>', raw: '```svg\n<svg>'},
    ]);
});

test('an svg block that is not one svg element stays code', () => {
    assert.deepEqual(parseAnswer('```svg\n<script>alert(1)</script>\n```'),
        [{type: 'code', lang: 'svg', text: '<script>alert(1)</script>', raw: '```svg\n<script>alert(1)</script>\n```'}]);
    assert.ok(isSVG('<?xml version="1.0"?>\n<!-- x -->\n' + svg));
    assert.ok(!isSVG(svg + '<p>after</p>'));
});

test('longer and tilde fences, Windows line ends', () => {
    // raw keeps the fences as written (for copies), the language as typed.
    assert.deepEqual(parseAnswer('````Md\n```\ninner\n```\n````\r\n~~~\nt\n~~~'), [
        {type: 'code', lang: 'md', text: '```\ninner\n```', raw: '````Md\n```\ninner\n```\n````'},
        {type: 'code', lang: '', text: 't', raw: '~~~\nt\n~~~'},
    ]);
});

test('svgURL and tokens', () => {
    assert.ok(svgURL(svg).startsWith('data:image/svg+xml;charset=utf-8,%3Csvg'));
    assert.equal(tokens({input: 3, cache_read: 2, cache_write: 1, output: 5}, 'm'), 'm · 6 tokens in (2 from cache), 5 out');
    assert.equal(tokens({input: 3, cache_read: 0, cache_write: 0, output: 5}), '3 tokens in, 5 out');
    assert.equal(tokens({input: 3, cache_read: 0, cache_write: 0, output: 5}, 'haiku', 'Claude Code'), 'Claude Code · haiku · 3 tokens in, 5 out');
    // A program that reports no tokens: no count.
    assert.equal(tokens({input: 0, cache_read: 0, cache_write: 0, output: 0}, '', 'gemini'), 'gemini');
    assert.equal(tokens(null, 'm'), 'm');
});

test('engineName', () => {
    assert.equal(engineName(undefined), 'Claude API');
    assert.equal(engineName('api'), 'Claude API');
    assert.equal(engineName('claude'), 'Claude Code');
    assert.equal(engineName('cmd:gemini'), 'gemini');
});

// flat is inline nodes as a short string: **x**, *x*, `x`, [x](url).
const flat = ns => ns.map(n => n.type === 'text' ? n.text : n.type === 'code' ? `\`${n.text}\`` :
    n.type === 'link' ? `[${flat(n.children)}](${n.url})` : `${n.type}(${flat(n.children)})`).join('');

test('parseInline', () => {
    assert.equal(flat(parseInline('a **bold** and *it* and __b2__ and _i2_ ~~gone~~')),
        'a strong(bold) and em(it) and strong(b2) and em(i2) del(gone)');
    assert.equal(flat(parseInline('**bold *inner* text**')), 'strong(bold em(inner) text)');
    assert.equal(flat(parseInline('use `a*b*c` here')), 'use `a*b*c` here');
    assert.equal(flat(parseInline('``a ` b``')), '`a ` b`');
    // Not marks: spaces inside, snake_case, a lone star, 2 * 3 * 4.
    assert.equal(flat(parseInline('snake_case_name and 2 * 3 * 4 and ** x**')), 'snake_case_name and 2 * 3 * 4 and ** x**');
    assert.equal(flat(parseInline('\\*not\\* italic')), '*not* italic');
    assert.equal(flat(parseInline('see [the docs](https://x.org/a_b) or <https://y.org> or https://z.org/p.')),
        'see [the docs](https://x.org/a_b) or [https://y.org](https://y.org) or [https://z.org/p](https://z.org/p).');
    // Only web and mail links.
    assert.equal(flat(parseInline('[x](javascript:alert(1))')), '[x](javascript:alert(1))');
    const link = parseInline('[a](https://q.org "t")')[0];
    assert.deepEqual([link.open, link.close], ['[', '](https://q.org "t")']);
});

test('parseMarkdown', () => {
    const bs = parseMarkdown([
        '# Title', 'Some **text**', 'on two lines.', '', '- one', '- [x] done', '  - nested', '1. first', '2) second',
        '', '> quoted *bit*', '> more', '', '---', '| a | b:|', '|---|--:|', '| 1 | 2 \\| 3 |', 'after',
    ].join('\n'));
    assert.deepEqual(bs.map(b => b.type), ['heading', 'para', 'list', 'quote', 'rule', 'table', 'para']);
    assert.equal(bs[0].level, 1);
    assert.equal(bs[1].md, 'Some **text**\non two lines.');
    assert.deepEqual(bs[2].items.map(i => [i.level, i.marker, i.task, flat(i.inline)]),
        [[0, '-', null, 'one'], [0, '-', true, 'done'], [1, '-', null, 'nested'], [0, '1.', null, 'first'], [0, '2)', null, 'second']]);
    assert.equal(bs[3].blocks[0].md, 'quoted *bit*\nmore');
    assert.deepEqual(bs[5].align, ['', 'right']);
    assert.equal(flat(bs[5].rows[0][1]), '2 | 3');
    assert.equal(bs[5].md, '| a | b:|\n|---|--:|\n| 1 | 2 \\| 3 |');
    // A list item's continuation line belongs to it.
    const li = parseMarkdown('- item\n  goes on\n- next')[0];
    assert.deepEqual(li.items.map(i => i.md), ['- item\n  goes on', '- next']);
});

// A fake DOM for toMarkdown: E(data, ...children), T(text); the selection
// covers whole nodes in set full and the given text of nodes in part.
const T = text => ({nodeType: 3, data: text});
const E = (dataset, ...childNodes) => ({nodeType: 1, dataset, childNodes});

test('toMarkdown', () => {
    const bold = T('bold');
    const strong = E({open: '**', close: '**'}, bold);
    const para = E({md: 'Some **bold** text'}, T('Some '), strong, T(' text'));
    const code = E({md: '```sh\necho\n```'}, E({skip: ''}, T('sh ⧉')), E({}, T('echo')));
    const table = E({md: '| a |\n|---|', whole: ''}, E({}, T('a')));
    const answer = E({sep: '\n\n'}, para, code, table);
    const all = new Set();
    const walk = n => (all.add(n), (n.childNodes ?? []).forEach(walk));
    walk(answer);
    const sel = (full, part = new Map()) => ({
        touches: n => full.has(n) || part.has(n) || (n.childNodes ?? []).some(c => sel(full, part).touches(c)),
        covers: n => full.has(n),
        text: n => part.get(n) ?? n.data,
    });
    // All selected: each block's source.
    assert.equal(toMarkdown(answer, sel(all)), 'Some **bold** text\n\n```sh\necho\n```\n\n| a |\n|---|');
    // Part of the bold word: the marks around it.
    assert.equal(toMarkdown(answer, sel(new Set(), new Map([[bold, 'ol']]))), '**ol**');
    // Inside a code block: the code only; a touched table: whole.
    const codeText = code.childNodes[1].childNodes[0];
    assert.equal(toMarkdown(answer, sel(new Set(), new Map([[codeText, 'ech'], [table.childNodes[0].childNodes[0], 'a']]))),
        'ech\n\n| a |\n|---|');
});
