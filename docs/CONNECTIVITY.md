# Connecting your workstation and the agent host

How Session Workbench reaches the machine where Claude Code runs, the ways that machine can get
at your files, and what each choice means for security. Status as of October 2026 (after v0.8.0).

**Implemented** items work today. **Planned** items are proposals; they are not in the app yet.

## The setup in one picture

```
 your workstation ("host")                         agent host (VM or machine)
 ┌─────────────────────────────┐                   ┌──────────────────────────────┐
 │ Session Workbench           │   ssh (terminal)  │ tmux session cw-<id>         │
 │   Remote tab ───────────────┼──────────────────►│   └─ claude                  │
 │   Local tab: your shell     │                   │                              │
 │   Files tab: your files     │                   │ the repository, via one of   │
 │ git + push credentials      │   file access ◄──►│ the file access modes below  │
 └─────────────────────────────┘                   └──────────────────────────────┘
```

Two separate things connect the machines:

1. **The terminal**: always `ssh` from the host to the agent host, into tmux. See [SSH](#ssh).
2. **File access**: how the agent host sees the repository you are working on. There are
   several ways to do this, with different trade-offs. See [File access modes](#file-access-modes).

The idea behind the split: **credentials stay on the host.** claude can read and change code on
the agent host, but it cannot push, because the git credentials, ssh keys and tokens never
reach that machine. Your ssh agent is not forwarded to it either.

## Requirements on the agent host

- An ssh server you can log in to **with a key**, without typing a password.
- `tmux`.
- `claude`, on the PATH of your *login* shell (for example `~/.local/bin`).
- Depending on the file access mode: an NFS client, `sshfs` + FUSE, or `rsync` (see below).

Nothing is installed on the agent host by the app.

## SSH

**Implemented.** The app runs your own `ssh` client, so `~/.ssh/config`, host aliases,
ProxyJump, `known_hosts` and your agent all work as usual. On top of that, every connection is
forced to:

| Option | Why |
|---|---|
| `ForwardAgent=no` | Forwarding your ssh agent would let the agent host use your keys, and so your git credentials. |
| `ForwardX11=no`, `ForwardX11Trusted=no` | No access to your display. |
| `ClearAllForwardings=yes` | No port forwards configured in `~/.ssh/config` take effect. |
| `RemoteCommand=none` | Your config's RemoteCommand cannot replace the tmux command. |
| `ServerAliveInterval=15`, `ServerAliveCountMax=3` | A dead connection is noticed within about 45 s and reconnected. |

These options win over anything in `~/.ssh/config`. The remote side runs `tmux new-session -A`
in your login shell, so a session survives closing the app and network drops (not a reboot of
the agent host).

## File access modes

| | A. Shared file system | B. Live share over ssh | C. Working copy + sync |
|---|---|---|---|
| Status | **Implemented** | **Planned** | **Planned** |
| How | NFS, SMB, or a hypervisor share (virtiofs / 9p) | the app serves the session directory; the agent host mounts it with `sshfs` | a copy on the agent host; you send changes there and fetch commits back |
| Set up by | you (admin) | the app | the app |
| Root on agent host | for the mount (once) | no (`sshfs` + FUSE) | no |
| Works while the app is closed | yes | **no** — claude loses its files until the app reconnects | yes |
| Speed | good on a LAN | slower: every file access is a round trip | local speed on the agent host |
| What the agent host can change on your host | everything under the share, **including `.git`** | the session directory only, **including `.git`** | nothing until you fetch; only commits come back |
| Windows hosts | yes | yes | needs a sync engine without rsync (planned) |

### A. Shared file system (implemented)

The repository lives on your host and is exported to the agent host; the same files are visible
on both sides, usually under different paths. Tell the app how the paths map when you add the
host:

```toml
[[hosts]]
name          = "vibe"
ssh_target    = "me@agent-host"
local_prefix  = "~/share"                       # on your workstation
remote_prefix = "/net/192.168.122.1/home/me/share"   # the same directory on the agent host
```

The Remote tab then starts in the mapped directory. Options:

- **NFS** (Linux/macOS hosts): export the parent of your work directories, read-write, to the
  agent host only. Keep `root_squash`. User ids must match on both sides (or use idmapping).
- **SMB/CIFS**: works too, also from Windows hosts; watch file permissions and symlinks.
- **Hypervisor share** (agent host is a local VM): libvirt/QEMU *virtiofs* (preferred) or
  *virtio-9p*. Configured in the VM definition, not in this app. No network setup needed.

### B. Live share over ssh (planned)

For users without NFS. The app would run a small SFTP file server **confined to the session
directory** (symlinks and `..` cannot leave it) and connect it through the existing ssh
connection to `sshfs -o passive` on the agent host, which mounts it at the remote path.

- No open ports, and **no credentials on the agent host**: the agent host never logs in to your
  host. Your host serves files over the connection that it opened itself.
- Needs `sshfs` and FUSE on the agent host; no root.
- **The share lives only while the app runs and is connected.** If you close the app or your
  laptop sleeps, claude in tmux sees errors such as *Transport endpoint is not connected* until
  the app reconnects and mounts again.
- We prefer this to 9p: a 9p mount needs root on the agent host (kernel `v9fs`) or plan9port's
  `9pfuse`, which is rarely installed, for the same result. In a local VM, a hypervisor 9p or
  virtiofs share (mode A) is the better way to use 9p.

### C. Working copy + sync (planned)

For users who today `rsync` the repository to the agent host and back. The session would get a
working copy on the agent host (for example `~/work/<session>`), plus two actions:

- **Send to agent** (host → agent host): `rsync` over the same locked-down ssh, with a dry-run
  preview of what will change before anything is written.
- **Get back** (agent host → host): **not a file copy back.** The host runs `git fetch` from
  the agent's copy. You receive exactly the commits, and then merge or fast-forward and push as
  usual. Uncommitted work can come back as a patch for you to review.

Why not `rsync` back? Copying the agent's `.git` directory onto your host has the problem
described next. Fetching commits does not: hooks and configuration are never transferred.

Windows has no built-in `rsync`; a Windows version needs its own sync engine (planned later).
macOS and Linux use `rsync`.

## Security: what the agent host can do to your host

Keeping credentials on the host protects against **pushing**. It does **not** protect against
code running on your host, as soon as the agent host can write files that your host's tools then
act on. With modes A and B, and with a manual rsync back, the agent host can write into the
repository's **`.git` directory**, and git on your host trusts what it finds there:

- **Hooks**: `.git/hooks/pre-commit`, `post-checkout`, `pre-push` and others run on your host
  the next time you commit, check out or push.
- **Repository config** (`.git/config`): many settings name a program that git runs, for
  example `core.fsmonitor`, `core.hooksPath`, `core.sshCommand`, `core.pager`, `core.editor`,
  `credential.helper`, `filter.<name>.clean`/`smudge`/`process`, `diff.<name>.textconv`,
  `diff.external`, `merge.<name>.driver`, `gpg.program`, `alias.*` (with `!`), and
  `include.path` / `includeIf`, which pull in more config.

So a misbehaving or compromised agent host could run commands **on your host, as you, with your
credentials**, for example by planting a `pre-push` hook, or a `credential.helper` that hands
your token to an outside server on the next push.

**Setting things in your global git config does not fix this**: repository settings override
global ones. A planted `core.hooksPath` in `.git/config` wins over yours.

Other things the agent host can read in shared modes:

- Secrets in the working tree (`.env` files, keys, test credentials): anything in the shared
  directory is readable on the agent host.
- **Tokens in remote URLs** (`https://user:TOKEN@github.com/…` in `.git/config`) are readable
  there too. Use a credential helper on the host instead of tokens in URLs.

### What you can do today

1. **Look before you run git on the host.** After a claude session, before committing or
   pushing from the host:
   ```sh
   ls .git/hooks | grep -v '\.sample$'     # expect nothing unless you added hooks yourself
   git config --local --list               # look for any of the keys listed above
   ```
2. **Run host-side git with risky settings switched off** for routine commands. Settings on the
   command line override the repository's:
   ```sh
   git -c core.hooksPath=/dev/null -c core.fsmonitor=false -c core.sshCommand=ssh \
       -c credential.helper= -c credential.helper=<your helper> push
   ```
   This narrows the risk but does not cover every key above. Inspecting (step 1) remains
   necessary.
3. **Prefer mode C (planned)** when the agent host is less trusted: only commits come back.
4. **Share only what claude needs.** Don't export your whole home directory; export a work area,
   and keep secrets outside it.

### What the app does

**Implemented:**

- Stores no secrets. The config holds host names and paths only.
- Forces the ssh options above on every connection.
- The Files tab never executes files. It refuses programs and files the OS would run instead of
  open (native binaries, `.exe`/`.bat`/`.lnk`…, `.app`/`.command`…, `.desktop`…), and it stays
  inside the session directory.
- The Files tab's git status runs read-only (`--no-optional-locks`), with `core.fsmonitor`
  disabled and without entering submodules. `git status` can also run a repository-defined
  *clean filter* (`filter.<name>.clean` / `.process`) when it re-reads a file whose timestamp
  changed. So the app first reads the repository's **own** config, without following includes
  (reading config runs nothing). If that config defines `filter.*`, `include.*` / `includeIf.*`
  or `core.fsmonitor`, it **skips git status** and the Files bar shows *"git status skipped"*,
  with the reason in its tooltip. Settings from your global config (for example git-lfs) are
  your own and remain in effect. *(Added in October 2026, after a test showed that a repository
  filter did run.)*

**Planned:**

- A **repository check**: warn in the Files tab when the shared repository has active hooks or
  any of the risky config keys listed above, so you notice before running git on the host.

## Choosing a mode

| Your situation | Use |
|---|---|
| Agent host is a VM on your workstation | A, with a virtiofs (or 9p) share |
| Agent host on your LAN, NFS available | A, with NFS |
| No NFS, agent host reachable by ssh, files only needed while you work | B (planned) |
| Agent host less trusted, or you already work with rsync | C (planned) |
| Windows workstation | A with SMB, or B (planned) |

In every mode: review what comes back before you run git on your host, and keep credentials and
secrets out of what you share.
