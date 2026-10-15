# Run `make check` to see what is missing, `make deps` to install it.
WAILS ?= $(shell command -v wails || echo $$(go env GOPATH 2>/dev/null)/bin/wails)

# Ubuntu 24.04+ / Debian 13+ only ship webkit2gtk-4.1; older systems use 4.0.
ifeq ($(shell uname -s),Linux)
TAGS ?= $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo webkit2_41)
endif

# Version stamped into the binary (shown in Settings and by -version).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -X main.version=$(VERSION)

.PHONY: check deps dev build build-windows run test test-ssh vet clean smoke smoke-sessions smoke-files smoke-ui smoke-all

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

vet:
	go vet ./internal/...
	GOOS=windows go vet ./internal/...
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
#   activity badges (the last takes ~2 minutes).
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
	$(XVFB) scripts/gui-smoke-badges.sh build/bin/session-workbench $(SMOKE_OUT)-badges

smoke-all: smoke smoke-sessions smoke-files smoke-ui
