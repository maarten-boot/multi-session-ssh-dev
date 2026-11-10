// What claude cost at API prices (SPEC F18): the figure in the top bar
// and a session's line in the list balloon.

// dollars says an amount: $0.42, $12.30, $1,234.50.
export function dollars(v) {
    return '$' + (v ?? 0).toLocaleString('en-US', {minimumFractionDigits: 2, maximumFractionDigits: 2});
}

// costText is the top bar's figure and its tooltip for a summary; models
// without a price make the amounts a lower bound (≥).
export function costText(s) {
    const unpriced = s?.unpriced ?? [];
    const ge = partial => partial ? '≥ ' : '';
    const text = `today ${ge(s?.today_partial)}${dollars(s?.today)} · total ${ge(s?.total_partial)}${dollars(s?.total)}`;
    const title = [
        'What claude cost at API prices (with a subscription: what it would have cost).',
        s?.since ? `The total counts from ${s.since} (reset).` : 'The total counts everything in the sessions\' transcripts.',
        unpriced.length ? `No price for ${unpriced.join(', ')}: add it under [prices] in the config file.` : '',
        'Click to reset the total.',
    ].filter(Boolean).join('\n');
    return {text, title};
}

// sessionCost is a session's balloon line ("" when nothing was counted).
export function sessionCost(a) {
    if (!a || (!a.total && !a.today)) return '';
    return `Cost: today ${dollars(a.today)} · total ${dollars(a.total)}`;
}
