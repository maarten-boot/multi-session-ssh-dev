export namespace config {
	
	export class Host {
	    name: string;
	    ssh_target: string;
	    local_prefix: string;
	    remote_prefix: string;
	    files: string;
	    remote_workdir: string;
	
	    static createFrom(source: any = {}) {
	        return new Host(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ssh_target = source["ssh_target"];
	        this.local_prefix = source["local_prefix"];
	        this.remote_prefix = source["remote_prefix"];
	        this.files = source["files"];
	        this.remote_workdir = source["remote_workdir"];
	    }
	}
	export class Link {
	    name: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new Link(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.url = source["url"];
	    }
	}
	export class UI {
	    font_family: string;
	    font_size: number;
	    scrollback: number;
	    connect_on_start: string;
	    theme: string;
	    color_mode: string;
	    min_contrast: number;
	    auto_reconnect?: boolean;
	    activity_badges?: boolean;
	    copy_on_select: boolean;
	    tab_order: string[];
	    hidden_tabs: string[];
	
	    static createFrom(source: any = {}) {
	        return new UI(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.font_family = source["font_family"];
	        this.font_size = source["font_size"];
	        this.scrollback = source["scrollback"];
	        this.connect_on_start = source["connect_on_start"];
	        this.theme = source["theme"];
	        this.color_mode = source["color_mode"];
	        this.min_contrast = source["min_contrast"];
	        this.auto_reconnect = source["auto_reconnect"];
	        this.activity_badges = source["activity_badges"];
	        this.copy_on_select = source["copy_on_select"];
	        this.tab_order = source["tab_order"];
	        this.hidden_tabs = source["hidden_tabs"];
	    }
	}

}

export namespace forge {
	
	export class Link {
	    name: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new Link(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.url = source["url"];
	    }
	}
	export class Repo {
	    kind: string;
	    host: string;
	    path: string;
	    web: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new Repo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.host = source["host"];
	        this.path = source["path"];
	        this.web = source["web"];
	        this.label = source["label"];
	    }
	}
	export class Run {
	    databaseId: number;
	    workflowName: string;
	    displayTitle: string;
	    headBranch: string;
	    event: string;
	    status: string;
	    conclusion: string;
	    createdAt: string;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new Run(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.databaseId = source["databaseId"];
	        this.workflowName = source["workflowName"];
	        this.displayTitle = source["displayTitle"];
	        this.headBranch = source["headBranch"];
	        this.event = source["event"];
	        this.status = source["status"];
	        this.conclusion = source["conclusion"];
	        this.createdAt = source["createdAt"];
	        this.url = source["url"];
	    }
	}

}

export namespace fsbrowse {
	
	export class Entry {
	    name: string;
	    rel: string;
	    dir: boolean;
	    symlink: boolean;
	    size: number;
	    mtime: number;
	    hidden: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.rel = source["rel"];
	        this.dir = source["dir"];
	        this.symlink = source["symlink"];
	        this.size = source["size"];
	        this.mtime = source["mtime"];
	        this.hidden = source["hidden"];
	    }
	}
	export class Listing {
	    entries: Entry[];
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Listing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entries = this.convertValues(source["entries"], Entry);
	        this.truncated = source["truncated"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace gitstatus {
	
	export class Finding {
	    kind: string;
	    name: string;
	    detail: string;
	    why: string;
	    fix: string;
	
	    static createFrom(source: any = {}) {
	        return new Finding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.detail = source["detail"];
	        this.why = source["why"];
	        this.fix = source["fix"];
	    }
	}
	export class Audit {
	    repo: boolean;
	    findings: Finding[];
	    fingerprint: string;
	
	    static createFrom(source: any = {}) {
	        return new Audit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo = source["repo"];
	        this.findings = this.convertValues(source["findings"], Finding);
	        this.fingerprint = source["fingerprint"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Status {
	    repo: boolean;
	    skipped: string;
	    branch: string;
	    detached: boolean;
	    upstream: string;
	    ahead: number;
	    behind: number;
	    files: Record<string, string>;
	    dirs: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo = source["repo"];
	        this.skipped = source["skipped"];
	        this.branch = source["branch"];
	        this.detached = source["detached"];
	        this.upstream = source["upstream"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.files = source["files"];
	        this.dirs = source["dirs"];
	    }
	}

}

export namespace main {
	
	export class Paths {
	    local: string;
	    remote: string;
	
	    static createFrom(source: any = {}) {
	        return new Paths(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.local = source["local"];
	        this.remote = source["remote"];
	    }
	}
	export class WebInfo {
	    repo?: forge.Repo;
	    links: forge.Link[];
	    custom: config.Link[];
	    gh: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WebInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repo = this.convertValues(source["repo"], forge.Repo);
	        this.links = this.convertValues(source["links"], forge.Link);
	        this.custom = this.convertValues(source["custom"], config.Link);
	        this.gh = source["gh"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace notes {
	
	export class Note {
	    text: string;
	    path: string;
	    mtime: number;
	
	    static createFrom(source: any = {}) {
	        return new Note(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.path = source["path"];
	        this.mtime = source["mtime"];
	    }
	}

}

export namespace session {
	
	export class Edit {
	    name: string;
	    start_mode: string;
	    color_mode: string;
	    group: string;
	
	    static createFrom(source: any = {}) {
	        return new Edit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.start_mode = source["start_mode"];
	        this.color_mode = source["color_mode"];
	        this.group = source["group"];
	    }
	}
	export class NewSession {
	    host: string;
	    local_path: string;
	    name: string;
	    start_mode: string;
	    color_mode: string;
	    group: string;
	
	    static createFrom(source: any = {}) {
	        return new NewSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.local_path = source["local_path"];
	        this.name = source["name"];
	        this.start_mode = source["start_mode"];
	        this.color_mode = source["color_mode"];
	        this.group = source["group"];
	    }
	}
	export class Preview {
	    local_path: string;
	    remote_path: string;
	    name: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.local_path = source["local_path"];
	        this.remote_path = source["remote_path"];
	        this.name = source["name"];
	        this.error = source["error"];
	    }
	}
	export class View {
	    id: string;
	    name: string;
	    host: string;
	    local_path: string;
	    start_mode: string;
	    color_mode: string;
	    group: string;
	    trusted_git: string;
	    files: string;
	    remote_path: string;
	    tmux_name: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new View(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.local_path = source["local_path"];
	        this.start_mode = source["start_mode"];
	        this.color_mode = source["color_mode"];
	        this.group = source["group"];
	        this.trusted_git = source["trusted_git"];
	        this.files = source["files"];
	        this.remote_path = source["remote_path"];
	        this.tmux_name = source["tmux_name"];
	        this.error = source["error"];
	    }
	}
	export class State {
	    hosts: config.Host[];
	    groups: string[];
	    sessions: View[];
	    workspace_roots: string[];
	    ui: config.UI;
	    keys: Record<string, string>;
	    config_path: string;
	    config_error: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hosts = this.convertValues(source["hosts"], config.Host);
	        this.groups = source["groups"];
	        this.sessions = this.convertValues(source["sessions"], View);
	        this.workspace_roots = source["workspace_roots"];
	        this.ui = this.convertValues(source["ui"], config.UI);
	        this.keys = source["keys"];
	        this.config_path = source["config_path"];
	        this.config_error = source["config_error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace share {
	
	export class Status {
	    state: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.error = source["error"];
	    }
	}

}

export namespace syncer {
	
	export class AgentState {
	    exists: boolean;
	    repo: boolean;
	    dirty: number;
	    unfetched: number;
	
	    static createFrom(source: any = {}) {
	        return new AgentState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.repo = source["repo"];
	        this.dirty = source["dirty"];
	        this.unfetched = source["unfetched"];
	    }
	}
	export class Change {
	    op: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new Change(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.op = source["op"];
	        this.path = source["path"];
	    }
	}
	export class Commit {
	    hash: string;
	    subject: string;
	
	    static createFrom(source: any = {}) {
	        return new Commit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.subject = source["subject"];
	    }
	}
	export class Fetched {
	    branch: string;
	    agent_ref: string;
	    new: Commit[];
	    host_ahead: number;
	    can_ff: boolean;
	    branches: string[];
	    agent_dirty: number;
	
	    static createFrom(source: any = {}) {
	        return new Fetched(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.agent_ref = source["agent_ref"];
	        this.new = this.convertValues(source["new"], Commit);
	        this.host_ahead = source["host_ahead"];
	        this.can_ff = source["can_ff"];
	        this.branches = source["branches"];
	        this.agent_dirty = source["agent_dirty"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Plan {
	    agent: AgentState;
	    changes: Change[];
	    new: number;
	    changed: number;
	    deleted: number;
	    truncated: boolean;
	    blocked: string;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Plan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent = this.convertValues(source["agent"], AgentState);
	        this.changes = this.convertValues(source["changes"], Change);
	        this.new = source["new"];
	        this.changed = source["changed"];
	        this.deleted = source["deleted"];
	        this.truncated = source["truncated"];
	        this.blocked = source["blocked"];
	        this.done = source["done"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

