// Answers in the Q&A tab: text, fenced code blocks and SVG pictures.

// parseAnswer splits an answer into parts: {type: 'text', text},
// {type: 'code', lang, text, raw} and {type: 'svg', text, raw} (raw: the
// block with its fences, as written). Only a closed
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
                fence = {marker: m[1], lang: m[2].toLowerCase(), open: line, lines: []};
            } else text.push(line);
            continue;
        }
        const close = new RegExp(`^ {0,3}${fence.marker[0] === '`' ? '`' : '~'}{${fence.marker.length},}\\s*$`);
        if (close.test(line)) {
            const body = fence.lines.join('\n');
            const raw = [fence.open, ...fence.lines, line].join('\n');
            parts.push(fence.lang === 'svg' && isSVG(body)
                ? {type: 'svg', text: body, raw}
                : {type: 'code', lang: fence.lang, text: body, raw});
            fence = null;
        } else fence.lines.push(line);
    }
    if (fence) parts.push({type: 'code', lang: fence.lang, text: fence.lines.join('\n'), raw: [fence.open, ...fence.lines].join('\n')});
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

// ------------------------------------------------------------ Markdown
// The text parts of an answer are shown with the common Markdown: headings,
// paragraphs, lists (also numbered, nested and task lists), quotes, tables,
// rules, and inline **bold**, *italic*, ~~struck~~, `code` and links. Each
// block keeps its source (md), so a copy can give the Markdown back.

const HEADING = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const RULE = /^ {0,3}([-*_])( *\1){2,} *$/;
const ITEM = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/;
const QUOTE = /^ {0,3}> ?(.*)$/;
const TABLE_SEP = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/;

// cells splits a table row at | (not \|), without the outer ones.
function cells(line) {
    let s = line.trim();
    if (s.startsWith('|')) s = s.slice(1);
    if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1);
    return s.split(/(?<!\\)\|/).map(c => c.trim().replaceAll('\\|', '|'));
}

// parseMarkdown splits text into blocks: {type: 'heading', level, inline},
// {type: 'para', inline}, {type: 'rule'}, {type: 'quote', blocks},
// {type: 'list', items: [{level, marker, task, inline, md}]},
// {type: 'table', head, align, rows}; each with md, its source.
export function parseMarkdown(text) {
    const lines = text.replaceAll('\r\n', '\n').split('\n');
    const blocks = [];
    let i = 0;
    const isTable = j => j + 1 < lines.length && lines[j].includes('|') && TABLE_SEP.test(lines[j + 1]) &&
        lines[j + 1].includes('-');
    const starts = j => !lines[j].trim() || HEADING.test(lines[j]) || RULE.test(lines[j]) ||
        ITEM.test(lines[j]) || QUOTE.test(lines[j]) || isTable(j);
    while (i < lines.length) {
        const line = lines[i];
        let m;
        if (!line.trim()) {
            i++;
        } else if ((m = HEADING.exec(line))) {
            blocks.push({type: 'heading', level: m[1].length, inline: parseInline(m[2]), md: line});
            i++;
        } else if (RULE.test(line)) {
            blocks.push({type: 'rule', md: line});
            i++;
        } else if (QUOTE.test(line)) {
            const body = [];
            const start = i;
            while (i < lines.length && (m = QUOTE.exec(lines[i]))) {
                body.push(m[1]);
                i++;
            }
            blocks.push({type: 'quote', blocks: parseMarkdown(body.join('\n')), md: lines.slice(start, i).join('\n')});
        } else if (ITEM.test(line)) {
            const items = [];
            const start = i;
            const base = ITEM.exec(line)[1].length;
            while (i < lines.length && (m = ITEM.exec(lines[i]))) {
                let body = m[3];
                const itemStart = i++;
                // Continuation lines: indented, not a new item or block.
                while (i < lines.length && lines[i].trim() && /^\s+\S/.test(lines[i]) && !ITEM.test(lines[i])) {
                    body += '\n' + lines[i].trim();
                    i++;
                }
                const task = /^\[([ xX])\]\s+/.exec(body);
                if (task) body = body.slice(task[0].length);
                items.push({level: Math.min(Math.floor(Math.max(0, m[1].length - base) / 2), 5), marker: m[2],
                    task: task ? task[1] !== ' ' : null, inline: parseInline(body),
                    md: lines.slice(itemStart, i).join('\n')});
                if (i < lines.length && !lines[i].trim() && i + 1 < lines.length && ITEM.test(lines[i + 1])) i++;
            }
            blocks.push({type: 'list', items, md: lines.slice(start, i).join('\n').trimEnd()});
        } else if (isTable(i)) {
            const start = i;
            const head = cells(lines[i]).map(parseInline);
            const align = cells(lines[i + 1]).map(c => c.startsWith(':') && c.endsWith(':') ? 'center' :
                c.endsWith(':') ? 'right' : c.startsWith(':') ? 'left' : '');
            i += 2;
            const rows = [];
            while (i < lines.length && lines[i].trim() && lines[i].includes('|')) rows.push(cells(lines[i++]).map(parseInline));
            blocks.push({type: 'table', head, align, rows, md: lines.slice(start, i).join('\n')});
        } else {
            const start = i;
            i++;
            while (i < lines.length && !starts(i)) i++;
            const md = lines.slice(start, i).join('\n');
            blocks.push({type: 'para', inline: parseInline(md), md});
        }
    }
    return blocks;
}

// The inline marks: [type, open, close] in the order they are tried.
const MARKS = [['strong', '**', '**'], ['strong', '__', '__'], ['del', '~~', '~~'], ['em', '*', '*'], ['em', '_', '_']];

// parseInline splits text into {type: 'text', text}, {type: 'code', text},
// {type: 'strong' | 'em' | 'del', open, close, children} and
// {type: 'link', url, children} (only http, https and mailto addresses).
export function parseInline(text) {
    const out = [];
    let plain = '';
    const flush = () => {
        if (plain) out.push({type: 'text', text: plain});
        plain = '';
    };
    let i = 0;
    while (i < text.length) {
        const c = text[i];
        const rest = text.slice(i);
        let m;
        if (c === '\\' && /[\\`*_~[\]()#+\-.!|>]/.test(text[i + 1] ?? '')) {
            plain += text[i + 1];
            i += 2;
            continue;
        }
        if (c === '`' && (m = /^(`+)([^`]|[^`][\s\S]*?[^`])\1(?!`)/.exec(rest))) {
            flush();
            const t = m[2];
            out.push({type: 'code', open: m[1], text: /^ .* $/.test(t) && t.trim() ? t.slice(1, -1) : t});
            i += m[0].length;
            continue;
        }
        if (c === '[' && (m = /^\[((?:\\.|[^\]\\])+)\]\(\s*<?((?:https?:\/\/|mailto:)[^\s)>]+)>?(?:\s+"[^"]*")?\s*\)/.exec(rest))) {
            flush();
            out.push({type: 'link', url: m[2], open: '[', close: m[0].slice(1 + m[1].length), children: parseInline(m[1])});
            i += m[0].length;
            continue;
        }
        if (c === '<' && (m = /^<((?:https?:\/\/|mailto:)[^\s>]+)>/.exec(rest))) {
            flush();
            out.push({type: 'link', url: m[1], open: '<', close: '>', children: [{type: 'text', text: m[1]}]});
            i += m[0].length;
            continue;
        }
        if ((c === 'h') && !/\w/.test(text[i - 1] ?? '') && (m = /^https?:\/\/[^\s<>()]*[^\s<>().,;:!?'"*_~]/.exec(rest))) {
            flush();
            out.push({type: 'link', url: m[0], open: '', close: '', children: [{type: 'text', text: m[0]}]});
            i += m[0].length;
            continue;
        }
        const mark = MARKS.find(([, open]) => rest.startsWith(open));
        if (mark) {
            const [type, open, close] = mark;
            // An opening mark is followed by a non-space; _ only at a word start.
            const after = text[i + open.length];
            const okOpen = after && !/\s/.test(after) && !(open[0] === '_' && /\w/.test(text[i - 1] ?? ''));
            const end = okOpen ? findClose(text, i + open.length, close) : -1;
            if (end > 0) {
                flush();
                out.push({type, open, close, children: parseInline(text.slice(i + open.length, end))});
                i = end + close.length;
                continue;
            }
            plain += open;
            i += open.length;
            continue;
        }
        plain += c;
        i++;
    }
    flush();
    return out;
}

// findClose finds the closing mark from position from: after a non-space,
// not followed by the same character (so ** does not close *), _ only at a
// word end; code spans in between are skipped.
function findClose(text, from, close) {
    for (let j = from + 1; j <= text.length - close.length; j++) {
        if (text[j] === '`') {
            const m = /^(`+)[\s\S]*?\1/.exec(text.slice(j));
            if (m) {
                j += m[0].length - 1;
                continue;
            }
        }
        if (text.startsWith(close, j) && !/\s/.test(text[j - 1]) && text[j + close.length] !== close[0] &&
            !(close.length === 1 && text[j - 1] === close[0]) &&
            !(close[0] === '_' && /\w/.test(text[j + close.length] ?? ''))) return j;
    }
    return -1;
}

// toMarkdown gives the Markdown of what is selected under node: a block
// selected whole gives its source (data-md); a partly selected one the
// selected text with the marks around it (data-open, data-close);
// data-skip leaves an element out (buttons, bullets), data-whole takes a
// touched element whole (tables, pictures), data-sep joins the children.
// sel: {touches(node), covers(node), text(textNode)} (see qa.js).
export function toMarkdown(node, sel) {
    if (node.nodeType === 3) return sel.touches(node) ? sel.text(node) : '';
    if (node.nodeType !== 1 || !sel.touches(node)) return '';
    const d = node.dataset ?? {};
    if (d.skip !== undefined) return '';
    if (d.md !== undefined && (d.whole !== undefined || sel.covers(node))) return d.md;
    const parts = [...node.childNodes].map(n => toMarkdown(n, sel)).filter(s => s !== '');
    const inner = parts.join(d.sep ?? '');
    return inner ? (d.open ?? '') + inner + (d.close ?? '') : '';
}
