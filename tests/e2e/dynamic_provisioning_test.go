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
	storageClassDelete    = "delete-storage-class"
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
		pvDeleted     bool
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

	//scenario 1: use 'Retain' retention policy. step1: create PVC and PV.
	It("should create PVC and PV using retain reclaim policy", func() {
		By("should create the PVC successfully")
		pvc, err := tc.CreatePVC(ctx, testNameSpaceName, testRetainPVCName, storageClassRetain, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())

		By("PVC status should be 'Bound'")
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testRetainPVCName)
		Expect(err).NotTo(HaveOccurred())

		By("PVC should be presented in kubernetes")
		pvc, err = tc.GetPVC(ctx, testNameSpaceName, testRetainPVCName)
		dynamicPVName = pvc.Spec.VolumeName
		Expect(dynamicPVName).NotTo(BeEmpty())

		By(fmt.Sprintf("PV [%s] should be presented in Kubernetes", dynamicPVName))
		pv, err := tc.GetPV(ctx, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV is should be presented in VCD")
		vcdDisk, err := utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())

		By("PV is should be presented in RDE")
		pvFound, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).NotTo(HaveOccurred())
		Expect(pvFound).To(BeTrue())
	})

	//scenario 1: use 'Retain' retention policy. step2: install a deployment using the above PVC.
	It("should install a deployment using the above PVC", func() {
		By("should create a deployment successfully")
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

		By("PVC status should be 'bound'")
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testRetainPVCName)
		Expect(err).NotTo(HaveOccurred())

		By("Deployment should be ready")
		err = tc.WaitForDeploymentReady(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 1: use 'Retain' retention policy. step3: delete pvc and deployment and Verify the PV is deleted in VCD
	It("should verify the presence of PV after PVC deletion", func() {
		By("should delete the deployment successfully in Kubernetes")
		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the PV successfully in Kubernetes")
		err = tc.DeletePVC(ctx, testNameSpaceName, testRetainPVCName)
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("PV [%s] should be presented in Kubernetes after PVC is deleted", dynamicPVName))
		pv, err = tc.GetPV(ctx, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV should be presented in VCD after PVC is deleted")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())

		By("PV should be presented in RDE after PVC is deleted")
		pvFound, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).NotTo(HaveOccurred())
		Expect(pvFound).To(BeTrue())
	})

	//scenario 1: use 'Retain' retention policy. step4: PV is still retained in RDE and VCD after PV is removed from kubernetes
	It("PV should be retained in RDE and VCD after PV deletion in kubernetes", func() {
		By("should delete the PV successfully in kubernetes")
		err = tc.DeletePV(ctx, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should be not presented in kubernetes")
		pv, err = tc.GetPV(ctx, dynamicPVName)
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(pv).To(BeNil())

		By("PV should still be retained in VCD after PV is deleted")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())

		By("PV should still be retained in RDE after PVC is deleted")
		pvFound, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).NotTo(HaveOccurred())
		Expect(pvFound).To(BeTrue())

		By("cleaning up the remainder of VCD named-disk")
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

	//scenario 2: use 'Delete' retention policy. step1: create PVC and PV.
	It("Should create PVC and PV using delete reclaim policy", func() {
		By("should create the PVC successfully")
		pvc, err := tc.CreatePVC(ctx, testNameSpaceName, testDeletePVCName, storageClassDelete, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())

		By("PVC status should be 'Bound'")
		err = tc.WaitForPvcReady(ctx, testNameSpaceName, testDeletePVCName)
		Expect(err).NotTo(HaveOccurred())

		By("PVC should be presented in kubernetes")
		pvc, err = tc.GetPVC(ctx, testNameSpaceName, testDeletePVCName)
		dynamicPVName = pvc.Spec.VolumeName
		Expect(dynamicPVName).NotTo(BeEmpty())

		By(fmt.Sprintf("PV [%s] should be presented in Kubernetes", dynamicPVName))
		pv, err := tc.GetPV(ctx, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV shoule be presented in VCD")
		vcdDisk, err := utils.VerifyDiskViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())

		By("PV should be presented in RDE")
		pvFound, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).NotTo(HaveOccurred())
		Expect(pvFound).To(BeTrue())

	})

	//scenario 2: use 'Delete' retention policy. step2: install a deployment using the above PVC.
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

	//scenario 2: use 'Delete' retention policy. step2: verify the PV is not presented after PVC deleted.
	It("PV resource should get deleted after PVC is deleted in kubernetes", func() {
		By("Should delete deployment successfully in Kubernetes")
		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete PVC successfully in Kubernetes")
		err = tc.DeletePVC(ctx, testNameSpaceName, testDeletePVCName)
		Expect(err).NotTo(HaveOccurred())

		By("should wait until Disk deleted within the time constraint")
		err = utils.WaitDiskDeleteViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should be not presented in Kubernetes")

		pvDeleted, err = tc.WaitForPVDeleted(ctx, dynamicPVName)
		Expect(pvDeleted).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())

		By("PV should be not presented in VCD")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, dynamicPVName)
		Expect(err).To(MatchError(govcd.ErrorEntityNotFound))
		Expect(vcdDisk).To(BeNil())
		By("PV should be not presented in RDE")
		found, err := utils.GetPVByNameViaRDE(dynamicPVName, tc, "named-disk")
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(found).To(BeFalse())

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
