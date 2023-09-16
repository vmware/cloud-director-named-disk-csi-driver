FROM photon:4.0-20230506

RUN tdnf install -y xfsprogs e2fsprogs udev && \
    tdnf clean all

WORKDIR /opt/vcloud/bin

ARG CSI_BUILD_DIR
ADD ${CSI_BUILD_DIR}/cloud-director-named-disk-csi-driver .
# copy multiple small files at 1 time to create a single layer
COPY LICENSE.txt NOTICE.txt open_source_license.txt /opt/vcloud/bin/

RUN chmod +x /opt/vcloud/bin/cloud-director-named-disk-csi-driver

# USER nobody
ENTRYPOINT ["/bin/bash", "-l", "-c"]
