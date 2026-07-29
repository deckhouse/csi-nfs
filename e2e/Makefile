# Makefile for csi-nfs e2e tests.

.PHONY: help
help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: deps
deps: ## Download dependencies
	go mod download
	go mod tidy
	@$(MAKE) fix-mod-permissions

.PHONY: test
test: ## Run the default suite (everything except the workload-nodes scenario)
	go test -v -count=1 -timeout 180m ./tests/ -ginkgo.timeout 180m \
		-ginkgo.label-filter '!workload-nodes'

.PHONY: test-all
test-all: ## Run every scenario, including workload-nodes (needs an NFSStorageClass-free cluster)
	go test -v -count=1 -timeout 180m ./tests/ -ginkgo.timeout 180m

.PHONY: test-tls
test-tls: ## Run only the RPC-with-TLS scenarios (tls + mtls)
	go test -v -count=1 -timeout 90m ./tests/ -ginkgo.timeout 90m \
		-ginkgo.label-filter 'lifecycle-tls || lifecycle-mtls'

.PHONY: test-mtls
test-scenario: ## Run one scenario by label (make test-scenario SCENARIO=lifecycle-v3)
	@if [ -z "$(SCENARIO)" ]; then \
		echo "Error: specify SCENARIO=<label>, e.g. lifecycle-v3 (see e2e/README.md)"; \
		exit 1; \
	fi
	go test -v -count=1 -timeout 120m ./tests/ -ginkgo.timeout 120m -ginkgo.label-filter '$(SCENARIO)'

.PHONY: test-workload-nodes
test-workload-nodes: ## Run only the workload-nodes scenario (needs an NFSStorageClass-free cluster)
	go test -v -count=1 -timeout 60m ./tests/ -ginkgo.timeout 60m -ginkgo.label-filter 'workload-nodes'

.PHONY: test-focus
test-focus: ## Run a focused Ginkgo spec (make test-focus FOCUS="v42 volume lifecycle")
	@if [ -z "$(FOCUS)" ]; then \
		echo "Error: specify FOCUS=<Ginkgo focus string>"; \
		exit 1; \
	fi
	go test -v -count=1 -timeout 120m ./tests/ -ginkgo.timeout 120m -ginkgo.focus "$(FOCUS)"

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: build
build: ## go build (compile-check only)
	go build ./...

.PHONY: lint
lint: ## golangci-lint (if installed)
	golangci-lint run ./...

.PHONY: check-env
check-env: ## Print relevant env vars
	@echo "=== Cluster (storage-e2e) ==="
	@echo "E2E_TEST_CLUSTER_PROVIDER:        $${E2E_TEST_CLUSTER_PROVIDER}  # dvp | commander (read by pkg/e2e Connect)"
	@echo "TEST_CLUSTER_CLEANUP:             $${TEST_CLUSTER_CLEANUP}      # set true to delete VMs after the run"
	@echo "TEST_CLUSTER_NAMESPACE:           $${TEST_CLUSTER_NAMESPACE}    # also the in-cluster namespace for PVCs/Pods"
	@echo "TEST_CLUSTER_STORAGE_CLASS:       $${TEST_CLUSTER_STORAGE_CLASS}"
	@echo "SSH_HOST:                         $${SSH_HOST}"
	@echo "SSH_USER:                         $${SSH_USER}"
	@echo "SSH_PRIVATE_KEY:                  $${SSH_PRIVATE_KEY}"
	@echo "SSH_PUBLIC_KEY:                   $${SSH_PUBLIC_KEY}  # required for alwaysCreateNew (VM authorized key)"
	@echo "SSH_VM_USER:                      $${SSH_VM_USER}  # required for alwaysCreateNew (SSH user inside the VMs)"
	@echo "DKP_LICENSE_KEY:                  $${DKP_LICENSE_KEY:+<set>}"
	@echo "REGISTRY_DOCKER_CFG:              $${REGISTRY_DOCKER_CFG:+<set>}"
	@echo "CSI_NFS_MODULE_PULL_OVERRIDE:     $${CSI_NFS_MODULE_PULL_OVERRIDE}  # overrides modulePullOverride (default main)"
	@echo "YAML_CONFIG_FILENAME:             $${YAML_CONFIG_FILENAME}  # default cluster_config.yml"
	@echo ""
	@echo "=== External NFS stand (required) ==="
	@echo "E2E_NFS_V3_HOST:                  $${E2E_NFS_V3_HOST}"
	@echo "E2E_NFS_V3_SHARE:                 $${E2E_NFS_V3_SHARE}  # default /srv/nfs/e2e"
	@echo "E2E_NFS_V4_HOST:                  $${E2E_NFS_V4_HOST}"
	@echo "E2E_NFS_V4_SHARE:                 $${E2E_NFS_V4_SHARE}  # default /srv/nfs/e2e"
	@echo "E2E_NFS_V4_SHARE_ALT:             $${E2E_NFS_V4_SHARE_ALT}  # default /srv/nfs/e2e-alt (Retain spec)"
	@echo "E2E_NFS_TLS_HOST:                 $${E2E_NFS_TLS_HOST}"
	@echo "E2E_NFS_TLS_SHARE:                $${E2E_NFS_TLS_SHARE}  # default /srv/nfs/tls"
	@echo "E2E_NFS_MTLS_HOST:                $${E2E_NFS_MTLS_HOST}  # MUST differ from E2E_NFS_TLS_HOST"
	@echo "E2E_NFS_MTLS_SHARE:               $${E2E_NFS_MTLS_SHARE}  # default /srv/nfs/mtls"
	@echo ""
	@echo "=== RPC-with-TLS material (base64 PEM, secrets) ==="
	@echo "E2E_NFS_TLS_CA:                   $${E2E_NFS_TLS_CA:+<set>}"
	@echo "E2E_NFS_TLS_CLIENT_CERT:          $${E2E_NFS_TLS_CLIENT_CERT:+<set>}"
	@echo "E2E_NFS_TLS_CLIENT_KEY:           $${E2E_NFS_TLS_CLIENT_KEY:+<set>}"
	@echo ""
	@echo "=== Suite knobs ==="
	@echo "E2E_PVC_SIZE:                     $${E2E_PVC_SIZE}  # default 1Gi"
	@echo "E2E_PROBE_IMAGE:                  $${E2E_PROBE_IMAGE}  # image with sh+cat+stat for probe/inspector pods"
	@echo "E2E_MODULE_READY_TIMEOUT:         $${E2E_MODULE_READY_TIMEOUT}"
	@echo "E2E_TLS_ROLLOUT_TIMEOUT:          $${E2E_TLS_ROLLOUT_TIMEOUT}  # default 15m"
	@echo "E2E_TEST_TIMEOUT:                 $${E2E_TEST_TIMEOUT}  # whole-suite Ginkgo timeout"
	@echo ""
	@echo "Keeping a cluster after a failed run is the e2e/keep-cluster PR LABEL,"
	@echo "not an env var - add it BEFORE the label that triggers the run."

.PHONY: fix-mod-permissions
fix-mod-permissions: ## Make storage-e2e module cache writable (PrepareBootstrapConfig writes temp/ under the module path)
	@GOMODCACHE=$${GOMODCACHE:-$(CURDIR)/.gomodcache}; \
	GOPATH=$${GOPATH:-$$HOME/go}; \
	for root in "$$GOMODCACHE" "$$GOPATH/pkg/mod"; do \
		[ -d "$$root" ] || continue; \
		for dir in "$$root"/github.com/deckhouse/storage-e2e@*; do \
			if [ -d "$$dir" ]; then \
				echo "Fixing permissions for $$dir"; \
				chmod -R +w "$$dir" 2>/dev/null || true; \
				mkdir -p "$$dir/temp/cluster" 2>/dev/null || true; \
			fi; \
		done; \
	done

.DEFAULT_GOAL := help
