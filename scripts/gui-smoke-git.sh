#!/bin/bash
# Headless GUI test of git markers in the Files tab, on a fixture repository
# with every kind of change.
# usage: xvfb-run -a -s "-screen 0 1400x900x24" scripts/gui-smoke-git.sh <binary> <outdir>
BIN=$1; OUT=$2; mkdir -p $OUT
R=$OUT/repo; rm -rf $R; mkdir -p $R
g() { git -C $R -c user.name=t -c user.email=t@example.com -c commit.gpgsign=false "$@" >/dev/null; }
g init -q -b main
mkdir -p $R/src/pkg $R/docs
echo one >$R/README.md; echo a >$R/src/main.go; echo b >$R/src/pkg/util.go; echo c >$R/docs/old.md
echo d >$R/src/remove-me.go; printf 'build/\n*.log\n' >$R/.gitignore
g add -A; g commit -q -m base; g branch base; g branch --set-upstream-to=base
echo two >>$R/README.md                                   # M
echo new >$R/src/added.go; g add src/added.go             # A
g mv docs/old.md docs/renamed.md                          # R
rm $R/src/remove-me.go                                    # D (not listed; marks src)
g commit -q -m more -- src/added.go                       # ahead 1 of upstream
echo staged-again >$R/src/new-staged.go; g add src/new-staged.go
echo u >$R/notes.txt; mkdir -p $R/scratch; echo x >$R/scratch/a.txt   # untracked
echo changed >>$R/src/pkg/util.go                         # M deep
mkdir -p $R/build; echo bin >$R/build/out.bin; echo log >$R/debug.log  # ignored
# A second repository whose own config names a filter program: status must be
# skipped (and the program must not run).
U=$OUT/unsafe; rm -rf $U $OUT/FILTER-RAN; mkdir -p $U
git -C $U init -q -b main; echo hi >$U/a.txt; echo '* filter=probe' >$U/.gitattributes
git -C $U -c user.name=t -c user.email=t@example.com add -A; git -C $U -c user.name=t -c user.email=t@example.com commit -q -m base
git -C $U config filter.probe.clean "sh -c 'touch $OUT/FILTER-RAN; cat'"; touch -d '+1 min' $U/a.txt
CFG=$OUT/config.toml
cat >$CFG <<TOML
[[hosts]]
name = "local"
ssh_target = "localhost"
[[sessions]]
id = "s-git"
name = "repo"
host = "local"
local_path = "$R"
start_mode = "shell"
[[sessions]]
id = "s-gitu"
name = "unsafe repo"
host = "local"
local_path = "$U"
start_mode = "shell"
TOML
k() { xdotool key "$@"; sleep 0.4; }
shot() { import -window root $OUT/$1.png; }
$BIN -config $CFG >>$OUT/app.log 2>&1 &
APP=$!
for i in $(seq 40); do W=$(xdotool search --name "Session Workbench" 2>/dev/null | head -1); [ -n "$W" ] && break; sleep 0.25; done
sleep 3; xdotool windowactivate --sync $W 2>/dev/null
k ctrl+shift+2; sleep 1.5
k Home; k Right; sleep 0.3                     # build/ (ignored, sorted first)
k Down; k Down; k Right; sleep 0.3             # docs/
k End; k Up; k Up; k Up; k Up                  # towards src/
shot 1-git
k ctrl+alt+2; k ctrl+shift+2; sleep 1.5
shot 2-unsafe-skipped
kill $APP; wait $APP 2>/dev/null
for id in s-git s-gitu; do ssh -o BatchMode=yes localhost "tmux kill-session -t '=cw-$id'" 2>/dev/null; done
[ -e $OUT/FILTER-RAN ] && echo "FAIL: the repository's filter program ran" || echo "ok: repository filter did not run"
exit 0
