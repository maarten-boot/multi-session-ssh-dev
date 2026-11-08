import {test} from 'node:test';
import assert from 'node:assert/strict';
import {idleLabel, idleOptions} from './idle.js';

test('idleLabel', () => {
    assert.equal(idleLabel(''), 'never');
    assert.equal(idleLabel('off'), 'never');
    assert.equal(idleLabel('5m'), '5 min');
    assert.equal(idleLabel('2h'), '2 h');
    assert.equal(idleLabel('1h30m'), '1 h 30 min');
    assert.equal(idleLabel('1h30m0s'), '1 h 30 min');
    assert.equal(idleLabel('90s'), '90s'); // as written
});

test('idleOptions', () => {
    const g = idleOptions('', 'never', false);
    assert.deepEqual(g.map(o => o.value), ['', '5m', '10m', '15m', '30m', '1h', '2h']);
    assert.equal(g[0].text, 'never');
    assert.equal(g[1].text, 'after 5 min');
    const s = idleOptions('45m', 'default (never, from Settings)', true);
    assert.deepEqual(s.map(o => o.value), ['', 'off', '5m', '10m', '15m', '30m', '1h', '2h', '45m']);
    assert.equal(s[1].text, 'never');
    assert.equal(s.at(-1).text, 'after 45 min');
});
