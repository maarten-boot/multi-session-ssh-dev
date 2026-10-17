// Terminal themes and color modes (SPEC T3). No imports, so it can be unit
// tested with plain `node --test`.

// Palettes. The light one is tuned for a white background: no yellow or
// pale green that disappears on white.
export const themes = {
    light: {
        background: '#ffffff', foreground: '#000000',
        cursor: '#000000', cursorAccent: '#ffffff',
        selectionBackground: '#b6d7ff', selectionInactiveBackground: '#dde6f0',
        black: '#24292f', red: '#cf222e', green: '#116329', yellow: '#7a4b00',
        blue: '#0550ae', magenta: '#8250df', cyan: '#1b7c83', white: '#6e7781',
        brightBlack: '#57606a', brightRed: '#a40e26', brightGreen: '#1a7f37', brightYellow: '#633c01',
        brightBlue: '#0969da', brightMagenta: '#a475f9', brightCyan: '#3192aa', brightWhite: '#8c959f',
    },
    dark: {
        background: '#1e1e1e', foreground: '#d4d4d4',
        cursor: '#d4d4d4', cursorAccent: '#1e1e1e',
        selectionBackground: '#264f78', selectionInactiveBackground: '#3a3d41',
        black: '#000000', red: '#cd3131', green: '#0dbc79', yellow: '#e5e510',
        blue: '#2472c8', magenta: '#bc3fbc', cyan: '#11a8cd', white: '#e5e5e5',
        brightBlack: '#666666', brightRed: '#f14c4c', brightGreen: '#23d18b', brightYellow: '#f5f543',
        brightBlue: '#3b8eea', brightMagenta: '#d670d6', brightCyan: '#29b8db', brightWhite: '#ffffff',
    },
};

// effectiveColorMode resolves a session override against the global setting
// (an unknown override, hand-edited, is ignored).
export function effectiveColorMode(ui, session) {
    const m = session?.color_mode;
    return m === 'full' || m === 'mono' ? m : ui.color_mode || 'full';
}

// termOptions returns the xterm.js options for a theme and color mode.
// mono also gets the maximum contrast ratio, which forces whatever colored
// text is already on screen to the theme's text color.
export function termOptions(ui, colorMode) {
    return {
        theme: themes[ui.theme] ?? themes.light,
        minimumContrastRatio: colorMode === 'mono' ? 21 : (ui.min_contrast || 4.5),
        drawBoldTextInBrightColors: false, // bright black is grey: bold must stay black
        fontFamily: ui.font_family || '"DejaVu Sans Mono", "Cascadia Mono", Menlo, monospace',
        fontSize: ui.font_size || 13,
        scrollback: ui.scrollback ?? 10000,
    };
}

const ESC = 0x1b;

// SGR parameters that set colors: foreground, background, underline color.
function isColorParam(n) {
    return (n >= 30 && n <= 39) || (n >= 40 && n <= 49) || (n >= 90 && n <= 97) || (n >= 100 && n <= 107) || n === 59;
}

// stripColors rewrites the parameters of one SGR sequence, keeping
// everything except colors. Returns null to drop the sequence entirely.
export function stripColors(params) {
    if (params === '') return params; // ESC[m: reset
    const toks = params.split(';');
    const out = [];
    for (let i = 0; i < toks.length; i++) {
        const t = toks[i];
        if (t.includes(':')) { // colon form: 38:2::r:g:b, 4:3 (curly underline)
            const main = Number(t.split(':')[0]);
            if (main !== 38 && main !== 48 && main !== 58) out.push(t);
            continue;
        }
        const n = t === '' ? 0 : Number(t);
        if (n === 38 || n === 48 || n === 58) { // 38;5;n or 38;2;r;g;b
            const kind = toks[i + 1];
            i += kind === '5' ? 2 : kind === '2' ? 4 : 1;
            continue;
        }
        if (!isColorParam(n)) out.push(t);
    }
    return out.length ? out.join(';') : null;
}

// SgrFilter removes color from a byte stream of terminal output, leaving
// every other escape sequence alone. Sequences split across chunks are
// held back until complete.
export class SgrFilter {
    constructor() {
        this.pending = new Uint8Array(0);
    }

    filter(chunk) {
        let buf = chunk;
        if (this.pending.length) {
            buf = new Uint8Array(this.pending.length + chunk.length);
            buf.set(this.pending);
            buf.set(chunk, this.pending.length);
            this.pending = new Uint8Array(0);
        }
        const out = new Uint8Array(buf.length);
        let o = 0;
        let i = 0;
        while (i < buf.length) {
            const b = buf[i];
            if (b !== ESC) {
                out[o++] = b;
                i++;
                continue;
            }
            if (i + 1 >= buf.length) { // lone ESC at the end: wait for more
                this.pending = buf.slice(i);
                break;
            }
            if (buf[i + 1] !== 0x5b) { // not CSI
                out[o++] = b;
                i++;
                continue;
            }
            // CSI: parameter bytes 0x30-0x3f, intermediates 0x20-0x2f, final 0x40-0x7e.
            let j = i + 2;
            while (j < buf.length && buf[j] >= 0x30 && buf[j] <= 0x3f) j++;
            const paramEnd = j;
            while (j < buf.length && buf[j] >= 0x20 && buf[j] <= 0x2f) j++;
            if (j >= buf.length) {
                if (buf.length - i > 256) { // not a real sequence; pass it through
                    out[o++] = b;
                    i++;
                    continue;
                }
                this.pending = buf.slice(i);
                break;
            }
            const fin = buf[j];
            const params = String.fromCharCode(...buf.subarray(i + 2, paramEnd));
            const isSGR = fin === 0x6d && paramEnd === j && !/^[<=>?]/.test(params);
            if (!isSGR) {
                out.set(buf.subarray(i, j + 1), o);
                o += j + 1 - i;
            } else {
                const kept = stripColors(params);
                if (kept !== null) {
                    const seq = '\x1b[' + kept + 'm';
                    for (let k = 0; k < seq.length; k++) out[o++] = seq.charCodeAt(k);
                }
            }
            i = j + 1;
        }
        return out.subarray(0, o);
    }
}
