import test from 'node:test';
import assert from 'node:assert/strict';
import {fmtDate, fmtSize, sortEntries} from './format.js';

test('dates are local YYYY-MM-DD HH:MM, zero-padded', () => {
    const sec = new Date(2021, 2, 4, 5, 6, 59).getTime() / 1000; // local time
    assert.equal(fmtDate(sec), '2021-03-04 05:06');
    assert.equal(fmtDate(new Date(1999, 11, 31, 23, 59).getTime() / 1000), '1999-12-31 23:59');
    const old = new Date(2000, 0, 2, 3, 4);
    old.setFullYear(999);
    assert.equal(fmtDate(old.getTime() / 1000), '0999-01-02 03:04'); // fixed width
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
    assert.equal(fmtSize(1024), '1.0 KB');
    assert.equal(fmtSize(1024 * 1024 - 1), '1.0 MB'); // not "1024 KB"
    assert.equal(fmtSize(1024 ** 3 - 1), '1.0 GB');
});

test('sort orders keep folders first, ties by name', () => {
    const e = [
        {name: 'b.txt', mtime: 300}, {name: 'A.txt', mtime: 100}, {name: 'src', dir: true, mtime: 50},
        {name: 'docs', dir: true, mtime: 900}, {name: 'c.txt', mtime: 300}, {name: 'new', mtime: 0},
    ];
    const names = sort => sortEntries(e, sort).map(x => x.name);
    assert.deepEqual(names('name'), ['docs', 'src', 'A.txt', 'b.txt', 'c.txt', 'new']);
    assert.deepEqual(names('newest'), ['docs', 'src', 'b.txt', 'c.txt', 'A.txt', 'new']);
    assert.deepEqual(names('oldest'), ['src', 'docs', 'A.txt', 'b.txt', 'c.txt', 'new']); // unknown date last
    assert.deepEqual(names('bogus'), names('name'));
    assert.equal(e[0].name, 'b.txt', 'input not changed');
});
