// The Dockerfile window's add-ons: common toolchains as lines for the
// project's software part of a container session's Dockerfile (Ubuntu 24.04).

const apt = packages => 'RUN apt-get update && apt-get install -y --no-install-recommends ' + packages + ' \\\n' +
    '    && rm -rf /var/lib/apt/lists/*';

export const TOOLCHAINS = {
    'Build tools': apt('build-essential pkg-config make cmake'),
    Go: 'ARG GO_VERSION=1.26.0\n' +
        'RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" \\\n' +
        '    | tar -C /usr/local -xz \\\n' +
        '    && echo \'export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"\' > /etc/profile.d/go.sh',
    'Node.js': apt('nodejs npm'),
    Python: apt('python3 python3-pip python3-venv'),
    Rust: apt('rustc cargo'),
    Java: apt('openjdk-21-jdk-headless maven'),
};

// MARK starts the part of the Dockerfile that Session Workbench needs.
export const MARK = '# ---- Needed by Session Workbench';

// addToolchain puts lines into the Dockerfile text, as their own block
// before the part Session Workbench needs (else at the end).
export function addToolchain(text, lines, name = '') {
    const block = `# ${name || 'Added'} (added by the Dockerfile window)\n${lines}\n\n`;
    const i = text.indexOf(MARK);
    if (i < 0) return text.replace(/\n*$/, '\n\n') + block.replace(/\n+$/, '\n');
    return text.slice(0, i) + block + text.slice(i);
}
