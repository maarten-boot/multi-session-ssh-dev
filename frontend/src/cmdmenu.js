// The command menu's entries (Local and Remote tabs). No imports, so
// `node --test` can load it.

// usable: a command the menu may type: a name, a one-line command without
// control characters (a hand-edited config can have others; they are
// listed as problems and left out here).
const usable = c => !!c?.name?.trim() && !!c.command?.trim() && !/[\u0000-\u001f\u007f-\u009f]/.test(c.command);

// cdCommand is the command that goes back to dir: POSIX shells get it in
// single quotes; on Windows (PowerShell, else cmd.exe) in double quotes,
// which both take (a Windows path cannot contain ").
export function cdCommand(dir, windows = false) {
    if (windows) return `cd "${dir}"`;
    return `cd '${dir.replaceAll("'", `'\\''`)}'`;
}

// homeItem is the Local tab's first command-menu entry: back to the
// session's directory, the one the tab starts in. null without a usable
// directory (no path, or control characters a terminal must not get).
export function homeItem(dir, send, disabled = null, windows = false) {
    if (!dir || /[\u0000-\u001f\u007f-\u009f]/.test(dir)) return null;
    return {
        label: 'Back to the session directory  ⏎', disabled: !!disabled, title: dir,
        action: () => send(cdCommand(dir, windows) + '\r'),
    };
}

// commandItems returns the menu entries for a session's commands (its own
// first, then the global ones); send(text) types into the terminal. Each
// entry types the command, then Enter (\r) unless its run is false;
// disabled (a reason) greys out all of them. Commands without a group come
// first; each group is a submenu (in the order the groups first appear, the
// session's and the global commands of one group together).
export function commandItems(session, global, send, disabled = null) {
    const all = [...(session?.data?.commands ?? []), ...(global ?? [])].filter(usable);
    const entry = c => {
        const run = c.run !== false;
        return {
            label: `${c.name}${run ? '  ⏎' : '  …'}`, disabled: !!disabled,
            action: () => send(c.command + (run ? '\r' : '')),
        };
    };
    const top = [], groups = new Map();
    for (const c of all) {
        const g = c.group?.trim();
        if (!g) top.push(entry(c));
        else groups.set(g, [...(groups.get(g) ?? []), entry(c)]);
    }
    const subs = [...groups].map(([label, submenu]) => ({label, submenu}));
    return [...top, ...(top.length && subs.length ? ['-'] : []), ...subs];
}
