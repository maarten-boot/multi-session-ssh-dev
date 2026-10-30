import {QAAsk, QAClear, QALoad, QAOpenLog, QAStop} from '../wailsjs/go/main/App';
import {ClipboardSetText, EventsOff, EventsOn} from '../wailsjs/runtime/runtime';
import {el, toast} from './ui';
import {parseAnswer, svgURL, tokens} from './qaparse';

const errText = err => err?.message ?? String(err);

// QAView is the Q&A tab: questions to Claude through the Claude API and
// their answers, one conversation per session (saved, and logged in the
// session's log folder). Enter asks, Shift+Enter starts a new line.
export class QAView {
    constructor(container, session) {
        this.session = session;
        this.turns = [];
        this.busy = false;
        this.live = null; // the turn being answered
        this.modelEl = el('span', {className: 'qa-model'});
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
            this.modelEl.textContent = `Model: ${info.model}`;
            this.modelEl.title = info.log_dir ? `Logged in ${info.log_dir}` : '';
        } catch (err) {
            this.turns = [];
            toast(errText(err), true);
        }
        this.render();
    }

    focus() {
        this.input.focus();
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
            foot.append(el('span', {textContent: [new Date(t.time).toLocaleString(), tokens(t.usage, t.model)].join(' · ')}));
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
