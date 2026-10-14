import {test} from 'node:test';
import assert from 'node:assert/strict';

import {SgrFilter, effectiveColorMode, stripColors, termOptions} from './appearance.js';

const enc = s => new TextEncoder().encode(s);
const dec = b => new TextDecoder().decode(b);
const run = (...chunks) => {
    const f = new SgrFilter();
    return chunks.map(c => dec(f.filter(enc(c)))).join('');
};

test('stripColors keeps attributes and drops colors', () => {
    assert.equal(stripColors(''), '');
    assert.equal(stripColors('0'), '0');
    assert.equal(stripColors('1;31'), '1');
    assert.equal(stripColors('31'), null);
    assert.equal(stripColors('1;4;7;33;44'), '1;4;7');
    assert.equal(stripColors('38;5;196;1'), '1');
    assert.equal(stripColors('38;2;255;128;0;4'), '4');
    assert.equal(stripColors('48;2;1;2;3'), null);
    assert.equal(stripColors('0;38;5;10;48;5;0;22'), '0;22');
    assert.equal(stripColors('38:2::255:0:0;1'), '1');
    assert.equal(stripColors('4:3;58:5:9'), '4:3');
    assert.equal(stripColors('39;49;1'), '1');
    assert.equal(stripColors('91;102'), null);
    assert.equal(stripColors(';31'), '');
});

test('filter rewrites SGR and leaves everything else', () => {
    assert.equal(run('\x1b[1;31mred\x1b[0m plain'), '\x1b[1mred\x1b[0m plain');
    assert.equal(run('\x1b[32mgreen\x1b[m'), 'green\x1b[m');
    assert.equal(run('\x1b[38;2;255;128;0mtc\x1b[39m'), 'tc');
    // Cursor movement, private modes, SGR-like with private markers, OSC: untouched.
    for (const s of ['\x1b[2J\x1b[H', '\x1b[?1049h', '\x1b[>4;1m', '\x1b]0;title\x07', '\x1b(B', '\x1b[3 q']) {
        assert.equal(run(s), s);
    }
    assert.equal(run('héllo ✓ 日本'), 'héllo ✓ 日本');
});

test('sequences split across chunks', () => {
    const s = '\x1b[1;38;5;196mX\x1b[0m';
    for (let cut = 1; cut < s.length; cut++) {
        assert.equal(run(s.slice(0, cut), s.slice(cut)), '\x1b[1mX\x1b[0m', `cut at ${cut}`);
    }
    assert.equal(run('a\x1b', '[31mb'), 'ab');
    assert.equal(run('a\x1b', 'Mb'), 'a\x1bMb'); // ESC M (reverse index), split
});

test('runaway CSI is passed through, not buffered forever', () => {
    const junk = '\x1b[' + '1'.repeat(300);
    assert.equal(run(junk + 'x'), junk + 'x');
});

test('termOptions', () => {
    const ui = {theme: 'light', min_contrast: 4.5, font_size: 14};
    assert.equal(termOptions(ui, 'full').minimumContrastRatio, 4.5);
    assert.equal(termOptions(ui, 'mono').minimumContrastRatio, 21);
    assert.equal(termOptions(ui, 'full').theme.background, '#ffffff');
    assert.equal(termOptions({...ui, theme: 'dark'}, 'full').theme.background, '#1e1e1e');
    assert.equal(termOptions(ui, 'full').drawBoldTextInBrightColors, false);
    assert.equal(effectiveColorMode({color_mode: 'full'}, {color_mode: ''}), 'full');
    assert.equal(effectiveColorMode({color_mode: 'full'}, {color_mode: 'mono'}), 'mono');
    assert.equal(effectiveColorMode({color_mode: 'mono'}, null), 'mono');
});
