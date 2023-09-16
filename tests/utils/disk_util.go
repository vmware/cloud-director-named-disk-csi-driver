package utils

import (
	"context"
	"errors"
	"fmt"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/util"
	csiClient "github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdcsiclient"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdtypes"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"github.com/vmware/go-vcloud-director/v2/types/v56"
	"k8s.io/apimachinery/pkg/util/wait"
	"net/http"
	"strings"
)

const (
	CSIVersion          = "1.3.0"
	MaxVCDUpdateRetries = 10
)

var (
	VcdResourceSetNotFound = errors.New("vcdResourceSet field not found in status->[csi] of RDE")
)

func DeleteDisk(vcdClient *vcdsdk.Client, diskName string) error {
	disk, err := GetDiskByNameViaVCD(vcdClient, diskName)
	if err != nil {
		if err == govcd.ErrorEntityNotFound {
			return err
		}
		return fmt.Errorf("unable to find disk with name [%s]: [%v]", diskName, err)
	}
	err = ValidateNoAttachedVM(vcdClient, disk)
	if err != nil {
		return fmt.Errorf("unable to validate there is no VM attached to disk [%s]: [%v]", diskName, err)
	}
	var deleteDiskLink *types.Link
	for _, diskLink := range disk.Link {
		if diskLink.Rel == types.RelRemove {
			deleteDiskLink = diskLink
			break
		}
	}

	if deleteDiskLink == nil {
		return fmt.Errorf("could not find request URL for delete disk in disk Link")
	}

	// Return the task
	task, err := vcdClient.VCDClient.Client.ExecuteTaskRequestWithApiVersion(deleteDiskLink.HREF, http.MethodDelete,
		"", "error delete disk: %s", nil,
		vcdClient.VCDClient.Client.APIVersion)
	if err != nil {
		return fmt.Errorf("unable to issue delete disk call for [%s]: [%v]", diskName, err)
	}

	err = task.WaitTaskCompletion()
	if err != nil {
		return fmt.Errorf("failed to wait for deletion task of disk [%s]: [%v]", diskName, err)
	}
	return nil
}

func RemoveDiskViaRDE(vcdClient *vcdsdk.Client, diskName string, clusterId string) error {
	rdeManager := vcdsdk.NewRDEManager(vcdClient, clusterId, util.CSIName, CSIVersion)
	rdeError := rdeManager.RemoveFromVCDResourceSet(context.Background(), vcdsdk.ComponentCSI, util.ResourcePersistentVolume, diskName)
	if rdeError != nil {
		return fmt.Errorf("failed to remove persistent volume [%s] from VCDResourceSet of RDE [%s]", diskName, rdeManager.ClusterID)
	}
	return nil
}

func ValidateNoAttachedVM(vcdClient *vcdsdk.Client, disk *vcdtypes.Disk) error {
	var attachedVMLink *types.Link

	// Find the proper link for request
	for _, diskLink := range disk.Link {
		if diskLink.Type == types.MimeVMs {
			attachedVMLink = diskLink
			break
		}
	}

	if attachedVMLink == nil {
		return fmt.Errorf("could not find request URL for attached vm in disk Link")
	}

	// Decode request
	attachedVMs := vcdtypes.Vms{}

	_, err := vcdClient.VCDClient.Client.ExecuteRequestWithApiVersion(attachedVMLink.HREF, http.MethodGet,
		attachedVMLink.Type, "error getting attached vms: %s", nil, &attachedVMs,
		vcdClient.VCDClient.Client.APIVersion)
	if err != nil {
		return err
	}
	if attachedVMs.VmReference != nil && len(attachedVMs.VmReference) > 0 {
		return fmt.Errorf("error getting attached vm references: %v", &attachedVMs.VmReference)
	}
	return nil
}

func CreateDisk(vcdClient *vcdsdk.Client, diskName string, diskSizeMB int64, storageProfileName string) error {
	spRef, err := vcdClient.VDC.FindStorageProfileReference(storageProfileName)
	d := &vcdtypes.Disk{
		Name:           diskName,
		SizeMb:         diskSizeMB,
		BusType:        csiClient.VCDBusTypeSCSI,
		BusSubType:     csiClient.VCDBusSubTypeVirtualSCSI,
		Description:    "",
		Shareable:      false,
		StorageProfile: &spRef,
	}

	diskCreateParams := &vcdtypes.DiskCreateParams{
		Xmlns: types.XMLNamespaceVCloud,
		Disk:  d,
	}

	var createDiskLink *types.Link
	for _, vdcLink := range vcdClient.VDC.Vdc.Link {
		if vdcLink.Rel == types.RelAdd && vdcLink.Type == types.MimeDiskCreateParams {
			createDiskLink = vdcLink
			break
		}
	}
	newDisk := vcdtypes.Disk{}
	_, err = vcdClient.VCDClient.Client.ExecuteRequestWithApiVersion(createDiskLink.HREF, http.MethodPost,
		createDiskLink.Type, "error creating Disk with params: [%#v]", diskCreateParams, &newDisk,
		vcdClient.VCDClient.Client.APIVersion)
	return err
}

func GetPVByNameViaRDE(pvName string, tc *testingsdk.TestClient, resourceType string) (bool, error) {
	resourceSet, err := testingsdk.GetVCDResourceSet(context.TODO(), tc.VcdClient, tc.ClusterId, vcdsdk.ComponentCSI)
	if err != nil {
		if strings.Contains(err.Error(), VcdResourceSetNotFound.Error()) {
			return false, testingsdk.ResourceNotFound
		}
		return false, fmt.Errorf("error occurred while getting resource set in cluster [%s]: [%v]", tc.ClusterId, err)
	}
	updatedVcdResourceSet := make([]vcdsdk.VCDResource, 0)
	for _, resource := range resourceSet {
		if resource.Name == pvName && resource.Type == resourceType {
			updatedVcdResourceSet = append(updatedVcdResourceSet, resource)
			continue
		}
	}
	if len(updatedVcdResourceSet) == 1 {
		return true, nil
	} else if len(updatedVcdResourceSet) > 1 {
		return false, fmt.Errorf("get more than one PV [%s] in cluster [%s]", pvName, tc.ClusterId)
	}

	return false, testingsdk.ResourceNotFound
}

func VerifyDiskViaVCD(vcdClient *vcdsdk.Client, diskName string) (*vcdtypes.Disk, error) {
	var disk *vcdtypes.Disk
	err := wait.PollImmediate(defaultLongRetryInterval, defaultLongRetryTimeout, func() (bool, error) {
		diskFound, err := GetDiskByNameViaVCD(vcdClient, diskName)
		if err != nil {
			if err == govcd.ErrorEntityNotFound {
				return false, nil
			}
			return false, fmt.Errorf("error occurred while getting disk [%s] from VCD: %v", diskName, err)
		}
		disk = diskFound
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return disk, nil
}

func WaitDiskDeleteViaVCD(vcdClient *vcdsdk.Client, diskName string) error {
	err := wait.PollImmediate(defaultLongRetryInterval, defaultLongRetryTimeout, func() (bool, error) {
		_, err := GetDiskByNameViaVCD(vcdClient, diskName)
		if err != nil {
			if err == govcd.ErrorEntityNotFound {
				return true, nil
			}
			return false, fmt.Errorf("error occurred while getting disk [%s] from VCD: %v", diskName, err)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}

func GetDiskByNameViaVCD(vcdClient *vcdsdk.Client, diskName string) (disk *vcdtypes.Disk, err error) {
	if vcdClient.VDC == nil {
		return nil, fmt.Errorf("error occurred while getting vcdClient VDC, [%v]", err)
	}

	for i := 0; i < MaxVCDUpdateRetries; i++ {
		var diskList []vcdtypes.Disk
		err = vcdClient.VDC.Refresh()
		if err != nil {
			return nil, fmt.Errorf("error when refreshing by disk id %s, [%v]", diskName, err)
		}
		for _, resourceEntities := range vcdClient.VDC.Vdc.ResourceEntities {
			for _, resourceEntity := range resourceEntities.ResourceEntity {
				if resourceEntity.Name == diskName && resourceEntity.Type == "application/vnd.vmware.vcloud.disk+xml" {
					disk, err := getDiskByHref(vcdClient, resourceEntity.HREF)
					if err != nil {
						return nil, err
					}
					diskList = append(diskList, *disk)
				}
			}
		}
		if len(diskList) == 0 {
			fmt.Printf("Retrying get of disk [%s] from VCD, [%d] remaining attempts\n", diskName, vcdsdk.MaxRDEUpdateRetries-i-1)
			continue
		}
		return &diskList[0], nil
	}
	return nil, govcd.ErrorEntityNotFound
}

func getDiskByHref(vcdClient *vcdsdk.Client, diskHref string) (*vcdtypes.Disk, error) {
	disk := &vcdtypes.Disk{}

	_, err := vcdClient.VCDClient.Client.ExecuteRequestWithApiVersion(diskHref, http.MethodGet,
		"", "error retrieving Disk: %#v", nil, disk,
		vcdClient.VCDClient.Client.APIVersion)
	if err != nil && strings.Contains(err.Error(), "MajorErrorCode:403") {
		return nil, govcd.ErrorEntityNotFound
	}
	if err != nil {
		return nil, err
	}
	return disk, nil
}

func ValidateDiskQuotaError(err error) bool {
	exceedQuotaErrorMsg := "The requested operation will exceed the VDC's storage quota: storage policy"
	if err == nil {
		return false
	}
	return strings.Contains(fmt.Sprint(err), exceedQuotaErrorMsg)
}
