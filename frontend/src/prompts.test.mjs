import test from 'node:test';
import assert from 'node:assert/strict';
import {parsePrompts} from './prompts.js';

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
