package utils

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	apiv1 "k8s.io/api/core/v1"
	stov1 "k8s.io/api/storage/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	defaultLongRetryInterval = 20 * time.Second
	defaultLongRetryTimeout  = 300 * time.Second
)

func WaitForPvcReady(ctx context.Context, k8sClient *kubernetes.Clientset, name, namespace string) error {
	return wait.PollImmediate(defaultRetryInterval, defaultRetryTimeout, func() (bool, error) {
		pvc, err := GetPVC(ctx, k8sClient, name, namespace)
		if err != nil {
			if testingsdk.IsRetryableError(err) {
				return false, nil
			}
			return false, fmt.Errorf("unexpected error occurred while getting pvc [%s]", name)
		}
		if pvc != nil && pvc.Status.Phase == apiv1.ClaimBound {
			return true, nil
		}

		klog.Info("pvc %s is not bound", name)

		return false, nil
	})
}

func WaitForPVDeleted(ctx context.Context, k8sClient *kubernetes.Clientset, name string) (bool, error) {
	err := wait.PollImmediate(defaultRetryInterval, defaultRetryTimeout, func() (bool, error) {
		_, err := GetPV(ctx, k8sClient, name)
		if err != nil {
			if err == testingsdk.ResourceNotFound {
				return true, nil
			}
			if testingsdk.IsRetryableError(err) {
				return false, nil
			}
			return false, fmt.Errorf("unexpected error occurred while getting persistentVolume [%s]", name)
		}
		return false, nil
	})
	if err != nil {
		return false, fmt.Errorf("error occurred while checking PV status: %w", err)
	}
	return true, nil
}

func WaitForPVCDeleted(ctx context.Context, k8sClient *kubernetes.Clientset, name, namespace string) (bool, error) {
	if err := wait.PollImmediate(defaultRetryInterval, defaultRetryTimeout, func() (bool, error) {
		_, err := GetPVC(ctx, k8sClient, name, namespace)
		if err != nil {
			if err == testingsdk.ResourceNotFound {
				return true, nil
			}
			if testingsdk.IsRetryableError(err) {
				return false, nil
			}
			return false, fmt.Errorf("unexpected error occurred while getting persistentVolumeClaim [%s]", name)
		}
		return false, nil
	}); err != nil {
		return false, fmt.Errorf("error occurred while checking PVC status: %w", err)
	}
	return true, nil
}

func waitForStorageClassDeleted(ctx context.Context, k8sClient *kubernetes.Clientset, name string) (bool, error) {
	if err := wait.PollImmediate(defaultLongRetryInterval, defaultLongRetryTimeout, func() (bool, error) {
		if _, err := k8sClient.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{}); err != nil {
			if apierrs.IsNotFound(err) {
				return true, nil
			}
			if testingsdk.IsRetryableError(err) {
				return false, nil
			}
			return false, fmt.Errorf("unexpected error occurred while getting storage class [%s]", name)
		}
		return false, nil
	}); err != nil {
		return false, fmt.Errorf("error occurred while checking namespace status: %w", err)
	}

	return true, nil
}

func GetPV(ctx context.Context, k8sClient *kubernetes.Clientset, name string) (pv *apiv1.PersistentVolume, err error) {
	if name == "" {
		return nil, testingsdk.ResourceNameNull
	}

	if pv, err = k8sClient.CoreV1().PersistentVolumes().Get(ctx, name, metav1.GetOptions{}); err != nil {
		if apierrs.IsNotFound(err) {
			return nil, testingsdk.ResourceNotFound
		}
		return nil, err
	}

	return pv, nil
}

func GetPVC(ctx context.Context, k8sClient *kubernetes.Clientset, name string, namespace string) (pvc *apiv1.PersistentVolumeClaim, err error) {
	if name == "" {
		return nil, testingsdk.ResourceNameNull
	}

	if pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
		if apierrs.IsNotFound(err) {
			return nil, testingsdk.ResourceNotFound
		}
		return nil, err
	}

	return pvc, nil
}

func GetStorageClass(ctx context.Context, k8sClient *kubernetes.Clientset, name string) (sc *stov1.StorageClass, err error) {
	if name == "" {
		return nil, testingsdk.ResourceNameNull
	}

	if sc, err = k8sClient.StorageV1().StorageClasses().Get(ctx, name, metav1.GetOptions{}); err != nil {
		if apierrs.IsNotFound(err) {
			return nil, testingsdk.ResourceNotFound
		}
		return nil, err
	}

	return sc, nil
}

func CreatePV(ctx context.Context, k8sClient *kubernetes.Clientset, reclaimPolicy apiv1.PersistentVolumeReclaimPolicy, name, storageClassName, storageProfileName, storageSize, fsType, busType string) (pv *apiv1.PersistentVolume, err error) {
	if name == "" {
		return nil, testingsdk.ResourceNameNull
	}
	persistentVolumeFilesystem := apiv1.PersistentVolumeFilesystem

	if pv, err = k8sClient.CoreV1().PersistentVolumes().Create(ctx, &apiv1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"pv.kubernetes.io/provisioned-by": "named-disk.csi.cloud-director.vmware.com",
			},
		},
		Spec: apiv1.PersistentVolumeSpec{
			StorageClassName: storageClassName,
			AccessModes: []apiv1.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			PersistentVolumeSource: apiv1.PersistentVolumeSource{
				CSI: &apiv1.CSIPersistentVolumeSource{
					Driver:       "named-disk.csi.cloud-director.vmware.com",
					FSType:       fsType,
					VolumeHandle: name,
					VolumeAttributes: map[string]string{
						"busType":        busType,
						"filesystem":     fsType,
						"storageProfile": storageProfileName,
					},
				},
			},
			Capacity: apiv1.ResourceList{
				"storage": resource.MustParse(storageSize),
			},
			VolumeMode:                    &persistentVolumeFilesystem,
			PersistentVolumeReclaimPolicy: reclaimPolicy,
		},
	}, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("error occurred while creating persistent volume [%s]: %w", name, err)
	}
	return pv, nil
}

func CreatePVC(ctx context.Context, k8sClient *kubernetes.Clientset, name, namespace, storageClass, storageSize string) (pvc *apiv1.PersistentVolumeClaim, err error) {
	if name == "" {
		return nil, testingsdk.ResourceNameNull
	}
	if namespace == "" {
		namespace = apiv1.NamespaceDefault
	}

	if pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, &apiv1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: apiv1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes: []apiv1.PersistentVolumeAccessMode{
				apiv1.ReadWriteOnce,
			},
			Resources: apiv1.ResourceRequirements{
				Requests: apiv1.ResourceList{
					apiv1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("error occurred while creating persistent volume claim [%s]: %w", name, err)
	}
	return pvc, nil
}

func CreateStorageClass(ctx context.Context, k8sClient *kubernetes.Clientset, reclaimPolicy apiv1.PersistentVolumeReclaimPolicy, name, storageProfile, fsType string) (sc *stov1.StorageClass, err error) {
	if name == "" {
		return nil, testingsdk.ResourceNameNull
	}
	if sc, err = k8sClient.StorageV1().StorageClasses().Create(ctx, &stov1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
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
	}, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("error occurred while creating new storageclass [%s]: %w", name, err)
	}

	return sc, nil
}

func DeletePVC(ctx context.Context, k8sClient *kubernetes.Clientset, name string, namespace string) error {
	if name == "" {
		return testingsdk.ResourceNameNull
	}
	if _, err := GetPVC(ctx, k8sClient, name, namespace); err != nil {
		if err == testingsdk.ResourceNotFound {
			return fmt.Errorf("the persistentVolumeClaim [%s] does not exist", name)
		}
		return fmt.Errorf("error occurred while deleting persistentVolumeClaim [%s]: [%v]", name, err)
	}
	if err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete persistentVolumeClaim [%s]", name)
	}

	if pvcDeleted, err := WaitForPVCDeleted(ctx, k8sClient, name, namespace); err != nil {
		return fmt.Errorf("error occurred while deleting persistentVolumeClaim [%s]: %w", name, err)
	} else if !pvcDeleted {
		return fmt.Errorf("persistentVolumeClaim [%s] still exists", name)
	}

	return nil
}

func DeletePV(ctx context.Context, k8sClient *kubernetes.Clientset, name string) error {
	if _, err := GetPV(ctx, k8sClient, name); err != nil {
		return fmt.Errorf("the persistentVolumeClaim [%s] does not exist", name)
	}
	if err := k8sClient.CoreV1().PersistentVolumes().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete persistentVolume [%s]", name)
	}

	if pvDeleted, err := WaitForPVDeleted(ctx, k8sClient, name); err != nil {
		return fmt.Errorf("error occurred while deleting persistentVolume [%s]: %w", name, err)
	} else if !pvDeleted {
		return fmt.Errorf("persistentVolume [%s] still exists", name)
	}

	return nil
}

func FindFsTypeWithMountPath(executedOutput, mountedPath, fsType string) (bool, error) {
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

func DeleteStorageClass(ctx context.Context, k8sClient *kubernetes.Clientset, name string) error {
	if _, err := GetStorageClass(ctx, k8sClient, name); err != nil {
		if err == testingsdk.ResourceNotFound {
			return fmt.Errorf("the storageClass [%s] does not exist", name)
		}
		klog.Info("error occurred while getting storageClass [%s]: %w", name, err)
	}

	if err := k8sClient.StorageV1().StorageClasses().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete service [%s]", name)
	}

	if scDeleted, err := waitForStorageClassDeleted(ctx, k8sClient, name); err != nil {
		return fmt.Errorf("error occurred while deleting storageClass [%s]: %w", name, err)
	} else if !scDeleted {
		return fmt.Errorf("storageClass [%s] still exists", name)
	}

	return nil
}
