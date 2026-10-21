// Spelling in the notes: which words of a line to check, and a checker
// (nspell, Hunspell dictionaries from the backend, see internal/spell) with
// the user's own words. No other imports, so `node --test` can load it.
import nspell from 'nspell';

const WORD = /\p{L}[\p{L}\p{M}'’]*/gu;
// Next to these a word is part of a name, path, address or number.
const NAMEISH = /[\p{N}_/\\@#$%&=~`.]/u;

// words yields the words of line worth checking as {from, to, word}
// (offsets in line): not single letters, not CamelCase or ACRONYMS, not
// parts of file names, paths, addresses or identifiers.
export function* words(line) {
    for (const m of line.matchAll(WORD)) {
        const word = m[0].replace(/['’]+$/, '');
        const from = m.index, to = from + word.length;
        const before = line[from - 1] ?? '', after = line[to] ?? '';
        if (word.length < 2 || /\p{Lu}/u.test(word.slice(1))) continue;
        if (NAMEISH.test(before) || (after !== '.' && NAMEISH.test(after)) || (after === '.' && /\p{L}|\p{N}/u.test(line[to + 1] ?? ''))) continue;
        yield {from, to, word};
    }
}

// distance is the Damerau–Levenshtein distance (adjacent swaps count one).
function distance(a, b) {
    const d = Array.from({length: a.length + 1}, (_, i) => [i, ...Array(b.length).fill(0)]);
    for (let j = 1; j <= b.length; j++) d[0][j] = j;
    for (let i = 1; i <= a.length; i++) {
        for (let j = 1; j <= b.length; j++) {
            const cost = a[i - 1] === b[j - 1] ? 0 : 1;
            d[i][j] = Math.min(d[i - 1][j] + 1, d[i][j - 1] + 1, d[i - 1][j - 1] + cost);
            if (i > 1 && j > 1 && a[i - 1] === b[j - 2] && a[i - 2] === b[j - 1]) d[i][j] = Math.min(d[i][j], d[i - 2][j - 2] + 1);
        }
    }
    return d[a.length][b.length];
}

// rank orders suggestions for word, closest first (ignoring case; the same
// letters in another order go first among equals: "teh" → "the" before
// "ten"), with a capital first letter when the word has one.
export function rank(word, list) {
    const w = word.toLowerCase();
    const letters = x => [...x].sort().join('');
    const cap = /^\p{Lu}/u.test(word);
    const seen = new Set();
    return list.map((s, i) => ({s: cap ? s[0].toUpperCase() + s.slice(1) : s,
        d: distance(w, s.toLowerCase()) - (letters(w) === letters(s.toLowerCase()) ? 0.5 : 0), i}))
        .sort((a, b) => a.d - b.d || a.i - b.i)
        .map(x => x.s).filter(s => !seen.has(s) && seen.add(s));
}

// TOOL_WORDS are known in every language: the tools this app is about.
export const TOOL_WORDS = ['claude', 'Claude', 'tmux', 'ssh', 'sshfs', 'rsync', 'git', 'GitHub', 'repo', 'repos', 'config',
    'frontend', 'backend', 'localhost', 'Wails', 'xterm', 'npm', 'Makefile'];

// Speller checks words against a dictionary plus TOOL_WORDS and the user's
// own words.
export class Speller {
    constructor(lang, aff, dic) {
        this.lang = lang;
        this.ns = nspell(aff, dic);
        this.own = new Set();
        this.cache = new Map();
        for (const w of TOOL_WORDS) this.ns.add(w);
    }

    // addWords adds the user's own words (again: known ones are skipped).
    addWords(list) {
        for (const w of list) {
            if (this.own.has(w)) continue;
            this.own.add(w);
            this.ns.add(w);
            this.cache.clear();
        }
    }

    correct(word) {
        let ok = this.cache.get(word);
        if (ok === undefined) {
            const w = word.replace(/’/g, "'");
            ok = this.ns.correct(w) || this.own.has(w.toLowerCase()) || this.own.has(w);
            this.cache.set(word, ok);
        }
        return ok;
    }

    // suggest gives at most max corrections, closest first; with a curly
    // apostrophe in the word they have one too.
    suggest(word, max = 6) {
        const curly = word.includes('’');
        return rank(word, this.ns.suggest(word.replace(/’/g, "'"))).slice(0, max).map(s => curly ? s.replace(/'/g, '’') : s);
    }
}
