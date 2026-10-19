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
