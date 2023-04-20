package e2e

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vmware/cloud-director-named-disk-csi-driver/tests/utils"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const xfsFsType = "xfs"
const mountPath = "/data"

var _ = Describe("CSI dynamic provisioning Test", func() {
	var (
		tc            *testingsdk.TestClient
		err           error
		dynamicPVName string
		//vcdDisk       *vcdtypes.Disk
		//pv            *apiv1.PersistentVolume
		//pvDeleted     bool
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
		xfsStorageClass, err := utils.CreateStorageClass(ctx, tc.Cs.(*kubernetes.Clientset), storageClassXfs, apiv1.PersistentVolumeReclaimDelete, defaultStorageProfile, xfsFsType)
		Expect(err).NotTo(HaveOccurred())
		Expect(xfsStorageClass).NotTo(BeNil())
	})

	It("should create PVC and PV using retain reclaim policy", func() {
		By("should create the PVC successfully")
		pvc, err := utils.CreatePVC(ctx, tc.Cs.(*kubernetes.Clientset), testNameSpaceName, testDeletePVCName, storageClassXfs, storageSize)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).NotTo(BeNil())

		By("PVC status should be 'Bound'")
		err = utils.WaitForPvcReady(ctx, tc.Cs.(*kubernetes.Clientset), testNameSpaceName, testDeletePVCName)
		Expect(err).NotTo(HaveOccurred())

		By("PVC should be presented in kubernetes")
		pvc, err = utils.GetPVC(ctx, tc.Cs.(*kubernetes.Clientset), testNameSpaceName, testDeletePVCName)
		dynamicPVName = pvc.Spec.VolumeName
		Expect(dynamicPVName).NotTo(BeEmpty())

		By(fmt.Sprintf("PV [%s] should be presented in Kubernetes", dynamicPVName))
		pv, err := utils.GetPV(ctx, tc.Cs.(*kubernetes.Clientset), dynamicPVName)
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
				"app": testDeploymentName,
			},
			ContainerParams: testingsdk.ContainerParams{
				ContainerName:  "nginx",
				ContainerImage: "nginx:1.14.2",
				ContainerPort:  80,
			},
			VolumeParams: testingsdk.VolumeParams{
				VolumeName: "nginx-deployment-volume",
				PvcRef:     testDeletePVCName,
				MountPath:  "/data",
			},
		}, testNameSpaceName)
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment).NotTo(BeNil())
		By("PVC status should be 'bound'")
		err = utils.WaitForPvcReady(ctx, tc.Cs.(*kubernetes.Clientset), testNameSpaceName, testDeletePVCName)
		Expect(err).NotTo(HaveOccurred())

		By("Deployment should be ready")
		err = tc.WaitForDeploymentReady(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())
		options := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app=%s", testDeploymentName),
		}
		podList, err := tc.Cs.CoreV1().Pods(testNameSpaceName).List(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		By(podList.Items[0].Name)
		//time.Sleep(1 * time.Minute)
		output, err := utils.ExecCmdExample(tc.Cs, tc.Config, testNameSpaceName, podList.Items[0].Name, []string{"df -Th"})
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("output: %s", output))
		fsTypeFound, err := utils.FindFsTypeWithMountPath(output, mountPath, "xfs")
		Expect(err).NotTo(HaveOccurred())
		Expect(fsTypeFound).To(BeTrue())
	})

	//scenario 2: use 'Delete' retention policy. step3: verify the PV is not presented after PVC deleted.
	It("PV resource should get deleted after PVC is deleted in kubernetes", func() {
		By("Should delete deployment successfully in Kubernetes")
		err = tc.DeleteDeployment(ctx, testNameSpaceName, testDeploymentName)
		Expect(err).NotTo(HaveOccurred())

		By("should delete PVC successfully in Kubernetes")
		err = utils.DeletePVC(ctx, tc.Cs.(*kubernetes.Clientset), testNameSpaceName, testDeletePVCName)
		Expect(err).NotTo(HaveOccurred())

		By("delete the retain storage class")
		err = tc.DeleteStorageClass(ctx, storageClassXfs)
		Expect(err).NotTo(HaveOccurred())

		By("delete the test nameSpace")
		err = tc.DeleteNameSpace(ctx, testNameSpaceName)
		Expect(err).NotTo(HaveOccurred())
	})
})
