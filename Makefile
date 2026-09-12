# The gate, and the loop that compares this implementation with the reference.
#
# Every path the port needs lives here once. GO.md says what the gate enforces.
# PORTING.md says what the comparison means and records how it was won.

BIN     ?= ./otransit
PKG     ?= .
CONF    ?= conformance
REF     ?= ../oc/target/release/otransit
WORK    ?= /tmp/otransit-conformance

# Which fixture, and which observation. Empty FX means all fourteen worlds.
# OBS is blank for text, or `semantic`, or `styles`.
FX  ?=
OBS ?=

FIXTURE := $(CONF)$(if $(FX),/$(FX))
LABEL   := $(if $(FX),$(FX),all fixtures) [$(if $(OBS),$(OBS),text)]

.DEFAULT_GOAL := help
.PHONY: help check fmt vet lint build test diff reference tools clean

help:
	@echo 'make check                        the gate: what CI runs, and all it runs'
	@echo 'make fmt                          rewrite what gofmt would change'
	@echo 'make build                        build $(BIN)'
	@echo 'make test                         go test -race ./...'
	@echo ''
	@echo 'make diff                         compare every fixture, as text'
	@echo 'make diff FX=empty                compare one fixture'
	@echo 'make diff FX=empty OBS=semantic   add the decisions to the text'
	@echo 'make diff FX=empty OBS=styles     add the palette to the text'
	@echo ''
	@echo 'make reference                    rebuild the reference binary'
	@echo 'make tools                        report which gate tools are missing'
	@echo ''
	@echo 'Take the passes in order: text, then semantic, then styles.'
	@echo 'A flag adds to the text output. It does not replace it.'

# ---- the gate ------------------------------------------------------------
# The one command. CI runs this and nothing else, so local and CI cannot drift:
# a job that restated these steps would be a second gate to keep in step.
#
# GO.md is the authority on what belongs here. The order is what fails fastest
# first: formatting costs nothing, a build error is the commonest mistake and the
# cheapest to find, and the two test passes are last because they are the slowest.
#
# `GOOS=linux go vet` is the fifth step for a reason this repository learned the
# hard way. This program is developed on darwin and built on linux, the only
# place that difference lives is internal/term, and a termios constant that
# exists on one unix and not the other compiles here and fails there. `go vet`
# reads the test files too, which is where it happened.

check: tools
	@echo '==> Formatting'
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "$$out"; exit 1; fi
	@out=$$(goimports -l .); if [ -n "$$out" ]; then echo "$$out"; exit 1; fi
	@echo '==> Build'
	go build ./...
	@echo '==> Static analysis'
	go vet ./...
	@echo '==> Static analysis, for the machine CI runs on'
	GOOS=linux go vet ./...
	@echo '==> Linting'
	golangci-lint run
	@echo '==> Tests'
	go test ./...
	@echo '==> Tests, with the race detector'
	go test -race ./...
	@echo 'gate clean'

tools:
	@missing=; \
	for t in go gofmt goimports golangci-lint; do \
	  command -v $$t >/dev/null 2>&1 || missing="$$missing $$t"; \
	done; \
	if [ -n "$$missing" ]; then \
	  echo "not on PATH:$$missing"; \
	  echo 'mise.toml pins every one of these.'; \
	  echo 'run: mise install'; \
	  exit 1; \
	fi

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run

build:
	go build -o $(BIN) $(PKG)

test:
	go test -race ./...

# ---- the comparison ------------------------------------------------------

# The reference is built from its own checkout. This is the only target that
# reaches outside this repository, and it only ever reads or builds there.
reference:
	cd ../oc && cargo build --release

diff: build
	@test -x $(REF) || { echo 'no reference binary at $(REF).'; \
	  echo 'run: make reference'; exit 1; }
	@test -e $(FIXTURE) || { echo 'no fixture at $(FIXTURE).'; \
	  echo 'conformance/ is a symlink to the reference checkout.'; exit 1; }
	@mkdir -p $(WORK)
	@$(REF) replay $(FIXTURE) $(OBS) > $(WORK)/ref.txt
	@$(BIN) replay $(FIXTURE) $(OBS) > $(WORK)/go.txt
	@if diff -u $(WORK)/ref.txt $(WORK)/go.txt; then \
	  echo 'identical: $(LABEL)'; \
	else \
	  echo ''; \
	  echo 'the first hunk above is the work item. fix it and run again.'; \
	  exit 1; \
	fi

clean:
	rm -f $(BIN)
	rm -rf $(WORK)
