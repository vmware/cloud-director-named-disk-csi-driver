FROM golang:1.19 AS builder

RUN apt-get update && \
    apt-get -y install \
        bash \
        git  \
        make

ADD . /go/src/github.com/vmware/cloud-director-named-disk-csi-driver
WORKDIR /go/src/github.com/vmware/cloud-director-named-disk-csi-driver

ENV GOPATH /go
RUN make build-within-docker && \
    chmod +x /build/vcloud/cloud-director-named-disk-csi-driver

########################################################

FROM photon:4.0-20230506
RUN tdnf install -y xfsprogs e2fsprogs udev && \
    tdnf clean all

WORKDIR /opt/vcloud/bin

# copy multiple small files at 1 time to create a single layer
COPY --from=builder /go/src/github.com/vmware/cloud-director-named-disk-csi-driver/LICENSE.txt /go/src/github.com/vmware/cloud-director-named-disk-csi-driver/NOTICE.txt /go/src/github.com/vmware/cloud-director-named-disk-csi-driver/open_source_license.txt .
COPY --from=builder /build/vcloud/cloud-director-named-disk-csi-driver .

# USER nobody
ENTRYPOINT ["/bin/bash", "-l", "-c"]
