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

// toast shows a short message at the bottom right.
export function toast(text, bad = false) {
    const t = el('div', {className: 'toast' + (bad ? ' bad' : ''), textContent: text});
    document.body.append(t);
    setTimeout(() => t.remove(), bad ? 8000 : 4000);
}

// showMenu opens the context menu at (x, y). items: {label, action, danger, disabled} or '-'.
export function showMenu(x, y, items) {
    const m = $('#menu');
    const prevFocus = document.activeElement;
    m.replaceChildren(...items.map(it => {
        if (it === '-') return el('hr');
        const b = el('button', {textContent: it.label, className: it.danger ? 'danger' : '', disabled: !!it.disabled});
        b.onclick = () => {
            hideMenu(); // focus back first; dialogs opened by the action take it from there
            it.action();
        };
        return b;
    }));
    m.hidden = false;
    m.restoreFocus = prevFocus;
    const r = m.getBoundingClientRect();
    m.style.left = Math.min(x, innerWidth - r.width - 4) + 'px';
    m.style.top = Math.min(y, innerHeight - r.height - 4) + 'px';
    m.onkeydown = menuKey;
    enabledItems()[0]?.focus();
}

const enabledItems = () => [...$('#menu').querySelectorAll('button:not(:disabled)')];

// Menus work from the keyboard: arrows, Home/End, Enter, Esc, and typing
// a letter jumps to the next item starting with it.
function menuKey(e) {
    const items = enabledItems();
    const i = items.indexOf(document.activeElement);
    const go = j => items[(j + items.length) % items.length]?.focus();
    if (e.key === 'ArrowDown') go(i + 1);
    else if (e.key === 'ArrowUp') go(i - 1);
    else if (e.key === 'Home') go(0);
    else if (e.key === 'End') go(items.length - 1);
    else if (e.key === 'Escape') hideMenu();
    else if (e.key === 'Enter' || e.key === ' ') document.activeElement?.click();
    else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
        const ch = e.key.toLowerCase();
        const order = [...items.slice(i + 1), ...items.slice(0, i + 1)];
        order.find(b => b.textContent.trim().toLowerCase().startsWith(ch))?.focus();
    } else return;
    e.preventDefault();
    e.stopPropagation();
}

// hideMenu closes the menu and returns focus to where it was.
export function hideMenu() {
    const m = $('#menu');
    if (m.hidden) return;
    m.hidden = true;
    m.restoreFocus?.focus?.();
}
