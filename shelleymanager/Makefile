SHELLEY_BINARY   ?= $(abspath ../shelley/shelley)
STATE_DIR        ?= $(abspath .shelleymanager-state)
TOOLS_DIR        ?= $(abspath ../test/fixtures/local-tools)
LISTEN           ?= 127.0.0.1:31337
NAMESPACE        ?= acme
RUNTIME_MODE     ?= bwrap
PROFILE          ?= predictable

ifeq ($(PROFILE),predictable)
MODEL := predictable
PREDICTABLE_ONLY := 1
else ifeq ($(PROFILE),claude-sonnet-4.6)
MODEL := claude-sonnet-4.6
PREDICTABLE_ONLY := 0
else ifeq ($(PROFILE),claude-haiku-4.5)
MODEL := claude-haiku-4.5
PREDICTABLE_ONLY := 0
else
$(error Unsupported PROFILE "$(PROFILE)"; use PROFILE=predictable, PROFILE=claude-sonnet-4.6, or PROFILE=claude-haiku-4.5)
endif

ifneq ($(filter 1 true yes,$(PREDICTABLE_ONLY)),)
PREDICTABLE_FLAG := --predictable-only
endif

.PHONY: build run dev dev-predictable dev-claude dev-haiku web web-build clean help

help: ## Show this help
	@grep -E '^[a-z][a-z_-]+:.*##' $(MAKEFILE_LIST) | sort | awk -F ':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

build: ## Build the manager binary
	go build -o bin/shelleymanager ./cmd/shelleymanager

build-shelley: ## Build the shelley runtime binary
	cd ../shelley && go build -o shelley ./cmd/shelley

run: build-shelley build ## Build and run the manager (defaults to PROFILE=predictable)
ifneq (,$(filter $(PROFILE),claude-sonnet-4.6 claude-haiku-4.5))
	@test -n "$$ANTHROPIC_API_KEY" || (echo "ANTHROPIC_API_KEY is required for PROFILE=$(PROFILE)" >&2; exit 1)
endif
	./bin/shelleymanager \
		--shelley-binary $(SHELLEY_BINARY) \
		--state-dir $(STATE_DIR) \
		--namespace $(NAMESPACE) \
		--runtime-mode $(RUNTIME_MODE) \
		--tools-dir $(TOOLS_DIR) \
		--default-model $(MODEL) \
		--listen $(LISTEN) \
		$(PREDICTABLE_FLAG) \
		--debug

dev: build-shelley ## Run manager from source with go run (defaults to PROFILE=predictable)
ifneq (,$(filter $(PROFILE),claude-sonnet-4.6 claude-haiku-4.5))
	@test -n "$$ANTHROPIC_API_KEY" || (echo "ANTHROPIC_API_KEY is required for PROFILE=$(PROFILE)" >&2; exit 1)
endif
	go run ./cmd/shelleymanager \
		--shelley-binary $(SHELLEY_BINARY) \
		--state-dir $(STATE_DIR) \
		--namespace $(NAMESPACE) \
		--runtime-mode $(RUNTIME_MODE) \
		--tools-dir $(TOOLS_DIR) \
		--default-model $(MODEL) \
		--listen $(LISTEN) \
		$(PREDICTABLE_FLAG) \
		--debug

dev-predictable: ## Run the manager with the predictable fixture model
	$(MAKE) dev PROFILE=predictable

dev-claude: ## Run the manager with Claude Sonnet 4.6 (requires ANTHROPIC_API_KEY)
	$(MAKE) dev PROFILE=claude-sonnet-4.6

dev-haiku: ## Run the manager with Claude Haiku 4.5 (requires ANTHROPIC_API_KEY)
	$(MAKE) dev PROFILE=claude-haiku-4.5

web: ## Start Vite dev server (port 5173, proxies to 31337)
	cd web && bun run dev

web-build: ## Production build of web UI
	cd web && bun run build

clean: ## Remove build artifacts and state
	rm -rf bin/
