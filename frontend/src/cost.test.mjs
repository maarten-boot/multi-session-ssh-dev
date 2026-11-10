import {test} from 'node:test';
import assert from 'node:assert/strict';
import {age, costParts, costRows, costText, dollars, sessionCost, usageText, used, warnings} from './cost.js';

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

const NOW = 1_800_000_000;
const usage = {five_hour: {used: 23.7, resets_at: NOW + 3600}, seven_day: {used: 41.2, resets_at: NOW + 86400 * 3}, at: NOW - 30, host: 'agent'};

test('used and age', () => {
    assert.equal(used(null, NOW), null);
    assert.equal(used({used: 50, resets_at: NOW + 1}, NOW), 50);
    assert.equal(used({used: 50, resets_at: NOW - 1}, NOW), 0); // reset since
    assert.equal(age(10), 'just now');
    assert.equal(age(12 * 60), '12 min');
    assert.equal(age(3 * 3600), '3 h');
    assert.equal(age(3 * 86400), '3 days');
});

test('the figure with usage and warnings', () => {
    const s = {today: 12, total: 50, usage};
    let p = costParts(s, {}, NOW);
    assert.equal(p.text, 'today $12.00 · total $50.00 · 5h 23% · 7d 41%');
    assert.ok(p.parts.every(x => !x.warn));
    assert.match(p.title, /5-hour 23% \(resets .*\), 7-day 41%/);
    // Old data says how old.
    p = costParts({...s, usage: {...usage, at: NOW - 12 * 60}}, {}, NOW);
    assert.equal(p.text, 'today $12.00 · total $50.00 · 5h 23% · 7d 41% (12 min ago)');
    // Limits reached: those parts warn.
    const ui = {cost_daily_limit: 10, usage_5h_limit: 80, usage_7d_limit: 40};
    p = costParts(s, ui, NOW);
    assert.deepEqual(p.parts.map(x => x.warn), [true, false, false, true]);
    assert.deepEqual(warnings(s, ui, NOW), {daily: true, five: false, seven: true});
    assert.match(p.title, /above your limit of \$10\.00/);
    // 0 = no warning; a window that has reset is 0%.
    assert.deepEqual(warnings(s, {}, NOW), {daily: false, five: false, seven: false});
    const reset = {...usage, five_hour: {used: 95, resets_at: NOW - 5}};
    assert.equal(warnings({usage: reset}, {usage_5h_limit: 80}, NOW).five, false);
    assert.equal(costParts({usage: reset}, {}, NOW).parts[2].text, '5h 0%');
});

test('usageText', () => {
    assert.match(usageText(null, NOW), /none reported yet/);
    assert.match(usageText(usage, NOW), /^Subscription usage: 5-hour 23% \(resets \d\d:\d\d\), 7-day 41% \(resets \w+ \d\d:\d\d\) — reported just now on agent\.$/);
});

test('sessionCost', () => {
    assert.equal(sessionCost(undefined), '');
    assert.equal(sessionCost({today: 0, total: 0}), '');
    assert.equal(sessionCost({today: 0, total: 3.5}), 'Cost: today $0.00 · total $3.50');
});

test('costRows', () => {
    const {rows, sum} = costRows([{id: 'a', name: 'A', host: 'h'}, {id: 'b', name: 'B', host: 'h'}],
        {sessions: {a: {today: 1, total: 5}}});
    assert.deepEqual(rows, [{name: 'A', host: 'h', today: 1, total: 5}, {name: 'B', host: 'h', today: 0, total: 0}]);
    assert.deepEqual(sum, {today: 1, total: 5});
});
