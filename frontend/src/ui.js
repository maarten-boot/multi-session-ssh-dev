// Shared DOM helpers.

export const $ = sel => document.querySelector(sel);
export const el = (tag, props = {}, ...children) => {
    const e = Object.assign(document.createElement(tag), props);
    e.append(...children);
    return e;
};

// Small per-machine UI memory (last session, collapsed groups). Optional.
export const store = {
    get(k, d) {
        try {
            return JSON.parse(localStorage.getItem('sw:' + k)) ?? d;
        } catch {
            return d;
        }
    },
    set(k, v) {
        try {
            localStorage.setItem('sw:' + k, JSON.stringify(v));
        } catch {
        }
    },
};

// showMenu opens the context menu at (x, y). items: {label, action, danger, disabled} or '-'.
export function showMenu(x, y, items) {
    const m = $('#menu');
    m.replaceChildren(...items.map(it => {
        if (it === '-') return el('hr');
        const b = el('button', {textContent: it.label, className: it.danger ? 'danger' : '', disabled: !!it.disabled});
        b.onclick = () => {
            hideMenu();
            it.action();
        };
        return b;
    }));
    m.hidden = false;
    const r = m.getBoundingClientRect();
    m.style.left = Math.min(x, innerWidth - r.width - 4) + 'px';
    m.style.top = Math.min(y, innerHeight - r.height - 4) + 'px';
}

export function hideMenu() {
    $('#menu').hidden = true;
}
