import {test} from 'node:test';
import assert from 'node:assert/strict';
import {costText, dollars, sessionCost} from './cost.js';

test('dollars', () => {
    assert.equal(dollars(0), '$0.00');
    assert.equal(dollars(0.425), '$0.43');
    assert.equal(dollars(1234.5), '$1,234.50');
    assert.equal(dollars(undefined), '$0.00');
});

test('costText', () => {
    const c = costText({today: 4.12, total: 87.3, unpriced: [], since: ''});
    assert.equal(c.text, 'today $4.12 · total $87.30');
    assert.match(c.title, /API prices/);
    assert.doesNotMatch(c.title, /No price/);
    const u = costText({today: 1, total: 2, unpriced: ['claude-new-9'], today_partial: true, total_partial: true, since: '2026-11-10 09:00'});
    assert.equal(u.text, 'today ≥ $1.00 · total ≥ $2.00');
    // After a reset the total may have no reply of the unpriced model.
    assert.equal(costText({today: 1, total: 0, unpriced: ['x'], today_partial: true}).text, 'today ≥ $1.00 · total $0.00');
    assert.match(u.title, /claude-new-9/);
    assert.match(u.title, /from 2026-11-10 09:00/);
});

test('sessionCost', () => {
    assert.equal(sessionCost(undefined), '');
    assert.equal(sessionCost({today: 0, total: 0}), '');
    assert.equal(sessionCost({today: 0, total: 3.5}), 'Cost: today $0.00 · total $3.50');
});
