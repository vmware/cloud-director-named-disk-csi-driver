GITCOMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
GITROOT := $(shell git rev-parse --show-toplevel)
GO_CODE := $(shell ls go.mod go.sum **/*.go)
version := $(shell cat ${GITROOT}/release/version)

csi_node_driver_registrar_version := v2.2.0
csi_attacher_version := v3.2.1
csi_provisioner_version := v2.2.2

REGISTRY?="projects-stg.registry.vmware.com/vmware-cloud-director"

.PHONY: build-within-docker vendor

build-within-docker: vendor
	mkdir -p /build/cloud-director-named-disk-csi-driver
	go build -ldflags "-X github.com/vmware/cloud-director-named-disk-csi-driver/version.Version=$(version)" -o /build/vcloud/cloud-director-named-disk-csi-driver cmd/csi/main.go

csi: $(GO_CODE)
	docker build -f Dockerfile . -t cloud-director-named-disk-csi-driver:$(version)
	docker tag cloud-director-named-disk-csi-driver:$(version) $(REGISTRY)/cloud-director-named-disk-csi-driver:$(version)
	docker tag cloud-director-named-disk-csi-driver:$(version) $(REGISTRY)/cloud-director-named-disk-csi-driver:$(version)-$(GITCOMMIT)
	docker push $(REGISTRY)/cloud-director-named-disk-csi-driver:$(version)
	touch out/$@

prod: csi prod-manifests update-gcr-images crs-artifacts-prod

dev: csi dev-manifests update-gcr-images crs-artifacts-dev
	docker push $(REGISTRY)/cloud-director-named-disk-csi-driver:$(version)-$(GITCOMMIT)

# The below artifact commands will generate csi-node-crs-airgap.yaml.template, csi-controller-crs-airgap.yaml.template which are to be pushed each time images are made
crs-artifacts-prod:
	sed -e "s/\-__GIT_COMMIT__//g" -e "s/__VERSION__/$(version)/g" artifacts/default-csi-controller-crs-airgap.yaml.template > artifacts/csi-controller-crs-airgap.yaml.template
	sed -e "s/\-__GIT_COMMIT__//g" -e "s/__VERSION__/$(version)/g" artifacts/default-csi-node-crs-airgap.yaml.template > artifacts/csi-node-crs-airgap.yaml.template
	sed -e "s/\-__GIT_COMMIT__//g" -e "s/__VERSION__/$(version)/g" -e "s~__REGISTRY__~$(REGISTRY)~g" artifacts/bom.json.template > artifacts/bom.json
	sed -e "s/\-__GIT_COMMIT__//g" -e "s/__VERSION__/$(version)/g" -e "s~__REGISTRY__~$(REGISTRY)~g" artifacts/dependencies.txt.template > artifacts/dependencies.txt
	docker build -f ./artifacts/Dockerfile . -t csi-crs-airgapped:$(version)
	docker tag csi-crs-airgapped:$(version) $(REGISTRY)/csi-crs-airgapped:$(version)
	docker push $(REGISTRY)/csi-crs-airgapped:$(version)

crs-artifacts-dev:
	sed -e "s/__GIT_COMMIT__/$(GITCOMMIT)/g" -e "s/__VERSION__/$(version)/g" artifacts/default-csi-controller-crs-airgap.yaml.template > artifacts/csi-controller-crs-airgap.yaml.template
	sed -e "s/__GIT_COMMIT__/$(GITCOMMIT)/g" -e "s/__VERSION__/$(version)/g" artifacts/default-csi-node-crs-airgap.yaml.template > artifacts/csi-node-crs-airgap.yaml.template
	sed -e "s/__GIT_COMMIT__/$(GITCOMMIT)/g" -e "s/__VERSION__/$(version)/g" -e "s~__REGISTRY__~$(REGISTRY)~g" artifacts/bom.json.template > artifacts/bom.json
	sed -e "s/__GIT_COMMIT__/$(GITCOMMIT)/g" -e "s/__VERSION__/$(version)/g" -e "s~__REGISTRY__~$(REGISTRY)~g" artifacts/dependencies.txt.template > artifacts/dependencies.txt
	docker build -f ./artifacts/Dockerfile . -t csi-crs-airgapped:$(version)-$(GITCOMMIT)
	docker tag csi-crs-airgapped:$(version)-$(GITCOMMIT) $(REGISTRY)/csi-crs-airgapped:$(version)-$(GITCOMMIT)
	docker push $(REGISTRY)/csi-crs-airgapped:$(version)-$(GITCOMMIT)

prod-manifests:
	sed -e "s/\-__GIT_COMMIT__//g" -e "s/__VERSION__/$(version)/g" manifests/csi-controller.yaml.template > manifests/csi-controller.yaml
	sed -e "s/\-__GIT_COMMIT__//g" -e "s/__VERSION__/$(version)/g" manifests/csi-controller-crs.yaml.template > manifests/csi-controller-crs.yaml
	sed -e "s/\-__GIT_COMMIT__//g" -e "s/__VERSION__/$(version)/g" manifests/csi-node.yaml.template > manifests/csi-node.yaml
	sed -e "s/\-__GIT_COMMIT__//g" -e "s/__VERSION__/$(version)/g" manifests/csi-node-crs.yaml.template > manifests/csi-node-crs.yaml

dev-manifests:
	sed -e "s/__GIT_COMMIT__/$(GITCOMMIT)/g" -e "s/__VERSION__/$(version)/g" manifests/csi-controller.yaml.template > manifests/csi-controller.yaml
	sed -e "s/__GIT_COMMIT__/$(GITCOMMIT)/g" -e "s/__VERSION__/$(version)/g" manifests/csi-controller-crs.yaml.template > manifests/csi-controller-crs.yaml
	sed -e "s/__GIT_COMMIT__/$(GITCOMMIT)/g" -e "s/__VERSION__/$(version)/g" manifests/csi-node.yaml.template > manifests/csi-node.yaml
	sed -e "s/__GIT_COMMIT__/$(GITCOMMIT)/g" -e "s/__VERSION__/$(version)/g" manifests/csi-node-crs.yaml.template > manifests/csi-node-crs.yaml

# Pulls and pushes CSI images from gcr registry to harbor for airgapped
update-gcr-images:
	docker pull registry.k8s.io/sig-storage/csi-node-driver-registrar:$(csi_node_driver_registrar_version)
	docker pull registry.k8s.io/sig-storage/csi-attacher:$(csi_attacher_version)
	docker pull registry.k8s.io/sig-storage/csi-provisioner:$(csi_provisioner_version)
	docker tag registry.k8s.io/sig-storage/csi-node-driver-registrar:$(csi_node_driver_registrar_version) $(REGISTRY)/sig-storage/csi-node-driver-registrar:$(csi_node_driver_registrar_version)
	docker tag registry.k8s.io/sig-storage/csi-attacher:$(csi_attacher_version) $(REGISTRY)/sig-storage/csi-attacher:$(csi_attacher_version)
	docker tag registry.k8s.io/sig-storage/csi-provisioner:$(csi_provisioner_version) $(REGISTRY)/sig-storage/csi-provisioner:$(csi_provisioner_version)
	docker push $(REGISTRY)/sig-storage/csi-node-driver-registrar:$(csi_node_driver_registrar_version)
	docker push $(REGISTRY)/sig-storage/csi-attacher:$(csi_attacher_version)
	docker push $(REGISTRY)/sig-storage/csi-provisioner:$(csi_provisioner_version)

test:
	go test -tags testing -v github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdclient -cover -count=1
	go test -tags testing -v github.com/vmware/cloud-director-named-disk-csi-driver/pkg/config -cover -count=1

integration-test: test
	go test -tags="testing integration" -v github.com/vmware/cloud-director-named-disk-csi-driver/vcdclient -cover -count=1

vendor:
	go mod edit -go=1.19
	go mod tidy -compat=1.19
	go mod vendor
