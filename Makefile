# yogilib-web Makefile
#
# Phase 6 — single binary deployment.
# `make build` produces ./yogilib with the SvelteKit SPA embedded.

SHELL := /bin/bash
SVELTE_DIR := ../yogilib-sveltekit
WEBDIST   := webdist
BIN       := yogilib

.PHONY: all build spa go clean run dev embed-check

all: build

# Full build: compile the Svelte SPA, sync into webdist/, then go build.
build: spa go

# Compile the SvelteKit project and copy its output into webdist/ for embedding.
spa:
	@echo "==> building SvelteKit SPA"
	cd $(SVELTE_DIR) && npm run build
	@echo "==> syncing build → $(WEBDIST)/"
	rm -rf $(WEBDIST)
	mkdir -p $(WEBDIST)
	cp -r $(SVELTE_DIR)/build/. $(WEBDIST)/
	@test -f $(WEBDIST)/index.html || (echo "ERROR: $(WEBDIST)/index.html missing" && exit 1)
	@echo "==> SPA ready ($(WEBDIST)/index.html present)"

# Compile the Go binary with the embedded SPA.
go: embed-check
	@echo "==> go build"
	go build -o $(BIN) .
	@ls -lh $(BIN)

# Sanity: webdist/ must contain index.html before `go build` (//go:embed needs it).
embed-check:
	@test -f $(WEBDIST)/index.html || (echo "ERROR: run 'make spa' first — $(WEBDIST)/index.html missing" && exit 1)

# Run the freshly built binary on :8080.
run: build
	./$(BIN)

# Quick dev rebuild without re-running npm (useful when only Go changed).
dev: go
	./$(BIN)

clean:
	rm -rf $(WEBDIST) $(BIN)
