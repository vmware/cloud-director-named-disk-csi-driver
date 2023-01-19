package e2e

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
		tc      *testingsdk.TestClient
		err     error
		vcdDisk *vcdtypes.Disk
		pv      *apiv1.PersistentVolume
	)

	tc, err = testingsdk.NewTestClient(&testingsdk.VCDAuthParams{
		Host:         host,
		OvdcName:     ovdc,
		OrgName:      org,
		Username:     userName,
		RefreshToken: refreshToken,
		UserOrg:      "system",
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
		retainStorageClass, err := tc.CreateStorageClass(ctx, storageClassRetain, apiv1.PersistentVolumeReclaimRetain, defaultStorageProfile)
		Expect(err).NotTo(HaveOccurred())
		Expect(retainStorageClass).NotTo(BeNil())
		deleteStorageClass, err := tc.CreateStorageClass(ctx, storageClassDelete, apiv1.PersistentVolumeReclaimDelete, defaultStorageProfile)
		Expect(err).NotTo(HaveOccurred())
		Expect(deleteStorageClass).NotTo(BeNil())
	})
	//case: under delete policy
	It("Create a disk using VCD API calls and Set up a PV based on the disk using delete policy", func() {
		err := utils.CreateDisk(tc.VcdClient, testDiskName, smallDiskSizeMB, defaultStorageProfile)
		Expect(err).NotTo(HaveOccurred())
		By("Disk is created successfully from VCD")

		pv, err = tc.CreatePV(ctx, testDiskName, storageClassDelete, defaultStorageProfile, storageSize, apiv1.PersistentVolumeReclaimDelete)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())
		By("Static PV is created successfully from VCD")

		pvc, err := tc.CreatePVC(ctx, testNameSpaceName, testStaticPVCName, storageClassDelete, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())
		By("PVC is created successfully")

		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())
		By("PVC status is 'bound'")

	})
	//case: under delete policy
	It("Deployment should be ready using a PVC connected to the above PV", func() {
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

		By("Deployment is ready")
		err = tc.WaitForDeploymentReady(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
	})
	//case: under delete policy
	It("PV is present in kubernetes and VCD after PVC AND Deployment is deleted", func() {
		err = tc.DeletePVC(ctx, testNameSpaceName, testStaticPVCName)
		By("PVC is deleted successfully")

		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		By("Deployment is deleted successfully")

		pv, err = tc.GetPV(ctx, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())
		By("PV is presented in Kubernetes")

		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())
		By("PV is verified in VCD")
	})
	//case: under delete policy
	It("VCD Disk should exists after PV gets deleted in RDE", func() {
		By("should delete PV successfully")
		err = tc.DeletePV(ctx, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("Verify PV is deleted in Kubernetes")
		pv, err = tc.GetPV(ctx, testDiskName)
		Expect(err).To(HaveOccurred())
		Expect(pv).To(BeNil())

		By("PV is presented in VCD")
		vcdDisk, err := utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())

		By("should delete vcd disk successfully")
		err = utils.DeleteDisk(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("Verify disk not found in VCD")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).To(MatchError(govcd.ErrorEntityNotFound))
		Expect(vcdDisk).To(BeNil())
	})
	//case: under retain policy
	It("Create a disk using VCD API calls and Set up a PV based on the disk using retain policy", func() {
		err := utils.CreateDisk(tc.VcdClient, testDiskName, smallDiskSizeMB, defaultStorageProfile)
		Expect(err).NotTo(HaveOccurred())
		By("Disk is created successfully from VCD")
		pv, err = tc.CreatePV(ctx, testDiskName, storageClassRetain, defaultStorageProfile, storageSize, apiv1.PersistentVolumeReclaimRetain)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		pv, err = tc.GetPV(ctx, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())
		By("PV is created successfully from VCD")
		pvc, err := tc.CreatePVC(ctx, testNameSpaceName, testStaticPVCName, storageClassRetain, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())
		By("PVC is created successfully")
	})
	//case: under retain policy
	It("Deployment should be ready using a PVC connected to the above PV", func() {

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
		By("Deployment is created successfully")

		err = tc.WaitForDeploymentReady(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
		By("The pods of the deployment come up.")

		//pvFound, err = GetPVByNameViaRDE(testDiskName, tc, "named-disk")
		//Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		//Expect(pvFound).To(BeFalse())
		//By("PV is not shown in RDE")

	})
	//case: under retain policy
	It("PV is present in kubernetes and VCD after PVC AND Deployment is deleted", func() {
		err = tc.DeletePVC(ctx, testNameSpaceName, testStaticPVCName)
		By("PVC is deleted successfully")
		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		By("Deployment is deleted successfully")
		pv, err = tc.GetPV(ctx, testDiskName)
		By("PV is presented in Kubernetes")
		vcdDisk, err := utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())
		By("PV is verified in VCD")
	})
	//case: under retain policy
	It("VCD Disk should exists after PV gets deleted in RDE", func() {
		By("should delete the PV successfully")
		err = tc.DeletePV(ctx, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("PV is deleted in Kubernetes")
		pv, err = tc.GetPV(ctx, testDiskName)
		Expect(err).To(HaveOccurred())
		Expect(pv).To(BeNil())

		By("Disk is present in VCD")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())

		By("Delete the vcd disk")
		err = utils.DeleteDisk(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("Verify vcd disk deleted ")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).To(MatchError(govcd.ErrorEntityNotFound))
		Expect(vcdDisk).To(BeNil())
	})

	It("Should clean up all the storageClass and namespace", func() {
		By("delete the retain storage class")
		err = tc.DeleteStorageClass(ctx, storageClassRetain)
		Expect(err).NotTo(HaveOccurred())

		By("delete the delete storage class")
		err = tc.DeleteStorageClass(ctx, storageClassDelete)
		Expect(err).NotTo(HaveOccurred())

		By("delete the test nameSpace")
		err = tc.DeleteNameSpace(ctx, testNameSpaceName)
		Expect(err).NotTo(HaveOccurred())
	})
})
