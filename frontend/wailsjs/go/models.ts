export namespace config {
	
	export class Host {
	    name: string;
	    ssh_target: string;
	    local_prefix: string;
	    remote_prefix: string;
	
	    static createFrom(source: any = {}) {
	        return new Host(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ssh_target = source["ssh_target"];
	        this.local_prefix = source["local_prefix"];
	        this.remote_prefix = source["remote_prefix"];
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

