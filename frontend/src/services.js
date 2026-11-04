// The Services window's add-ons: common services as Compose blocks for a
// container session's compose file (SPEC C7). Full image names: Podman
// does not guess a registry for a short one.

export const SERVICES = {
    PostgreSQL: {
        lines: 'postgres:\n' +
            '  image: docker.io/library/postgres:17\n' +
            '  environment:\n' +
            '    POSTGRES_USER: dev\n' +
            '    POSTGRES_PASSWORD: dev\n' +
            '    POSTGRES_DB: dev\n' +
            '  volumes:\n' +
            '    - pgdata:/var/lib/postgresql/data',
        volume: 'pgdata',
        hint: 'postgres://dev:dev@postgres:5432/dev',
    },
    'MySQL/MariaDB': {
        lines: 'mariadb:\n' +
            '  image: docker.io/library/mariadb:11\n' +
            '  environment:\n' +
            '    MARIADB_ROOT_PASSWORD: dev\n' +
            '    MARIADB_USER: dev\n' +
            '    MARIADB_PASSWORD: dev\n' +
            '    MARIADB_DATABASE: dev\n' +
            '  volumes:\n' +
            '    - mariadb:/var/lib/mysql',
        volume: 'mariadb',
        hint: 'mysql://dev:dev@mariadb:3306/dev',
    },
    Redis: {
        lines: 'redis:\n' +
            '  image: docker.io/library/redis:7',
        hint: 'redis://redis:6379',
    },
    MongoDB: {
        lines: 'mongo:\n' +
            '  image: docker.io/library/mongo:8\n' +
            '  volumes:\n' +
            '    - mongodata:/data/db',
        volume: 'mongodata',
        hint: 'mongodb://mongo:27017',
    },
    nginx: {
        lines: 'nginx:\n' +
            '  image: docker.io/library/nginx:stable\n' +
            '  # Your configuration next to this file, e.g.:\n' +
            '  # volumes:\n' +
            '  #   - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro\n' +
            '  ports:\n' +
            '    - "127.0.0.1:8080:80"  # http://localhost:8080 on this computer',
        hint: 'http://nginx:80 (from the dev container)',
    },
};

const indent = (text, by) => text.split('\n').map(l => l ? by + l : l).join('\n');

// topKey finds a top-level key ("services:") and returns the index of its
// line and of the line after its section, or null.
function section(lines, key) {
    const start = lines.findIndex(l => new RegExp(`^${key}:\\s*(\\{\\s*\\}\\s*)?(#.*)?$`).test(l));
    if (start < 0) return null;
    let end = start + 1;
    for (let i = start + 1; i < lines.length; i++) {
        if (/^\S/.test(lines[i]) && !/^#/.test(lines[i])) break;
        if (lines[i].trim() && !/^\s*#/.test(lines[i])) end = i + 1;
    }
    return {start, end};
}

// hasService: the file already has a service of that name.
export function hasService(text, name) {
    const lines = text.split('\n');
    const s = section(lines, 'services');
    return !!s && lines.slice(s.start + 1, s.end).some(l => l.startsWith(`  ${name}:`));
}

// addService puts a service block into the compose text under services:
// (made when missing; "services: {}" becomes a section) and its volume
// under volumes:. Returns the text unchanged when the service is there.
export function addService(text, label) {
    const svc = SERVICES[label];
    const name = svc.lines.split(':')[0];
    if (hasService(text, name)) return text;
    let lines = text.replace(/\n*$/, '').split('\n');
    if (lines.length === 1 && lines[0] === '') lines = [];
    const block = [`  # ${label} (added by the Services window): ${svc.hint}`, ...indent(svc.lines, '  ').split('\n')];
    let s = section(lines, 'services');
    if (!s) {
        lines.push('services:');
        s = {start: lines.length - 1, end: lines.length};
    }
    lines[s.start] = 'services:';
    lines.splice(s.end, 0, ...block);
    if (svc.volume) {
        const v = section(lines, 'volumes');
        if (!v) lines.push('volumes:', `  ${svc.volume}:`);
        else if (!lines.slice(v.start + 1, v.end).some(l => l.startsWith(`  ${svc.volume}:`))) {
            lines[v.start] = 'volumes:';
            lines.splice(v.end, 0, `  ${svc.volume}:`);
        }
    }
    return lines.join('\n') + '\n';
}
