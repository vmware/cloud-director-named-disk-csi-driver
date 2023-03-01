package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	csiClient "github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdcsiclient"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdtypes"
	"github.com/vmware/cloud-director-named-disk-csi-driver/tests/utils"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	apiv1 "k8s.io/api/core/v1"
)

const (
	testDiskName      = "test-named-disk"
	testStaticPVCName = "test-static-pvc"
)

var _ = Describe("CSI static provisioning Test", func() {
	var (
		tc        *testingsdk.TestClient
		err       error
		vcdDisk   *vcdtypes.Disk
		pv        *apiv1.PersistentVolume
		pvDeleted bool
	)

	tc, err = testingsdk.NewTestClient(&testingsdk.VCDAuthParams{
		Host:         host,
		OvdcName:     ovdc,
		OrgName:      org,
		Username:     userName,
		RefreshToken: refreshToken,
		UserOrg:      userOrg,
		GetVdcClient: true,
	}, rdeId)
	Expect(err).NotTo(HaveOccurred())
	Expect(tc).NotTo(BeNil())
	Expect(&tc.Cs).NotTo(BeNil())

	ctx := context.TODO()

	It("Should create the name space AND different storage classes", func() {
		ns, err := tc.CreateNameSpace(ctx, testNameSpaceName)
		Expect(err).NotTo(HaveOccurred())
		Expect(ns).NotTo(BeNil())
		retainStorageClass, err := tc.CreateStorageClass(ctx, storageClassRetain, apiv1.PersistentVolumeReclaimRetain, storageProfile, busType)
		Expect(err).NotTo(HaveOccurred())
		Expect(retainStorageClass).NotTo(BeNil())
		deleteStorageClass, err := tc.CreateStorageClass(ctx, storageClassDelete, apiv1.PersistentVolumeReclaimDelete, storageProfile, busType)
		Expect(err).NotTo(HaveOccurred())
		Expect(deleteStorageClass).NotTo(BeNil())
	})

	//scenario 1: use 'Delete' retention policy. step1: create VCD named-disk and PV.
	It("should create a disk using VCD API calls and set up a PV based on the disk", func() {
		By("should create the disk successfully from VCD")
		err = utils.CreateDisk(tc.VcdClient, testDiskName, smallDiskSizeMB, csiClient.BusTypesSet[busType], storageProfile)
		Expect(err).NotTo(HaveOccurred())

		By("should create the static PV successfully in kubernetes")
		pv, err = tc.CreatePV(ctx, testDiskName, storageClassDelete, storageProfile, storageSize, busType, apiv1.PersistentVolumeReclaimDelete)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV should not be shown in RDE")
		pvFound, err := utils.GetPVByNameViaRDE(testDiskName, tc, "named-disk")
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(pvFound).To(BeFalse())

		By("should create the PVC successfully in kubernetes")
		pvc, err := tc.CreatePVC(ctx, testNameSpaceName, testStaticPVCName, storageClassDelete, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())

		By("PVC status should be 'bound'")
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 1: use 'Delete' retention policy. step2: install a deployment using the above PVC.
	It("should create a deployment using a PVC connected to the above PV", func() {
		By("should create the deployment successfully")
		deployment, err := tc.CreateDeployment(ctx, &testingsdk.DeployParams{
			Name: testDeploymentName,
			Labels: map[string]string{
				"app": "nginx",
			},
			ContainerParams: testingsdk.ContainerParams{
				ContainerName:  "nginx",
				ContainerImage: "nginx:1.14.2",
				ContainerPort:  80,
			},
			VolumeParams: testingsdk.VolumeParams{
				VolumeName: testDiskName,
				PvcRef:     testStaticPVCName,
				MountPath:  "/init-container-msg-mount-path",
			},
		}, testNameSpaceName)
		Expect(deployment).NotTo(BeNil())
		Expect(err).NotTo(HaveOccurred())

		By("Deployment should be ready")
		err = tc.WaitForDeploymentReady(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 1: use 'Delete' retention policy. step3: PV should not be presented in kubernetes and VCD after PVC deleted
	It("PV should be presented in kubernetes and VCD after PVC AND Deployment is deleted", func() {
		By("should delete the PVC successfully")
		err = tc.DeletePVC(ctx, testNameSpaceName, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the deployment successfully")
		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should deleted in VCD")
		err = utils.WaitDiskDeleteViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should be not presented in Kubernetes")
		pvDeleted, err = tc.WaitForPVDeleted(ctx, testDiskName)
		Expect(pvDeleted).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())
		pv, err = tc.GetPV(ctx, testDiskName)
		Expect(err).To(HaveOccurred())
		Expect(pv).To(BeNil())
	})

	//scenario 2: use 'Retain' retention policy. step1: create VCD named-disk and PV.
	It("Create a disk using VCD API calls and Set up a PV based on the disk using retain policy", func() {
		By("should create the disk successfully from VCD")
		err := utils.CreateDisk(tc.VcdClient, testDiskName, smallDiskSizeMB, csiClient.BusTypesSet[busType], storageProfile)
		Expect(err).NotTo(HaveOccurred())

		By("should create the static PV successfully in kubernetes")
		pv, err = tc.CreatePV(ctx, testDiskName, storageClassRetain, storageProfile, storageSize, busType, apiv1.PersistentVolumeReclaimRetain)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV should be presented in kubernetes")
		pv, err = tc.GetPV(ctx, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV should not be shown in RDE")
		pvFound, err := utils.GetPVByNameViaRDE(testDiskName, tc, "named-disk")
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(pvFound).To(BeFalse())

		By("should create the PVC successfully in kubernetes")
		pvc, err := tc.CreatePVC(ctx, testNameSpaceName, testStaticPVCName, storageClassRetain, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())

		By("PVC status should be 'bound'")
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 2: use 'Retain' retention policy. step2: install a deployment using the above PVC.
	It("should create a deployment using a PVC connected to the above PV", func() {
		By("deployment should be created successfully")
		deployment, err := tc.CreateDeployment(ctx, &testingsdk.DeployParams{
			Name: testDeploymentName,
			Labels: map[string]string{
				"app": "nginx",
			},
			ContainerParams: testingsdk.ContainerParams{
				ContainerName:  "nginx",
				ContainerImage: "nginx:1.14.2",
				ContainerPort:  80,
			},
			VolumeParams: testingsdk.VolumeParams{
				VolumeName: testDiskName,
				PvcRef:     testStaticPVCName,
				MountPath:  "/init-container-msg-mount-path",
			},
		}, testNameSpaceName)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())

		By("pods of the deployment should come up.")
		err = tc.WaitForDeploymentReady(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 2: use 'Retain' retention policy. step3: PV should be presented in kubernetes and VCD after PVC deleted
	It("PV is present in kubernetes and VCD after PVC AND Deployment is deleted", func() {
		By("should delete the PVC successfully")
		err = tc.DeletePVC(ctx, testNameSpaceName, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the deployment successfully")
		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should be presented in Kubernetes")
		pv, err = tc.GetPV(ctx, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV should be presented in VCD")
		vcdDisk, err := utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())
	})

	//scenario 2: use 'Retain' retention policy. step4: PV should be presented in VCD after PV deleted
	It("VCD Disk should be presented after PV gets deleted in RDE", func() {
		By("should delete the PV successfully")
		err = tc.DeletePV(ctx, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should not be presented in Kubernetes")
		pv, err = tc.GetPV(ctx, testDiskName)
		Expect(err).To(HaveOccurred())
		Expect(pv).To(BeNil())

		By("Disk should be present in VCD")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())

		By("clean up the remaining name-disk in VCD and namespace and storage classes in kubernetes")
		err = utils.DeleteDisk(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the named-disk in VCD")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).To(MatchError(govcd.ErrorEntityNotFound))
		Expect(vcdDisk).To(BeNil())

		By("should delete the retain storage class")
		err = tc.DeleteStorageClass(ctx, storageClassRetain)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the delete storage class")
		err = tc.DeleteStorageClass(ctx, storageClassDelete)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the test nameSpace")
		err = tc.DeleteNameSpace(ctx, testNameSpaceName)
		Expect(err).NotTo(HaveOccurred())
	})
})
