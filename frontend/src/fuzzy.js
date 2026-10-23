// Fuzzy matching for the quick switcher. No imports, so `node --test` can
// load it directly.

const isBoundary = (s, i) => i === 0 || /[\s/\-_.:@]/.test(s[i - 1]) || (s[i - 1] === s[i - 1].toLowerCase() && s[i] !== s[i].toLowerCase());

// score returns how well query matches text as a subsequence
// (case-insensitive), or -1 if it does not. Consecutive characters and
// matches at word starts score higher; earlier matches beat later ones.
// positions receives the matched indices when given.
export function score(query, text, positions = null) {
    const q = query.toLowerCase();
    const t = text.toLowerCase();
    if (!q) return 0;
    let best = -1;
    let bestPos = null;
    // Try each occurrence of the first character as the start; greedy from
    // there. Cheap and good enough for short session names and paths.
    for (let start = t.indexOf(q[0]); start !== -1; start = t.indexOf(q[0], start + 1)) {
        let s = 0;
        let prev = -2;
        const pos = [];
        let ti = start;
        for (let qi = 0; qi < q.length; qi++, ti++) {
            ti = t.indexOf(q[qi], ti);
            if (ti === -1) {
                s = -1;
                break;
            }
            s += 1;
            if (ti === prev + 1) s += 4;
            if (isBoundary(text, ti)) s += 3;
            pos.push(ti);
            prev = ti;
        }
        if (s < 0) break; // later starts cannot match either
        s -= pos[0] * 0.05;
        if (s > best) {
            best = s;
            bestPos = pos;
        }
    }
    if (positions && bestPos) positions.push(...bestPos);
    return best;
}

// rank orders items by match quality for query. fields(item) returns
// [text, weight, asTyped] entries; an item's score is its best weighted
// field score. An asTyped field matches only when it contains the query
// (case-insensitive), scoring as a whole-query match: for long text, where a
// subsequence would match nearly anything. With an empty query the input
// order is kept.
export function rank(items, query, fields) {
    if (!query.trim()) return items.slice();
    const out = [];
    items.forEach((item, i) => {
        let best = -1;
        const q = query.trim();
        for (const [text, weight, asTyped] of fields(item)) {
            const s = !asTyped ? score(q, text || '') : (text || '').toLowerCase().includes(q.toLowerCase()) ? score(q, q) : -1;
            if (s >= 0) best = Math.max(best, s * weight);
        }
        if (best >= 0) out.push({item, best, i});
    });
    out.sort((a, b) => b.best - a.best || a.i - b.i);
    return out.map(o => o.item);
}
