# Test targets. `test` runs the fast unit suite; `test-envtest` runs the build-tagged
# functional cache-staleness tests against a real kube-apiserver via controller-runtime/envtest.

SHELL := /usr/bin/env bash

LOCALBIN ?= $(CURDIR)/bin
SETUP_ENVTEST ?= $(LOCALBIN)/setup-envtest
# Kubebuilder envtest control-plane (kube-apiserver + etcd) version used by the `-tags envtest` tests.
ENVTEST_K8S_VERSION ?= 1.36.0

.PHONY: test
test: ## Run the unit tests (fast; excludes the envtest-tagged functional tests).
	go test ./... -count=1

.PHONY: test-race
test-race: ## Run the unit tests with the race detector.
	go test ./... -race -count=1

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: setup-envtest
setup-envtest: $(SETUP_ENVTEST) ## Install setup-envtest into ./bin.
$(SETUP_ENVTEST): | $(LOCALBIN)
	# Pin to the controller-runtime 0.22 line (matches sigs.k8s.io/controller-runtime v0.22.3 in
	# go.mod). Do NOT use @latest: newer setup-envtest (v0.24+) requires go >= 1.26 and fails under
	# this module's go 1.25 toolchain (GOTOOLCHAIN=local) with "requires go >= 1.26.0".
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.22

.PHONY: test-envtest
test-envtest: setup-envtest ## Run the envtest (real-apiserver) functional cache-staleness tests.
	KUBEBUILDER_ASSETS="$$($(SETUP_ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		go test -tags envtest ./... -count=1

.PHONY: help
help: ## Show this help.
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n",$$1,$$2}'
