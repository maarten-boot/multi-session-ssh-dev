# Run `make check` to see what is missing, `make deps` to install it.
WAILS ?= $(shell command -v wails || echo $$(go env GOPATH 2>/dev/null)/bin/wails)

# Ubuntu 24.04+ / Debian 13+ only ship webkit2gtk-4.1; older systems use 4.0.
ifeq ($(shell uname -s),Linux)
TAGS ?= $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo webkit2_41)
endif

.PHONY: check deps dev build test vet clean smoke

check:
	@scripts/deps.sh check

deps:
	@scripts/deps.sh install

dev: check
	$(WAILS) dev -tags "$(TAGS)"

build: check
	$(WAILS) build -tags "$(TAGS)"

test:
	go test -race ./internal/...

vet:
	go vet ./internal/...
	GOOS=windows go vet ./internal/...
	GOOS=darwin go vet ./internal/...

clean:
	rm -rf build/bin frontend/dist/assets

# Headless GUI smoke test (Linux); screenshots land in $(SMOKE_OUT).
SMOKE_OUT ?= /tmp/sw-smoke
smoke: build
	xvfb-run -a -s "-screen 0 1280x860x24" scripts/gui-smoke.sh build/bin/session-workbench $(SMOKE_OUT)
