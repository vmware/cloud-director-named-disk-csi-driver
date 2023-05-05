package utils

import (
	"context"
	"fmt"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	apiv1 "k8s.io/api/core/v1"
	stov1 "k8s.io/api/storage/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"strings"
	"time"
)

const (
	defaultLongRetryInterval = 20 * time.Second
	defaultLongRetryTimeout  = 300 * time.Second
)

func WaitForPvcReady(ctx context.Context, k8sClient *kubernetes.Clientset, nameSpace string, pvcName string) error {
	err := wait.PollImmediate(defaultRetryInterval, defaultRetryTimeout, func() (bool, error) {
		ready := false
		pvc, err := GetPVC(ctx, k8sClient, nameSpace, pvcName)
		if err != nil {
			if testingsdk.IsRetryableError(err) {
				return false, nil
			}
			return false, fmt.Errorf("unexpected error occurred while getting pvc [%s]", pvcName)
		}
		if pvc != nil && pvc.Status.Phase == apiv1.ClaimBound {
			ready = true
		}
		if !ready {
			fmt.Printf("pvc %s is not bound\n", pvc.Name)
			return false, nil
		}
		return true, nil
	})
	return err
}

func WaitForPVDeleted(ctx context.Context, k8sClient *kubernetes.Clientset, pvName string) (bool, error) {
	err := wait.PollImmediate(defaultRetryInterval, defaultRetryTimeout, func() (bool, error) {
		_, err := GetPV(ctx, k8sClient, pvName)
		if err != nil {
			if err == testingsdk.ResourceNotFound {
				return true, nil
			}
			if testingsdk.IsRetryableError(err) {
				return false, nil
			}
			return false, fmt.Errorf("unexpected error occurred while getting persistentVolume [%s]", pvName)
		}
		return false, nil
	})
	if err != nil {
		return false, fmt.Errorf("error occurred while checking PV status: %v", err)
	}
	return true, nil
}

func WaitForPVCDeleted(ctx context.Context, k8sClient *kubernetes.Clientset, nameSpace string, pvcName string) (bool, error) {
	err := wait.PollImmediate(defaultRetryInterval, defaultRetryTimeout, func() (bool, error) {
		_, err := GetPVC(ctx, k8sClient, pvcName, nameSpace)
		if err != nil {
			if err == testingsdk.ResourceNotFound {
				return true, nil
			}
			if testingsdk.IsRetryableError(err) {
				return false, nil
			}
			return false, fmt.Errorf("unexpected error occurred while getting persistentVolumeClaim [%s]", pvcName)
		}
		return false, nil
	})
	if err != nil {
		return false, fmt.Errorf("error occurred while checking PVC status: %v", err)
	}
	return true, nil
}

func waitForStorageClassDeleted(ctx context.Context, k8sClient *kubernetes.Clientset, scName string) (bool, error) {
	err := wait.PollImmediate(defaultLongRetryInterval, defaultLongRetryTimeout, func() (bool, error) {
		_, err := k8sClient.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
		if err != nil {
			if apierrs.IsNotFound(err) {
				return true, nil
			}
			if testingsdk.IsRetryableError(err) {
				return false, nil
			}
			return false, fmt.Errorf("unexpected error occurred while getting storage class [%s]")
		}
		return false, nil
	})
	if err != nil {
		return false, fmt.Errorf("error occurred while checking namespace status: %v", err)
	}
	return true, nil
}

func GetPV(ctx context.Context, k8sClient *kubernetes.Clientset, pvName string) (*apiv1.PersistentVolume, error) {
	if pvName == "" {
		return nil, testingsdk.ResourceNameNull
	}
	pv, err := k8sClient.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, testingsdk.ResourceNotFound
		}
		return nil, err
	}
	return pv, nil
}

func GetPVC(ctx context.Context, k8sClient *kubernetes.Clientset, nameSpace string, pvcName string) (*apiv1.PersistentVolumeClaim, error) {
	if pvcName == "" {
		return nil, testingsdk.ResourceNameNull
	}
	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(nameSpace).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, testingsdk.ResourceNotFound
		}
		return nil, err
	}
	return pvc, nil
}

func GetStorageClass(ctx context.Context, k8sClient *kubernetes.Clientset, scName string) (*stov1.StorageClass, error) {
	if scName == "" {
		return nil, testingsdk.ResourceNameNull
	}
	sc, err := k8sClient.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			return nil, testingsdk.ResourceNotFound
		}
		return nil, err
	}
	return sc, nil
}

func CreatePV(ctx context.Context, k8sClient *kubernetes.Clientset, persistentVolumeName string, storageClass string, storageProfile string, storageSize string, reclaimPolicy apiv1.PersistentVolumeReclaimPolicy) (*apiv1.PersistentVolume, error) {
	if persistentVolumeName == "" {
		return nil, testingsdk.ResourceNameNull
	}
	persistentVolumeFilesystem := apiv1.PersistentVolumeFilesystem
	pv := &apiv1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: persistentVolumeName,
			Annotations: map[string]string{
				"pv.kubernetes.io/provisioned-by": "named-disk.csi.cloud-director.vmware.com",
			},
		},
		Spec: apiv1.PersistentVolumeSpec{
			StorageClassName: storageClass,
			AccessModes: []apiv1.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			PersistentVolumeSource: apiv1.PersistentVolumeSource{
				CSI: &apiv1.CSIPersistentVolumeSource{
					Driver:       "named-disk.csi.cloud-director.vmware.com",
					FSType:       "ext4",
					VolumeHandle: persistentVolumeName,
					VolumeAttributes: map[string]string{
						"busSubType":     "VirtualSCSI",
						"busType":        "SCSI",
						"filesystem":     "ext4",
						"storageProfile": storageProfile,
					},
				},
			},
			Capacity: apiv1.ResourceList{
				"storage": resource.MustParse(storageSize),
			},
			VolumeMode:                    &persistentVolumeFilesystem,
			PersistentVolumeReclaimPolicy: reclaimPolicy,
		},
	}
	newPV, err := k8sClient.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error occurred while creating persistent volume [%s]: [%v]", persistentVolumeName, err)
	}
	return newPV, nil
}

func CreatePVC(ctx context.Context, k8sClient *kubernetes.Clientset, nameSpace string, pvcName string, storageClass string, storageSize string) (*apiv1.PersistentVolumeClaim, error) {
	if pvcName == "" {
		return nil, testingsdk.ResourceNameNull
	}
	if nameSpace == "" {
		nameSpace = apiv1.NamespaceDefault
	}
	var storageClassName = storageClass
	pvc := &apiv1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: nameSpace,
		},
		Spec: apiv1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			AccessModes: []apiv1.PersistentVolumeAccessMode{
				apiv1.ReadWriteOnce,
			},
			Resources: apiv1.ResourceRequirements{
				Requests: apiv1.ResourceList{
					apiv1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	newPVC, err := k8sClient.CoreV1().PersistentVolumeClaims(nameSpace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error occurred while creating persistent volume claim [%s]: [%v]", pvcName, err)
	}
	return newPVC, nil
}

func CreateStorageClass(ctx context.Context, k8sClient *kubernetes.Clientset, scName string, reclaimPolicy apiv1.PersistentVolumeReclaimPolicy, storageProfile string, fsType string) (*stov1.StorageClass, error) {
	if scName == "" {
		return nil, testingsdk.ResourceNameNull
	}
	sc := &stov1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: scName,
			Annotations: map[string]string{
				"storageclass.kubernetes.io/is-default-class": "false",
			},
		},
		ReclaimPolicy: &reclaimPolicy,
		Provisioner:   "named-disk.csi.cloud-director.vmware.com",
		Parameters: map[string]string{
			"storageProfile": storageProfile,
			"filesystem":     fsType,
		},
	}
	newSC, err := k8sClient.StorageV1().StorageClasses().Create(ctx, sc, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("error occurred while creating new storageclass [%s]: %v", scName, err)
	}
	return newSC, nil
}

func DeletePVC(ctx context.Context, k8sClient *kubernetes.Clientset, nameSpace string, pvcName string) error {
	if pvcName == "" {
		return testingsdk.ResourceNameNull
	}
	_, err := GetPVC(ctx, k8sClient, nameSpace, pvcName)
	if err != nil {
		if err == testingsdk.ResourceNotFound {
			return fmt.Errorf("the persistentVolumeClaim [%s] does not exist", pvcName)
		}
		return fmt.Errorf("error occurred while deleting persistentVolumeClaim [%s]: [%v]", pvcName, err)
	}
	err = k8sClient.CoreV1().PersistentVolumeClaims(nameSpace).Delete(ctx, pvcName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete persistentVolumeClaim [%s]", pvcName)
	}
	pvcDeleted, err := WaitForPVCDeleted(ctx, k8sClient, nameSpace, pvcName)
	if err != nil {
		return fmt.Errorf("error occurred while deleting persistentVolumeClaim [%s]: [%v]", pvcName, err)
	}
	if !pvcDeleted {
		return fmt.Errorf("persistentVolumeClaim [%s] still exists", pvcName)
	}
	return nil
}

func DeletePV(ctx context.Context, k8sClient *kubernetes.Clientset, pvName string) error {
	_, err := GetPV(ctx, k8sClient, pvName)
	if err != nil {
		return fmt.Errorf("the persistentVolumeClaim [%s] does not exist", pvName)
	}
	err = k8sClient.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete persistentVolume [%s]", pvName)
	}
	pvDeleted, err := WaitForPVDeleted(ctx, k8sClient, pvName)
	if err != nil {
		return fmt.Errorf("error occurred while deleting persistentVolume [%s]: [%v]", pvName, err)
	}
	if !pvDeleted {
		return fmt.Errorf("persistentVolume [%s] still exists", pvName)
	}
	return nil
}

func FindFsTypeWithMountPath(executedOutput string, mountedPath string, fsType string) (bool, error) {
	filesystemSlices := strings.Split(executedOutput, "\n")
	if len(filesystemSlices) < 2 {
		return false, fmt.Errorf("error occurred while parsing filesystemData into multiple lines")
	}
	for _, filesystemSlice := range filesystemSlices[1:] {
		filesystemSet := strings.Fields(filesystemSlice)
		// skip this filesystem line because the data is not formed
		if len(filesystemSet) == 7 {
			if filesystemSet[len(filesystemSet)-1] == mountedPath {
				if fsType == filesystemSet[1] {
					return true, nil
				}
			}
		}

	}
	return false, fmt.Errorf("the desired filesystemType [%s] not found with the mountedPath provided: [%s]", fsType, mountedPath)
}

func DeleteStorageClass(ctx context.Context, k8sClient *kubernetes.Clientset, scName string) error {
	_, err := GetStorageClass(ctx, k8sClient, scName)
	if err != nil {
		if err == testingsdk.ResourceNotFound {
			return fmt.Errorf("the storageClass [%s] does not exist", scName)
		}
		klog.Info("error occurred while getting storageClass [%s]: [%v]", scName, err)
	}
	err = k8sClient.StorageV1().StorageClasses().Delete(ctx, scName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete service [%s]", scName)
	}
	scDeleted, err := waitForStorageClassDeleted(ctx, k8sClient, scName)
	if err != nil {
		return fmt.Errorf("error occurred while deleting storageClass [%s]: [%v]", scName, err)
	}
	if !scDeleted {
		return fmt.Errorf("storageClass [%s] still exists", scName)
	}
	return nil
}
