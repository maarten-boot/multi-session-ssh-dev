import {QAAsk, QAChoose, QAClear, QALoad, QAModels, QAOpenLog, QAStop} from '../wailsjs/go/main/App';
import {ClipboardGetText, ClipboardSetText, EventsOff, EventsOn} from '../wailsjs/runtime/runtime';
import {el, showMenu, toast} from './ui';
import {engineName, parseAnswer, svgURL, tokens} from './qaparse';

const errText = err => err?.message ?? String(err);
const isMac = navigator.platform.startsWith('Mac');

// QAView is the Q&A tab: questions to Claude through the Claude API and
// their answers, one conversation per session (saved, and logged in the
// session's log folder). Enter asks, Shift+Enter starts a new line. The
// bar chooses the engine (Claude API, Claude Code, configured commands) and
// its model; dialogs.confirm asks before another engine starts over.
export class QAView {
    constructor(container, session, dialogs) {
        this.session = session;
        this.dialogs = dialogs;
        this.turns = [];
        this.busy = false;
        this.live = null; // the turn being answered
        this.engine = '';
        this.model = '';
        this.engineEl = el('select', {className: 'qa-engine', title: 'What answers'});
        this.engineEl.onchange = () => this.chooseEngine(this.engineEl.value);
        this.modelSel = el('select', {className: 'qa-model-select', title: 'Model'});
        this.modelSel.onchange = () => this.chooseModel(this.modelSel.value);
        this.modelEl = el('span', {className: 'qa-model'}, this.engineEl, this.modelSel);
        const clear = el('button', {textContent: 'New conversation', title: 'Start over (the log keeps this one)'});
        clear.onclick = () => this.clear();
        const log = el('button', {textContent: 'Open log folder'});
        log.onclick = () => QAOpenLog(this.session.id).catch(err => toast(errText(err), true));
        this.listEl = el('div', {className: 'qa-list'});
        this.input = el('textarea', {className: 'qa-input', rows: 3,
            placeholder: 'Ask Claude… (Enter asks, Shift+Enter for a new line)'});
        this.input.addEventListener('keydown', e => {
            if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
                e.preventDefault();
                this.ask();
            }
        });
        this.askBtn = el('button', {className: 'primary', textContent: 'Ask'});
        this.askBtn.onclick = () => this.busy ? QAStop(this.session.id) : this.ask();
        this.listEl.addEventListener('contextmenu', e => {
            e.preventDefault();
            this.listMenu(e.clientX, e.clientY);
        });
        this.input.addEventListener('contextmenu', e => {
            e.preventDefault();
            this.inputMenu(e.clientX, e.clientY);
        });
        container.append(el('div', {className: 'qa'},
            el('div', {className: 'qa-bar'}, this.modelEl, clear, log),
            this.listEl,
            el('div', {className: 'qa-ask'}, this.input, this.askBtn)));
        EventsOn(`qa:${session.id}`, s => {
            if (!this.live) return;
            this.live.answer += s;
            this.renderTurn(this.live);
            this.scrollDown();
        });
        this.refresh();
    }

    async refresh() {
        if (this.busy) return;
        try {
            const info = await QALoad(this.session.id);
            this.turns = info.turns ?? [];
            this.engineEl.replaceChildren(...(info.engines ?? []).map(e => el('option', {value: e.id, textContent: e.name})));
            this.engine = this.engineEl.value = info.engine;
            this.model = info.model;
            this.modelEl.title = info.log_dir ? `Logged in ${info.log_dir}` : '';
            this.loadModels();
        } catch (err) {
            this.turns = [];
            toast(errText(err), true);
        }
        this.render();
    }

    focus() {
        this.input.focus();
    }

    // loadModels fills the model list of the engine (the API's comes from
    // the API); the session's model is in it even when not listed.
    async loadModels() {
        const engine = this.engine;
        const option = (id, name) => el('option', {value: id, textContent: name || id || 'default'});
        this.modelSel.replaceChildren(option(this.model, this.model));
        this.modelSel.hidden = engine !== 'api' && engine !== 'claude';
        let list = [];
        try {
            list = await QAModels(engine) ?? [];
            this.modelSel.title = 'Model';
        } catch (err) {
            this.modelSel.title = `No model list: ${errText(err)}`;
        }
        if (engine !== this.engine) return; // changed meanwhile
        const opts = list.map(m => option(m.id, m.name));
        if (!list.some(m => m.id === this.model)) opts.unshift(option(this.model, this.model));
        this.modelSel.replaceChildren(...opts);
        this.modelSel.value = this.model;
        this.modelSel.hidden = opts.length < 2 && !this.model;
    }

    async chooseModel(model) {
        try {
            await QAChoose(this.session.id, this.engine, model);
            this.model = model;
        } catch (err) {
            this.modelSel.value = this.model;
            toast(errText(err), true);
        }
    }

    // chooseEngine switches the engine; with a conversation it starts over
    // (each engine knows only its own), after asking.
    async chooseEngine(engine) {
        const name = this.engineEl.selectedOptions[0]?.textContent ?? engine;
        this.engineEl.value = this.engine;
        if (this.busy) return toast('Stop the answer first', true);
        const change = async () => {
            if (this.turns.length) await QAClear(this.session.id);
            await QAChoose(this.session.id, engine, '');
        };
        try {
            if (this.turns.length) {
                const ok = await this.dialogs.confirm({title: `Ask ${name}?`,
                    text: `${name} starts a new conversation: it does not know this one. The log keeps it.`,
                    ok: 'New conversation', action: change});
                if (!ok) return;
            } else await change();
        } catch (err) {
            return toast(errText(err), true);
        }
        await this.refresh();
        this.focus();
    }

    // listMenu is the right-click menu of the questions and answers.
    listMenu(x, y) {
        const k = isMac ? '⌘' : 'Ctrl+';
        const sel = window.getSelection();
        const text = sel && this.listEl.contains(sel.anchorNode) ? sel.toString() : '';
        showMenu(x, y, [
            {label: `Copy   ${k}C`, disabled: !text, action: () => ClipboardSetText(text).then(() => toast('Copied'))},
            {label: 'Copy to the question', disabled: !text, action: () => {
                this.insert(text);
            }},
            {label: 'Paste into the question', action: () => ClipboardGetText().then(t => t && this.insert(t))},
            {label: 'Select all', action: () => sel?.selectAllChildren(this.listEl)},
        ]);
    }

    // inputMenu is the question box's right-click menu.
    inputMenu(x, y) {
        const k = isMac ? '⌘' : 'Ctrl+';
        const i = this.input;
        const [from, to] = [i.selectionStart, i.selectionEnd];
        const text = i.value.slice(from, to);
        const back = () => {
            i.focus();
            i.setSelectionRange(from, to);
        };
        showMenu(x, y, [
            {label: `Cut   ${k}X`, disabled: !text, action: () => ClipboardSetText(text).then(() => {
                back();
                i.setRangeText('', from, to, 'end');
            })},
            {label: `Copy   ${k}C`, disabled: !text, action: () => ClipboardSetText(text).then(back)},
            {label: `Paste   ${k}V`, action: () => ClipboardGetText().then(t => {
                back();
                if (t) i.setRangeText(t, from, to, 'end');
            })},
            {label: 'Select all', action: () => {
                i.focus();
                i.select();
            }},
        ]);
    }

    // insert puts text into the question box at the cursor.
    insert(text) {
        const i = this.input;
        i.focus();
        i.setRangeText(text, i.selectionStart, i.selectionEnd, 'end');
    }

    render() {
        this.listEl.replaceChildren(...this.turns.map(t => this.renderTurn(t)));
        if (!this.turns.length) {
            this.listEl.append(el('p', {className: 'qa-empty',
                textContent: 'Ask Claude a question. Only your questions and this conversation are sent — ' +
                    'nothing from the session. Every question and answer is saved and logged in the session\'s log folder.'}));
        }
        this.scrollDown();
    }

    // renderTurn (re)draws a turn; it keeps its element in t.el.
    renderTurn(t) {
        const answer = el('div', {className: 'qa-answer'});
        for (const p of parseAnswer(t.answer ?? '')) answer.append(this.part(p));
        if (t === this.live && !t.answer) answer.append(el('p', {className: 'qa-wait', textContent: 'Thinking…'}));
        const foot = el('div', {className: 'qa-foot'});
        if (t.error) foot.append(el('span', {className: 'qa-error', textContent: '✗ ' + t.error}));
        if (t !== this.live) {
            foot.append(el('span', {textContent: [new Date(t.time).toLocaleString(), tokens(t.usage, t.model, engineName(t.engine))]
                .filter(Boolean).join(' · ')}));
            if (t.answer) {
                const copy = el('button', {className: 'icon', textContent: '⧉', title: 'Copy the answer'});
                copy.onclick = () => ClipboardSetText(t.answer).then(() => toast('Answer copied'));
                foot.append(copy);
            }
        }
        const e = el('div', {className: 'qa-turn'},
            el('div', {className: 'qa-question'}, el('b', {textContent: 'You: '}), t.question),
            answer, foot);
        t.el?.replaceWith(e);
        t.el = e;
        return e;
    }

    part(p) {
        if (p.type === 'svg') {
            const img = el('img', {className: 'qa-svg', src: svgURL(p.text), alt: 'Picture from the answer'});
            const src = el('details', {}, el('summary', {textContent: 'SVG source'}), el('pre', {textContent: p.text}));
            return el('div', {className: 'qa-picture'}, img, src);
        }
        if (p.type === 'code') {
            const copy = el('button', {className: 'icon', textContent: '⧉', title: 'Copy'});
            copy.onclick = () => ClipboardSetText(p.text).then(() => toast('Copied'));
            return el('div', {className: 'qa-code'}, el('div', {className: 'qa-code-head'}, p.lang || 'code', copy),
                el('pre', {textContent: p.text}));
        }
        return el('div', {className: 'qa-text', textContent: p.text});
    }

    scrollDown() {
        this.listEl.scrollTop = this.listEl.scrollHeight;
    }

    setBusy(b) {
        this.busy = b;
        this.askBtn.textContent = b ? 'Stop' : 'Ask';
        this.askBtn.classList.toggle('primary', !b);
    }

    async ask() {
        const q = this.input.value.trim();
        if (!q || this.busy) return;
        if (!this.turns.length) this.listEl.replaceChildren();
        this.live = {question: q, answer: '', time: new Date().toISOString()};
        this.turns.push(this.live);
        this.listEl.append(this.renderTurn(this.live));
        this.scrollDown();
        this.input.value = '';
        this.setBusy(true);
        const live = this.live;
        let done;
        try {
            done = await QAAsk(this.session.id, q);
        } catch (err) {
            done = {...live, error: errText(err)};
        }
        this.live = null;
        this.setBusy(false);
        Object.assign(live, done, {el: live.el});
        this.renderTurn(live);
        this.scrollDown();
        this.input.focus();
    }

    async clear() {
        if (this.busy) return toast('Stop the answer first', true);
        try {
            await QAClear(this.session.id);
        } catch (err) {
            return toast(errText(err), true);
        }
        this.turns = [];
        this.render();
        this.focus();
    }

    dispose() {
        EventsOff(`qa:${this.session.id}`);
    }
}
