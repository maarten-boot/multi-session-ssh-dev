#!/bin/bash
# Headless GUI test of services next to a container session's dev
# container (SPEC C7) with a real engine and compose (Podman, else Docker;
# SW_ENGINE=docker chooses): the Services window adds Redis with its
# button and an environment variable for "dev", Save checks with compose
# (a renamed dev container is refused, the file stays); the Remote tab
# starts the dev container and Redis as one Compose project, the shell
# inside has the variable and reaches redis by name; Save and restart
# services adds a second service without making the dev container again;
# Stop container stops both, Reconnect starts both; deleting the session
# with its services' data removes containers, network and volumes. Pulls
# redis:7 the first time.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-compose.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
export SW_IN_DBUS=1
. "$(dirname "$0")/smoke-lib.sh"
ENGINE=${SW_ENGINE:-$(command -v podman >/dev/null && echo podman || echo docker)}
command -v $ENGINE >/dev/null || { echo "SKIP: neither Podman nor Docker installed"; exit 0; }
$ENGINE compose version >/dev/null 2>&1 || PODMAN_COMPOSE_WARNING_LOGS=false $ENGINE compose version >/dev/null 2>&1 ||
	{ echo "SKIP: $ENGINE has no compose"; exit 0; }
cat >$OUT/storage.conf <<CONF
[storage]
driver = "overlay"
graphroot = "$HOME/.local/share/containers/storage"
runroot = "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/containers"
CONF
export CONTAINERS_STORAGE_CONF=$OUT/storage.conf
ID=s-cmp$$
P=sw-$ID
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
name = "composed"
host = "boxes"
local_path = "$PROJ"
start_mode = "shell"
own_claude_volume = true
TOML
k() { xdotool key "$@"; sleep 0.6; }
t() { xdotool type --delay 15 "$1"; xdotool key Return; sleep ${2:-1.5}; }
shot() { import -window root $OUT/$1.png; }
# lines types each argument and Return (xdotool type drops line breaks in the text area).
lines() { for l in "$@"; do xdotool type --delay 10 -- "$l"; xdotool key Return; sleep 0.2; done; }
waitfile() { for i in $(seq ${2:-60}); do [ -s "$1" ] && return 0; sleep 2; done; return 1; }
names() { $ENGINE ps ${1:-} --filter label=com.docker.compose.project=$P --format '{{.Names}}' 2>/dev/null | sort | tr '\n' ' '; }
cleanup() {
	for c in $($ENGINE ps -aq --filter label=com.docker.compose.project=$P 2>/dev/null); do $ENGINE rm -f $c >/dev/null 2>&1; done
	$ENGINE rm -f $P >/dev/null 2>&1; $ENGINE rmi -f $P >/dev/null 2>&1; $ENGINE volume rm sw-claude-$ID >/dev/null 2>&1
	for n in $($ENGINE network ls -q --filter label=com.docker.compose.project=$P 2>/dev/null); do $ENGINE network rm $n >/dev/null 2>&1; done
	for v in $($ENGINE volume ls -q --filter label=com.docker.compose.project=$P 2>/dev/null); do $ENGINE volume rm $v >/dev/null 2>&1; done
}
trap cleanup EXIT
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
menu() { xdotool mousemove 114 131 click 3; sleep 0.8; xdotool mousemove 170 $1 click 1; sleep 2; }
CF=$XDG_DATA_HOME/session-workbench/containers/$ID/compose.yaml
menu 220                                               # Services (compose)…
shot 1-services
k ctrl+a; k BackSpace
lines 'services:' '  dev:' '    environment:' '      REDIS_URL: redis://redis:6379' '    depends_on: [redis]'
k shift+Tab; k shift+Tab; k shift+Tab; k space        # + Redis (Redis, MongoDB, nginx are the last three)
shot 2-redis-added
k Tab; k Tab; k Return; sleep 3                        # (the button puts the focus in the text) Save to → Save
check "Save wrote the compose file (the app's data folder)" test -s $CF
check "it has the Redis block from the button" grep -q 'image: docker.io/library/redis:7' $CF
check "and the dev service's variable" grep -q 'REDIS_URL: redis://redis:6379' $CF
cp $CF $OUT/saved.yaml
menu 220                                               # Services… again: a renamed dev container is refused
k ctrl+Home; k Down; k End; k Return
xdotool type --delay 10 '    container_name: other'
k Tab; k Tab; k Return; sleep 3                        # Save
shot 3-refused
check "a renamed dev container is refused (the file stays)" cmp -s $CF $OUT/saved.yaml
k Escape; sleep 1
k ctrl+alt+1; k ctrl+shift+3                           # Remote: build, compose up, connect
for i in $(seq 300); do [ "$($ENGINE ps --filter name=^$P\$ --format '{{.State}}' 2>/dev/null)" = running ] && break; sleep 2; done
sleep 10
shot 4-connected
t 'echo "$REDIS_URL" > env.txt; getent hosts redis >/dev/null && echo found >> env.txt'
waitfile $PROJ/env.txt 10
expect "inside: the variable from the file, redis by name" "redis://redis:6379|found|" "$(tr '\n' '|' < $PROJ/env.txt)"
expect "the Compose project: dev and redis run" "$P $P-redis-1 " "$(names)"
DEV1=$($ENGINE inspect -f '{{.Id}}' $P)
menu 220                                               # Services…: add a second service, Save and restart
k ctrl+End
lines '  cache2:' '    image: docker.io/library/redis:7' '    volumes: [cachedata:/data]' 'volumes:' '  cachedata:'
k Tab; k Tab; k Tab; k Return; sleep 2                 # Save and restart services: asks (it runs)
k Return                                               # Restart services
for i in $(seq 120); do names | grep -q cache2 && break; sleep 1; done; sleep 2
shot 5-restarted
expect "Save and restart started the new service" "$P $P-cache2-1 $P-redis-1 " "$(names)"
check "the new service's data volume" bash -c "$ENGINE volume ls --format '{{.Name}}' | grep -qx ${P}_cachedata"
check "the dev container was not made again" test "$($ENGINE inspect -f '{{.Id}}' $P)" = "$DEV1"
k Escape; sleep 1
t 'echo still > still.txt'
waitfile $PROJ/still.txt 10
check "the Remote tab's shell is still the same" test -s $PROJ/still.txt
menu 250                                               # Stop container…
k Return; sleep 15
expect "Stop container stopped dev and its services" "" "$(names)"
k Return                                               # Reconnect (Enter): starts all again
for i in $(seq 90); do [ "$(names)" = "$P $P-cache2-1 $P-redis-1 " ] && break; sleep 1; done; sleep 8
expect "Reconnect started dev and its services" "$P $P-cache2-1 $P-redis-1 " "$(names)"
t 'echo again > again.txt'
waitfile $PROJ/again.txt 10
check "and the Remote tab works again" test -s $PROJ/again.txt
menu 637                                               # Delete session… (after Deactivate)
shot 6-delete
k Tab; k space; k Tab; k space; k Tab; k Return        # tick container+services, their data; Delete
for i in $(seq 60); do grep -q $ID $CFG || break; sleep 1; done; sleep 1
shot 7-deleted
expect "no container of the project is left" "" "$(names -a)"
check "the dev container is gone" bash -c "! $ENGINE ps -a --format '{{.Names}}' | grep -qx $P"
check "the project's network is gone" test -z "$($ENGINE network ls -q --filter label=com.docker.compose.project=$P)"
check "the services' data volume is gone" bash -c "! $ENGINE volume ls --format '{{.Name}}' | grep -qx ${P}_cachedata"
check "its own login volume is gone" bash -c "! $ENGINE volume ls --format '{{.Name}}' | grep -qx sw-claude-$ID"
check "the app's files (with compose.yaml) are gone" test ! -e $XDG_DATA_HOME/session-workbench/containers/$ID
check "the session is gone" bash -c "! grep -q $ID $CFG"
alive
kill $APP; wait $APP 2>/dev/null
finish
