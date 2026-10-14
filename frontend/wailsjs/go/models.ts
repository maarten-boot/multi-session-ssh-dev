export namespace main {
	
	export class SessionInfo {
	    name: string;
	    target: string;
	    localPath: string;
	    remotePath: string;
	    tmuxName: string;
	    hasRemote: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.target = source["target"];
	        this.localPath = source["localPath"];
	        this.remotePath = source["remotePath"];
	        this.tmuxName = source["tmuxName"];
	        this.hasRemote = source["hasRemote"];
	        this.error = source["error"];
	    }
	}

}

