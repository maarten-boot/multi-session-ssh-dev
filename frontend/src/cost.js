// What claude cost at API prices and the subscription usage (SPEC F18):
// the figure in the top bar, a session's line in the list balloon, the
// warnings and the Cost page of Settings.

// dollars says an amount: $0.42, $12.30, $1,234.50.
export function dollars(v) {
    return '$' + (v ?? 0).toLocaleString('en-US', {minimumFractionDigits: 2, maximumFractionDigits: 2});
}

// used is a usage window's percentage now: 0 once it has reset (now and
// resets_at in Unix seconds); null without the window.
export function used(w, now) {
    if (!w) return null;
    return w.resets_at && w.resets_at <= now ? 0 : w.used;
}

// age says how long ago (seconds): "just now", "12 min", "3 h", "2 days".
export function age(sec) {
    if (sec < 60) return 'just now';
    if (sec < 3600) return `${Math.floor(sec / 60)} min`;
    if (sec < 2 * 86400) return `${Math.floor(sec / 3600)} h`;
    return `${Math.floor(sec / 86400)} days`;
}

// resetText says when a window resets: the time today, else weekday and time.
export function resetText(at, now) {
    const d = new Date(at * 1000);
    const hm = d.toLocaleTimeString('en-GB', {hour: '2-digit', minute: '2-digit'});
    return new Date(now * 1000).toDateString() === d.toDateString() ? hm : `${d.toLocaleDateString('en-GB', {weekday: 'short'})} ${hm}`;
}

// warnings: which limits (ui: cost_daily_limit dollars, usage_5h_limit and
// usage_7d_limit percent; 0 = none) are reached.
export function warnings(s, ui, now) {
    const five = used(s?.usage?.five_hour, now), seven = used(s?.usage?.seven_day, now);
    return {
        daily: !!ui?.cost_daily_limit && (s?.today ?? 0) >= ui.cost_daily_limit,
        five: !!ui?.usage_5h_limit && five !== null && five >= ui.usage_5h_limit,
        seven: !!ui?.usage_7d_limit && seven !== null && seven >= ui.usage_7d_limit,
    };
}

// costParts is the top bar's figure as parts (warn: a limit is reached)
// and its tooltip; models without a price make an amount a lower bound
// (≥). The usage follows with its age when it is older than 2 minutes.
export function costParts(s, ui, now) {
    const w = warnings(s, ui, now);
    const ge = partial => partial ? '≥ ' : '';
    const parts = [
        {text: `today ${ge(s?.today_partial)}${dollars(s?.today)}`, warn: w.daily},
        {text: `total ${ge(s?.total_partial)}${dollars(s?.total)}`, warn: false},
    ];
    const u = s?.usage;
    const five = used(u?.five_hour, now), seven = used(u?.seven_day, now);
    if (five !== null) parts.push({text: `5h ${Math.floor(five)}%`, warn: w.five});
    if (seven !== null) parts.push({text: `7d ${Math.floor(seven)}%`, warn: w.seven});
    const old = u && now - u.at > 120 ? ` (${age(now - u.at)} ago)` : '';
    if (old && parts.length > 2) parts[parts.length - 1].text += old;
    const unpriced = s?.unpriced ?? [];
    const title = [
        'What claude cost at API prices (with a subscription: what it would have cost).',
        s?.since ? `The total counts from ${s.since} (reset).` : 'The total counts everything in the sessions\' transcripts.',
        unpriced.length ? `No price for ${unpriced.join(', ')}: add it under [prices] in the config file.` : '',
        five !== null || seven !== null ? usageText(u, now) : '',
        w.daily ? `Today is above your limit of ${dollars(ui.cost_daily_limit)}.` : '',
        w.five || w.seven ? 'The subscription usage is at or above your limit.' : '',
        'Click for the cost per session (Settings → Cost).',
    ].filter(Boolean).join('\n');
    return {parts, text: parts.map(p => p.text).join(' · '), title};
}

// costText is the figure as one string and its tooltip.
export function costText(s, ui, now = Date.now() / 1000) {
    const {text, title} = costParts(s, ui, now);
    return {text, title};
}

// usageText describes the subscription usage in a sentence.
export function usageText(u, now) {
    const five = used(u?.five_hour, now), seven = used(u?.seven_day, now);
    if (five === null && seven === null) {
        return 'Subscription usage: none reported yet (Pro, Max or Ultra; it comes with the first reply of a claude the app started).';
    }
    const win = (name, w, p) => p === null ? '' : `${name} ${Math.floor(p)}%` +
        (w.resets_at > now ? ` (resets ${resetText(w.resets_at, now)})` : ' (reset since)');
    return `Subscription usage: ${[win('5-hour', u.five_hour, five), win('7-day', u.seven_day, seven)].filter(Boolean).join(', ')}` +
        ` — reported ${age(now - u.at)}${now - u.at >= 60 ? ' ago' : ''} on ${u.host}.`;
}

// sessionCost is a session's balloon line ("" when nothing was counted).
export function sessionCost(a) {
    if (!a || (!a.total && !a.today)) return '';
    return `Cost: today ${dollars(a.today)} · total ${dollars(a.total)}`;
}

// costRows are the Cost page's rows: each session (in list order) with its
// today and total, then their sum.
export function costRows(sessions, s) {
    const rows = sessions.map(d => ({name: d.name, host: d.host, ...(s?.sessions?.[d.id] ?? {today: 0, total: 0})}));
    const sum = rows.reduce((a, r) => ({today: a.today + r.today, total: a.total + r.total}), {today: 0, total: 0});
    return {rows, sum};
}
