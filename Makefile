LINT_VERSION := 1.51.2
GOSEC_VERSION := "v2.16.0"

GOLANGCI_EXIT_CODE ?= 1
# Set PATH to pick up cached tools. The additional 'sed' is required for cross-platform support of quoting the args to 'env'
SHELL := /usr/bin/env PATH=$(shell echo $(GITROOT)/bin:${PATH} | sed 's/ /\\ /g') bash
# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
	GOBIN = $(shell go env GOPATH)/bin
else
	GOBIN = $(shell go env GOBIN)
endif

GITCOMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null)
GITROOT ?= $(shell git rev-parse --show-toplevel)

CSI_IMG := cloud-director-named-disk-csi-driver
ARTIFACT_IMG := csi-crs-airgapped
VERSION ?= $(shell cat $(GITROOT)/release/version)

PLATFORM ?= linux/amd64
OS ?= linux
ARCH ?= amd64
CGO ?= 0

CSI_NODE_DRIVER_REGISTRAR_VERSION ?= v2.2.0
CSI_ATTACHER_VERSION ?= v3.2.1
CSI_PROVISIONER_VERSION ?= v2.2.2

GOLANGCI_LINT ?= bin/golangci-lint
GOSEC ?= bin/gosec
SHELLCHECK ?= bin/shellcheck


.PHONY: all
all: vendor lint dev

.PHONY: csi
capvcd: vendor lint docker-build-csi ## Run checks, and build csi docker image.

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)


##@ Development

.PHONY: vendor
vendor: ## Update go mod dependencies.
	go mod edit -go=1.20
	go mod tidy -compat=1.20
	go mod vendor

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run --issues-exit-code $(GOLANGCI_EXIT_CODE)

.PHONY: gosec
gosec: $(GOSEC) ## Run gosec against code.
	$(GOSEC) -conf .gosec.json ./...

.PHONY: shellcheck
shellcheck: $(SHELLCHECK) ## Run shellcheck against code.
	find . -name '*.*sh' -not -path '*/vendor/*' | xargs $(SHELLCHECK) --color

.PHONY: lint
lint: lint-deps golangci-lint gosec shellcheck ## Run golangci-lint, gosec, shellcheck.

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run --fix

.PHONY: test
test:
	go test -tags testing -v github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdclient -cover -count=1
	go test -tags testing -v github.com/vmware/cloud-director-named-disk-csi-driver/pkg/config -cover -count=1

.PHONY: integration-test
integration-test: test
	go test -tags="testing integration" -v github.com/vmware/cloud-director-named-disk-csi-driver/vcdclient -cover -count=1



##@ Build

.PHONY: build
build: bin ## Build CSI binary. To be used from within a Dockerfile
	GOOS=$(OS) GOARCH=$(ARCH) CGO_ENABLED=$(CGO) go build -ldflags "-s -w -X github.com/vmware/cloud-director-named-disk-csi-driver/version.Version=$(VERSION)" -o bin/cloud-director-named-disk-csi-driver cmd/csi/main.go

.PHONY: docker-build-csi
docker-build-csi: manifests build
	docker build  \
		--platform $(PLATFORM) \
		--file Dockerfile \
		--tag $(CSI_IMG):$(VERSION) \
		--build-arg CSI_BUILD_DIR=bin \
		.

.PHONY: docker-build-artifacts
docker-build-artifacts: release-prep
	docker build  \
		--platform $(PLATFORM) \
		--file artifacts/Dockerfile \
		--tag $(ARTIFACT_IMG):$(VERSION) \
		.

.PHONY: docker-build
docker-build: docker-build-csi docker-build-artifacts ## Build CSI docker image and artifact image.

##@ Publish

.PHONY: dev
dev: VERSION := $(VERSION)-$(GITCOMMIT)
dev: git-check release ## Build development images and push to registry.

.PHONY: release
release: docker-build docker-push ## Build release images and push to registry.

.PHONY: release-prep
release-prep:  ## Generate BOM and dependencies files.
	sed -e "s/__VERSION__/$(VERSION)/g"  artifacts/default-csi-controller-crs-airgap.yaml.template > artifacts/csi-controller-crs-airgap.yaml.template
	sed -e "s/__VERSION__/$(VERSION)/g"  artifacts/default-csi-node-crs-airgap.yaml.template > artifacts/csi-node-crs-airgap.yaml.template
	sed -e "s/__VERSION__/$(VERSION)/g"  artifacts/bom.json.template > artifacts/bom.json
	sed -e "s/__VERSION__/$(VERSION)/g"  artifacts/dependencies.txt.template > artifacts/dependencies.txt

.PHONY: manifests
manifests: ## Generate CSI manifests
	sed -e "s/__VERSION__/$(VERSION)/g" manifests/csi-controller.yaml.template > manifests/csi-controller.yaml
	sed -e "s/__VERSION__/$(VERSION)/g" manifests/csi-controller-crs.yaml.template > manifests/csi-controller-crs.yaml
	sed -e "s/__VERSION__/$(VERSION)/g" manifests/csi-node.yaml.template > manifests/csi-node.yaml
	sed -e "s/__VERSION__/$(VERSION)/g" manifests/csi-node-crs.yaml.template > manifests/csi-node-crs.yaml

.PHONY: gcr-csi
gcr-csi:
	docker pull registry.k8s.io/sig-storage/csi-node-driver-registrar:$(CSI_NODE_DRIVER_REGISTRAR_VERSION)
	docker pull registry.k8s.io/sig-storage/csi-attacher:$(CSI_ATTACHER_VERSION)
	docker pull registry.k8s.io/sig-storage/csi-provisioner:$(CSI_PROVISIONER_VERSION)
	docker tag registry.k8s.io/sig-storage/csi-node-driver-registrar:$(CSI_NODE_DRIVER_REGISTRAR_VERSION) projects-stg.registry.vmware.com/vmware-cloud-director/sig-storage/csi-node-driver-registrar:$(CSI_NODE_DRIVER_REGISTRAR_VERSION)
	docker tag registry.k8s.io/sig-storage/csi-attacher:$(CSI_ATTACHER_VERSION) projects-stg.registry.vmware.com/vmware-cloud-director/sig-storage/csi-attacher:$(CSI_ATTACHER_VERSION)
	docker tag registry.k8s.io/sig-storage/csi-provisioner:$(CSI_PROVISIONER_VERSION) projects-stg.registry.vmware.com/vmware-cloud-director/sig-storage/csi-provisioner:$(CSI_PROVISIONER_VERSION)

.PHONY: docker-push-csi
docker-push-csi: # Push CSI image to registry.
	docker tag $(CSI_IMG)/$(VERSION) projects-stg.registry.vmware.com/vmware-cloud-director/$(CSI_IMG):$(VERSION)
	docker push projects-stg.registry.vmware.com/vmware-cloud-director/$(CSI_IMG):$(VERSION)

.PHONY: docker-push-artifacts
docker-push-artifacts: # Push artifacts image to registry
	docker tag $(ARTIFACT_IMG):$(VERSION) projects-stg.registry.vmware.com/vmware-cloud-director/$(ARTIFACT_IMG):$(VERSION)
	docker push projects-stg.registry.vmware.com/vmware-cloud-director/$(ARTIFACT_IMG):$(VERSION)

.PHONY: docker-push-gcr-csi
docker-push-gcr-csi: gcr-csi ## Publish GCR images to container registry.
	docker push projects-stg.registry.vmware.com/vmware-cloud-director/sig-storage/csi-node-driver-registrar:$(CSI_NODE_DRIVER_REGISTRAR_VERSION)
	docker push projects-stg.registry.vmware.com/vmware-cloud-director/sig-storage/csi-attacher:$(CSI_ATTACHER_VERSION)
	docker push projects-stg.registry.vmware.com/vmware-cloud-director/sig-storage/csi-provisioner:$(CSI_PROVISIONER_VERSION)

.PHONY: docker-push
docker-push: docker-push-csi docker-push-artifacts docker-push-gcr-csi ## Push images to container registry.



##@ Dependencies

.PHONY: lint-deps
lint-deps: $(GOLANGCI_LINT) $(GOSEC) $(SHELLCHECK) ## Download lint dependencies locally.





.PHONY: clean
clean:
	rm -rf bin
	rm \
		artifacts/csi-controller-crs-airgap.yaml.template \
		artifacts/csi-node-crs-airgap.yaml.template \
		artifacts/bom.json \
		artifacts/dependencies.txt

bin:
	mkdir -p bin

$(GOLANGCI_LINT): bin
	@set -o pipefail && \
		wget -q -O - https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GITROOT)/bin v$(LINT_VERSION);

$(GOSEC): bin
	@GOBIN=$(GITROOT)/bin go install github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}

$(SHELLCHECK): bin
	@cd bin && \
		set -o pipefail && \
		wget -q -O - https://github.com/koalaman/shellcheck/releases/download/stable/shellcheck-stable.$$(uname).x86_64.tar.xz | tar -xJv --strip-components=1 shellcheck-stable/shellcheck && \
		chmod +x $(GITROOT)/bin/shellcheck

.PHONY: git-check
git-check:
	@git diff --exit-code --quiet artifacts/ cmd/ manifests/ pkg/ Dockerfile || (echo 'Uncommitted changes found. Please commit your changes before proceeding.'; exit 1)
