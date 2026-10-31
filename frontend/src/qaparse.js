// Answers in the Q&A tab: text, fenced code blocks and SVG pictures.

// parseAnswer splits an answer into parts: {type: 'text', text},
// {type: 'code', lang, text} and {type: 'svg', text}. Only a closed
// ```svg block is a picture; while an answer streams in, an open fence is
// code until it closes.
export function parseAnswer(answer) {
    const parts = [];
    const lines = answer.replaceAll('\r\n', '\n').split('\n');
    let text = [];
    let fence = null; // {marker, lang, lines}
    const flushText = () => {
        const t = text.join('\n');
        if (t.trim()) parts.push({type: 'text', text: t.replace(/^\n+|\n+$/g, '')});
        text = [];
    };
    for (const line of lines) {
        if (!fence) {
            const m = /^ {0,3}(`{3,}|~{3,})\s*([\w+#.-]*)/.exec(line);
            if (m) {
                flushText();
                fence = {marker: m[1], lang: m[2].toLowerCase(), lines: []};
            } else text.push(line);
            continue;
        }
        const close = new RegExp(`^ {0,3}${fence.marker[0] === '`' ? '`' : '~'}{${fence.marker.length},}\\s*$`);
        if (close.test(line)) {
            const body = fence.lines.join('\n');
            parts.push(fence.lang === 'svg' && isSVG(body)
                ? {type: 'svg', text: body}
                : {type: 'code', lang: fence.lang, text: body});
            fence = null;
        } else fence.lines.push(line);
    }
    if (fence) parts.push({type: 'code', lang: fence.lang, text: fence.lines.join('\n')});
    flushText();
    return parts;
}

// isSVG: the block is one <svg> element (an XML declaration or comments
// before it are fine).
export function isSVG(s) {
    return /^\s*(<\?xml[^>]*>\s*)?(<!--[\s\S]*?-->\s*)*<svg[\s>]/i.test(s) && /<\/svg>\s*$/i.test(s);
}

// svgURL is the picture as a data: URL for an <img>: an SVG loaded as an
// image runs no scripts and loads nothing from elsewhere.
export const svgURL = svg => 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);

// engineName is an engine's name (see internal/qa EngineName).
export function engineName(id) {
    if (!id || id === 'api') return 'Claude API';
    if (id === 'claude') return 'Claude Code';
    return id.replace(/^cmd:/, '');
}

// tokens describes an answer's engine, model and token count for the line
// under it (a program that reports no tokens: no count).
export function tokens(u, model, engine = '') {
    const who = [engine, model].filter(Boolean).join(' · ');
    if (!u || !(u.input || u.output || u.cache_read || u.cache_write)) return who;
    const input = u.input + u.cache_read + u.cache_write;
    const cached = u.cache_read ? ` (${u.cache_read} from cache)` : '';
    return `${who ? who + ' · ' : ''}${input} tokens in${cached}, ${u.output} out`;
}
