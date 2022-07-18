GITCOMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
GITROOT := $(shell git rev-parse --show-toplevel)
GO_CODE := $(shell ls go.mod go.sum **/*.go)
version := $(shell cat ${GITROOT}/release/version)

REGISTRY?="harbor-repo.vmware.com/vcloud"

.PHONY: build-within-docker vendor

build-within-docker:
	mkdir -p /build/cloud-director-named-disk-csi-driver
	go build -ldflags "-X github.com/vmware/cloud-director-named-disk-csi-driver/version.Version=$(version)" -o /build/vcloud/cloud-director-named-disk-csi-driver cmd/csi/main.go

csi: $(GO_CODE)
	docker build -f Dockerfile . -t cloud-director-named-disk-csi-driver:$(version)
	docker tag cloud-director-named-disk-csi-driver:$(version) $(REGISTRY)/cloud-director-named-disk-csi-driver:$(version)
	# docker tag cloud-director-named-disk-csi-driver:$(version) $(REGISTRY)/cloud-director-named-disk-csi-driver:$(version).$$(docker images cloud-director-named-disk-csi-driver:$(version) -q)
	docker push $(REGISTRY)/cloud-director-named-disk-csi-driver:$(version)
	touch out/$@

all: csi

test:
	go test -tags testing -v github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdclient -cover -count=1
	go test -tags testing -v github.com/vmware/cloud-director-named-disk-csi-driver/pkg/config -cover -count=1

integration-test: test
	go test -tags="testing integration" -v github.com/vmware/cloud-director-named-disk-csi-driver/vcdclient -cover -count=1

vendor:
	go mod edit -go=1.17
	go mod tidy
	go mod vendor
