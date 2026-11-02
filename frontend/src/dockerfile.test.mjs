import {test} from 'node:test';
import assert from 'node:assert/strict';
import {MARK, TOOLCHAINS, addToolchain} from './dockerfile.js';

test('addToolchain goes before the part the app needs', () => {
    const df = 'FROM ubuntu:24.04\nRUN apt-get install x\n\n' + MARK + ' (keep this part) ----\nARG SW_USER=dev\n';
    const out = addToolchain(df, TOOLCHAINS.Python, 'Python');
    assert.ok(out.indexOf('python3-venv') < out.indexOf(MARK));
    assert.ok(out.includes('# Python (added by the Dockerfile window)\nRUN apt-get update'));
    assert.ok(out.endsWith('ARG SW_USER=dev\n'));
    // Without the mark: at the end.
    const end = addToolchain('FROM x\n', TOOLCHAINS.Rust, 'Rust');
    assert.ok(end.startsWith('FROM x\n\n# Rust'));
    assert.ok(end.endsWith('rm -rf /var/lib/apt/lists/*\n'));
});

test('toolchain lines are valid Dockerfile continuations', () => {
    for (const [name, lines] of Object.entries(TOOLCHAINS)) {
        for (const l of lines.split('\n').slice(0, -1)) {
            if (!l.startsWith('ARG ')) assert.ok(l.endsWith(' \\'), `${name}: ${l}`);
        }
        assert.ok(!lines.trimEnd().endsWith('\\'), name);
    }
});
