import {EditorSelection, EditorState, RangeSetBuilder, StateEffect} from '@codemirror/state';
import {Decoration, EditorView, MatchDecorator, ViewPlugin, WidgetType, drawSelection, keymap, placeholder} from '@codemirror/view';
import {defaultKeymap, history, historyKeymap, indentLess, indentMore, insertNewline} from '@codemirror/commands';
import {HighlightStyle, indentUnit, syntaxHighlighting, syntaxTree} from '@codemirror/language';
import {markdown, markdownLanguage} from '@codemirror/lang-markdown';
import {tags as t} from '@lezer/highlight';
import {AddSpellWord, SpellDictionary} from '../wailsjs/go/main/App';
import {ClipboardGetText, ClipboardSetText} from '../wailsjs/runtime/runtime';
import {LISTS, MARKUP, continueList, listItem, toggleList, toggleMarkup, toggleTasks} from './markup';
import {Speller, words} from './spelling';
import {showMenu} from './ui';

const isMac = navigator.platform.startsWith('Mac');

// The notes editor: Markdown source shown with its formatting (bold drawn
// bold, struck text struck through…) while the markers stay visible, in
// italics (black like all text: no grey).

const style = HighlightStyle.define([
    {tag: t.strong, fontWeight: 'bold'},
    {tag: t.emphasis, fontStyle: 'italic'},
    {tag: t.strikethrough, textDecoration: 'line-through'},
    {tag: t.heading1, fontWeight: 'bold', fontSize: '1.4em'},
    {tag: t.heading2, fontWeight: 'bold', fontSize: '1.2em'},
    {tag: [t.heading3, t.heading4, t.heading5, t.heading6], fontWeight: 'bold'},
    {tag: [t.processingInstruction, t.contentSeparator], fontStyle: 'italic'}, // the markers: ** _ ~~ # - > ```
    {tag: t.quote, fontStyle: 'italic'},
    {tag: [t.link, t.url], textDecoration: 'underline'},
    {tag: t.monospace, fontFamily: 'var(--mono-family, monospace)'},
]);

// Markdown has no underline: <u>…</u>, drawn underlined with the tags in
// italics like the other markers.
const underlineMatcher = new MatchDecorator({
    regexp: /<u>(.+?)<\/u>/g,
    decorate(add, from, to) {
        add(from, from + 3, Decoration.mark({class: 'cm-md-mark'}));
        add(from + 3, to - 4, Decoration.mark({class: 'cm-md-underline'}));
        add(to - 4, to, Decoration.mark({class: 'cm-md-mark'}));
    },
});
const underline = ViewPlugin.define(view => ({
    decorations: underlineMatcher.createDeco(view),
    update(u) {
        this.decorations = underlineMatcher.updateDeco(u, this.decorations);
    },
}), {decorations: v => v.decorations});

// Spelling: one checker for every notes editor, loaded when the first
// editor is made (see configureSpelling).
let spellUI = null; // the UI settings: spell_check, spell_language, spell_words
let speller = null; // the checker of the last language loaded
let spellSeq = 0;
const editors = new Set();
const refresh = StateEffect.define();
const spellOn = () => spellUI && spellUI.spell_check !== false && speller?.lang === (spellUI.spell_language || 'en_US') ? speller : null;

// configureSpelling takes the UI settings (on startup and after Settings);
// "Add to dictionary" adds to its spell_words too.
export function configureSpelling(ui) {
    spellUI = ui;
    if (editors.size) loadSpelling();
}

async function loadSpelling() {
    const seq = ++spellSeq;
    const lang = spellUI?.spell_language || 'en_US';
    if (spellUI?.spell_check !== false && speller?.lang !== lang) {
        try {
            const d = await SpellDictionary(lang);
            if (seq !== spellSeq) return;
            speller = new Speller(d.lang, d.aff, d.dic);
        } catch (err) {
            console.warn('spelling:', err);
        }
    }
    if (seq !== spellSeq) return;
    speller?.addWords(spellUI?.spell_words ?? []);
    for (const v of editors) v.dispatch({effects: refresh.of(null)});
}

function addWord(word) {
    AddSpellWord(word).catch(err => console.warn('spelling:', err));
    if (spellUI) spellUI.spell_words = [...(spellUI.spell_words ?? []), word];
    speller?.addWords([word]);
    for (const v of editors) v.dispatch({effects: refresh.of(null)});
}

// Not checked: code, links' addresses, HTML.
const NO_SPELLING = new Set(['InlineCode', 'FencedCode', 'CodeBlock', 'CodeText', 'URL', 'Autolink', 'HTMLTag', 'HTMLBlock',
    'CommentBlock', 'Comment', 'LinkLabel']);
const misspelledMark = Decoration.mark({class: 'cm-misspelled'});

// misspelled is the misspelled word at pos (in a checked place), or null.
function misspelled(state, pos) {
    const sp = spellOn();
    if (!sp) return null;
    const line = state.doc.lineAt(pos);
    for (const w of words(line.text)) {
        const from = line.from + w.from, to = line.from + w.to;
        if (pos >= from && pos <= to) return sp.correct(w.word) || unchecked(state, from) ? null : {from, to, word: w.word};
    }
    return null;
}

function unchecked(state, pos) {
    for (let n = syntaxTree(state).resolveInner(pos, 1); n; n = n.parent) if (NO_SPELLING.has(n.name)) return true;
    return false;
}

// spelling underlines misspelled words on screen, except the one being
// typed (until the cursor leaves it).
const spelling = ViewPlugin.define(view => ({
    typing: null,
    init() {
        this.build(view);
        return this;
    },
    decorations: Decoration.none,
    build(v) {
        const sp = spellOn();
        const b = new RangeSetBuilder();
        if (sp) {
            let next = 0;
            for (const r of v.visibleRanges) {
                for (let pos = Math.max(r.from, next); pos <= r.to;) {
                    const line = v.state.doc.lineAt(pos);
                    for (const w of words(line.text)) {
                        const from = line.from + w.from, to = line.from + w.to;
                        if (to !== this.typing && !sp.correct(w.word) && !unchecked(v.state, from)) b.add(from, to, misspelledMark);
                    }
                    pos = next = line.to + 1;
                }
            }
        }
        this.decorations = b.finish();
    },
    update(u) {
        const typed = u.docChanged && u.transactions.some(tr => tr.isUserEvent('input.type'));
        const typing = typed ? u.state.selection.main.head : u.selectionSet || u.focusChanged ? null : this.typing;
        if (typing !== this.typing || u.docChanged || u.viewportChanged || syntaxTree(u.startState) !== syntaxTree(u.state) ||
            u.transactions.some(tr => tr.effects.some(e => e.is(refresh)))) {
            this.typing = typing;
            this.build(u.view);
        }
    },
}).init(), {decorations: v => v.decorations});

// Check lists: the "[ ]" / "[x]" of a task drawn as a check box; a click
// ticks it.
class TaskBox extends WidgetType {
    constructor(on) {
        super();
        this.on = on;
    }

    eq(o) {
        return o.on === this.on;
    }

    toDOM() {
        const b = document.createElement('input');
        b.type = 'checkbox';
        b.checked = this.on;
        b.className = 'cm-task-box';
        b.title = `Tick or untick (${isMac ? '⌘' : 'Ctrl+'}Enter)`;
        return b;
    }

    ignoreEvent() {
        return false;
    }
}

function taskBoxes(view) {
    const b = new RangeSetBuilder();
    for (const {from, to} of view.visibleRanges) {
        syntaxTree(view.state).iterate({from, to, enter: n => {
            if (n.name === 'TaskMarker') {
                b.add(n.from, n.to, Decoration.replace({widget: new TaskBox(/x/i.test(view.state.sliceDoc(n.from, n.to)))}));
            }
        }});
    }
    return b.finish();
}

const tasks = ViewPlugin.define(view => ({
    decorations: taskBoxes(view),
    update(u) {
        if (u.docChanged || u.viewportChanged || syntaxTree(u.startState) !== syntaxTree(u.state)) this.decorations = taskBoxes(u.view);
    },
}), {
    decorations: v => v.decorations,
    eventHandlers: {
        mousedown(e, view) {
            if (!e.target.classList?.contains('cm-task-box') || e.button !== 0) return false;
            e.preventDefault();
            const pos = view.posAtDOM(e.target);
            const r = toggleTasks(view.state.doc.toString(), pos, pos);
            if (r) view.dispatch({changes: r.changes, userEvent: 'input.tick'});
            return true;
        },
    },
});

const theme = EditorView.theme({
    '&': {height: '100%', backgroundColor: 'var(--bg)', color: 'var(--text-strong)'},
    '&.cm-focused': {outline: 'none'},
    '.cm-scroller': {
        fontFamily: 'var(--mono-family, "DejaVu Sans Mono", "Cascadia Mono", Menlo, monospace)',
        fontSize: 'var(--mono-size, 13px)', lineHeight: '1.5',
    },
    '.cm-content': {padding: '12px 16px', caretColor: 'var(--text-strong)'},
    '.cm-cursor': {borderLeftColor: 'var(--text-strong)'},
    // The base theme's focused selection ("&light.cm-focused > .cm-scroller >
    // .cm-selectionLayer .cm-selectionBackground", lavender) has five
    // classes: ".cm-editor" makes ours six, so it wins in both themes.
    '&.cm-editor.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionLayer .cm-selectionBackground, ::selection':
        {background: 'var(--focus-selected)'},
    '.cm-placeholder': {color: 'var(--text-strong)', fontStyle: 'italic'},
    '.cm-md-mark': {fontStyle: 'italic'},
    '.cm-md-underline': {textDecoration: 'underline'},
    '.cm-misspelled': {textDecoration: 'underline wavy #d00000', textDecorationSkipInk: 'none', textUnderlineOffset: '3px'},
    // Check boxes drawn black (the native ones are grey).
    '.cm-task-box': {appearance: 'none', width: '0.95em', height: '0.95em', margin: '0 0.2em', verticalAlign: '-0.12em',
        border: '1.5px solid var(--text-strong)', borderRadius: '2px', cursor: 'pointer', position: 'relative'},
    '.cm-task-box:checked::after': {content: '"✓"', position: 'absolute', left: '0', right: '0', top: '-0.25em',
        textAlign: 'center', fontWeight: 'bold', color: 'var(--text-strong)', lineHeight: '1.2em'},
});

// format toggles one of MARKUP on the main selection.
export function format(view, name) {
    const {open, close} = MARKUP[name];
    const sel = view.state.selection.main;
    const r = toggleMarkup(view.state.doc.toString(), sel.from, sel.to, open, close);
    view.dispatch({changes: r.changes, selection: EditorSelection.single(r.anchor, r.head), userEvent: 'input.format', scrollIntoView: true});
    view.focus();
    return true;
}

// listFormat makes the selected lines list items of kind (see LISTS), or
// plain lines again.
export function listFormat(view, kind) {
    const sel = view.state.selection.main;
    const r = toggleList(view.state.doc.toString(), sel.from, sel.to, kind);
    view.dispatch({changes: r.changes, selection: EditorSelection.single(r.anchor, r.head), userEvent: 'input.format', scrollIntoView: true});
    view.focus();
    return true;
}

// tick ticks or unticks the check boxes of the selected lines.
export function tick(view) {
    const sel = view.state.selection.main;
    const r = toggleTasks(view.state.doc.toString(), sel.from, sel.to);
    if (!r) return false;
    view.dispatch({changes: r.changes, userEvent: 'input.tick'});
    return true;
}

// listEnter continues a list (see continueList).
function listEnter(view) {
    const sel = view.state.selection.main;
    if (view.state.selection.ranges.length > 1 || !sel.empty) return false;
    const r = continueList(view.state.doc.toString(), sel.head);
    if (!r) return false;
    view.dispatch({changes: r.changes, selection: {anchor: r.anchor}, userEvent: 'input', scrollIntoView: true});
    return true;
}

const inListItem = v => !!listItem(v.state.doc.lineAt(v.state.selection.main.head).text);

// Editor wraps a CodeMirror view. onChange(text) runs on every edit by the
// user, onBlur when the editor loses focus.
export class Editor {
    constructor(parent, {onChange, onBlur}) {
        this.onChange = onChange;
        this.placeholderText = '';
        this.view = new EditorView({parent, state: this.state('')});
        if (!editors.size) loadSpelling();
        editors.add(this.view);
        this.view.contentDOM.addEventListener('blur', () => onBlur?.());
        this.view.dom.addEventListener('contextmenu', e => {
            e.preventDefault();
            this.contextMenu(e.clientX, e.clientY);
        });
    }

    // contextMenu is the editor's right-click menu: clipboard and formatting.
    // A click outside the selection moves the cursor there first, as in
    // other editors.
    contextMenu(x, y) {
        const v = this.view;
        const at = v.posAtCoords({x, y});
        const sel = v.state.selection.main;
        if (at !== null && (at < sel.from || at > sel.to)) v.dispatch({selection: {anchor: at}});
        v.focus();
        const range = v.state.selection.main;
        const text = v.state.sliceDoc(range.from, range.to);
        const k = isMac ? '⌘' : 'Ctrl+';
        const key = m => m.key.replace('Mod-', k).replace('Shift-', 'Shift+').replace(/[a-z]$/, c => c.toUpperCase());
        // On a misspelled word: its corrections and "Add to dictionary" first.
        const bad = at !== null && range.empty ? misspelled(v.state, at) : null;
        const fixes = bad ? speller.suggest(bad.word) : [];
        const spellItems = !bad ? [] : [
            ...(fixes.length ? fixes.map(f => ({label: f, action: () => {
                v.dispatch({changes: {from: bad.from, to: bad.to, insert: f}, userEvent: 'input.spell'});
                v.focus();
            }})) : [{info: 'No suggestions'}]),
            {label: `Add “${bad.word}” to dictionary`, action: () => {
                addWord(bad.word);
                v.focus();
            }},
            '-',
        ];
        const hasBox = !!toggleTasks(v.state.doc.toString(), range.from, range.to);
        showMenu(x, y, [
            ...spellItems,
            {label: `Cut   ${k}X`, disabled: range.empty, action: () => ClipboardSetText(text).then(() => {
                v.dispatch({changes: {from: range.from, to: range.to}, userEvent: 'delete.cut'});
                v.focus();
            })},
            {label: `Copy   ${k}C`, disabled: range.empty, action: () => ClipboardSetText(text).then(() => v.focus())},
            {label: `Paste   ${k}V`, action: () => ClipboardGetText().then(t => {
                if (t) v.dispatch({...v.state.replaceSelection(t), userEvent: 'input.paste', scrollIntoView: true});
                v.focus();
            })},
            {label: 'Select all', action: () => {
                v.dispatch({selection: {anchor: 0, head: v.state.doc.length}});
                v.focus();
            }},
            '-',
            ...Object.entries(MARKUP).map(([name, m]) => ({label: `${m.title}   ${key(m)}`, action: () => format(v, name)})),
            '-',
            ...Object.entries(LISTS).map(([kind, m]) => ({label: `${m.title}   ${key(m)}`, action: () => listFormat(v, kind)})),
            {label: `Tick / untick   ${k}Enter`, disabled: !hasBox, action: () => {
                tick(v);
                v.focus();
            }},
        ]);
    }

    state(doc) {
        return EditorState.create({
            doc,
            extensions: [
                history(),
                indentUnit.of('  '),
                drawSelection(),
                EditorView.lineWrapping,
                // Spelling is checked here (spelling), not by the web view.
                EditorView.contentAttributes.of({spellcheck: 'false', 'aria-label': 'Notes'}),
                markdown({base: markdownLanguage, addKeymap: false}),
                syntaxHighlighting(style),
                underline,
                spelling,
                tasks,
                theme,
                placeholder(this.placeholderText),
                keymap.of([
                    ...Object.entries(MARKUP).map(([name, m]) => ({key: m.key, run: v => format(v, name)})),
                    ...Object.entries(LISTS).map(([kind, m]) => ({key: m.key, run: v => listFormat(v, kind)})),
                    {key: 'Mod-Enter', run: tick},
                    // Enter continues a list, else starts a plain new line (no
                    // copied indentation). Tab inserts two spaces instead of
                    // leaving the editor; with a selection, or in a list item, it
                    // indents the lines (Shift+Tab outdents a list item).
                    {key: 'Enter', run: v => listEnter(v) || insertNewline(v)},
                    {key: 'Tab', run: v => v.state.selection.ranges.some(r => !r.empty) || inListItem(v) ? indentMore(v) :
                        (v.dispatch({...v.state.replaceSelection('  '), userEvent: 'input'}), true)},
                    {key: 'Shift-Tab', run: v => (v.state.selection.ranges.some(r => !r.empty) || inListItem(v)) && indentLess(v)},
                    ...historyKeymap,
                    ...defaultKeymap,
                ]),
                // setText replaces the state without a transaction: every
                // change seen here is an edit.
                EditorView.updateListener.of(u => u.docChanged && this.onChange?.(u.state.doc.toString())),
            ],
        });
    }

    get text() {
        return this.view.state.doc.toString();
    }

    // setText shows another text (another page, or a reload) with its own
    // undo history; it is not an edit.
    setText(text, placeholderText = this.placeholderText) {
        this.placeholderText = placeholderText;
        this.view.setState(this.state(text));
    }

    focus() {
        this.view.focus();
    }
}
