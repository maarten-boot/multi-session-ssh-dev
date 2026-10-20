// The command menu's entries (Local and Remote tabs). No imports, so
// `node --test` can load it.

// usable: a command the menu may type: a name, a one-line command without
// control characters (a hand-edited config can have others; they are
// listed as problems and left out here).
const usable = c => !!c?.name?.trim() && !!c.command?.trim() && !/[\u0000-\u001f\u007f-\u009f]/.test(c.command);

// commandItems returns the menu entries for a session's commands (its own
// first, then the global ones); send(text) types into the terminal. Each
// entry types the command, then Enter (\r) unless its run is false;
// disabled (a reason) greys out all of them.
export function commandItems(session, global, send, disabled = null) {
    const all = [...(session?.data?.commands ?? []), ...(global ?? [])].filter(usable);
    return all.map(c => {
        const run = c.run !== false;
        return {
            label: `${c.name}${run ? '  ⏎' : '  …'}`, disabled: !!disabled,
            action: () => send(c.command + (run ? '\r' : '')),
        };
    });
}
