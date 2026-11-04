#!/bin/bash
# Headless GUI test of a container host (internal/container) with a real
# engine (Podman, else Docker; SW_ENGINE=docker chooses): the first Remote
# connection builds the image and starts the container, the shell inside
# runs as you in the mounted directory with Claude Code there; the
# Dockerfile window saves an edit and rebuilds (replacing the container);
# Stop container, then Reconnect starts it
# again; deleting the session removes container and image. The first build
# downloads packages (minutes).
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-container.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
# Rootless Podman needs the user's systemd bus; this test opens nothing
# on the desktop.
export SW_IN_DBUS=1
. "$(dirname "$0")/smoke-lib.sh"
ENGINE=${SW_ENGINE:-$(command -v podman >/dev/null && echo podman || echo docker)}
command -v $ENGINE >/dev/null || { echo "SKIP: neither Podman nor Docker installed"; exit 0; }
# smoke-lib moves XDG_DATA_HOME into $OUT; Podman keeps its images there,
# so point it back at its usual store (no rebuild from scratch per run).
cat >$OUT/storage.conf <<CONF
[storage]
driver = "overlay"
graphroot = "$HOME/.local/share/containers/storage"
runroot = "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/containers"
CONF
export CONTAINERS_STORAGE_CONF=$OUT/storage.conf
ID=s-smk$$
PROJ=$OUT/proj; rm -rf $PROJ; mkdir -p $PROJ
CFG=$OUT/config.toml
cat >$CFG <<TOML
[ui]
container_engine = "$ENGINE"
[[hosts]]
name = "boxes"
kind = "container"
[[sessions]]
id = "$ID"
name = "boxed"
host = "boxes"
local_path = "$PROJ"
start_mode = "shell"
own_claude_volume = true
TOML
k() { xdotool key "$@"; sleep 0.6; }
t() { xdotool type --delay 15 "$1"; xdotool key Return; sleep ${2:-1.5}; }
shot() { import -window root $OUT/$1.png; }
waitfile() { for i in $(seq ${2:-60}); do [ -s "$1" ] && return 0; sleep 2; done; return 1; }
cleanup() { $ENGINE rm -f sw-$ID >/dev/null 2>&1; $ENGINE rmi -f sw-$ID >/dev/null 2>&1; $ENGINE volume rm sw-claude-$ID >/dev/null 2>&1; }
trap cleanup EXIT
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; k ctrl+shift+3                          # Remote: builds, starts, connects
for i in $(seq 300); do $ENGINE ps --format '{{.Names}}' 2>/dev/null | grep -qx sw-$ID && break; sleep 2; done
sleep 8                                                # sshd up, ssh and tmux attached
shot 1-connected
t 'id -un > inside.txt; id -u >> inside.txt; hostname >> inside.txt; pwd >> inside.txt; command -v claude >> inside.txt; echo $CLAUDE_CONFIG_DIR >> inside.txt'
waitfile $PROJ/inside.txt 10
expect "inside: you, your id, the container, the same path" "$(id -un)|$(id -u)|sw-$ID|$PROJ|" \
	"$(head -4 $PROJ/inside.txt | tr '\n' '|')"
check "Claude Code in the container" grep -q '/.local/bin/claude$' $PROJ/inside.txt
check "its ~/.claude is the login volume" grep -q '/.claude$' $PROJ/inside.txt
check "the file belongs to you" test "$(stat -c %u $PROJ/inside.txt)" = "$(id -u)"
check "ssh only on 127.0.0.1" bash -c "$ENGINE port sw-$ID 22/tcp | grep -q '^127.0.0.1:'"
check "its own Claude login volume" bash -c "$ENGINE volume ls --format '{{.Name}}' | grep -qx sw-claude-$ID"
menu() { xdotool mousemove 114 131 click 3; sleep 0.8; xdotool mousemove 170 $1 click 1; sleep 1.5; }
menu 190                                               # Dockerfile…
shot 2-dockerfile
k ctrl+Home; xdotool type --delay 15 '# smoke-mark'; k Return
k Tab; k Tab; k Return; sleep 1                        # Save (to the app's data folder)
DF=$XDG_DATA_HOME/session-workbench/containers/$ID/Dockerfile
check "the Dockerfile window saved the edit" grep -q '^# smoke-mark' $DF
check "the saved Dockerfile is the template" grep -q 'Needed by Session Workbench' $DF
state() { $ENGINE ps -a --filter name=^sw-$ID\$ --format '{{.State}}' 2>/dev/null | head -1; }
menu 190                                               # Dockerfile… again
k Tab; k Tab; k Tab; k Return; sleep 1                 # (text →) Save to → Save → Save and rebuild: asks (it runs)
k Return                                               # Rebuild
for i in $(seq 300); do [ -z "$(state)" ] && break; sleep 1; done; sleep 1
shot 2b-rebuilt
check "Save and rebuild replaced the running container" test -z "$(state)"
k Escape
k Return                                               # Reconnect (Enter): the new container
for i in $(seq 60); do [ "$(state)" = running ] && break; sleep 1; done; sleep 8
t 'echo rebuilt > rebuilt.txt'
waitfile $PROJ/rebuilt.txt 10
check "the Remote tab connects to the rebuilt container" test -s $PROJ/rebuilt.txt
menu 250                                               # Stop container… (after Services)
k Return; sleep 10                                     # confirm
check "Stop container stopped it" bash -c "[ \"$(state)\" != running ] && [ -n \"$(state)\" ]"
shot 3-stopped
k Return                                               # Reconnect (Enter): starts it again
for i in $(seq 60); do [ "$(state)" = running ] && break; sleep 1; done; sleep 8
t 'echo again > again.txt'
waitfile $PROJ/again.txt 10
check "Reconnect started the container again" test -s $PROJ/again.txt
menu 607                                               # Delete session… (after Export… and Services)
shot 4-delete
k Tab; k space; k Tab; k Return; sleep 8               # tick "remove its container", Delete
check "the container is gone" bash -c "! $ENGINE ps -a --format '{{.Names}}' | grep -qx sw-$ID"
check "the image is gone" bash -c "! $ENGINE image inspect sw-$ID >/dev/null 2>&1"
check "its own login volume is gone" bash -c "! $ENGINE volume ls --format '{{.Name}}' | grep -qx sw-claude-$ID"
check "the app's files for it are gone" test ! -e $XDG_DATA_HOME/session-workbench/containers/$ID
check "the session is gone" bash -c "! grep -q $ID $CFG"
check "the project files stay" test -s $PROJ/inside.txt
alive
kill $APP; wait $APP 2>/dev/null
finish
