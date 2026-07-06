LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

BIN ?= $(LOCALBIN)/crd2proto

# Colors for terminal output
BLUE := \033[34m
GREEN := \033[32m
YELLOW := \033[33m
RESET := \033[0m

GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.11.4
GOIMPORTS ?= $(LOCALBIN)/goimports
GOIMPORTS_VERSION ?= latest

.PHONY: build
build: $(LOCALBIN)
	go build -o $(BIN) ./cmd/crd2proto

.PHONY: install
install:
	go install ./cmd/crd2proto

##@ Setup & Installation

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint binary locally if neccessary
$(GOLANGCI_LINT): $(LOCALBIN)
	test -s $(GOLANGCI_LINT) $(GOLANGCI_LINT) --version 2>/dev/null | grep -q $(GOLANGCI_LINT_VERSION) || \
	GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \

lint: golangci-lint ## Run golangci-lint
	@echo "$(BLUE)Running golangci-lint...$(RESET)"
	@$(GOLANGCI_LINT) run ./...

# protoc_check is a function: $(call protoc_check, <submodule-dir>, <proto1 proto2 ...>, <extras>)
define protoc_check
	set -e; \
	cd $(1); \
	API_DIR=$$(go list -m -f '{{.Dir}}' k8s.io/api 2>/dev/null) ; \
	APIM_DIR=$$(go list -m -f '{{.Dir}}' k8s.io/apimachinery) ; \
	APIEXT_DIR=$$(go list -m -f '{{.Dir}}' k8s.io/apiextensions-apiserver 2>/dev/null) ; \
	cd - >/dev/null ; \
	PROTO_ROOT=$$(mktemp -d) ; \
	trap "rm -rf $$PROTO_ROOT" EXIT ; \
	mkdir -p "$$PROTO_ROOT/k8s.io" ; \
	[ -n "$$API_DIR"  ] && ln -s "$$API_DIR"  "$$PROTO_ROOT/k8s.io/api" ; \
	[ -n "$$APIEXT_DIR" ] && ln -s "$$APIEXT_DIR" "$$PROTO_ROOT/k8s.io/apiextensions-apiserver" ; \
	ln -s "$$APIM_DIR" "$$PROTO_ROOT/k8s.io/apimachinery" ; \
	for spec in $(3); do \
		src=$${spec%%=*}; imp=$${spec#*=}; \
		mkdir -p "$$PROTO_ROOT/$$(dirname $$imp)" ; \
		ln -s "$(CURDIR)/$$src" "$$PROTO_ROOT/$$imp" ; \
	done ; \
	for f in $(2); do \
		echo "  protoc-check $(1)/$$f" ; \
		( cd $(1) && protoc -I . -I "$$PROTO_ROOT" "$$f" -o /dev/null ) ; \
	done
endef

# Run against the bundled guestbook example.
.PHONY: run-guestbook
run-guestbook: build
	cd examples/guestbook/guestbook-crd && $(BIN) generate my.domain/guestbook/api/v1
	@$(call protoc_check, examples/guestbook/guestbook-crd, api/v1/generated.proto)

.PHONY: run-gateway
run-gateway: build
	cd testdata/crds/gateway-api && $(BIN) generate sigs.k8s.io/gateway-api/apis/v1
	@$(call protoc_check, testdata/crds/gateway-api, apis/v1/generated.proto)

.PHONY: run-cert-manager
run-cert-manager: build
	cd testdata/crds/cert-manager && $(BIN) generate \
		github.com/cert-manager/cert-manager/pkg/apis/meta/v1,github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1,github.com/cert-manager/cert-manager/pkg/apis/acme/v1
	@$(call protoc_check, testdata/crds/cert-manager, pkg/apis/meta/v1/generated.proto pkg/apis/certmanager/v1/generated.proto pkg/apis/acme/v1/generated.proto, \
		testdata/crds/cert-manager=github.com/cert-manager/cert-manager)

.PHONY: run-cdi
run-cdi: build
	cd testdata/crds/cdi-api && $(BIN) generate kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1

.PHONY: run-kubevirt
run-kubevirt: build
	cd testdata/crds/kubevirt-api && $(BIN) generate kubevirt.io/api/core/v1
	@$(call protoc_check, testdata/crds/kubevirt-api, core/v1/generated.proto)

.PHONY: python-grpc-compile
python-grpc-compile: run-kubevirt
	./examples/python-grpc/compile.sh

.PHONY: python-grpc-server
python-grpc-server: python-grpc-compile
	cd examples/python-grpc && python server.py
