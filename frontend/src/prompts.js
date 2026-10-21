// Prompts in the app-wide notes: every "## name" heading starts one, and
// its text runs to the next heading ("#" or "##"). Lines inside ``` or ~~~
// fenced code blocks are text, never headings (a "# comment" in a shell
// block stays in the prompt). No imports, so
// `node --test` can load it.

// parsePrompts returns [{name, text}] in the order they appear; prompts
// without text are left out.
export function parsePrompts(notes) {
    const out = [];
    let cur = null;
    let fence = null; // the open fence ("```" or "~~~", maybe longer), or null
    const flush = () => {
        if (cur) {
            const text = cur.lines.join('\n').replace(/^\n+|\s+$/g, '');
            if (text) out.push({name: cur.name, text});
        }
        cur = null;
    };
    for (const line of String(notes).split(/\r?\n/)) {
        const f = /^ {0,3}(`{3,}|~{3,})/.exec(line)?.[1];
        if (f && (!fence || (f[0] === fence[0] && f.length >= fence.length && /^ {0,3}[`~]+\s*$/.test(line)))) {
            fence = fence ? null : f;
        }
        const h = !fence && /^(#{1,2})\s+(.*\S)\s*$/.exec(line);
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

// promptMenu returns the entries of the Prompts menu (Local and Remote
// tabs): the session's own prompts, then those for all sessions; paste(text)
// puts one into the terminal (no Enter).
export function promptMenu(own, global, paste) {
    const item = p => ({label: p.name, title: p.text, action: () => paste(p.text)});
    const items = [];
    if (own.length) items.push({info: 'This session'}, ...own.map(item));
    if (own.length && global.length) items.push('-');
    if (global.length) items.push({info: 'All sessions'}, ...global.map(item));
    if (!items.length) {
        items.push({info: 'No prompts yet: "## name" headings on a page named Prompts,',
            title: 'Prompts come from the Prompts page (and its subpages) of this session\'s notes and of the notes for all sessions'},
        {info: 'in this session\'s notes or the notes for all sessions'});
    }
    return items;
}
