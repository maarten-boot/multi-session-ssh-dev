// Keyboard shortcuts: defaults per OS (SPEC 4.6), overridable in the
// config's [keys] table. No imports, so `node --test` can load it.
//
// A key is written like "Ctrl+Shift+PageDown": modifiers Ctrl, Shift, Alt
// (Option), Cmd (Meta, Super) and Mod (Cmd on macOS, Ctrl elsewhere),
// then one key. Keys match physical positions (KeyboardEvent.code), so
// "Ctrl+Alt+1" is the 1 key whatever the layout. "none" disables an action.

export const ACTIONS = {
    next_session: ['Ctrl+Shift+PageDown', 'Cmd+Shift+]'],
    prev_session: ['Ctrl+Shift+PageUp', 'Cmd+Shift+['],
    tab_remote: ['Ctrl+Shift+1', 'Cmd+Shift+1'],
    tab_local: ['Ctrl+Shift+2', 'Cmd+Shift+2'],
    tab_files: ['Ctrl+Shift+3', 'Cmd+Shift+3'],
    new_session: ['Ctrl+Shift+N', 'Cmd+Shift+N'],
    quick_switcher: ['Ctrl+Shift+P', 'Cmd+Shift+P'],
    settings: ['Ctrl+Shift+,', 'Cmd+,'],
};
for (let n = 1; n <= 9; n++) ACTIONS[`session_${n}`] = [`Ctrl+Alt+${n}`, `Cmd+${n}`];

const NAMED = {
    pageup: 'PageUp', pagedown: 'PageDown', home: 'Home', end: 'End', insert: 'Insert', delete: 'Delete',
    tab: 'Tab', enter: 'Enter', return: 'Enter', escape: 'Escape', esc: 'Escape', space: 'Space', backspace: 'Backspace',
    up: 'ArrowUp', down: 'ArrowDown', left: 'ArrowLeft', right: 'ArrowRight',
    '[': 'BracketLeft', ']': 'BracketRight', ',': 'Comma', '.': 'Period', '/': 'Slash', ';': 'Semicolon',
    "'": 'Quote', '`': 'Backquote', '-': 'Minus', '=': 'Equal', '\\': 'Backslash',
    comma: 'Comma', period: 'Period', slash: 'Slash', minus: 'Minus', equal: 'Equal',
    bracketleft: 'BracketLeft', bracketright: 'BracketRight', semicolon: 'Semicolon', quote: 'Quote',
    backquote: 'Backquote', backslash: 'Backslash',
};

// parse turns "Ctrl+Shift+N" into {ctrl, shift, alt, meta, code}; null for
// "none"/""; throws on anything it does not understand.
export function parse(text, isMac) {
    const s = String(text).trim();
    if (s === '' || s.toLowerCase() === 'none') return null;
    // Split on "+" but allow "+" itself as the final key ("Ctrl++").
    const parts = s.endsWith('++') ? [...s.slice(0, -2).split('+'), '+'] : s.split('+');
    const b = {ctrl: false, shift: false, alt: false, meta: false, code: null};
    for (const [i, raw] of parts.entries()) {
        const p = raw.trim();
        const l = p.toLowerCase();
        const last = i === parts.length - 1;
        if (!last) {
            if (l === 'ctrl' || l === 'control') b.ctrl = true;
            else if (l === 'shift') b.shift = true;
            else if (l === 'alt' || l === 'option' || l === 'opt') b.alt = true;
            else if (l === 'cmd' || l === 'command' || l === 'meta' || l === 'super' || l === 'win') b.meta = true;
            else if (l === 'mod') isMac ? (b.meta = true) : (b.ctrl = true);
            else throw new Error(`unknown modifier "${p}" in "${s}"`);
            continue;
        }
        if (/^[a-z]$/i.test(p)) b.code = 'Key' + p.toUpperCase();
        else if (/^[0-9]$/.test(p)) b.code = 'Digit' + p;
        else if (/^f([1-9]|1[0-9]|2[0-4])$/i.test(p)) b.code = p.toUpperCase();
        else if (NAMED[l]) b.code = NAMED[l];
        else if (p === '+') b.code = 'Equal'; // shifted "=" on US layouts
        else throw new Error(`unknown key "${p}" in "${s}"`);
    }
    if (!b.code) throw new Error(`no key in "${s}"`);
    if (!b.ctrl && !b.alt && !b.meta && !/^F\d+$/.test(b.code)) {
        throw new Error(`"${s}" needs Ctrl, Alt or Cmd: the terminal needs plain keys`);
    }
    return b;
}

export function matches(e, b) {
    return !!b && e.code === b.code && e.ctrlKey === b.ctrl && e.shiftKey === b.shift &&
        e.altKey === b.alt && e.metaKey === b.meta;
}

const id = b => `${b.ctrl}${b.shift}${b.alt}${b.meta}${b.code}`;

// buildKeymap merges overrides into the defaults. Unknown actions, bad keys
// and two actions on the same key are reported in errors; the default is
// kept for a bad override.
export function buildKeymap(overrides, isMac) {
    const map = {};
    const errors = [];
    for (const [action, defs] of Object.entries(ACTIONS)) map[action] = parse(defs[isMac ? 1 : 0], isMac);
    for (const [action, text] of Object.entries(overrides || {})) {
        if (!(action in ACTIONS)) {
            errors.push(`[keys] unknown action "${action}"`);
            continue;
        }
        try {
            map[action] = parse(text, isMac);
        } catch (err) {
            errors.push(`[keys] ${action}: ${err.message}`);
        }
    }
    const seen = new Map();
    for (const [action, b] of Object.entries(map)) {
        if (!b) continue;
        const k = id(b);
        if (seen.has(k)) {
            errors.push(`[keys] ${action} and ${seen.get(k)} both use ${format(b, isMac)}; ${action} disabled`);
            map[action] = null;
        } else {
            seen.set(k, action);
        }
    }
    return {map, errors};
}

// actionFor returns the action bound to a keydown event, or null.
export function actionFor(e, map) {
    if (e.type !== 'keydown') return null;
    for (const [action, b] of Object.entries(map)) {
        if (matches(e, b)) return action;
    }
    return null;
}

const SYMBOL = {BracketLeft: '[', BracketRight: ']', Comma: ',', Period: '.', Slash: '/', Semicolon: ';',
    Quote: "'", Backquote: '`', Minus: '-', Equal: '=', Backslash: '\\',
    ArrowUp: '↑', ArrowDown: '↓', ArrowLeft: '←', ArrowRight: '→', PageUp: 'PgUp', PageDown: 'PgDn'};

// format renders a binding for hints: "Ctrl+Shift+1", or "⇧⌘1" on macOS.
export function format(b, isMac) {
    if (!b) return '';
    const key = b.code.replace(/^Key|^Digit/, '');
    const k = SYMBOL[b.code] ?? key;
    if (isMac) return (b.ctrl ? '⌃' : '') + (b.alt ? '⌥' : '') + (b.shift ? '⇧' : '') + (b.meta ? '⌘' : '') + k;
    return [b.ctrl && 'Ctrl', b.alt && 'Alt', b.shift && 'Shift', b.meta && 'Super', k].filter(Boolean).join('+');
}
