import {SetLinks, WebInfo, WebRuns} from '../wailsjs/go/main/App';
import {BrowserOpenURL} from '../wailsjs/runtime/runtime';
import {el} from './ui';

const errText = err => err?.message ?? String(err);

// Status shown as text first (✓ ✗ …), so it reads without colors.
const STATE = {
    success: ['✓', 'success'], failure: ['✗', 'failure'], cancelled: ['⊘', 'cancelled'], skipped: ['–', 'skipped'],
    timed_out: ['✗', 'timed out'], action_required: ['!', 'action required'], neutral: ['–', 'neutral'],
    startup_failure: ['✗', 'startup failure'],
};

function runState(r) {
    if (r.status !== 'completed') return [r.status === 'in_progress' ? '●' : '○', r.status.replace('_', ' '), 'running'];
    const [icon, text] = STATE[r.conclusion] ?? ['?', r.conclusion || 'completed'];
    return [icon, text, r.conclusion];
}

function ago(iso) {
    const s = (Date.now() - Date.parse(iso)) / 1000;
    if (!isFinite(s)) return '';
    if (s < 60) return 'just now';
    if (s < 3600) return `${Math.round(s / 60)} min ago`;
    if (s < 86400) return `${Math.round(s / 3600)} h ago`;
    return `${Math.round(s / 86400)} d ago`;
}

const open = url => /^https?:\/\//.test(url) && BrowserOpenURL(url);

// WebView is the Web tab: links for the repository (opened in the system
// browser), the session's own links, and the latest GitHub Actions runs via
// the GitHub CLI, refreshed while the tab is visible.
export class WebView {
    constructor(container, session) {
        this.session = session;
        this.info = null;
        this.repoEl = el('div', {className: 'web-repo'});
        this.linksEl = el('div', {className: 'web-links'});
        this.customEl = el('div', {className: 'web-links'});
        this.runsEl = el('div', {className: 'web-runs'});
        this.runsStatus = el('span', {className: 'web-runs-status'});
        const refresh = el('button', {className: 'icon', textContent: '⟳', title: 'Refresh'});
        refresh.onclick = () => this.loadRuns();
        this.form = this.addForm();
        container.append(el('div', {className: 'web'},
            el('div', {className: 'web-scroll'},
                this.repoEl, this.linksEl,
                el('h3', {}, 'Build runs ', this.runsStatus, refresh), this.runsEl,
                el('h3', {textContent: 'Your links'}), this.customEl, this.form)));
        this.refresh();
    }

    async refresh() {
        try {
            this.info = await WebInfo(this.session.id);
        } catch (err) {
            this.repoEl.textContent = errText(err);
            return;
        }
        const r = this.info.repo;
        this.repoEl.replaceChildren(r
            ? el('span', {}, el('b', {textContent: r.label}), ` on ${r.host}`)
            : el('span', {className: 'muted', textContent: 'No git remote found for this directory; add your own links below.'}));
        this.linksEl.replaceChildren(...this.info.links.map(l => this.linkButton(l)));
        this.renderCustom();
        this.loadRuns();
    }

    linkButton(l, extra) {
        const b = el('button', {className: 'web-link', textContent: l.name + ' ↗', title: l.url});
        b.onclick = () => open(l.url);
        return extra ? el('span', {className: 'web-custom'}, b, extra) : b;
    }

    renderCustom() {
        const links = this.info?.custom ?? [];
        this.customEl.replaceChildren(...links.map((l, i) => {
            const rm = el('button', {className: 'icon', textContent: '×', title: `Remove “${l.name}”`});
            rm.onclick = () => this.saveLinks(links.filter((_, j) => j !== i));
            return this.linkButton(l, rm);
        }));
        if (!links.length) this.customEl.append(el('span', {className: 'muted', textContent: 'None yet.'}));
    }

    addForm() {
        const name = el('input', {placeholder: 'Name', spellcheck: false});
        const url = el('input', {placeholder: 'https://…', spellcheck: false});
        const add = el('button', {type: 'submit', textContent: 'Add link'});
        this.formError = el('span', {className: 'bad'});
        const f = el('form', {className: 'web-add'}, name, url, add, this.formError);
        f.onsubmit = async e => {
            e.preventDefault();
            const ok = await this.saveLinks([...(this.info?.custom ?? []), {name: name.value, url: url.value}]);
            if (ok) name.value = url.value = '';
        };
        return f;
    }

    async saveLinks(links) {
        try {
            await SetLinks(this.session.id, links);
            this.formError.textContent = '';
            this.info.custom = links.map(l => ({name: l.name.trim(), url: l.url.trim()}));
            this.renderCustom();
            return true;
        } catch (err) {
            this.formError.textContent = errText(err);
            return false;
        }
    }

    async loadRuns() {
        clearTimeout(this.timer);
        const r = this.info?.repo;
        if (!r || r.kind !== 'github') {
            this.runsStatus.textContent = '';
            this.runsEl.replaceChildren(el('span', {className: 'muted', textContent:
                r ? 'Runs are shown for GitHub repositories (use the links above).' : 'No repository.'}));
            return;
        }
        if (!this.info.gh) {
            this.runsEl.replaceChildren(el('span', {className: 'muted', textContent:
                'Install the GitHub CLI (gh) and run “gh auth login” once in the Local tab to see the runs here.'}));
            return;
        }
        this.runsStatus.textContent = 'loading…';
        let runs;
        try {
            runs = await WebRuns(this.session.id);
        } catch (err) {
            this.runsStatus.textContent = '';
            this.runsEl.replaceChildren(el('span', {className: 'bad', textContent: errText(err)}));
            this.schedule(120000);
            return;
        }
        this.runsStatus.textContent = `updated ${new Date().toLocaleTimeString()}`;
        this.runsEl.replaceChildren(...runs.map(run => {
            const [icon, text, cls] = runState(run);
            const row = el('div', {className: `web-run ${cls}`, title: `Open run in the browser: ${run.url}`},
                el('span', {className: 'state', textContent: `${icon} ${text}`}),
                el('span', {className: 'wf', textContent: run.workflowName}),
                el('span', {className: 'title', textContent: run.displayTitle}),
                el('span', {className: 'branch', textContent: run.headBranch}),
                el('span', {className: 'when', textContent: ago(run.createdAt), title: run.createdAt}));
            row.onclick = () => open(run.url);
            return row;
        }));
        if (!runs.length) this.runsEl.append(el('span', {className: 'muted', textContent: 'No runs yet.'}));
        // Poll faster while something is still running.
        this.schedule(runs.some(r => r.status !== 'completed') ? 15000 : 120000);
    }

    schedule(ms) {
        clearTimeout(this.timer);
        this.timer = setTimeout(() => this.visible && this.loadRuns(), ms);
    }

    focus() {
    }

    disconnect() {
    }

    dispose() {
        clearTimeout(this.timer);
    }
}
