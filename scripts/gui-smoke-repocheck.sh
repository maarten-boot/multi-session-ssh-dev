#!/bin/bash
# Headless GUI test of the repository check: planted hook and config are
# flagged (sidebar + Files banner + details), "Mark as reviewed" silences
# them, and a later change brings the warning back. Nothing planted runs.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-repocheck.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
. "$(dirname "$0")/smoke-lib.sh"
B=$OUT/bad; C=$OUT/clean; rm -rf $B $C $OUT/HOOK-RAN; mkdir -p $B $C
for r in $B $C; do git -C $r init -q -b main; echo x >$r/a.txt; git -C $r add -A; git -C $r -c user.name=t -c user.email=t@example.com commit -q -m base; done
printf '#!/bin/sh\ntouch %s\n' "$OUT/HOOK-RAN" >$B/.git/hooks/pre-push; chmod +x $B/.git/hooks/pre-push
git -C $B config credential.helper '!f() { cat >/dev/null; }; f'
git -C $B config remote.origin.pushurl 'https://me:secret-token@evil.example/repo.git'
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-rcb"
name = "planted"
host = "local"
local_path = "$B"
start_mode = "shell"
[[sessions]]
id = "s-rcc"
name = "clean"
host = "local"
local_path = "$C"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.5; }
shot() { import -window root $OUT/$1.png; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
wait_window
sleep 4; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+alt+1; k ctrl+shift+2; sleep 1.5
shot 1-warned                                   # sidebar ⚠ on "planted" only, banner in Files
k shift+Tab; k Return; sleep 1                  # banner: Details…
shot 2-details
k Return; sleep 1.5                             # Mark as reviewed
shot 3-reviewed                                 # warning gone
printf '#!/bin/sh\ntouch %s\necho changed\n' "$OUT/HOOK-RAN" >$B/.git/hooks/pre-push   # the agent changes it
k ctrl+alt+2; sleep 1; k ctrl+alt+1; sleep 2
shot 4-changed-again                            # warning back
alive
kill $APP; wait $APP 2>/dev/null
expect "\"Mark as reviewed\" saved for the planted repository only" 1 "$(grep -c trusted_git $CFG)"
check "the planted hook did not run" test ! -e $OUT/HOOK-RAN
for id in s-rcb s-rcc; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
finish
