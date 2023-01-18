package e2e

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdtypes"
	"github.com/vmware/cloud-director-named-disk-csi-driver/tests/utils"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	apiv1 "k8s.io/api/core/v1"
)

const (
	testNameSpaceName     = "provisioning-test-ns"
	testRetainPVCName     = "test-retain-pvc"
	testDeletePVCName     = "test-delete-pvc"
	testDeploymentName    = "test-deploy-name"
	storageClassDelete    = "delete-sc"
	storageClassRetain    = "retain-storage-class"
	storageSize           = "2Gi"
	defaultStorageProfile = "*"
	volumeName            = "deployment-pv"
)

var _ = Describe("CSI dynamic provisioning Test", func() {
	var (
		tc            *testingsdk.TestClient
		err           error
		dynamicPVName string
		vcdDisk       *vcdtypes.Disk
		pv            *apiv1.PersistentVolume
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
	//under retain policy
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

	//Start retain policy test
	It("Should create PVC and PV using retain reclaim policy", func() {
		pvc, err := tc.CreatePVC(ctx, testNameSpaceName, testRetainPVCName, storageClassRetain, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testRetainPVCName)
		Expect(err).NotTo(HaveOccurred())
		By("PVC is created successfully")
		pvc, err = tc.GetPVC(ctx, testNameSpaceName, testRetainPVCName)
		dynamicPVName = pvc.Spec.VolumeName
		Expect(dynamicPVName).NotTo(BeEmpty())
		pv, err := tc.GetPV(ctx, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())
		By(fmt.Sprintf("PV [%s] is verified in Kubernetes", dynamicPVName))
		vcdDisk, err := utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())
		By("PV is verified in VCD")
		pvFound, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).NotTo(HaveOccurred())
		Expect(pvFound).To(BeTrue())
		By("PV is verified in RDE")
	})

	//under retain policy
	It("Should create deployment using retain reclaim policy", func() {
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
				VolumeName: "nginx-deployment-volume",
				PvcRef:     testRetainPVCName,
				MountPath:  "/init-container-msg-mount-path",
			},
		}, testNameSpaceName)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
		By("Deployment is created in kubernetes")

		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testRetainPVCName)
		Expect(err).NotTo(HaveOccurred())
		By("PVC status is 'bound'")

		err = tc.WaitForDeploymentReady(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
		By("Deployment is ready")
	})

	//under retain policy
	It("PV should still presents after PVC deletion", func() {
		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
		By("Deployment is deleted in Kubernetes")
		err = tc.DeletePVC(ctx, testNameSpaceName, testRetainPVCName)
		Expect(err).NotTo(HaveOccurred())
		By("PVC is deleted in Kubernetes")
		pv, err = tc.GetPV(ctx, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())
		By(fmt.Sprintf("PV [%s] is verified in Kubernetes after PVC is deleted", dynamicPVName))
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())
		By("PV is verified in VCD after PVC is deleted")
		pvFound, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).NotTo(HaveOccurred())
		Expect(pvFound).To(BeTrue())
		By("PV is verified in RDE after PVC is deleted")

	})

	//under retain policy
	It("Disk should still presents after k8s PV deletion", func() {
		err = tc.DeletePV(ctx, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		pv, err = tc.GetPV(ctx, dynamicPVName)
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(pv).To(BeNil())
		By("PV is deleted in Kubernetes")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())
		By("Disk is verified in VCD after PV is deleted")
		pvFound, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).NotTo(HaveOccurred())
		Expect(pvFound).To(BeTrue())
		By("PV is verified in RDE after PVC is deleted")

		err = utils.DeleteDisk(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("Disk [%s] is deleted successfully in VCD", dynamicPVName))
		err = utils.RemoveDiskViaRDE(tc.VcdClient, dynamicPVName, tc.ClusterId)
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("Disk [%s] is deleted successfully in RDE", dynamicPVName))
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).To(MatchError(govcd.ErrorEntityNotFound))
		Expect(vcdDisk).To(BeNil())
		By(fmt.Sprintf("Disk [%s] is not shown after deletion in RDE", dynamicPVName))
		found, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(found).To(BeFalse())
		By(fmt.Sprintf("Disk [%s] is not shown after deletion in RDE", dynamicPVName))

	})

	//Start delete policy test
	It("Should create PVC and PV using delete reclaim policy", func() {

		pvc, err := tc.CreatePVC(ctx, testNameSpaceName, testDeletePVCName, storageClassDelete, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testDeletePVCName)
		Expect(err).NotTo(HaveOccurred())
		By("PVC is created successfully")
		pvc, err = tc.GetPVC(ctx, testNameSpaceName, testDeletePVCName)
		//Todo: Expect
		dynamicPVName = pvc.Spec.VolumeName
		Expect(dynamicPVName).NotTo(BeEmpty())
		pv, err := tc.GetPV(ctx, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())
		By(fmt.Sprintf("PV [%s] is verified in Kubernetes", dynamicPVName))
		vcdDisk, err := utils.VerifyDiskViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())
		By("PV is verified in VCD")
		pvFound, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).NotTo(HaveOccurred())
		Expect(pvFound).To(BeTrue())
		By("PV is verified in RDE")
	})

	//under delete policy
	It("Should create Deployment using delete reclaim policy", func() {
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
				VolumeName: volumeName,
				PvcRef:     testDeletePVCName,
				MountPath:  "/init-container-msg-mount-path",
			},
		}, testNameSpaceName)
		Expect(deployment).NotTo(BeNil())
		Expect(err).NotTo(HaveOccurred())
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testDeletePVCName)
		Expect(err).NotTo(HaveOccurred())
		By("PVC status should be 'bound'")
		err = tc.WaitForDeploymentReady(ctx, testNameSpaceName, testDeletePVCName)
		Expect(err).NotTo(HaveOccurred())
		By("Deployment should be ready")
	})

	//under delete policy
	It("PV resource should get deleted after PV is deleted in kubernetes", func() {
		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
		By("PVC is deleted in Kubernetes")
		pv, err = tc.GetPV(ctx, dynamicPVName)
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(pv).To(BeNil())
		By("PV is deleted in Kubernetes")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).To(MatchError(govcd.ErrorEntityNotFound))
		Expect(vcdDisk).To(BeNil())
		By("PV is deleted in VCD")
		found, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(found).To(BeFalse())
		By("PV is deleted in RDE")
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
