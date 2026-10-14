import {test} from 'node:test';
import assert from 'node:assert/strict';

import {actionFor, buildKeymap, format, parse} from './keys.js';

const ev = (code, mods = {}) => ({type: 'keydown', code, ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, ...mods});

test('parse', () => {
    assert.deepEqual(parse('Ctrl+Shift+PageDown'), {ctrl: true, shift: true, alt: false, meta: false, code: 'PageDown'});
    assert.deepEqual(parse('ctrl+alt+3'), {ctrl: true, shift: false, alt: true, meta: false, code: 'Digit3'});
    assert.equal(parse('Cmd+Shift+]').code, 'BracketRight');
    assert.equal(parse('Ctrl+Shift+,').code, 'Comma');
    assert.equal(parse('Ctrl+F5').code, 'F5');
    assert.equal(parse('F6').code, 'F6'); // function keys alone are fine
    assert.equal(parse('Mod+K', true).meta, true);
    assert.equal(parse('Mod+K', false).ctrl, true);
    assert.equal(parse('none'), null);
    assert.equal(parse(''), null);
    for (const bad of ['Ctrl+Hyper+K', 'Ctrl+Shift+Banana', 'Shift+N', 'N', 'Ctrl+']) {
        assert.throws(() => parse(bad), Error, bad);
    }
});

test('defaults per OS', () => {
    const lin = buildKeymap({}, false);
    const mac = buildKeymap({}, true);
    assert.deepEqual(lin.errors, []);
    assert.deepEqual(mac.errors, []);
    assert.equal(actionFor(ev('PageDown', {ctrlKey: true, shiftKey: true}), lin.map), 'next_session');
    assert.equal(actionFor(ev('BracketRight', {metaKey: true, shiftKey: true}), mac.map), 'next_session');
    assert.equal(actionFor(ev('Digit4', {ctrlKey: true, altKey: true}), lin.map), 'session_4');
    assert.equal(actionFor(ev('Digit4', {metaKey: true}), mac.map), 'session_4');
    assert.equal(actionFor(ev('Digit2', {ctrlKey: true, shiftKey: true}), lin.map), 'tab_local');
    assert.equal(actionFor(ev('KeyP', {ctrlKey: true, shiftKey: true}), lin.map), 'quick_switcher');
    // Exact modifiers: Ctrl+Shift+Alt+1 is not Ctrl+Shift+1, plain keys go to the terminal.
    assert.equal(actionFor(ev('Digit1', {ctrlKey: true, shiftKey: true, altKey: true}), lin.map), null);
    assert.equal(actionFor(ev('KeyC', {ctrlKey: true}), lin.map), null);
    assert.equal(actionFor({...ev('KeyP', {ctrlKey: true, shiftKey: true}), type: 'keyup'}, lin.map), null);
});

test('overrides, disabling, errors and conflicts', () => {
    const {map, errors} = buildKeymap({
        next_session: 'Ctrl+Tab',
        prev_session: 'Ctrl+Shift+Tab',
        settings: 'none',
        quick_switcher: 'Ctrl+Shift+Banana',
        bogus_action: 'Ctrl+B',
        new_session: 'Ctrl+Shift+1', // clashes with tab_remote
    }, false);
    assert.equal(actionFor(ev('Tab', {ctrlKey: true}), map), 'next_session');
    assert.equal(actionFor(ev('Tab', {ctrlKey: true, shiftKey: true}), map), 'prev_session');
    assert.equal(map.settings, null);
    assert.equal(actionFor(ev('KeyP', {ctrlKey: true, shiftKey: true}), map), 'quick_switcher'); // bad override keeps default
    assert.equal(errors.length, 3);
    assert.match(errors.join('\n'), /Banana/);
    assert.match(errors.join('\n'), /bogus_action/);
    assert.match(errors.join('\n'), /both use Ctrl\+Shift\+1/);
    assert.equal([map.tab_remote, map.new_session].filter(Boolean).length, 1);
});

test('format', () => {
    assert.equal(format(parse('Ctrl+Shift+PageDown'), false), 'Ctrl+Shift+PgDn');
    assert.equal(format(parse('Ctrl+Alt+1'), false), 'Ctrl+Alt+1');
    assert.equal(format(parse('Cmd+Shift+]', true), true), '⇧⌘]');
    assert.equal(format(parse('Cmd+,', true), true), '⌘,');
    assert.equal(format(null, false), '');
});
