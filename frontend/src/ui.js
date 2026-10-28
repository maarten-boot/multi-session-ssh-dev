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

// balloon shows a balloon next to target once the mouse has rested on it
// for delay() milliseconds: fill() returns its contents. It goes when the
// mouse leaves, a button is pressed, the list scrolls or a menu opens.
let balloonTimer = 0;

export function balloon(target, fill, delay) {
    target.addEventListener('mouseenter', () => {
        hideBalloon();
        balloonTimer = setTimeout(() => showBalloon(target, fill()), delay());
    });
    for (const ev of ['mouseleave', 'mousedown', 'contextmenu', 'dragstart', 'wheel']) target.addEventListener(ev, hideBalloon);
}

function showBalloon(target, children) {
    if (!target.isConnected) return;
    let b = $('#balloon');
    if (!b) {
        b = el('div', {id: 'balloon', role: 'tooltip'});
        document.body.append(b);
    }
    b.replaceChildren(...children);
    b.hidden = false;
    const r = target.getBoundingClientRect(), w = b.offsetWidth, h = b.offsetHeight;
    b.style.left = Math.max(4, Math.min(r.right + 8, innerWidth - w - 4)) + 'px';
    b.style.top = Math.max(4, Math.min(r.top, innerHeight - h - 4)) + 'px';
}

export function hideBalloon() {
    clearTimeout(balloonTimer);
    const b = $('#balloon');
    if (b) b.hidden = true;
}

addEventListener('blur', hideBalloon); // the window loses focus

// showMenu opens the context menu at (x, y). items: {label, action, danger, disabled},
// {label, submenu: items} (opens next to it on hover, → or Enter),
// {info} (a line of text, not clickable) or '-'.
export function showMenu(x, y, items) {
    hideBalloon();
    const m = $('#menu');
    const prevFocus = document.activeElement;
    fillMenu(m, items);
    m.hidden = false;
    m.restoreFocus = prevFocus;
    const r = m.getBoundingClientRect();
    m.style.left = Math.min(x, innerWidth - r.width - 4) + 'px';
    m.style.top = Math.min(y, innerHeight - r.height - 4) + 'px';
    m.onkeydown = menuKey;
    // Keys work in a menu of info lines only, too (Esc closes it).
    m.tabIndex = -1;
    (enabledItems(m)[0] ?? m).focus();
}

// fillMenu puts the items into box: the menu, or a submenu inside it.
function fillMenu(box, items) {
    box.replaceChildren(...items.map(it => {
        if (it === '-') return el('hr');
        if (it.info) return el('div', {className: 'info', textContent: it.info, title: it.title ?? ''});
        const b = el('button', {textContent: it.label, className: (it.danger ? 'danger' : '') + (it.submenu ? ' has-sub' : ''),
            disabled: !!it.disabled || (it.submenu && !it.submenu.length), title: it.title ?? ''});
        if (it.submenu) {
            b.append(el('span', {className: 'sub-arrow', textContent: '▸'}));
            b.onclick = () => openSubmenu(b, it.submenu, true);
            b.onmouseenter = () => openSubmenu(b, it.submenu, false);
        } else {
            b.onclick = () => {
                hideMenu(); // focus back first; dialogs opened by the action take it from there
                it.action();
            };
            if (box.id === 'menu') b.onmouseenter = closeSubmenu;
        }
        return b;
    }));
}

// openSubmenu shows a submenu next to its item b (inside #menu, so a click
// in it is a click in the menu); focus: on its first item (keys).
function openSubmenu(b, items, focus) {
    let sub = $('#menu > .submenu');
    if (sub?.owner !== b) {
        closeSubmenu();
        sub = el('div', {className: 'menu submenu'});
        sub.owner = b;
        fillMenu(sub, items);
        $('#menu').append(sub);
        b.classList.add('open');
        const r = b.getBoundingClientRect(), w = sub.offsetWidth, h = sub.offsetHeight;
        sub.style.left = (r.right - 2 + w > innerWidth - 4 ? Math.max(4, r.left - w + 2) : r.right - 2) + 'px';
        sub.style.top = Math.max(4, Math.min(r.top - 5, innerHeight - h - 4)) + 'px';
    }
    if (focus) (enabledItems(sub)[0] ?? b).focus();
}

function closeSubmenu() {
    const sub = $('#menu > .submenu');
    if (!sub) return;
    sub.owner.classList.remove('open');
    sub.remove();
}

const enabledItems = box => [...box.children].filter(c => c.tagName === 'BUTTON' && !c.disabled);

// Menus work from the keyboard: arrows, Home/End, Enter, Esc, and typing
// a letter jumps to the next item starting with it. → opens a submenu,
// ← and Esc close it.
function menuKey(e) {
    const cur = document.activeElement;
    const box = cur?.closest('.menu') ?? $('#menu');
    const inSub = box.classList.contains('submenu');
    const items = enabledItems(box);
    const i = items.indexOf(cur);
    const go = j => {
        if (!inSub) closeSubmenu();
        items[(j + items.length) % items.length]?.focus();
    };
    if (e.key === 'ArrowDown') go(i + 1);
    else if (e.key === 'ArrowUp') go(i - 1);
    else if (e.key === 'Home') go(0);
    else if (e.key === 'End') go(items.length - 1);
    else if (e.key === 'ArrowRight' && cur?.classList.contains('has-sub')) cur.click();
    else if ((e.key === 'ArrowLeft' || e.key === 'Escape') && inSub) {
        const owner = box.owner;
        closeSubmenu();
        owner.focus();
    } else if (e.key === 'Escape') hideMenu();
    else if (e.key === 'Enter' || e.key === ' ') cur?.click();
    else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
        const ch = e.key.toLowerCase();
        const order = [...items.slice(i + 1), ...items.slice(0, i + 1)];
        const next = order.find(b => b.textContent.trim().toLowerCase().startsWith(ch));
        if (next) go(items.indexOf(next));
    } else return;
    e.preventDefault();
    e.stopPropagation();
}

// hideMenu closes the menu and returns focus to where it was.
export function hideMenu() {
    const m = $('#menu');
    if (m.hidden) return;
    closeSubmenu();
    m.hidden = true;
    m.restoreFocus?.focus?.();
}
