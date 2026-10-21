import test from 'node:test';
import assert from 'node:assert/strict';
import {parsePrompts, promptMenu} from './prompts.js';

test('prompts come from ## headings', () => {
    const notes = [
        'Notes about everything.',
        '',
        '## Review',
        '',
        'Review the diff for bugs.',
        'Be brief.',
        '',
        '### a sub heading stays in the text',
        '## Empty',
        '',
        '# Other notes',
        'not a prompt',
        '## Tests  ',
        'Run the tests.',
    ].join('\n');
    assert.deepEqual(parsePrompts(notes), [
        {name: 'Review', text: 'Review the diff for bugs.\nBe brief.\n\n### a sub heading stays in the text'},
        {name: 'Tests', text: 'Run the tests.'},
    ]);
});

test('no headings, no prompts; Windows line ends', () => {
    assert.deepEqual(parsePrompts('just text\nmore'), []);
    assert.deepEqual(parsePrompts('## A\r\nline one\r\nline two\r\n'), [{name: 'A', text: 'line one\nline two'}]);
});

test('headings inside fenced code blocks stay in the prompt', () => {
    const notes = [
        '## Deploy',
        'Run this:',
        '```sh',
        '# build first',
        'make build',
        '## not a prompt either',
        '```',
        'Then check.',
        '~~~~',
        '# tilde fence',
        '```',
        '~~~~',
        '## Next',
        'next text',
    ].join('\n');
    assert.deepEqual(parsePrompts(notes), [
        {name: 'Deploy', text: 'Run this:\n```sh\n# build first\nmake build\n## not a prompt either\n```\nThen check.\n~~~~\n# tilde fence\n```\n~~~~'},
        {name: 'Next', text: 'next text'},
    ]);
});

test('a fence left open keeps the rest of the page in the prompt', () => {
    const notes = ['## Open', 'text', '```', '## not a heading', '# nor this', 'end'].join('\n');
    assert.deepEqual(parsePrompts(notes), [
        {name: 'Open', text: 'text\n```\n## not a heading\n# nor this\nend'},
    ]);
});

test('indented, bare and unspaced ## are not headings', () => {
    const notes = ['## A', 'one', '  ## indented', '##', '##x', 'two'].join('\n');
    assert.deepEqual(parsePrompts(notes), [{name: 'A', text: 'one\n  ## indented\n##\n##x\ntwo'}]);
    assert.deepEqual(parsePrompts('  ## x\ntext'), []);
});

test('the Prompts menu: own prompts first, then those for all sessions', () => {
    const pasted = [];
    const items = promptMenu([{name: 'Tests', text: 'run them'}], [{name: 'Review', text: 'review it'}], t => pasted.push(t));
    assert.deepEqual(items.map(i => i === '-' ? '-' : i.info ?? i.label), ['This session', 'Tests', '-', 'All sessions', 'Review']);
    assert.equal(items[1].title, 'run them');
    items[4].action();
    assert.deepEqual(pasted, ['review it']);
    assert.deepEqual(promptMenu([], [{name: 'R', text: 'x'}], () => {}).map(i => i.info ?? i.label), ['All sessions', 'R']);
    assert.ok(promptMenu([], [], () => {}).every(i => i.info));   // only an explanation
});
