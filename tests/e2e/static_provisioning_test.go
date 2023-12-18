package e2e

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdtypes"
	"github.com/vmware/cloud-director-named-disk-csi-driver/tests/utils"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	testDiskName        = "test-named-disk"
	testStaticPVCName   = "test-static-pvc"
	testStaticNameSpace = "static-test-ns-2"
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
		ns, err := tc.CreateNameSpace(ctx, testStaticNameSpace)
		Expect(err).NotTo(HaveOccurred())
		Expect(ns).NotTo(BeNil())
		retainStorageClass, err := utils.CreateStorageClass(ctx, tc.Cs.(*kubernetes.Clientset), storageClassRetain,
			apiv1.PersistentVolumeReclaimRetain, defaultStorageProfile, storageClassExt4, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(retainStorageClass).NotTo(BeNil())
		deleteStorageClass, err := utils.CreateStorageClass(ctx, tc.Cs.(*kubernetes.Clientset), storageClassDelete,
			apiv1.PersistentVolumeReclaimDelete, defaultStorageProfile, storageClassExt4, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(deleteStorageClass).NotTo(BeNil())
	})

	//scenario 1: use 'Delete' retention policy. step1: create VCD named-disk and PV.
	It("should create a disk using VCD API calls and set up a PV based on the disk", func() {
		By("should create the disk successfully from VCD")
		err = utils.CreateDisk(tc.VcdClient, testDiskName, smallDiskSizeMB, defaultStorageProfile)
		Expect(err).NotTo(HaveOccurred())

		By("should create the static PV successfully in kubernetes")
		pv, err = utils.CreatePV(ctx, tc.Cs.(*kubernetes.Clientset), testDiskName, storageClassDelete, defaultStorageProfile, storageSize, apiv1.PersistentVolumeReclaimDelete)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV should not be shown in RDE")
		pvFound, err := utils.GetPVByNameViaRDE(testDiskName, tc, "named-disk")
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(pvFound).To(BeFalse())

		By("should create the PVC successfully in kubernetes")
		pvc, err := utils.CreatePVC(ctx, tc.Cs.(*kubernetes.Clientset), testStaticNameSpace, testStaticPVCName, storageClassDelete, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())

		By("PVC status should be 'bound'")
		err = utils.WaitForPvcReady(ctx, tc.Cs.(*kubernetes.Clientset), testStaticNameSpace, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 1: use 'Delete' retention policy. step2: install a deployment using the above PVC.
	It("should create a deployment using a PVC connected to the above PV", func() {
		By("should create the deployment successfully")
		deployment, err := utils.CreateDeployment(ctx, tc, testDeploymentName, testDiskName, ContainerImage, testStaticPVCName, utils.InitContainerMountPath, testStaticNameSpace)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())

		By("Deployment should be ready")
		err = tc.WaitForDeploymentReady(ctx, testStaticNameSpace, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 1: use 'Delete' retention policy. step3: PV should not be presented in kubernetes and VCD after PVC deleted
	It("PV should be presented in kubernetes and VCD after PVC AND Deployment is deleted", func() {
		// We should delete the Deployment first as it has a dependency on the PVC.
		By("should delete the deployment successfully")
		err = tc.DeleteDeployment(ctx, testStaticNameSpace, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the PVC successfully")
		err = utils.DeletePVC(ctx, tc.Cs.(*kubernetes.Clientset), testStaticNameSpace, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())

		By("should wait until Disk deleted within the time constraint")
		err = utils.WaitDiskDeleteViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should be not presented in Kubernetes")
		pvDeleted, err = utils.WaitForPVDeleted(ctx, tc.Cs.(*kubernetes.Clientset), testDiskName)
		Expect(pvDeleted).To(BeTrue())
		Expect(err).NotTo(HaveOccurred())
		pv, err = utils.GetPV(ctx, tc.Cs.(*kubernetes.Clientset), testDiskName)
		Expect(err).To(HaveOccurred())
		Expect(pv).To(BeNil())

		By("PV should not be shown in RDE")
		pvFound, err := utils.GetPVByNameViaRDE(testDiskName, tc, "named-disk")
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(pvFound).To(BeFalse())
	})

	//scenario 2: use 'Retain' retention policy. step1: create VCD named-disk and PV.
	It("Create a disk using VCD API calls and Set up a PV based on the disk using retain policy", func() {
		By("should create the disk successfully from VCD")
		err := utils.CreateDisk(tc.VcdClient, testDiskName, smallDiskSizeMB, defaultStorageProfile)
		Expect(err).NotTo(HaveOccurred())

		By("should create the static PV successfully in kubernetes")
		pv, err = utils.CreatePV(ctx, tc.Cs.(*kubernetes.Clientset), testDiskName, storageClassRetain, defaultStorageProfile, storageSize, apiv1.PersistentVolumeReclaimRetain)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV should be presented in kubernetes")
		pv, err = utils.GetPV(ctx, tc.Cs.(*kubernetes.Clientset), testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(pv).NotTo(BeNil())

		By("PV should not be shown in RDE")
		pvFound, err := utils.GetPVByNameViaRDE(testDiskName, tc, "named-disk")
		Expect(err).To(MatchError(testingsdk.ResourceNotFound))
		Expect(pvFound).To(BeFalse())

		By("should create the PVC successfully in kubernetes")
		pvc, err := utils.CreatePVC(ctx, tc.Cs.(*kubernetes.Clientset), testStaticNameSpace, testStaticPVCName, storageClassRetain, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())

		By("PVC status should be 'bound'")
		err = utils.WaitForPvcReady(ctx, tc.Cs.(*kubernetes.Clientset), testStaticNameSpace, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 2: use 'Retain' retention policy. step2: install a deployment using the above PVC.
	It("should create a deployment using a PVC connected to the above PV", func() {
		By("deployment should be created successfully")
		deployment, err := utils.CreateDeployment(ctx, tc, testDeploymentName, testDiskName, ContainerImage, testStaticPVCName, utils.InitContainerMountPath, testStaticNameSpace)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())

		By("pods of the deployment should come up.")
		err = tc.WaitForDeploymentReady(ctx, testStaticNameSpace, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
	})

	//scenario 2: use 'Retain' retention policy. step3: PV should be presented in kubernetes and VCD after PVC deleted
	It("PV is present in kubernetes and VCD after PVC AND Deployment is deleted", func() {
		// We should delete the Deployment first as it has a dependency on the PVC.
		By("should delete the deployment successfully")
		err = tc.DeleteDeployment(ctx, testStaticNameSpace, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the PVC successfully")
		err = utils.DeletePVC(ctx, tc.Cs.(*kubernetes.Clientset), testStaticNameSpace, testStaticPVCName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should be presented in Kubernetes")
		pv, err = utils.GetPV(ctx, tc.Cs.(*kubernetes.Clientset), testDiskName)
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
		err = utils.DeletePV(ctx, tc.Cs.(*kubernetes.Clientset), testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("PV should not be presented in Kubernetes")
		pv, err = utils.GetPV(ctx, tc.Cs.(*kubernetes.Clientset), testDiskName)
		Expect(err).To(HaveOccurred())
		Expect(pv).To(BeNil())

		By("Disk should be present in VCD")
		vcdDisk, err = utils.GetDiskByNameViaVCD(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vcdDisk).NotTo(BeNil())

		By("clean up the remaining name-disk in VCD and namespace and storage classes in kubernetes")
		err = utils.DeleteDisk(tc.VcdClient, testDiskName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the retain storage class")
		err = utils.DeleteStorageClass(ctx, tc.Cs.(*kubernetes.Clientset), storageClassRetain)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the delete storage class")
		err = utils.DeleteStorageClass(ctx, tc.Cs.(*kubernetes.Clientset), storageClassDelete)
		Expect(err).NotTo(HaveOccurred())

		By("should delete the test nameSpace")
		err = tc.DeleteNameSpace(ctx, testStaticNameSpace)
		Expect(err).NotTo(HaveOccurred())
	})
})
