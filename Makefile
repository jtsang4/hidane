# hidane — Go + Wails v3 desktop app with a Svelte frontend.
# pnpm is the only JavaScript package manager used here.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/jtsang4/hidane/internal/app.Version=$(VERSION)
BIN := bin

ifeq ($(shell uname -s),Darwin)
export CGO_CFLAGS := -mmacosx-version-min=12.0
export CGO_LDFLAGS := -mmacosx-version-min=12.0
endif

.PHONY: all frontend build build-nogui fakeagent test test-go test-frontend e2e screenshots smoke-gui smoke-live acceptance app clean

all: build

frontend:
	pnpm install --frozen-lockfile
	pnpm -C frontend build

# Desktop app binary (also every CLI subcommand).
build: frontend
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN)/hidane .

# No cgo, no Wails: CI, E2E and headless servers.
build-nogui: frontend
	CGO_ENABLED=0 go build -tags nogui -trimpath -ldflags="$(LDFLAGS)" -o $(BIN)/hidane-nogui .

# Protocol-faithful stand-ins for claude / codex / pi, for E2E.
fakeagent:
	go build -o $(BIN)/fake/fakeagent ./cmd/fakeagent
	cd $(BIN)/fake && for n in claude codex pi; do ln -sf fakeagent $$n; done

test: test-go test-frontend

test-go:
	go vet ./...
	go vet -tags nogui ./...
	go test -race ./...

test-frontend:
	pnpm -C frontend check
	pnpm -C frontend test

# Playwright against the real Go backend (`hidane serve`) with fake agent CLIs.
e2e: build-nogui fakeagent
	pnpm -C frontend e2e

# Every page in zh/en at desktop and phone width → bin/screenshots/, so a UI
# change can be looked at rather than inferred from the diff. ONLY=run-as,focus
# takes only the pages and states whose names start with those prefixes.
screenshots: build-nogui fakeagent
	ONLY="$(ONLY)" node frontend/e2e/screenshots.mjs bin/screenshots

# The real Wails window loads the app and receives pushed frames, then quits.
smoke-gui: build
	HIDANE_HOME=$$(mktemp -d) HIDANE_GUI_SMOKE=1 $(BIN)/hidane

# One real round trip per role on the locally installed CLIs (spends tokens).
smoke-live: build-nogui
	$(BIN)/hidane-nogui agents
	$(BIN)/hidane-nogui model --ping

# Agent-driven acceptance of acceptance/scenarios.md (spends tokens). Scope it:
# make acceptance ARGS="--only 4F,6E" or ARGS="--changed origin/main".
acceptance:
	scripts/acceptance.sh $(ARGS)

# macOS .app bundle.
app: build
	rm -rf $(BIN)/Hidane.app
	mkdir -p $(BIN)/Hidane.app/Contents/MacOS $(BIN)/Hidane.app/Contents/Resources
	cp $(BIN)/hidane $(BIN)/Hidane.app/Contents/MacOS/hidane
	sed "s/@VERSION@/$(VERSION)/g" build/darwin/Info.plist > $(BIN)/Hidane.app/Contents/Info.plist
	cp build/darwin/icons.icns $(BIN)/Hidane.app/Contents/Resources/icons.icns

clean:
	rm -rf $(BIN) frontend/dist/assets frontend/dist/index.html
