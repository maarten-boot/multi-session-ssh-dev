// Idle times for stopping a claude that waits for input (SPEC F17).

// IDLE_CHOICES are the idle times offered.
export const IDLE_CHOICES = ['5m', '10m', '15m', '30m', '1h', '2h'];

// idleLabel says an idle time (a Go duration) in words; "" and "off" are
// "never".
export function idleLabel(v) {
    if (!v || v === 'off') return 'never';
    const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:0s)?$/.exec(v);
    if (!m || (!m[1] && !m[2])) return v;
    const h = Number(m[1] ?? 0), min = Number(m[2] ?? 0);
    return [h ? `${h} h` : '', min ? `${min} min` : ''].filter(Boolean).join(' ') || v;
}

// idleOptions are the options of an idle time select: "" (named first),
// for a session also "off" (never), the choices, and value when it is
// another one (set in config.toml).
export function idleOptions(value, first, session) {
    const opts = ['', ...(session ? ['off'] : []), ...IDLE_CHOICES];
    if (!opts.includes(value)) opts.push(value);
    return opts.map(v => ({value: v, text: v === '' ? first : v === 'off' ? 'never' : `after ${idleLabel(v)}`}));
}
