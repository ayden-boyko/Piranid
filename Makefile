# Piranid build and verification tasks.
#
# The root Makefile was previously empty, so nothing here had a canonical
# command; each module had to be built by hand.

SHELL := /bin/bash
.DEFAULT_GOAL := help

GO      ?= go
MODULES := nodes/Auth nodes/Event_Queue nodes/Notifications pkg
KEYDIR  ?= jwt-keys

.PHONY: help
help: ## Show this help
	@echo "Piranid targets:"
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build every module
	@for m in $(MODULES); do \
		echo "==> building $$m"; \
		$(GO) build ./$$m/... || exit 1; \
	done

.PHONY: vet
vet: ## Run go vet on every module
	@for m in $(MODULES); do \
		echo "==> vetting $$m"; \
		$(GO) vet ./$$m/... || exit 1; \
	done

.PHONY: fmt
fmt: ## Format changed Go files
	@for m in $(MODULES); do $(GO) fmt ./$$m/... ; done

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is unformatted
	@out=$$(gofmt -l $(MODULES)); \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi; \
	echo "all files formatted"

.PHONY: test
test: ## Run every test
	@for m in $(MODULES); do \
		echo "==> testing $$m"; \
		$(GO) test ./$$m/... -count=1 || exit 1; \
	done

.PHONY: test-race
test-race: ## Run every test under the race detector
	@for m in $(MODULES); do \
		echo "==> testing $$m (race)"; \
		$(GO) test ./$$m/... -count=1 -race || exit 1; \
	done

.PHONY: check
check: fmt-check vet test ## Everything CI should run

.PHONY: validate-manifests
validate-manifests: ## Validate the Kubernetes manifests
	@python3 scripts/validate-manifests.py

.PHONY: check-all
check-all: check validate-manifests ## Full local gate, including manifests

.PHONY: images
images: ## Build all three service images
	docker build -f nodes/Auth/AUTH.Dockerfile            -t piranid-auth:latest .
	docker build -f nodes/Event_Queue/EVENT.Dockerfile    -t piranid-event:latest .
	docker build -f nodes/Notifications/NOTIF.Dockerfile -t piranid-notifications:latest .

.PHONY: keys
keys: ## Generate an RSA signing key pair into $(KEYDIR)
	@cd nodes/Auth && $(GO) run ./cmd/genkey -out ../../$(KEYDIR) -bits 2048

.PHONY: tidy
tidy: ## Tidy every module's go.mod
	@for m in $(MODULES); do \
		echo "==> tidying $$m"; \
		(cd $$m && $(GO) mod tidy) || exit 1; \
	done

.PHONY: clean
clean: ## Remove build artifacts
	$(GO) clean -cache -testcache
	rm -f auth.db nodes/Auth/auth.db