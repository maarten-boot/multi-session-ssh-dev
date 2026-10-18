import test from 'node:test';
import assert from 'node:assert/strict';
import {fmtDate, fmtSize} from './format.js';

test('dates are local YYYY-MM-DD HH:MM, zero-padded', () => {
    const sec = new Date(2021, 2, 4, 5, 6, 59).getTime() / 1000; // local time
    assert.equal(fmtDate(sec), '2021-03-04 05:06');
    assert.equal(fmtDate(new Date(1999, 11, 31, 23, 59).getTime() / 1000), '1999-12-31 23:59');
});

test('an unknown date is empty', () => {
    assert.equal(fmtDate(0), '');
    assert.equal(fmtDate(undefined), '');
});

test('sizes', () => {
    assert.equal(fmtSize(0), '0 B');
    assert.equal(fmtSize(1023), '1023 B');
    assert.equal(fmtSize(1536), '1.5 KB');
    assert.equal(fmtSize(34 * 1024), '34 KB');
    assert.equal(fmtSize(5 * 1024 ** 3), '5.0 GB');
});
