FROM golang:1.16 AS builder

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

FROM photon:4.0-20210910

RUN tdnf install yum && \
	yum install -y udev

WORKDIR /opt/vcloud/bin

COPY --from=builder /build/vcloud/cloud-director-named-disk-csi-driver .

RUN chmod +x /opt/vcloud/bin/cloud-director-named-disk-csi-driver

# USER nobody
ENTRYPOINT ["/bin/bash", "-l", "-c"]
