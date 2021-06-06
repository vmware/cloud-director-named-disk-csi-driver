# Container Storage Interface (CSI) driver for VMware Cloud Director Named Independent Disks

**cloud-director-named-disk-csi-driver** contains the source code and build methods to build a Kubernetes CSI driver that helps provision [VMware Cloud Director Named Independent Disks](https://docs.vmware.com/en/VMware-Cloud-Director/10.0/com.vmware.vcloud.tenantportal.doc/GUID-8F8BFCD3-071A-4E45-BAC0-A9B78F2C19CE.html) as a storage solution for Kubernetes Applications. This uses VMware Cloud Director API for functionality and hence needs an appropriate VMware Cloud Director Installation. This CSI driver will help enable common scenarios with persistent volumes and stateful-sets using VMware Cloud Director Shareable Named Disks.

The version of the VMware Cloud Director API and Installation that are compatible for a given CSI container image are provided in the following compatibility matrix:

| CSI Version | VMware Cloud Director API | VMware Cloud Director Installation |
| :---------: | :-----------------------: | :--------------------------------: |
| 0.1.0.latest | TBA | TBA|

This extension is intended to be installed into a Kubernetes cluster installed with [VMware Cloud Director](https://www.vmware.com/products/cloud-director.html) as a Cloud Provider, by a user that has the same rights as a [vApp Author](https://docs.vmware.com/en/VMware-Cloud-Director/9.7/com.vmware.vcloud.admin.doc/GUID-BC504F6B-3D38-4F25-AACF-ED584063754F.html).

**cloud-director-named-disk-csi-driver** is distributed as a container image hosted at [Distribution Harbor](https://projects.registry.vmware.com) as `projects.registry.vmware.com/vmware-cloud-director/cloud-director-named-disk-csi-driver:<tag>`.

This driver is in a preliminary `alpha` state and is not yet ready to be used in production.

## VMware Cloud Director Configuration

The [vApp Author Role](https://docs.vmware.com/en/VMware-Cloud-Director/9.7/com.vmware.vcloud.admin.doc/GUID-BC504F6B-3D38-4F25-AACF-ED584063754F.html) must have the additional `Create a Shared Disk` role in order to create shared disks.

## Documentation

Documentation for the VMware Cloud Director CSI driver can be obtained here:
* TBD


## Contributing

Please see [CONTRIBUTING.md](CONTRIBUTING.md) for instructions on how to contribute.


## License

[Apache-2.0](LICENSE.txt)
