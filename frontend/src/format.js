// Formatting for the Files tab's size and date columns.

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
