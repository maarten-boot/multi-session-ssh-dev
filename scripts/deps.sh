#!/bin/bash
# Check (default) or install the build requirements.
#   scripts/deps.sh check    report what is missing, exit 1 if anything is
#   scripts/deps.sh install  install what is missing (system packages via sudo)
set -u
MODE=${1:-check}
WAILS_VERSION=v2.16.0
GO_MIN=1.25
NODE_MIN=18.0 # vite 6 (Ubuntu 24.04 ships 18.19)

missing=()   # human-readable list
pkgs=()      # system packages to install
need_wails=0

ok()   { printf '  \033[32mok\033[0m       %s\n' "$*"; }
miss() { printf '  \033[31mmissing\033[0m  %s\n' "$*"; missing+=("$1"); }

# version_ge 1.26.0 1.25 -> true
version_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -1)" = "$2" ]; }

OS=$(uname -s)
PM=
if [ "$OS" = Linux ]; then
	if command -v apt-get >/dev/null; then PM=apt
	elif command -v dnf >/dev/null; then PM=dnf
	elif command -v pacman >/dev/null; then PM=pacman
	fi
fi

# pkgname <apt> <dnf> <pacman>
pkgname() {
	case $PM in
	apt) echo "$1" ;; dnf) echo "$2" ;; pacman) echo "$3" ;;
	esac
}

echo "Checking build requirements ($OS${PM:+, $PM}):"

if command -v go >/dev/null; then
	v=$(go env GOVERSION | sed 's/^go//')
	if version_ge "$v" "$GO_MIN"; then ok "go $v"; else miss "go >= $GO_MIN (found $v)"; pkgs+=("$(pkgname golang-go golang go)"); fi
else
	miss "go >= $GO_MIN"; pkgs+=("$(pkgname golang-go golang go)")
fi

if command -v node >/dev/null && command -v npm >/dev/null; then
	v=$(node --version | sed 's/^v//')
	if version_ge "$v" "$NODE_MIN"; then ok "node $v / npm $(npm --version)"; else miss "node >= $NODE_MIN (found $v)"; fi
else
	miss "node >= $NODE_MIN and npm"; pkgs+=("$(pkgname npm npm npm)")
fi

if [ "$OS" = Linux ]; then
	if command -v gcc >/dev/null; then ok "gcc"; else miss "C compiler (gcc)"; pkgs+=("$(pkgname build-essential gcc-c++ base-devel)"); fi
	if command -v pkg-config >/dev/null; then
		ok "pkg-config"
		if pkg-config --exists gtk+-3.0; then ok "gtk+-3.0 dev"; else miss "GTK 3 development files"; pkgs+=("$(pkgname libgtk-3-dev gtk3-devel gtk3)"); fi
		if pkg-config --exists webkit2gtk-4.1 || pkg-config --exists webkit2gtk-4.0; then
			ok "webkit2gtk dev ($(pkg-config --exists webkit2gtk-4.1 && echo 4.1 || echo 4.0))"
		else
			miss "WebKitGTK development files"; pkgs+=("$(pkgname libwebkit2gtk-4.1-dev webkit2gtk4.1-devel webkit2gtk-4.1)")
		fi
	else
		miss "pkg-config"; pkgs+=("$(pkgname pkg-config pkgconf-pkg-config pkgconf)")
		miss "GTK 3 / WebKitGTK development files (cannot check without pkg-config)"
		pkgs+=("$(pkgname libgtk-3-dev gtk3-devel gtk3)" "$(pkgname libwebkit2gtk-4.1-dev webkit2gtk4.1-devel webkit2gtk-4.1)")
	fi
elif [ "$OS" = Darwin ]; then
	if xcode-select -p >/dev/null 2>&1; then ok "Xcode command line tools"; else miss "Xcode command line tools (run: xcode-select --install)"; fi
fi

WAILS=$(command -v wails || true)
if [ -z "$WAILS" ] && command -v go >/dev/null && [ -x "$(go env GOPATH)/bin/wails" ]; then
	WAILS=$(go env GOPATH)/bin/wails
fi
if [ -n "$WAILS" ]; then
	ok "wails $("$WAILS" version 2>/dev/null | head -1) ($WAILS)"
else
	miss "wails CLI $WAILS_VERSION"; need_wails=1
fi

if [ ${#missing[@]} -eq 0 ]; then
	echo "All requirements present."
	exit 0
fi

# Drop empty names (unknown package manager).
clean=(); for p in "${pkgs[@]}"; do [ -n "$p" ] && clean+=("$p"); done; pkgs=("${clean[@]+"${clean[@]}"}")

install_cmd=
case $PM in
apt) install_cmd="sudo apt-get install -y ${pkgs[*]+${pkgs[*]}}" ;;
dnf) install_cmd="sudo dnf install -y ${pkgs[*]+${pkgs[*]}}" ;;
pacman) install_cmd="sudo pacman -S --needed ${pkgs[*]+${pkgs[*]}}" ;;
esac

if [ "$MODE" != install ]; then
	echo
	echo "Missing requirements. Run 'make deps' to install them, or manually:"
	[ ${#pkgs[@]} -gt 0 ] && echo "  ${install_cmd:-install: ${pkgs[*]}}"
	[ $need_wails = 1 ] && echo "  go install github.com/wailsapp/wails/v2/cmd/wails@$WAILS_VERSION"
	exit 1
fi

set -e
if [ ${#pkgs[@]} -gt 0 ]; then
	if [ -z "$install_cmd" ]; then
		echo "Unknown package manager; please install: ${pkgs[*]}" >&2
		exit 1
	fi
	echo "+ $install_cmd"
	[ $PM = apt ] && sudo apt-get update
	$install_cmd
fi
if [ $need_wails = 1 ]; then
	echo "+ go install github.com/wailsapp/wails/v2/cmd/wails@$WAILS_VERSION"
	go install github.com/wailsapp/wails/v2/cmd/wails@$WAILS_VERSION
fi
exec "$0" check
