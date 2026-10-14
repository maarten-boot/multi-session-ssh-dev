WAILS ?= $(shell command -v wails || echo $(HOME)/go/bin/wails)

# Ubuntu 24.04+ / Debian 13+ only ship webkit2gtk-4.1.
ifeq ($(shell uname -s),Linux)
TAGS ?= webkit2_41
endif

.PHONY: dev build test vet clean smoke

dev:
	$(WAILS) dev -tags "$(TAGS)"

build:
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
