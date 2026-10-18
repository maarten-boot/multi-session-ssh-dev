// Helpers for the Files tab (no DOM, so node can test them): size and date
// columns, and the sort order.

// fmtDate gives local time as YYYY-MM-DD HH:MM: fixed width, so the column
// lines up and sorts by eye; "" when unknown.
export function fmtDate(sec) {
    if (!sec) return '';
    const d = new Date(sec * 1000);
    const p = n => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

// fmtSize gives a short human size: 512 B, 1.5 KB, 34 KB.
export function fmtSize(n) {
    if (n < 1024) return `${n} B`;
    const u = ['KB', 'MB', 'GB', 'TB'];
    let i = -1;
    do {
        n /= 1024;
        i++;
    } while (n >= 1024 && i < u.length - 1);
    return `${n < 10 ? n.toFixed(1) : Math.round(n)} ${u[i]}`;
}

// SORTS are the Files tab's orders. Folders always come first; ties fall
// back to the name (case-insensitive, like the listing from the backend).
export const SORTS = {name: 'Name', newest: 'Newest first', oldest: 'Oldest first'};

// sortEntries returns entries in the given order (unknown: by name).
export function sortEntries(entries, sort) {
    const byName = (a, b) => {
        const x = a.name.toLowerCase(), y = b.name.toLowerCase();
        return x < y ? -1 : x > y ? 1 : 0;
    };
    const byDate = {newest: (a, b) => (b.mtime || 0) - (a.mtime || 0), oldest: (a, b) => (a.mtime || 0) - (b.mtime || 0)}[sort];
    return [...entries].sort((a, b) => (!!b.dir - !!a.dir) || (byDate && byDate(a, b)) || byName(a, b));
}
