# Container Storage Interface (CSI) driver for VMware Cloud Director Named Independent Disks
This repository contains the source code and build methods to build a Kubernetes CSI driver that helps provision [VMware Cloud Director Named Independent Disks](https://docs.vmware.com/en/VMware-Cloud-Director/10.3/VMware-Cloud-Director-Tenant-Portal-Guide/GUID-8F8BFCD3-071A-4E45-BAC0-A9B78F2C19CE.html) as a storage solution for Kubernetes Applications. This uses VMware Cloud Director API for functionality and hence needs an appropriate VMware Cloud Director Installation. This CSI driver will help enable common scenarios with persistent volumes and stateful-sets using VMware Cloud Director Shareable Named Disks.

The version of the VMware Cloud Director API and Installation that are compatible for a given CSI container image are provided in the following compatibility matrix:

| CSI Version | VMware Cloud Director API | VMware Cloud Director Installation |
| :---------: | :-----------------------: | :--------------------------------: |
| 0.1.0-beta | 36.0+ | 10.3.0+|

This extension is intended to be installed into a Kubernetes cluster installed with [VMware Cloud Director](https://www.vmware.com/products/cloud-director.html) as a Cloud Provider, by a user that has the rights as described in the sections below.

**cloud-director-named-disk-csi-driver** is distributed as a container image hosted at [Distribution Harbor](https://projects.registry.vmware.com) as `projects.registry.vmware.com/vmware-cloud-director/cloud-director-named-disk-csi-driver:<CSI version>`.

This driver is in a preliminary `beta` state and is not yet ready to be used in production.

## Terminology
1. VCD: VMware Cloud Director
2. ClusterAdminRole: This is a user who has enough rights to create and administer a Kubernetes Cluster in VCD. This role can be created by cloning the [vApp Author Role](https://docs.vmware.com/en/VMware-Cloud-Director/10.3/VMware-Cloud-Director-Tenant-Portal-Guide/GUID-BC504F6B-3D38-4F25-AACF-ED584063754F.html) and then adding the following rights:
   1. Full Control: CSE:NATIVECLUSTER
   2. Edit: CSE:NATIVECLUSTER
   3. View: CSE:NATIVECLUSTER
3. CSI user: CSI needs to be running in the cluster as a user with a set of rights as described in this section and the Rights section below. For convenience, let us term this user as the `CSI user`.

## VMware Cloud Director Configuration
In this section, we assume that the Kubernetes cluster is created using the [Container Service Extension](https://github.com/vmware/container-service-extension). However that is not a mandatory requirement.

### Rights
The `CSI user` should have view access to the vApp containing the Kubernetes cluster. If the `CSI user` itself has created the cluster, it will have this access by default.

This `CSI user` needs to be created from a `ClusterAdminRole` with the following additional rights:
1. Organization VDC => Create a Shared Disk

### Additional Setup Steps for 0.1.0-beta
**Note:** If you also use CPI for VCD, you will not need to redo these steps. A Kubernetes cluster will need these steps to be executed exactly once in its lifetime.

There is a set of additional steps needed in order to feed the `CSI user` credentials into the Kubernetes cluster. These steps lead to a less secure cluster and are only applicable for the Beta release. The GA release of this product will not need these additional steps and will therefore result in a more secure cluster.

These additional steps are as follows:
1. Get the `KUBECONFIG` file from the cluster created. If the cluster was created using the Container Service Extension, the following command can be used:
```
    vcd cse cluster config <cluster name>  > myk8sclusterkubeconfig
    export KUBECONFIG="<path to myk8sclusterkubeconfig>"
```
2. Create a Kubernetes secret with the username and password of the `CSI user` as follows:
```
VCDUSER=$(echo -n '<csi user name>' | base64)
PASSWORD=$(echo -n '<csi user password>' | base64)

cat > vcloud-basic-auth.yaml << END
---
apiVersion: v1
kind: Secret
metadata:
name: vcloud-basic-auth
namespace: kube-system
data:
username: "VCDUSER"
password: "$PASSWORD"
END

kubectl apply  -f vcloud-basic-auth.yaml
```
This will create a secret and in a while start the CSI cleanly with the right credentials. If you wish, you can monitor it as follows:
```
kubectl get po -A -o wide # <== look for the pod whose name starts with `vmware-cloud-director-ccm-`
kubectl logs -f -n kube-system <pod whose name starts with csi>
```

After a while, the CSI pods `Pending` to `Running` state.

### Known Issues in Beta
With the manual secret-injection method of Beta as described above, the credentials expire after 24 hours. These credentials are not automatically refreshed later.

This limitation is for the beta release only. The current workaround for this limitation is to delete the CCM pod. Once deleted, the pod will be restarted automatically with the refreshed credentials.


## CSI Feature matrix
| Feature | Support Scope |
| :---------: | :-----------------------: |
| Storage Type | Independent Shareable Named Disks of VCD |
|Provisioning|<ul><li>Static Provisioning</li><li>Dynamic Provisioning</li></ul>|
|Access Modes|<ul><li>ReadOnlyMany</li><li>ReadWriteOnly</li></ul>|
|Volume|Block|
|VolumeMode|<ul><li>FileSystem</li></ul>|
|Topology|<ul><li>Static Provisioning: reuses VCD topology capabilities</li><li>Dynamic Provisioning: places disk in the OVDC of the CSI user based on the StorageProfile specified.</li></ul>|

## Contributing
Please see [CONTRIBUTING.md](CONTRIBUTING.md) for instructions on how to contribute.


## License
[Apache-2.0](LICENSE.txt)
