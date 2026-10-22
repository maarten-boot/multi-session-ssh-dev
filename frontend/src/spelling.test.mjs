import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {Speller, rank, words} from './spelling.js';

const dict = n => readFileSync(new URL(`../../internal/spell/dict/en_US.${n}`, import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const sp = new Speller('en_US', dict('aff'), dict('dic'));
const list = line => [...words(line)].map(w => w.word);

test('which words are checked', () => {
    assert.deepEqual(list("Don't recieve teh parser’s output."), ["Don't", 'recieve', 'teh', 'parser’s', 'output']);
    assert.deepEqual(list('see main.go, ~/src/app and foo_bar or x1 v2.3'), ['see', 'and', 'or']);
    assert.deepEqual(list('CamelCase and HTTP but Capital'), ['and', 'but', 'Capital']);
    assert.deepEqual(list('mail me@example.com: done.'), ['mail', 'done']);
    assert.deepEqual(list('well-known état a'), ['well', 'known', 'état']);
});

test('checks and suggests', () => {
    for (const w of ['parser', 'Session', "don't", 'doesn’t', 'The', 'items']) assert.ok(sp.correct(w), w);
    for (const w of ['recieve', 'teh', 'Wokbench', 'tmuxx']) assert.ok(!sp.correct(w), w);
    for (const w of ['claude', 'tmux', 'ssh', 'GitHub']) assert.ok(sp.correct(w), w);   // TOOL_WORDS
    assert.equal(sp.suggest('recieve')[0], 'receive');
    assert.equal(sp.suggest('teh')[0], 'the');
    assert.equal(sp.suggest('Teh')[0], 'The');
    assert.equal(sp.suggest('definately')[0], 'definitely');
    assert.equal(sp.suggest('dont’')[0]?.includes("'"), false);
    assert.ok(sp.suggest('teh').length <= 6);
});

test('own words', () => {
    assert.ok(!sp.correct('kubectl'));
    sp.addWords(['kubectl', 'Zim']);
    assert.ok(sp.correct('kubectl') && sp.correct('Zim') && sp.correct('Kubectl')); // at the start of a sentence
    assert.equal(sp.ownWord('Kubectl'), 'kubectl');
    assert.equal(sp.ownWord('parser'), null);                       // in the dictionary, not added
    sp.removeWord('kubectl');
    assert.ok(!sp.correct('kubectl') && sp.ownWord('kubectl') === null);
    sp.removeWord('tmux');                                          // a tool word: never added, stays
    assert.ok(sp.correct('tmux'));
});

test('rank: closest first', () => {
    assert.deepEqual(rank('teh', ['ten', 'eh', 'the', 'tech']), ['the', 'ten', 'eh', 'tech']);
    assert.deepEqual(rank('Teh', ['the', 'The']), ['The']);
});
