FROM scratch
COPY /artifacts/csi-controller-crs-airgap.yaml.template \
    /artifacts/csi-node-crs-airgap.yaml.template \
    /artifacts/csi-driver-airgap.yaml /src/artifact/
ENTRYPOINT ["/bin/sh"]