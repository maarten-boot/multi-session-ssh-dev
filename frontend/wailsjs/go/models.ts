export namespace config {
	
	export class Command {
	    name: string;
	    command: string;
	    run?: boolean;
	    group: string;
	    where: string;
	
	    static createFrom(source: any = {}) {
	        return new Command(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
	        this.run = source["run"];
	        this.group = source["group"];
	        this.where = source["where"];
	    }
	}
	export class Host {
	    name: string;
	    kind: string;
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
	        this.kind = source["kind"];
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
	export class QACommand {
	    name: string;
	    command: string[];
	    models: string[];
	
	    static createFrom(source: any = {}) {
	        return new QACommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
	        this.models = source["models"];
	    }
	}
	export class Session {
	    id: string;
	    name: string;
	    description: string;
	    host: string;
	    local_path: string;
	    start_mode: string;
	    env: Record<string, string>;
	    local_init: string;
	    color_mode: string;
	    group: string;
	    trusted_git: string;
	    links: Link[];
	    record: string;
	    commands: Command[];
	    history: string;
	    own_claude_volume: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.host = source["host"];
	        this.local_path = source["local_path"];
	        this.start_mode = source["start_mode"];
	        this.env = source["env"];
	        this.local_init = source["local_init"];
	        this.color_mode = source["color_mode"];
	        this.group = source["group"];
	        this.trusted_git = source["trusted_git"];
	        this.links = this.convertValues(source["links"], Link);
	        this.record = source["record"];
	        this.commands = this.convertValues(source["commands"], Command);
	        this.history = source["history"];
	        this.own_claude_volume = source["own_claude_volume"];
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
	export class StartCommand {
	    name: string;
	    command: string;
	
	    static createFrom(source: any = {}) {
	        return new StartCommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.command = source["command"];
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
	    ctrl_copy_paste?: boolean;
	    tab_order: string[];
	    hidden_tabs: string[];
	    claude_backup?: boolean;
	    notifications?: boolean;
	    claude_backup_every: string;
	    record_remote: boolean;
	    record_every: string;
	    record_dir: string;
	    private_history?: boolean;
	    save_agent_copies?: boolean;
	    save_agent_copies_every: string;
	    save_agent_copies_at_quit?: boolean;
	    saved_copies_hours: number;
	    saved_copies_days?: number;
	    spell_check?: boolean;
	    spell_language: string;
	    spell_words: string[];
	    qa_model: string;
	    container_engine: string;
	    qa_engine: string;
	    qa_claude: string;
	    qa_claude_model: string;
	    qa_commands: QACommand[];
	    description_delay?: number;
	    commands: Command[];
	    start_commands: StartCommand[];
	
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
	        this.ctrl_copy_paste = source["ctrl_copy_paste"];
	        this.tab_order = source["tab_order"];
	        this.hidden_tabs = source["hidden_tabs"];
	        this.claude_backup = source["claude_backup"];
	        this.notifications = source["notifications"];
	        this.claude_backup_every = source["claude_backup_every"];
	        this.record_remote = source["record_remote"];
	        this.record_every = source["record_every"];
	        this.record_dir = source["record_dir"];
	        this.private_history = source["private_history"];
	        this.save_agent_copies = source["save_agent_copies"];
	        this.save_agent_copies_every = source["save_agent_copies_every"];
	        this.save_agent_copies_at_quit = source["save_agent_copies_at_quit"];
	        this.saved_copies_hours = source["saved_copies_hours"];
	        this.saved_copies_days = source["saved_copies_days"];
	        this.spell_check = source["spell_check"];
	        this.spell_language = source["spell_language"];
	        this.spell_words = source["spell_words"];
	        this.qa_model = source["qa_model"];
	        this.container_engine = source["container_engine"];
	        this.qa_engine = source["qa_engine"];
	        this.qa_claude = source["qa_claude"];
	        this.qa_claude_model = source["qa_claude_model"];
	        this.qa_commands = this.convertValues(source["qa_commands"], QACommand);
	        this.description_delay = source["description_delay"];
	        this.commands = this.convertValues(source["commands"], Command);
	        this.start_commands = this.convertValues(source["start_commands"], StartCommand);
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
	    outside: boolean;
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
	        this.outside = source["outside"];
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
	
	export class ContainerFile {
	    path: string;
	    text: string;
	    in_repo: boolean;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new ContainerFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.text = source["text"];
	        this.in_repo = source["in_repo"];
	        this.state = source["state"];
	    }
	}
	export class ExportPart {
	    id: string;
	    label: string;
	    always: boolean;
	    default: boolean;
	    files: number;
	    size: number;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportPart(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.always = source["always"];
	        this.default = source["default"];
	        this.files = source["files"];
	        this.size = source["size"];
	        this.note = source["note"];
	    }
	}
	export class ExportResult {
	    file: string;
	    missing: string[];
	    skipped: number;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.missing = source["missing"];
	        this.skipped = source["skipped"];
	    }
	}
	export class ImportInfo {
	    path: string;
	    manifest: sessionarchive.Manifest;
	    host_exists: boolean;
	    not_imported: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.manifest = this.convertValues(source["manifest"], sessionarchive.Manifest);
	        this.host_exists = source["host_exists"];
	        this.not_imported = source["not_imported"];
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
	export class ImportRequest {
	    path: string;
	    host: string;
	    add_host: boolean;
	    local_path: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.host = source["host"];
	        this.add_host = source["add_host"];
	        this.local_path = source["local_path"];
	        this.name = source["name"];
	    }
	}
	export class ImportResult {
	    session: session.View;
	    notes: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session = this.convertValues(source["session"], session.View);
	        this.notes = source["notes"];
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
	export class QAEngine {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new QAEngine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class QAInfo {
	    turns: qa.Turn[];
	    busy: boolean;
	    engines: QAEngine[];
	    engine: string;
	    model: string;
	    log_dir: string;
	
	    static createFrom(source: any = {}) {
	        return new QAInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.turns = this.convertValues(source["turns"], qa.Turn);
	        this.busy = source["busy"];
	        this.engines = this.convertValues(source["engines"], QAEngine);
	        this.engine = source["engine"];
	        this.model = source["model"];
	        this.log_dir = source["log_dir"];
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
	export class RecordInfo {
	    default_dir: string;
	    recording: Record<string, boolean>;
	
	    static createFrom(source: any = {}) {
	        return new RecordInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default_dir = source["default_dir"];
	        this.recording = source["recording"];
	    }
	}
	export class SavedInfo {
	    base: string;
	    exists: boolean;
	    latest: savedcopy.Copy;
	    copies: savedcopy.Copy[];
	
	    static createFrom(source: any = {}) {
	        return new SavedInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.base = source["base"];
	        this.exists = source["exists"];
	        this.latest = this.convertValues(source["latest"], savedcopy.Copy);
	        this.copies = this.convertValues(source["copies"], savedcopy.Copy);
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
	export class SyncInfo {
	    git: boolean;
	    saved: SavedInfo;
	
	    static createFrom(source: any = {}) {
	        return new SyncInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.git = source["git"];
	        this.saved = this.convertValues(source["saved"], SavedInfo);
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
	export class TakeResult {
	    done: string[];
	    failed: string;
	
	    static createFrom(source: any = {}) {
	        return new TakeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.done = source["done"];
	        this.failed = source["failed"];
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
	export class Page {
	    path: string;
	    name: string;
	    has_text: boolean;
	    children: Page[];
	
	    static createFrom(source: any = {}) {
	        return new Page(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.has_text = source["has_text"];
	        this.children = this.convertValues(source["children"], Page);
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

export namespace qa {
	
	export class Model {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new Model(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class Usage {
	    input: number;
	    output: number;
	    cache_read: number;
	    cache_write: number;
	
	    static createFrom(source: any = {}) {
	        return new Usage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input = source["input"];
	        this.output = source["output"];
	        this.cache_read = source["cache_read"];
	        this.cache_write = source["cache_write"];
	    }
	}
	export class Turn {
	    // Go type: time
	    time: any;
	    question: string;
	    answer: string;
	    engine?: string;
	    model?: string;
	    usage: Usage;
	    error?: string;
	    raw?: number[];
	    session?: string;
	
	    static createFrom(source: any = {}) {
	        return new Turn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = this.convertValues(source["time"], null);
	        this.question = source["question"];
	        this.answer = source["answer"];
	        this.engine = source["engine"];
	        this.model = source["model"];
	        this.usage = this.convertValues(source["usage"], Usage);
	        this.error = source["error"];
	        this.raw = source["raw"];
	        this.session = source["session"];
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

export namespace savedcopy {
	
	export class Copy {
	    stamp: string;
	    dir: string;
	    when: string;
	    from: string;
	    files: number;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new Copy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stamp = source["stamp"];
	        this.dir = source["dir"];
	        this.when = source["when"];
	        this.from = source["from"];
	        this.files = source["files"];
	        this.note = source["note"];
	    }
	}

}

export namespace session {
	
	export class Edit {
	    name: string;
	    description?: string;
	    start_mode: string;
	    color_mode: string;
	    group: string;
	    record: string;
	    history: string;
	    own_claude_volume?: boolean;
	    commands: config.Command[];
	
	    static createFrom(source: any = {}) {
	        return new Edit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.start_mode = source["start_mode"];
	        this.color_mode = source["color_mode"];
	        this.group = source["group"];
	        this.record = source["record"];
	        this.history = source["history"];
	        this.own_claude_volume = source["own_claude_volume"];
	        this.commands = this.convertValues(source["commands"], config.Command);
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
	export class NewSession {
	    host: string;
	    local_path: string;
	    name: string;
	    description: string;
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
	        this.description = source["description"];
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
	    description: string;
	    host: string;
	    local_path: string;
	    start_mode: string;
	    color_mode: string;
	    group: string;
	    trusted_git: string;
	    files: string;
	    remote_path: string;
	    tmux_name: string;
	    record: string;
	    history: string;
	    own_claude_volume: boolean;
	    commands: config.Command[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new View(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.host = source["host"];
	        this.local_path = source["local_path"];
	        this.start_mode = source["start_mode"];
	        this.color_mode = source["color_mode"];
	        this.group = source["group"];
	        this.trusted_git = source["trusted_git"];
	        this.files = source["files"];
	        this.remote_path = source["remote_path"];
	        this.tmux_name = source["tmux_name"];
	        this.record = source["record"];
	        this.history = source["history"];
	        this.own_claude_volume = source["own_claude_volume"];
	        this.commands = this.convertValues(source["commands"], config.Command);
	        this.error = source["error"];
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
	export class State {
	    hosts: config.Host[];
	    groups: string[];
	    sessions: View[];
	    workspace_roots: string[];
	    ui: config.UI;
	    keys: Record<string, string>;
	    config_path: string;
	    config_error: string;
	    problems: string[];
	
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
	        this.problems = source["problems"];
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

export namespace sessionarchive {
	
	export class Manifest {
	    format: number;
	    app: string;
	    // Go type: time
	    exported: any;
	    session: config.Session;
	    host: config.Host;
	    parts: string[];
	    missing?: string[];
	    dockerfile_in_repo?: boolean;
	    skipped?: number;
	
	    static createFrom(source: any = {}) {
	        return new Manifest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.app = source["app"];
	        this.exported = this.convertValues(source["exported"], null);
	        this.session = this.convertValues(source["session"], config.Session);
	        this.host = this.convertValues(source["host"], config.Host);
	        this.parts = source["parts"];
	        this.missing = source["missing"];
	        this.dockerfile_in_repo = source["dockerfile_in_repo"];
	        this.skipped = source["skipped"];
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

export namespace spell {
	
	export class Dictionary {
	    lang: string;
	    aff: string;
	    dic: string;
	
	    static createFrom(source: any = {}) {
	        return new Dictionary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lang = source["lang"];
	        this.aff = source["aff"];
	        this.dic = source["dic"];
	    }
	}

}

export namespace syncer {
	
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
	export class AgentBranch {
	    name: string;
	    agent_ref: string;
	    state: string;
	    ahead: number;
	    behind: number;
	    current: boolean;
	    commits: Commit[];
	    changes: Change[];
	    more: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentBranch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.agent_ref = source["agent_ref"];
	        this.state = source["state"];
	        this.ahead = source["ahead"];
	        this.behind = source["behind"];
	        this.current = source["current"];
	        this.commits = this.convertValues(source["commits"], Commit);
	        this.changes = this.convertValues(source["changes"], Change);
	        this.more = source["more"];
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
	export class AgentState {
	    exists: boolean;
	    repo: boolean;
	    dirty: number;
	    unfetched: number;
	    branch: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.repo = source["repo"];
	        this.dirty = source["dirty"];
	        this.unfetched = source["unfetched"];
	        this.branch = source["branch"];
	    }
	}
	
	
	export class Fetched {
	    branch: string;
	    branches: string[];
	    agent_dirty: number;
	    agent: AgentBranch[];
	    more_branches: boolean;
	    agent_branch: string;
	
	    static createFrom(source: any = {}) {
	        return new Fetched(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.branches = source["branches"];
	        this.agent_dirty = source["agent_dirty"];
	        this.agent = this.convertValues(source["agent"], AgentBranch);
	        this.more_branches = source["more_branches"];
	        this.agent_branch = source["agent_branch"];
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
	    overwrites: number;
	    note: string;
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
	        this.overwrites = source["overwrites"];
	        this.note = source["note"];
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

