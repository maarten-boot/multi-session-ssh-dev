// Formatting in the notes editor: toggling Markdown markers around the
// selection. No imports, so `node --test` can load it.

// MARKUP are the formats of the notes toolbar and keys. Markdown has no
// underline: <u>…</u> (GitHub shows it). Italic uses _ so it never mixes
// up with the ** of bold.
export const MARKUP = {
    bold: {open: '**', close: '**', label: 'B', title: 'Bold', key: 'Mod-b'},
    italic: {open: '_', close: '_', label: 'I', title: 'Italic', key: 'Mod-i'},
    underline: {open: '<u>', close: '</u>', label: 'U', title: 'Underline', key: 'Mod-u'},
    strike: {open: '~~', close: '~~', label: 'S', title: 'Strikethrough', key: 'Mod-Shift-x'},
};

// toggleMarkup adds or removes open/close around [from, to) of text and
// returns {changes: [{from, to, insert}] (ascending, in text's positions),
// anchor, head} (the new selection, in the changed text's positions).
// - No selection: inside an empty pair the pair goes, else one is inserted
//   with the cursor between.
// - One line: whitespace at the ends stays outside the markers (Markdown
//   ignores "** x**"); markers inside or just outside the selection are
//   removed, else added.
// - Several lines: each non-blank line separately; removed when every line
//   has them, else added where missing.
export function toggleMarkup(text, from, to, open, close) {
    const o = open.length, c = close.length;
    const at = (p, s) => text.startsWith(s, p);
    if (from === to) {
        if (from >= o && at(from - o, open) && at(from, close)) {
            return {changes: [{from: from - o, to: from + c, insert: ''}], anchor: from - o, head: from - o};
        }
        return {changes: [{from, to, insert: open + close}], anchor: from + o, head: from + o};
    }
    const segs = [];
    let p = from;
    for (const line of text.slice(from, to).split('\n')) {
        const s = p + (line.length - line.trimStart().length);
        const e = p + line.trimEnd().length;
        if (e > s) segs.push({s, e});
        p += line.length + 1;
    }
    if (!segs.length) return {changes: [], anchor: from, head: to};
    const inner = g => g.e - g.s >= o + c && at(g.s, open) && at(g.e - c, close);
    if (segs.length === 1) {
        const {s, e} = segs[0];
        if (inner(segs[0])) {
            return {changes: [{from: s, to: s + o, insert: ''}, {from: e - c, to: e, insert: ''}], anchor: s, head: e - o - c};
        }
        if (s >= o && at(s - o, open) && at(e, close)) {
            return {changes: [{from: s - o, to: s, insert: ''}, {from: e, to: e + c, insert: ''}], anchor: s - o, head: e - o};
        }
        return {changes: [{from: s, to: s, insert: open}, {from: e, to: e, insert: close}], anchor: s + o, head: e + o};
    }
    const remove = segs.every(inner);
    const changes = [];
    for (const g of segs) {
        if (remove) changes.push({from: g.s, to: g.s + o, insert: ''}, {from: g.e - c, to: g.e, insert: ''});
        else if (!inner(g)) changes.push({from: g.s, to: g.s, insert: open}, {from: g.e, to: g.e, insert: close});
    }
    // The selection grows to take in markers added at its ends.
    const map = (q, end) => changes.reduce((n, ch) => ch.to < q || (end && ch.to === q) ? n + ch.insert.length - (ch.to - ch.from) : n, q);
    return {changes, anchor: map(from, false), head: map(to, true)};
}

// apply makes the changes to text (for tests and callers without an editor).
export function apply(text, changes) {
    let out = text;
    for (const ch of [...changes].reverse()) out = out.slice(0, ch.from) + ch.insert + out.slice(ch.to);
    return out;
}

// LISTS are the list formats of the toolbar and keys: Markdown "- item"
// and the check list "- [ ] item" / "- [x] item" (GitHub's task lists).
export const LISTS = {
    bullet: {label: '•', title: 'Bulleted list', key: 'Mod-Shift-8'},
    task: {label: '☐', title: 'Check list', key: 'Mod-Shift-9'},
};

// ITEM matches the start of a list line: indent, marker (- * + or 1. 1)),
// and a check box.
const ITEM = /^([ \t]*)([-*+]|\d{1,9}[.)]) (\[[ xX]\](?: |$))?/;

// listItem parses a line: null, or {indent, marker, box (the "[ ]" text or
// ""), end (where the item's text starts)}.
export function listItem(line) {
    const m = ITEM.exec(line);
    return m && {indent: m[1], marker: m[2], box: (m[3] ?? '').trimEnd(), end: m[0].length};
}

// mapPos moves pos over ascending changes; inside a replaced range it goes
// to the end of the new text. before: text inserted at pos goes after it
// (the start of a selection takes in markers added there).
const mapPos = (changes, pos, before = false) => changes.reduce((n, ch) =>
    before && ch.from === pos ? n
        : ch.to <= pos ? n + ch.insert.length - (ch.to - ch.from) : ch.from < pos ? n + ch.from + ch.insert.length - pos : n, pos);

// toggleList makes the lines of [from, to) list items of kind (bullet or
// task), or plain lines when they all are already; blank lines stay.
// Returns {changes, anchor, head} like toggleMarkup.
export function toggleList(text, from, to, kind) {
    const start = text.lastIndexOf('\n', from - 1) + 1;
    let end = text.indexOf('\n', to > from && text[to - 1] === '\n' ? to - 1 : to);
    if (end < 0) end = text.length;
    const lines = [];
    for (let p = start; p <= end;) {
        let e = text.indexOf('\n', p);
        if (e < 0 || e > end) e = end;
        const line = text.slice(p, e);
        if (line.trim() || (from === to && p === start)) lines.push({at: p, line, item: listItem(line)});
        p = e + 1;
    }
    const is = it => it && (kind === 'task' ? it.box !== '' : it.box === '' && /^[-*+]$/.test(it.marker));
    const remove = lines.every(l => is(l.item));
    const mark = kind === 'task' ? '- [ ] ' : '- ';
    const changes = [];
    for (const {at, line, item} of lines) {
        const indent = item ? item.indent.length : line.length - line.trimStart().length;
        const old = item ? item.end : indent;
        if (remove) changes.push({from: at + indent, to: at + old, insert: ''});
        else if (!is(item)) changes.push({from: at + indent, to: at + old, insert: mark});
    }
    return {changes, anchor: mapPos(changes, from, from !== to), head: mapPos(changes, to)};
}

// continueList is Enter at pos (no selection) in a list item: the next item
// starts with the same marker (numbers count up, check boxes empty). In an
// empty item Enter outdents it, or at the left edge ends the list. null:
// not in a list item, Enter is a plain new line.
export function continueList(text, pos) {
    const start = text.lastIndexOf('\n', pos - 1) + 1;
    let end = text.indexOf('\n', pos);
    if (end < 0) end = text.length;
    const line = text.slice(start, end);
    const it = listItem(line);
    if (!it || pos < start + it.end) return null;
    if (!line.slice(it.end).trim()) {
        if (it.indent) {
            const cut = it.indent.startsWith('\t') ? 1 : Math.min(2, it.indent.length - it.indent.trimStart().length);
            return {changes: [{from: start, to: start + cut, insert: ''}], anchor: pos - cut};
        }
        return {changes: [{from: start, to: end, insert: ''}], anchor: start};
    }
    const n = /^\d+/.exec(it.marker);
    const marker = n ? String(Number(n[0]) + 1) + it.marker.slice(n[0].length) : it.marker;
    const insert = '\n' + it.indent + marker + ' ' + (it.box ? '[ ] ' : '');
    return {changes: [{from: pos, to: pos, insert}], anchor: pos + insert.length};
}

// toggleTasks ticks or unticks the check boxes of the lines of [from, to):
// all ticked when any is not, else all unticked. null: no check boxes there.
export function toggleTasks(text, from, to) {
    const start = text.lastIndexOf('\n', from - 1) + 1;
    const boxes = [];
    for (let p = start; p <= to;) {
        let e = text.indexOf('\n', p);
        if (e < 0) e = text.length;
        const it = listItem(text.slice(p, e));
        if (it?.box) boxes.push({at: p + it.indent.length + it.marker.length + 1, box: it.box});
        p = e + 1;
    }
    if (!boxes.length) return null;
    const tick = boxes.some(b => b.box === '[ ]');
    return {changes: boxes.map(b => ({from: b.at + 1, to: b.at + 2, insert: tick ? 'x' : ' '})), anchor: from, head: to};
}
