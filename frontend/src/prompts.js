// Prompts in the app-wide notes: every "## name" heading starts one, and
// its text runs to the next heading ("#" or "##"). No imports, so
// `node --test` can load it.

// parsePrompts returns [{name, text}] in the order they appear; prompts
// without text are left out.
export function parsePrompts(notes) {
    const out = [];
    let cur = null;
    const flush = () => {
        if (cur) {
            const text = cur.lines.join('\n').replace(/^\n+|\s+$/g, '');
            if (text) out.push({name: cur.name, text});
        }
        cur = null;
    };
    for (const line of String(notes).split(/\r?\n/)) {
        const h = /^(#{1,2})\s+(.*\S)\s*$/.exec(line);
        if (h) {
            flush();
            if (h[1] === '##') cur = {name: h[2], lines: []};
        } else if (cur) {
            cur.lines.push(line);
        }
    }
    flush();
    return out;
}
