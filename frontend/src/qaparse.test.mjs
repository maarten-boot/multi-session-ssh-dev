import {test} from 'node:test';
import assert from 'node:assert/strict';
import {engineName, isSVG, parseAnswer, svgURL, tokens} from './qaparse.js';

const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>';

test('text, code and svg parts', () => {
    const a = `Here:\n\n\`\`\`go\nfmt.Println("x")\n\`\`\`\n\nA picture:\n\`\`\`svg\n${svg}\n\`\`\`\nDone.`;
    assert.deepEqual(parseAnswer(a), [
        {type: 'text', text: 'Here:'},
        {type: 'code', lang: 'go', text: 'fmt.Println("x")'},
        {type: 'text', text: 'A picture:'},
        {type: 'svg', text: svg},
        {type: 'text', text: 'Done.'},
    ]);
});

test('an open fence is code until it closes (streaming)', () => {
    assert.deepEqual(parseAnswer('Look:\n```svg\n<svg>'), [
        {type: 'text', text: 'Look:'},
        {type: 'code', lang: 'svg', text: '<svg>'},
    ]);
});

test('an svg block that is not one svg element stays code', () => {
    assert.deepEqual(parseAnswer('```svg\n<script>alert(1)</script>\n```'),
        [{type: 'code', lang: 'svg', text: '<script>alert(1)</script>'}]);
    assert.ok(isSVG('<?xml version="1.0"?>\n<!-- x -->\n' + svg));
    assert.ok(!isSVG(svg + '<p>after</p>'));
});

test('longer and tilde fences, Windows line ends', () => {
    assert.deepEqual(parseAnswer('````md\n```\ninner\n```\n````\r\n~~~\nt\n~~~'), [
        {type: 'code', lang: 'md', text: '```\ninner\n```'},
        {type: 'code', lang: '', text: 't'},
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
