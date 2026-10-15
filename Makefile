# Run `make check` to see what is missing, `make deps` to install it.
WAILS ?= $(shell command -v wails || echo $$(go env GOPATH 2>/dev/null)/bin/wails)

# Ubuntu 24.04+ / Debian 13+ only ship webkit2gtk-4.1; older systems use 4.0.
ifeq ($(shell uname -s),Linux)
TAGS ?= $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo webkit2_41)
endif

# Version stamped into the binary (shown in Settings and by -version).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -X main.version=$(VERSION)

.PHONY: check deps dev build build-windows packages run test test-ssh vet clean smoke smoke-sessions smoke-files smoke-ui smoke-all

check:
	@scripts/deps.sh check

deps:
	@scripts/deps.sh install

dev: check
	$(WAILS) dev -tags "$(TAGS)"

build: check
	$(WAILS) build -tags "$(TAGS)" -ldflags "$(LDFLAGS)"

# Cross-build the Windows exe from Linux (no cgo needed); macOS builds need a Mac.
build-windows: check
	$(WAILS) build -platform windows/amd64 -ldflags "$(LDFLAGS)" -o session-workbench.exe

# Linux .deb and .rpm from build/bin/session-workbench (CI does the same).
NFPM ?= $(shell command -v nfpm || echo $$(go env GOPATH)/bin/nfpm)
packages: build
	@test -x "$(NFPM)" || go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.48.0
	mkdir -p dist
	VERSION=$$(echo "$(VERSION)" | sed -E 's/^v//; s/-([0-9]+)-g([0-9a-f]+)/+\1.g\2/') \
		&& export VERSION && $(NFPM) package -f packaging/nfpm.yaml -p deb -t dist/ \
		&& $(NFPM) package -f packaging/nfpm.yaml -p rpm -t dist/

run: build
	build/bin/session-workbench

test:
	go test -race ./...
	cd frontend && npm test --silent

# Round trip against a real agent host (needs key auth and tmux there, and
# the same /tmp path visible on both sides, e.g. localhost).
SSH_TARGET ?= localhost
test-ssh:
	SW_SSH_TARGET=$(SSH_TARGET) go test -count=1 -run SSH -v ./internal/remote

# The main package needs cgo (webkit) on Linux and macOS: vet it natively and
# for Windows (pure Go); macOS only the internal packages.
vet:
	go vet -tags "$(TAGS)" ./...
	GOOS=windows go vet ./...
	GOOS=darwin go vet ./internal/...

clean:
	rm -rf build/bin frontend/dist/assets

# Headless GUI tests (Linux; need xvfb, xdotool, imagemagick). Screenshots
# land in $(SMOKE_OUT)-*. They use throwaway configs, never yours.
# smoke: terminal fidelity in a Local tab.
# smoke-sessions: sidebar, dialogs, switching, menus, restart; needs
#   `ssh localhost` without prompts and tmux.
# smoke-files: Files tab (tree, keyboard, open/refuse, copy paths) with a
#   fake xdg-open.
# smoke-ui: colors/themes, quick switcher, settings + keys, reconnect overlay,
#   find/unicode/copy-on-select, folders, git markers, activity badges (the
#   last takes ~2 minutes).
SMOKE_OUT ?= /tmp/sw-smoke
XVFB = xvfb-run -a -s "-screen 0 1400x900x24"
smoke: build
	$(XVFB) scripts/gui-smoke.sh build/bin/session-workbench $(SMOKE_OUT)-terminal

smoke-sessions: build
	$(XVFB) scripts/gui-smoke-sessions.sh build/bin/session-workbench $(SMOKE_OUT)-sessions
	$(XVFB) scripts/gui-smoke-hosts.sh build/bin/session-workbench $(SMOKE_OUT)-hosts

smoke-files: build
	$(XVFB) scripts/gui-smoke-files.sh build/bin/session-workbench $(SMOKE_OUT)-files

smoke-ui: build
	$(XVFB) scripts/gui-smoke-colors.sh build/bin/session-workbench $(SMOKE_OUT)-colors
	$(XVFB) scripts/gui-smoke-switcher.sh build/bin/session-workbench $(SMOKE_OUT)-switcher
	$(XVFB) scripts/gui-smoke-settings.sh build/bin/session-workbench $(SMOKE_OUT)-settings
	$(XVFB) scripts/gui-smoke-reconnect.sh build/bin/session-workbench $(SMOKE_OUT)-reconnect
	$(XVFB) scripts/gui-smoke-terminal-extras.sh build/bin/session-workbench $(SMOKE_OUT)-extras
	$(XVFB) scripts/gui-smoke-folders.sh build/bin/session-workbench $(SMOKE_OUT)-folders
	$(XVFB) scripts/gui-smoke-git.sh build/bin/session-workbench $(SMOKE_OUT)-git
	$(XVFB) scripts/gui-smoke-badges.sh build/bin/session-workbench $(SMOKE_OUT)-badges

smoke-all: smoke smoke-sessions smoke-files smoke-ui
