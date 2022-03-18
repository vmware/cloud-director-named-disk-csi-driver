# Container Storage Interface (CSI) driver for VMware Cloud Director Named Independent Disks
This repository contains the source code and build methods to build a Kubernetes CSI driver that helps provision [VMware Cloud Director Named Independent Disks](https://docs.vmware.com/en/VMware-Cloud-Director/10.3/VMware-Cloud-Director-Tenant-Portal-Guide/GUID-8F8BFCD3-071A-4E45-BAC0-A9B78F2C19CE.html) as a storage solution for Kubernetes Applications. This uses VMware Cloud Director API for functionality and hence needs an appropriate VMware Cloud Director Installation. This CSI driver will help enable common scenarios with persistent volumes and stateful-sets using VMware Cloud Director Shareable Named Disks.

The version of the VMware Cloud Director API and Installation that are compatible for a given CSI container image are provided in the following compatibility matrix:

| CSI Version | VMware Cloud Director API | VMware Cloud Director Installation | Notes | Kubernetes Versions |
| :---------: | :-----------------------: | :--------------------------------: | :----: | :----------------- |
| 1.0.0 | 36.0+ | 10.3.1+ <br/>(10.3.1 needs hot-patch to prevent VCD cell crashes in multi-cell environments) | First cut with support for Named Independent Disks |<ul><li>1.21</li><li>1.20</li><li>1.19</li></ul>|
| 1.1.0 | 36.0+ | 10.3.1+ <br/>(10.3.1 needs hot-patch to prevent VCD cell crashes in multi-cell environments) |<ul><li>Remove legacy Kubernetes dependencies.</li><li>Support for CAPVCD RDEs.</li></ul> |<ul><li>1.21</li><li>1.20</li><li>1.19</li></ul>|
| 1.1.0 | 36.0+ | 10.3.1+ <br/>(10.3.1 needs hot-patch to prevent VCD cell crashes in multi-cell environments) |<ul><li>Fixed refresh token based authentication issue observed when VCD cells are fronted by a load balancer.</ul> |<ul><li>1.21</li><li>1.20</li><li>1.19</li></ul>|

This extension is intended to be installed into a Kubernetes cluster installed with [VMware Cloud Director](https://www.vmware.com/products/cloud-director.html) as a Cloud Provider, by a user that has the rights as described in the sections below.

**cloud-director-named-disk-csi-driver** is distributed as a container image hosted at [Distribution Harbor](https://projects.registry.vmware.com) as `projects.registry.vmware.com/vmware-cloud-director/cloud-director-named-disk-csi-driver:<CSI version>.latest`.

This driver is in a GA state and will be supported in production.

Note: This driver is not impacted by the Apache Log4j open source component vulnerability.

## Terminology
1. VCD: VMware Cloud Director
2. ClusterAdminRole: This is the role that has enough rights to create and administer a Kubernetes Cluster in VCD. This role can be created by cloning the [vApp Author Role](https://docs.vmware.com/en/VMware-Cloud-Director/10.3/VMware-Cloud-Director-Tenant-Portal-Guide/GUID-BC504F6B-3D38-4F25-AACF-ED584063754F.html) and then adding the following rights (details on adding the rights below can be found in the [CSE docs](https://github.com/rocknes/container-service-extension/blob/cse_3_1_docs/docs/cse3_1/RBAC.md#additional-required-rights)):
    1. Full Control: CSE:NATIVECLUSTER
    2. Edit: CSE:NATIVECLUSTER
    3. View: CSE:NATIVECLUSTER
3. ClusterAdminUser: For CSI functionality, there needs to be a set of additional rights added to the `ClusterAdminRole` as described in the "Additional Rights for CSI" section below. The Kubernetes Cluster needs to be **created** by a user belonging to this **enhanced** `ClusterAdminRole`. For convenience, let us term this user as the `ClusterAdminUser`.

## VMware Cloud Director Configuration
In this section, we assume that the Kubernetes cluster is created using the [Container Service Extension](https://github.com/vmware/container-service-extension). However that is not a mandatory requirement.

### Additional Rights for CSI
The `ClusterAdminUser` should have view access to the vApp containing the Kubernetes cluster. Since the `ClusterAdminUser` itself creates the cluster, it will have this access by default.
This `ClusterAdminUser` needs to be created from a `ClusterAdminRole` with the following additional rights:
1. Access Control =>
   1. User => Manage user's own API TOKEN
2. Organization VDC => Create a Shared Disk

## Upgrde CSI
To upgrade CSI to the latest version (v1.1.1), please execute the following command
```shell
kubectl patch StatefulSet -n kube-system csi-vcd-controllerplugin -p '{"spec": {"template": {"spec": {"containers": [{"name": "vcd-csi-plugin", "image": "projects.registry.vmware.com/vmware-cloud-director/cloud-director-named-disk-csi-driver:1.1.1.latest"}]}}}}'
kubectl patch DaemonSet -n kube-system csi-vcd-nodeplugin -p '{"spec": {"template": {"spec": {"containers": [{"name": "vcd-csi-plugin", "image": "harbor-repo.vmware.com/vcloud/cloud-director-named-disk-csi-driver:1.1.1.latest"}]}}}}'
```
## CSI Feature matrix
| Feature | Support Scope |
| :---------: | :----------------------- |
| Storage Type | Independent Shareable Named Disks of VCD |
|Provisioning|<ul><li>Static Provisioning</li><li>Dynamic Provisioning</li></ul>|
|Access Modes|<ul><li>ReadOnlyMany</li><li>ReadWriteOnly</li></ul>|
|Volume|Block|
|VolumeMode|<ul><li>FileSystem</li></ul>|
|Topology|<ul><li>Static Provisioning: reuses VCD topology capabilities</li><li>Dynamic Provisioning: places disk in the OVDC of the `ClusterAdminUser` based on the StorageProfile specified.</li></ul>|

## Contributing
Please see [CONTRIBUTING.md](CONTRIBUTING.md) for instructions on how to contribute.


## License
[Apache-2.0](LICENSE.txt)
