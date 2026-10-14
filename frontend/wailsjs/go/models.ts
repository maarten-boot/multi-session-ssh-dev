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
	
	    static createFrom(source: any = {}) {
	        return new UI(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.font_family = source["font_family"];
	        this.font_size = source["font_size"];
	        this.scrollback = source["scrollback"];
	        this.connect_on_start = source["connect_on_start"];
	    }
	}

}

export namespace session {
	
	export class NewSession {
	    host: string;
	    local_path: string;
	    name: string;
	    start_mode: string;
	
	    static createFrom(source: any = {}) {
	        return new NewSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.local_path = source["local_path"];
	        this.name = source["name"];
	        this.start_mode = source["start_mode"];
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
	        this.remote_path = source["remote_path"];
	        this.tmux_name = source["tmux_name"];
	        this.error = source["error"];
	    }
	}
	export class State {
	    hosts: config.Host[];
	    sessions: View[];
	    workspace_roots: string[];
	    ui: config.UI;
	    config_path: string;
	    config_error: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hosts = this.convertValues(source["hosts"], config.Host);
	        this.sessions = this.convertValues(source["sessions"], View);
	        this.workspace_roots = source["workspace_roots"];
	        this.ui = this.convertValues(source["ui"], config.UI);
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

