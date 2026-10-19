import {EditorSelection, EditorState} from '@codemirror/state';
import {Decoration, EditorView, MatchDecorator, ViewPlugin, drawSelection, keymap, placeholder} from '@codemirror/view';
import {defaultKeymap, history, historyKeymap, indentMore, insertNewline} from '@codemirror/commands';
import {HighlightStyle, indentUnit, syntaxHighlighting} from '@codemirror/language';
import {markdown, markdownLanguage} from '@codemirror/lang-markdown';
import {tags as t} from '@lezer/highlight';
import {ClipboardGetText, ClipboardSetText} from '../wailsjs/runtime/runtime';
import {MARKUP, toggleMarkup} from './markup';
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

// Editor wraps a CodeMirror view. onChange(text) runs on every edit by the
// user, onBlur when the editor loses focus.
export class Editor {
    constructor(parent, {onChange, onBlur}) {
        this.onChange = onChange;
        this.placeholderText = '';
        this.view = new EditorView({parent, state: this.state('')});
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
        showMenu(x, y, [
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
                EditorView.contentAttributes.of({spellcheck: 'true', 'aria-label': 'Notes'}),
                markdown({base: markdownLanguage, addKeymap: false}),
                syntaxHighlighting(style),
                underline,
                theme,
                placeholder(this.placeholderText),
                keymap.of([
                    ...Object.entries(MARKUP).map(([name, m]) => ({key: m.key, run: v => format(v, name)})),
                    // Enter starts a plain new line (no copied indentation), Tab
                    // inserts two spaces instead of leaving the editor; with a
                    // selection it indents the selected lines.
                    {key: 'Enter', run: insertNewline},
                    {key: 'Tab', run: v => v.state.selection.ranges.some(r => !r.empty) ? indentMore(v) :
                        (v.dispatch({...v.state.replaceSelection('  '), userEvent: 'input'}), true)},
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
