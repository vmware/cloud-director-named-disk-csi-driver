FROM golang:1.17 AS builder

RUN apt-get update && \
    apt-get -y install \
        bash \
        git  \
        make

ADD . /go/src/github.com/vmware/cloud-director-named-disk-csi-driver
WORKDIR /go/src/github.com/vmware/cloud-director-named-disk-csi-driver

ENV GOPATH /go
RUN ["make", "build-within-docker"]

########################################################

FROM photon:4.0
RUN tdnf install -y xfsprogs
# udev is to get scsi_id, e2fsprogs is for mkfs.ext4
RUN tdnf install -y e2fsprogs
RUN tdnf install -y udev

WORKDIR /opt/vcloud/bin

COPY --from=builder /go/src/github.com/vmware/cloud-director-named-disk-csi-driver/LICENSE.txt .
COPY --from=builder /go/src/github.com/vmware/cloud-director-named-disk-csi-driver/NOTICE.txt .
COPY --from=builder /go/src/github.com/vmware/cloud-director-named-disk-csi-driver/open_source_license.txt .
COPY --from=builder /build/vcloud/cloud-director-named-disk-csi-driver .

RUN chmod +x /opt/vcloud/bin/cloud-director-named-disk-csi-driver

# USER nobody
ENTRYPOINT ["/bin/bash", "-l", "-c"]
