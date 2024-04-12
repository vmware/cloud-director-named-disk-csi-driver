/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdcsiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/util"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdtypes"
	"github.com/vmware/cloud-director-named-disk-csi-driver/version"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient_37_2"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"github.com/vmware/go-vcloud-director/v2/types/v56"
	"k8s.io/klog"
	"net/http"
	"strings"
	"time"
)

type DiskManager struct {
	VCDClient            *vcdsdk.Client
	ClusterID            string
	Org                  *govcd.Org
	IsZoneEnabledCluster bool
	ZoneMap              *vcdsdk.ZoneMap
}

const (
	VCDBusTypeSCSI           = "6"
	VCDBusSubTypeVirtualSCSI = "VirtualSCSI"
	NoRdePrefix              = `NO_RDE_`
)

// Returns a Disk structure as JSON
func prettyDisk(disk vcdtypes.Disk) string {
	if byteBuf, err := json.MarshalIndent(disk, " ", " "); err == nil {
		return fmt.Sprintf("%s\n", string(byteBuf))
	}

	return ""
}

// Create an independent disk in VDC
// Reference: vCloud API Programming Guide for Service Providers vCloud API 35.0 PDF Page 107-108,
// https://vdc-download.vmware.com/vmwb-repository/dcr-public/715b0387-34d7-4568-b2d8-d11454c52d51/944f905e-fa4e-4005-be7d-19c3cea70ffd/vmware_cloud_director_sp_api_guide_35_0.pdf
func (diskManager *DiskManager) createDisk(diskCreateParams *vcdtypes.DiskCreateParams, vdc *govcd.Vdc) (govcd.Task, error) {
	klog.Infof("Create disk, name: %s, size: %dMB \n",
		diskCreateParams.Disk.Name,
		diskCreateParams.Disk.SizeMb,
	)

	if diskCreateParams.Disk.Name == "" {
		return govcd.Task{}, fmt.Errorf("disk name is required")
	}

	if diskCreateParams.Disk.SizeMb < 1 {
		return govcd.Task{}, fmt.Errorf("disk size should be greater than or equal to 1MB")
	}

	var err error
	var createDiskLink *types.Link

	// Find the proper link for request
	for _, vdcLink := range vdc.Vdc.Link {
		if vdcLink.Rel == types.RelAdd && vdcLink.Type == types.MimeDiskCreateParams {
			klog.Infof(
				"Create disk - found the proper link for request, HREF: %s, name: %s, type: %s, id: %s, rel: %s \n",
				vdcLink.HREF,
				vdcLink.Name,
				vdcLink.Type,
				vdcLink.ID,
				vdcLink.Rel)
			createDiskLink = vdcLink
			break
		}
	}
	if createDiskLink == nil {
		return govcd.Task{}, fmt.Errorf("could not find request URL for create disk in vdc Link")
	}

	newDisk := vcdtypes.Disk{}
	resp, err := diskManager.VCDClient.VCDClient.Client.ExecuteRequestWithApiVersion(createDiskLink.HREF, http.MethodPost,
		createDiskLink.Type, "error creating Disk with params: [%#v]", diskCreateParams, &newDisk,
		diskManager.VCDClient.VCDClient.Client.APIVersion)
	if err != nil {
		return govcd.Task{}, fmt.Errorf("unable to post create link [%v]: resp: [%v]: [%v]",
			createDiskLink.HREF, resp, err)
	}

	// Obtain disk task
	if newDisk.Tasks == nil || newDisk.Tasks.Task == nil || len(newDisk.Tasks.Task) == 0 {
		return govcd.Task{}, fmt.Errorf("error cannot find disk creation task in API response")
	}
	klog.Infof("AFTER CREATE DISK\n %s\n", prettyDisk(newDisk))

	// Return the task that is waiting
	newTask := govcd.NewTask(&diskManager.VCDClient.VCDClient.Client)
	newTask.Task = newDisk.Tasks.Task[0]

	return *newTask, nil
}

// CreateDisk will create a new independent disk with params specified
func (diskManager *DiskManager) CreateDisk(diskName string, vdc *govcd.Vdc, sizeMB int64,
	busType string, busSubType string, description string, storageProfile string, shareable bool,
	zm *vcdsdk.ZoneMap) (*vcdtypes.Disk, error) {
	diskManager.VCDClient.RWLock.Lock()
	defer diskManager.VCDClient.RWLock.Unlock()

	klog.Infof("Entered CreateDisk with name [%s] size [%d]MB, storageProfile [%s] shareable[%v]\n",
		diskName, sizeMB, storageProfile, shareable)

	disk, err := diskManager.GetDiskByNameOrId(diskName, zm, diskManager.VCDClient.ClusterOVDCIdentifier)
	if err != nil && !errors.Is(err, govcd.ErrorEntityNotFound) {
		if rdeErr := diskManager.AddToErrorSet(util.DiskQueryError, "", diskName,
			map[string]interface{}{"Detailed Error": fmt.Errorf("unable to query disk [%s]: [%v]",
				diskName, err)}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.DiskQueryError,
				diskManager.ClusterID, rdeErr)
		}
		return nil, fmt.Errorf("unable to check if disk [%s] already exists: [%v]",
			diskName, err)
	}
	if removeErrorRdeErr := diskManager.RemoveFromErrorSet(util.DiskQueryError, "",
		diskName); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", "DiskCreateError",
			diskManager.ClusterID)
	}

	if disk != nil {
		if disk.SizeMb != sizeMB ||
			disk.BusType != busType ||
			disk.BusSubType != busSubType ||
			(storageProfile != "") && (disk.StorageProfile.Name != storageProfile) ||
			disk.Shareable != shareable {
			return nil, fmt.Errorf("disk [%s] already exists but with different properties: [%v]",
				diskName, disk)
		}

		klog.Infof("Disk with name [%s] already exists", diskName)
		return disk, nil
	}

	d := &vcdtypes.Disk{
		Name:        diskName,
		SizeMb:      sizeMB,
		BusType:     busType,
		BusSubType:  busSubType,
		Description: description,
		Shareable:   shareable,
	}

	diskParams := &vcdtypes.DiskCreateParams{
		Xmlns: types.XMLNamespaceVCloud,
		Disk:  d,
	}
	if storageProfile != "" {
		storageReference, err := vdc.FindStorageProfileReference(storageProfile)
		if err != nil {
			return nil, fmt.Errorf("unable to find storage profile [%s] for disk [%s]",
				storageProfile, diskName)
		}

		diskParams.Disk.StorageProfile = &types.Reference{
			HREF: storageReference.HREF,
		}
	}

	task, err := diskManager.createDisk(diskParams, vdc)
	if err != nil {
		return nil, fmt.Errorf("unable to create disk with name [%s] size [%d]MB: [%v]", diskName, sizeMB, err)
	}

	klog.Infof("START: Waiting for creation of disk [%s] size [%d]MB", diskName, sizeMB)
	err = task.WaitTaskCompletion()
	if err != nil {
		return nil, fmt.Errorf("error waiting to finish creation of independent disk: [%v]", err)
	}
	klog.Infof("END  : Waiting for creation of disk [%s] size [%d]MB", diskName, sizeMB)

	diskHref := task.Task.Owner.HREF
	disk, err = diskManager.govcdGetDiskByHref(diskHref)
	if err != nil {
		return nil, fmt.Errorf("unable to find disk with href [%s]: [%v]", diskHref, err)
	}
	if addEventRdeErr := diskManager.AddToEventSet(util.DiskCreateEvent, "", diskName,
		map[string]interface{}{
			"Detailed Info": fmt.Sprintf("Successfully created disk [%s] of size [%d]MB", diskName, sizeMB),
		}); addEventRdeErr != nil {
		klog.Errorf("unable to add event [%s] into [CSI.Events] in RDE [%s]", util.DiskCreateEvent, diskManager.ClusterID)
	}
	klog.Infof("Disk created: [%#v]", disk)

	rdeManager := vcdsdk.NewRDEManager(diskManager.VCDClient, diskManager.ClusterID, util.CSIName, version.Version)
	if diskManager.ClusterID != "" && !strings.HasPrefix(diskManager.ClusterID, NoRdePrefix) {
		if err = diskManager.addPvToRDE(disk.Id, disk.Name, rdeManager); err != nil {
			return nil, vcdsdk.NewNoRDEError(fmt.Sprintf("Unable to add PV Id [%s] to RDE; RDE ID is generated", disk.Id))
		}
	}

	return disk, nil
}

// GetDiskByHref finds a Disk by HREF
// On success, returns a pointer to the Disk structure and a nil error
// On failure, returns a nil pointer and an error
func (diskManager *DiskManager) govcdGetDiskByHref(diskHref string) (*vcdtypes.Disk, error) {
	klog.Infof("[TRACE] Get Disk By Href: %s\n", diskHref)
	disk := &vcdtypes.Disk{}

	_, err := diskManager.VCDClient.VCDClient.Client.ExecuteRequestWithApiVersion(diskHref, http.MethodGet,
		"", "error retrieving Disk: %#v", nil, disk,
		diskManager.VCDClient.VCDClient.Client.APIVersion)
	if err != nil && strings.Contains(err.Error(), "MajorErrorCode:403") {
		return nil, govcd.ErrorEntityNotFound
	}
	if err != nil {
		return nil, err
	}
	return disk, nil
}

func (diskManager *DiskManager) govcdGetDiskById(diskId string, vdc *govcd.Vdc, refresh bool) (*vcdtypes.Disk, error) {
	klog.Infof("Get Disk By Id: %s\n", diskId)
	if refresh {
		if err := vdc.Refresh(); err != nil {
			return nil, fmt.Errorf("error when refreshing vdc [%s]: [%v]", vdc.Vdc.Name, err)
		}
	}
	for _, resourceEntities := range vdc.Vdc.ResourceEntities {
		for _, resourceEntity := range resourceEntities.ResourceEntity {
			if resourceEntity.ID == diskId && resourceEntity.Type == "application/vnd.vmware.vcloud.disk+xml" {
				disk, err := diskManager.govcdGetDiskByHref(resourceEntity.HREF)
				if err != nil {
					return nil, err
				}
				return disk, nil
			}
		}
	}
	return nil, govcd.ErrorEntityNotFound
}

// GetDisksByName finds one or more Disks by Name
// On success, returns a pointer to the Disk list and a nil error
// On failure, returns a nil pointer and an error
func (diskManager *DiskManager) govcdGetDisksByName(diskName string, vdc *govcd.Vdc,
	refresh bool) (*[]vcdtypes.Disk, error) {
	klog.Infof("Get Disk By Name: %s\n", diskName)
	var diskList []vcdtypes.Disk

	if refresh {
		if err := vdc.Refresh(); err != nil {
			return nil, fmt.Errorf("unable ot refresh VDC [%s]: [%v]", vdc.Vdc.Name, err)
		}
	}

	for _, resourceEntities := range vdc.Vdc.ResourceEntities {
		for _, resourceEntity := range resourceEntities.ResourceEntity {
			if resourceEntity.Name == diskName && resourceEntity.Type == "application/vnd.vmware.vcloud.disk+xml" {
				disk, err := diskManager.govcdGetDiskByHref(resourceEntity.HREF)
				if err != nil {
					return nil, err
				}
				diskList = append(diskList, *disk)
			}
		}
	}
	if len(diskList) == 0 {
		return nil, govcd.ErrorEntityNotFound
	}
	return &diskList, nil
}

func isUrn(identifier string) bool {
	if identifier == "" {
		return false
	}

	ss := strings.Split(identifier, ":")
	if len(ss) != 4 {
		return false
	}

	if ss[0] != "urn" && !govcd.IsUuid(ss[3]) {
		return false
	}

	return true
}

type genericGetter func(string, bool) (interface{}, error)

func getEntityByNameOrIdSkipNonId(getByName, getById genericGetter, identifier string, refresh bool) (interface{}, error) {

	var byNameErr, byIdErr error
	var entity interface{}

	// Only check by ID if it is an ID or an URN
	if isUrn(identifier) || govcd.IsUuid(identifier) {
		entity, byIdErr = getById(identifier, refresh)
		if byIdErr == nil {
			// Found by ID
			return entity, nil
		}
	}

	if govcd.IsNotFound(byIdErr) || byIdErr == nil {
		// Not found by ID, try by name
		entity, byNameErr = getByName(identifier, false)
		return entity, byNameErr
	} else {
		// On any other error, we return it
		return nil, byIdErr
	}
}

func (diskManager *DiskManager) govcdGetDisksByNameOrId(identifier string, vdc *govcd.Vdc,
	refresh bool) (*[]vcdtypes.Disk, error) {
	getByName := func(name string, refresh bool) (interface{}, error) {
		return diskManager.govcdGetDisksByName(identifier, vdc, refresh)
	}
	getById := func(id string, refresh bool) (interface{}, error) {
		return diskManager.govcdGetDiskById(identifier, vdc, refresh)
	}
	entity, err := getEntityByNameOrIdSkipNonId(getByName, getById, identifier, refresh)
	if entity == nil {
		return nil, err
	}
	return entity.(*[]vcdtypes.Disk), err
}

// GetDiskByNameOrId will get disk by name
func (diskManager *DiskManager) GetDiskByNameOrId(name string, zm *vcdsdk.ZoneMap,
	vdcIdentifier string) (*vcdtypes.Disk, error) {
	klog.Infof("Entered GetDiskByNameOrId for name [%s]", name)

	if name == "" {
		return nil, fmt.Errorf("disk identifier should not be empty")
	}
	if vdcIdentifier == "" && zm == nil && len(zm.VdcToZoneMap) == 0 {
		return nil, fmt.Errorf("unable to find disk [%s] since zone and OrgVDC are not specified", name)
	}

	if zm != nil && len(zm.VdcToZoneMap) != 0 {
		for ovdcName, zone := range zm.VdcToZoneMap {
			vdc, err := diskManager.Org.GetVDCByNameOrId(ovdcName, false)
			if err != nil {
				klog.Errorf("unable to get VDC [%s] of zone [%s] by name in org [%s]: [%v]", ovdcName, zone,
					diskManager.Org.Org.Name, err)
				continue
			}
			disks, err := diskManager.govcdGetDisksByName(name, vdc, true)
			if err != nil && !errors.Is(err, govcd.ErrorEntityNotFound) {
				klog.Infof("error looking for disk [%s] in OVDC [%s] of Org [%s]: [%v]",
					name, ovdcName, diskManager.Org.Org.Name, err)
				continue
			}
			if errors.Is(err, govcd.ErrorEntityNotFound) || disks == nil || len(*disks) == 0 {
				// disk not found is a useful error code in some scenarios
				continue
			}
			if len(*disks) > 1 {
				klog.Errorf("found [%d] > 1 disks with name [%s] in ovdc [%s] of org [%s]",
					len(*disks), name, ovdcName, diskManager.Org.Org.Name)
				continue
			}

			return &(*disks)[0], nil
		}
	} else {
		vdc, err := diskManager.Org.GetVDCByNameOrId(vdcIdentifier, false)
		if err != nil {
			return nil, fmt.Errorf("unable to find vdc [%s] in org [%s]: [%v]", vdcIdentifier,
				diskManager.Org.Org.Name, err)
		}
		disks, err := diskManager.govcdGetDisksByNameOrId(name, vdc, true)
		if err != nil && !errors.Is(err, govcd.ErrorEntityNotFound) {
			klog.Infof("error looking for disk [%s] in OVDC [%s] of Org [%s]: [%v]",
				name, vdc.Vdc.Name, diskManager.Org.Org.Name, err)
			return nil, fmt.Errorf("error looking for disk [%s] in OVDC [%s] of Org [%s]: [%v]",
				name, vdc.Vdc.Name, diskManager.Org.Org.Name, err)
		}
		if errors.Is(err, govcd.ErrorEntityNotFound) || disks == nil || len(*disks) == 0 {
			// disk not found is a useful error code in some scenarios
			return nil, govcd.ErrorEntityNotFound
		}
		if len(*disks) > 1 {
			klog.Errorf("found [%d] > 1 disks with name [%s] in ovdc [%s] of org [%s]",
				len(*disks), name, vdc.Vdc.Name, diskManager.Org.Org.Name)
			return nil, fmt.Errorf("found [%d] > 1 disks with name [%s] in ovdc [%s] of org [%s]",
				len(*disks), name, vdc.Vdc.Name, diskManager.Org.Org.Name)
		}
		return &(*disks)[0], nil
	}

	return nil, govcd.ErrorEntityNotFound
}

func (diskManager *DiskManager) GovcdAttachedVM(disk *vcdtypes.Disk) ([]*types.Reference, error) {
	klog.Infof("[TRACE] Entered Disk attached VM, HREF: %s\n", disk.HREF)

	var attachedVMLink *types.Link

	// Find the proper link for request
	for _, diskLink := range disk.Link {
		if diskLink.Type == types.MimeVMs {
			klog.Infof("[TRACE] Disk attached VM - found the proper link for request, "+
				"HREF: %s, name: %s, type: %s,id: %s, rel: %s \n",
				diskLink.HREF,
				diskLink.Name,
				diskLink.Type,
				diskLink.ID,
				diskLink.Rel)

			attachedVMLink = diskLink
			break
		}
	}

	if attachedVMLink == nil {
		return nil, fmt.Errorf("could not find request URL for attached vm in disk Link")
	}

	// Decode request
	attachedVMs := vcdtypes.Vms{}

	_, err := diskManager.VCDClient.VCDClient.Client.ExecuteRequestWithApiVersion(attachedVMLink.HREF, http.MethodGet,
		attachedVMLink.Type, "error getting attached vms: %s", nil, &attachedVMs,
		diskManager.VCDClient.VCDClient.Client.APIVersion)
	if err != nil {
		return nil, err
	}

	// Note that VmReference could be a null pointer or a C-like array of VmReference structs
	return attachedVMs.VmReference, nil
}

// Remove an independent disk
// 1 Verify the independent disk is not connected to any VM
// 2 Delete the independent disk. Make a DELETE request to the URL in the rel="remove" link in the Disk
// 3 Return task of independent disk deletion
// If the independent disk is connected to a VM, the task will be failed.
// Reference: vCloud API Programming Guide for Service Providers vCloud API 30.0 PDF Page 106 - 107,
// https://vdc-download.vmware.com/vmwb-repository/dcr-public/1b6cf07d-adb3-4dba-8c47-9c1c92b04857/
// 241956dd-e128-4fcc-8131-bf66e1edd895/vcloud_sp_api_guide_30_0.pdf
func (diskManager *DiskManager) govcdDelete(disk *vcdtypes.Disk) (govcd.Task, error) {
	klog.Infof("[TRACE] Delete disk, HREF: %s \n", disk.HREF)

	var err error

	// Verify the independent disk is not connected to any VM
	vmRefs, err := diskManager.GovcdAttachedVM(disk)
	if err != nil {
		return govcd.Task{}, fmt.Errorf("error find attached VM: %s", err)
	}
	if vmRefs != nil && len(vmRefs) > 0 {
		return govcd.Task{}, fmt.Errorf("error disk is attached")
	}

	var deleteDiskLink *types.Link

	// Find the proper link for request
	for _, diskLink := range disk.Link {
		if diskLink.Rel == types.RelRemove {
			klog.Infof("[TRACE] Delete disk - found the proper link for request, HREF: %s, name: %s, type: %s,id: %s, rel: %s \n",
				diskLink.HREF,
				diskLink.Name,
				diskLink.Type,
				diskLink.ID,
				diskLink.Rel)
			deleteDiskLink = diskLink
			break
		}
	}

	if deleteDiskLink == nil {
		return govcd.Task{}, fmt.Errorf("could not find request URL for delete disk in disk Link")
	}

	// Return the task
	return diskManager.VCDClient.VCDClient.Client.ExecuteTaskRequestWithApiVersion(deleteDiskLink.HREF, http.MethodDelete,
		"", "error delete disk: %s", nil,
		diskManager.VCDClient.VCDClient.Client.APIVersion)
}

// UpdateDisk updates an independent disk
// 1 Verify the independent disk is not connected to any VM
// 2 Use newDiskInfo to change update the independent disk
// 3 Return task of independent disk update
// If the independent disk is connected to a VM, the task will be failed.
// Reference: vCloud API Programming Guide for Service Providers vCloud API 30.0 PDF Page 104 - 106,
// https://vdc-download.vmware.com/vmwb-repository/dcr-public/1b6cf07d-adb3-4dba-8c47-9c1c92b04857/
// 241956dd-e128-4fcc-8131-bf66e1edd895/vcloud_sp_api_guide_30_0.pdf
// copied from govcd and modified for independent disks
func (diskManager *DiskManager) UpdateDisk(disk *vcdtypes.Disk, newDiskInfo *types.Disk) (govcd.Task, error) {
	klog.Infof("Update disk, name: [%s=>%s], size: [%d=>%d], HREF: [%s] \n",
		disk.Name, newDiskInfo.Name,
		disk.SizeMb, newDiskInfo.SizeMb,
		disk.HREF,
	)

	var err error

	if newDiskInfo.Name == "" {
		return govcd.Task{}, fmt.Errorf("disk name is required")
	}

	// Verify the independent disk is not connected to any VM
	vmRefs, err := diskManager.GovcdAttachedVM(disk)
	if err != nil {
		return govcd.Task{}, fmt.Errorf("error finding attached VM for disk [%s]: [%v]", disk.Name, err)
	}
	if vmRefs != nil && len(vmRefs) > 0 {
		return govcd.Task{}, fmt.Errorf("error disk [%s] is attached to [%d] VMs", disk.Name, len(vmRefs))
	}

	var updateDiskLink *types.Link

	// Find the proper link for request
	for _, diskLink := range disk.Link {
		if diskLink.Rel == types.RelEdit && diskLink.Type == types.MimeDisk {
			klog.Infof(
				"Update disk [%s]: found proper link for request, HREF: %s, name: %s, type: %s,id: %s, rel: %s",
				disk.Name,
				diskLink.HREF,
				diskLink.Name,
				diskLink.Type,
				diskLink.ID,
				diskLink.Rel)
			updateDiskLink = diskLink
			break
		}
	}

	if updateDiskLink == nil {
		return govcd.Task{},
			fmt.Errorf("could not find request URL for update disk for [%s] in disk Link", disk.Name)
	}

	// Prepare the request payload
	xmlPayload := &types.Disk{
		Xmlns:       types.XMLNamespaceVCloud,
		Description: newDiskInfo.Description,
		SizeMb:      newDiskInfo.SizeMb,
		Name:        newDiskInfo.Name,
		Owner:       newDiskInfo.Owner,
	}

	// Return the task
	task, err := diskManager.VCDClient.VCDClient.Client.ExecuteTaskRequest(updateDiskLink.HREF, http.MethodPut,
		updateDiskLink.Type, "error updating disk: %s", xmlPayload)
	if err != nil {
		return govcd.Task{}, fmt.Errorf("unable to execute PUT request to update disk: [%v]", err)
	}

	return task, nil
}

// DeleteDisk will delete independent disk by its name
func (diskManager *DiskManager) DeleteDisk(name string, zm *vcdsdk.ZoneMap) error {
	diskManager.VCDClient.RWLock.Lock()
	defer diskManager.VCDClient.RWLock.Unlock()

	klog.Infof("Entered DeleteDisk for disk [%s]\n", name)

	disk, err := diskManager.GetDiskByNameOrId(name, zm, diskManager.VCDClient.ClusterOVDCIdentifier)
	if err != nil {
		if errors.Is(err, govcd.ErrorEntityNotFound) {
			// ignore deletes for non-existent entities
			klog.Infof("Unable to find disk with name [%s]: [%v]", name, err)
			return nil
		}

		return fmt.Errorf("unable to find disk with name [%s]: [%v]", name, err)
	}

	attachedVMs, err := diskManager.GovcdAttachedVM(disk)
	if err != nil {
		return fmt.Errorf("unable to find if disk [%s] is attached to a VM: [%v]", name, err)
	}
	if attachedVMs != nil && len(attachedVMs) > 0 {
		return fmt.Errorf("unable to delete disk [%s] that is attached to VMs [%#v]", name, attachedVMs)
	}

	task, err := diskManager.govcdDelete(disk)
	if err != nil {
		return fmt.Errorf("unable to issue delete disk call for [%s]: [%v]", name, err)
	}

	err = task.WaitTaskCompletion()
	if err != nil {
		return fmt.Errorf("failed to wait for deletion task of disk [%s]: [%v]", name, err)
	}

	if addEventRdeErr := diskManager.AddToEventSet(util.DiskDeleteEvent, "", disk.Name, map[string]interface{}{"Detailed Info": fmt.Sprintf("Volume %s deleted successfully", name)}); addEventRdeErr != nil {
		klog.Errorf("unable to add event [%s] into [CSI.Events] in RDE [%s]", util.DiskDeleteEvent, diskManager.ClusterID)
	}
	rdeManager := vcdsdk.NewRDEManager(diskManager.VCDClient, diskManager.ClusterID, util.CSIName, version.Version)
	// update RDE
	if diskManager.ClusterID != "" && !strings.HasPrefix(diskManager.ClusterID, NoRdePrefix) {
		if err = diskManager.removePvFromRDE(disk.Id, disk.Name, rdeManager); err != nil {
			return vcdsdk.NewNoRDEError(fmt.Sprintf("unable to remove PV Id [%s] from RDE; RDE ID is generated", disk.Id))
		}
	}

	return nil
}

// Refresh the disk information by disk href
func (diskManager *DiskManager) govcdRefresh(disk *vcdtypes.Disk) error {
	klog.Infof("[TRACE] Disk refresh, HREF: %s\n", disk.HREF)

	if disk == nil || disk.HREF == "" {
		return fmt.Errorf("cannot refresh, Object is empty")
	}

	unmarshalledDisk := &vcdtypes.Disk{}

	_, err := diskManager.VCDClient.VCDClient.Client.ExecuteRequestWithApiVersion(disk.HREF, http.MethodGet,
		"", "error refreshing independent disk: %s", nil, unmarshalledDisk,
		diskManager.VCDClient.VCDClient.Client.APIVersion)
	if err != nil {
		return err
	}

	// shallow-copy content from unmarshalledDisk to Disk
	*disk = *unmarshalledDisk

	// The request was successful
	return nil
}

// AttachVolume will attach diskName to vm
func (diskManager *DiskManager) AttachVolume(vm *govcd.VM, disk *vcdtypes.Disk) error {
	diskManager.VCDClient.RWLock.Lock()
	defer diskManager.VCDClient.RWLock.Unlock()

	if disk == nil {
		return fmt.Errorf("disk passed shoulf not be nil")
	}

	klog.Infof("Entered AttachVolume for vm [%v], disk [%s]\n", vm, disk.Name)

	attachedVMs, err := diskManager.GovcdAttachedVM(disk)
	if err != nil {
		return fmt.Errorf("unable to find volume attached to disk [%s]: [%v]", disk.Name, err)
	}

	if attachedVMs != nil && len(attachedVMs) > 0 {
		// if disk is already attached to current VM, it's fine
		for _, attachedVM := range attachedVMs {
			if attachedVM.HREF == vm.VM.HREF {
				klog.Infof("Disk [%s] already attached to VM [%s], so nothing to do.",
					disk.Name, vm.VM.Name)
				return nil
			}
		}

		// if disk is not shareable and there are other attached VMs, fail
		if !disk.Shareable {
			return fmt.Errorf("cannot attach disk since disk is not shareable and [%#v] VMs are attached",
				attachedVMs)
		}
	}

	params := &types.DiskAttachOrDetachParams{
		Disk: &types.Reference{HREF: disk.HREF},
	}

	klog.Infof("Attaching disk with params [%v], [%v]", params, params.Disk)
	task, err := vm.AttachDisk(params)
	if err != nil {
		return fmt.Errorf("failed to run AttachDisk with params [%v]: [%v]", params, err)
	}
	klog.Infof("AttachDisk returned task: [%#v]", task.Task)

	err = task.WaitTaskCompletion()
	if err != nil {
		return fmt.Errorf("failed waiting for disk [%s] to attach to vm [%s]",
			disk.Name, vm.VM.Name)
	}

	if err = diskManager.govcdRefresh(disk); err != nil {
		return fmt.Errorf("unable to refresh disk [%s] for verification: [%v]", disk.Name, err)
	}
	if addEventRdeErr := diskManager.AddToEventSet(util.DiskAttachEvent, "", disk.Name, map[string]interface{}{"Detailed Info": fmt.Sprintf("Successfully attached volume %s to node %s ", disk.Name, vm.VM.Name)}); addEventRdeErr != nil {
		klog.Errorf("unable to add event [%s] into [CSI.Events] in RDE [%s]", util.DiskAttachEvent, diskManager.ClusterID)
	}

	return nil
}

// DetachVolume will detach diskName from vm
func (diskManager *DiskManager) DetachVolume(vm *govcd.VM, diskName string, zm *vcdsdk.ZoneMap) error {
	diskManager.VCDClient.RWLock.Lock()
	defer diskManager.VCDClient.RWLock.Unlock()

	klog.Infof("Entered DetachVolume for vm [%v], disk [%s]\n", vm, diskName)

	disk, err := diskManager.GetDiskByNameOrId(diskName, zm, diskManager.VCDClient.ClusterOVDCIdentifier)
	if errors.Is(err, govcd.ErrorEntityNotFound) {
		klog.Warningf("Unable to find disk [%s]. It is probably already deleted.", diskName)
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to get disk details for [%s]: [%v]", diskName, err)
	}

	attachedVMs, err := diskManager.GovcdAttachedVM(disk)
	if err != nil {
		return fmt.Errorf("error looking for VM attached to disk [%s]: [%v]", diskName, err)
	}

	if attachedVMs == nil || len(attachedVMs) == 0 {
		klog.Infof("No VM attached to disk [%s]. Hence OK to detach.", diskName)
		return nil
	}

	vmAttached := false
	for _, attachedVM := range attachedVMs {
		if attachedVM.HREF == vm.VM.HREF {
			vmAttached = true
			break
		}
	}
	if !vmAttached {
		klog.Infof("Disk [%s] not attached to VM [%s]. Hence returning.", disk.Name, vm.VM.Name)
		return nil
	}

	params := &types.DiskAttachOrDetachParams{
		Disk: &types.Reference{HREF: disk.HREF},
	}
	task, err := vm.DetachDisk(params)
	if err != nil {
		return fmt.Errorf("unable to detach disk [%s] from VM [%s]: [%v]", disk.Name, vm.VM.Name, err)
	}
	err = task.WaitTaskCompletion()
	if err != nil {
		return fmt.Errorf("error while waiting for detach task for disk [%s] from VM [%s]",
			diskName, vm.VM.Name)
	}
	if addEventRdeErr := diskManager.AddToEventSet(util.DiskDetachEvent, "", disk.Name, map[string]interface{}{"Detailed Info": fmt.Sprintf("Successfully detached volume %s from node %s ", disk.Name, vm.VM.Name)}); addEventRdeErr != nil {
		klog.Errorf("unable to add event [%s] into [CSI.Events] in RDE [%s]", util.DiskDetachEvent, diskManager.ClusterID)
	}
	klog.Infof("Successfully detached disk [%s] from VM [%s]", disk.Name, vm.VM.Name)

	return nil
}

func (diskManager *DiskManager) GetRDEPersistentVolumes(rde *swaggerClient.DefinedEntity) ([]string, error) {
	pvStrs, err := util.GetPVsFromRDE(rde)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve PVs from RDE: [%v]", err)
	}
	return pvStrs, nil
}

// This function will modify the passed param in defEnt and only for native cluster
func (diskManager *DiskManager) addRDEPersistentVolumes(updatedPvs []string, etag string,
	defEnt *swaggerClient.DefinedEntity) (*http.Response, error) {
	updatedRDE, err := util.AddPVsInRDE(defEnt, updatedPvs)
	if err != nil {
		return nil, fmt.Errorf("failed to replace persistentVolumes section for RDE with ID [%s]: [%v]",
			diskManager.ClusterID, err)
	}
	clusterOrg, err := diskManager.VCDClient.VCDClient.GetOrgByName(diskManager.VCDClient.ClusterOrgName)
	if err != nil {
		return nil, fmt.Errorf("failed to get org by name for org [%s]: [%v]", diskManager.VCDClient.ClusterOrgName, err)
	}

	if clusterOrg == nil || clusterOrg.Org == nil {
		return nil, fmt.Errorf("obtained nil org when getting org by name [%s]", diskManager.VCDClient.ClusterOrgName)
	}
	// can set invokeHooks as optional parameter
	_, httpResponse, err := diskManager.VCDClient.APIClient.DefinedEntityApi.UpdateDefinedEntity(context.TODO(), *updatedRDE, etag,
		diskManager.ClusterID, clusterOrg.Org.ID, nil)
	if err != nil {
		return httpResponse, fmt.Errorf("error when updating defined entity: [%v]", err)
	}
	klog.Infof("Successfully updated RDE [%s] with persistentVolumes: [%s]",
		diskManager.ClusterID, updatedPvs)
	return httpResponse, nil
}

func (diskManager *DiskManager) removeRDEPersistentVolumes(updatedPvs []string, etag string,
	defEnt *swaggerClient.DefinedEntity) (*http.Response, error) {
	updatedRDE, err := util.RemovePVInRDE(defEnt, updatedPvs)
	if err != nil {
		return nil, fmt.Errorf("failed to replace persistentVolumes section for RDE with ID [%s]: [%v]",
			diskManager.ClusterID, err)
	}

	clusterOrg, err := diskManager.VCDClient.VCDClient.GetOrgByName(diskManager.VCDClient.ClusterOrgName)
	if err != nil {
		return nil, fmt.Errorf("failed to get org by name for org [%s]: [%v]", diskManager.VCDClient.ClusterOrgName, err)
	}

	if clusterOrg == nil || clusterOrg.Org == nil {
		return nil, fmt.Errorf("obtained nil org when getting org by name [%s]", diskManager.VCDClient.ClusterOrgName)
	}

	_, httpResponse, err := diskManager.VCDClient.APIClient.DefinedEntityApi.UpdateDefinedEntity(context.TODO(), *updatedRDE, etag,
		diskManager.ClusterID, clusterOrg.Org.ID, nil)
	if err != nil {
		return httpResponse, fmt.Errorf("error when updating defined entity: [%v]", err)
	}
	klog.Infof("Successfully updated RDE [%s] with persistentVolumes: [%s]",
		diskManager.ClusterID, updatedPvs)
	return httpResponse, nil

}

func (diskManager *DiskManager) addPvToRDE(addPvId string, addPvName string, rdeManager *vcdsdk.RDEManager) error {
	for i := 0; i < vcdsdk.MaxRDEUpdateRetries; i++ {
		clusterOrg, err := diskManager.VCDClient.VCDClient.GetOrgByName(diskManager.VCDClient.ClusterOrgName)
		if err != nil {
			return fmt.Errorf("failed to get org by name for org [%s]: [%v]", diskManager.VCDClient.ClusterOrgName, err)
		}

		if clusterOrg == nil || clusterOrg.Org == nil {
			return fmt.Errorf("obtained nil org when getting org by name [%s]", diskManager.VCDClient.ClusterOrgName)
		}
		defEnt, _, etag, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
			diskManager.ClusterID, clusterOrg.Org.ID, nil)
		if err != nil {
			return fmt.Errorf("error when getting defined entity: [%v]", err)
		}
		if vcdsdk.IsNativeClusterEntityType(defEnt.EntityType) {
			currPvs, err := diskManager.GetRDEPersistentVolumes(&defEnt)
			if err != nil {
				return fmt.Errorf("error for getting current RDE PVs: [%v]", err)
			}
			foundAddPv := false
			for _, pv := range currPvs {
				if pv == addPvName {
					foundAddPv = true
					break
				}
			}
			if foundAddPv {
				return nil // no need to update RDE
			}
			updatedPvIDs := append(currPvs, addPvId)
			httpResponse, err := diskManager.addRDEPersistentVolumes(updatedPvIDs, etag, &defEnt)
			if err != nil {
				if httpResponse.StatusCode == http.StatusPreconditionFailed {
					continue
				}
				return fmt.Errorf("error when adding pv [%s] to RDE [%s]: [%v]",
					addPvName, diskManager.ClusterID, err)
			}
			return nil
		} else if vcdsdk.IsCAPVCDEntityType(defEnt.EntityType) {
			rdeError := rdeManager.AddToVCDResourceSet(context.Background(), vcdsdk.ComponentCSI, util.ResourcePersistentVolume, addPvName, addPvId, nil)
			if rdeError != nil {
				return fmt.Errorf("failed to add persistent volume [%s] to VCDResourceSet of RDE [%s]", addPvName, rdeManager.ClusterID)
			}
			return nil
		}
		return fmt.Errorf("entity type %s not supported by CSI", defEnt.EntityType)
	}
	return fmt.Errorf("unable to update rde due to incorrect etag after %d tries", vcdsdk.MaxRDEUpdateRetries)
}

func (diskManager *DiskManager) removePvFromRDE(removePvId string, removePvName string, rdeManager *vcdsdk.RDEManager) error {
	for i := 0; i < vcdsdk.MaxRDEUpdateRetries; i++ {
		clusterOrg, err := diskManager.VCDClient.VCDClient.GetOrgByName(diskManager.VCDClient.ClusterOrgName)
		if err != nil {
			return fmt.Errorf("failed to get org by name for org [%s]: [%v]", diskManager.VCDClient.ClusterOrgName, err)
		}

		if clusterOrg == nil || clusterOrg.Org == nil {
			return fmt.Errorf("obtained nil org when getting org by name [%s]", diskManager.VCDClient.ClusterOrgName)
		}
		defEnt, _, etag, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
			diskManager.ClusterID, clusterOrg.Org.ID, nil)
		if err != nil {
			return fmt.Errorf("error when getting defined entity: [%v]", err)
		}
		if vcdsdk.IsNativeClusterEntityType(defEnt.EntityType) {
			currPvs, err := diskManager.GetRDEPersistentVolumes(&defEnt)
			if err != nil {
				return fmt.Errorf("error for getting current RDE PVs: [%v]", err)
			}
			foundIdx := -1
			for idx, pv := range currPvs {
				if pv == removePvId {
					foundIdx = idx
					break
				}
			}
			if foundIdx == -1 {
				return nil // no need to update RDE
			}
			updatedPvs := append(currPvs[:foundIdx], currPvs[foundIdx+1:]...)
			httpResponse, err := diskManager.removeRDEPersistentVolumes(updatedPvs, etag, &defEnt)
			if err == nil {
				return nil
			}
			if httpResponse.StatusCode != http.StatusPreconditionFailed {
				return fmt.Errorf("error when removing pv [%s] from RDE [%s]: [%v]",
					removePvId, diskManager.ClusterID, err)
			}
		} else if vcdsdk.IsCAPVCDEntityType(defEnt.EntityType) {
			rdeError := rdeManager.RemoveFromVCDResourceSet(context.Background(), vcdsdk.ComponentCSI, util.ResourcePersistentVolume, removePvName)
			if rdeError != nil {
				return fmt.Errorf("failed to remove persistent volume [%s] from VCDResourceSet of RDE [%s]", removePvName, rdeManager.ClusterID)
			}
			return nil
		}

	}
	return fmt.Errorf("unable to update rde due to incorrect etag after %d tries", vcdsdk.MaxRDEUpdateRetries)
}

// UpgradeRDEPersistentVolumes This function will only upgrade RDE CSI section for CAPVCD cluster
func (diskManager *DiskManager) UpgradeRDEPersistentVolumes() error {
	for retries := 0; retries < vcdsdk.MaxRDEUpdateRetries; retries++ {
		clusterOrg, err := diskManager.VCDClient.VCDClient.GetOrgByName(diskManager.VCDClient.ClusterOrgName)
		if err != nil {
			return fmt.Errorf("failed to get org by name for org [%s]: [%v]", diskManager.VCDClient.ClusterOrgName, err)
		}

		if clusterOrg == nil || clusterOrg.Org == nil {
			return fmt.Errorf("obtained nil org when getting org by name [%s]", diskManager.VCDClient.ClusterOrgName)
		}
		rde, _, etag, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
			diskManager.ClusterID, clusterOrg.Org.ID, nil)
		if err != nil {
			return fmt.Errorf("error when getting defined entity from VCD: [%v]", err)
		}
		rdeManager := vcdsdk.NewRDEManager(diskManager.VCDClient, diskManager.ClusterID, util.CSIName, version.Version)
		statusEntry, ok := rde.Entity["status"]
		if !ok {
			klog.Infof("key 'Status' is missing in the RDE [%s]; skipping upgrade of CSI section in RDE status", diskManager.ClusterID)
			return nil
		}
		statusMap, ok := statusEntry.(map[string]interface{})
		if !ok {
			rdeIncorrectFormatError := vcdsdk.BackendError{
				Name:              util.RdeIncorrectFormatError,
				OccurredAt:        time.Now(),
				VcdResourceId:     "",
				VcdResourceName:   "",
				AdditionalDetails: nil,
			}
			if rdeErr := rdeManager.AddToErrorSet(context.Background(), vcdsdk.ComponentCSI, rdeIncorrectFormatError, util.DefaultWindowSize); rdeErr != nil {
				klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.RdeIncorrectFormatError, diskManager.ClusterID, rdeErr)
			}
			klog.Errorf("content under section ['status'] has incorrect format in the RDE [%s]: skipping upgrade of CSI section in RDE status",
				diskManager.ClusterID)
			return nil
		}
		// a. get list of PV
		oldPvs, err := util.GetOldPVsFromRDE(statusMap, diskManager.ClusterID)
		if err != nil {
			rdeIncorrectFormatError := vcdsdk.BackendError{
				Name:              util.RdeIncorrectFormatError,
				OccurredAt:        time.Now(),
				VcdResourceId:     "",
				VcdResourceName:   "",
				AdditionalDetails: map[string]interface{}{"Detailed Error": err.Error()},
			}
			if rdeErr := rdeManager.AddToErrorSet(context.Background(), vcdsdk.ComponentCSI, rdeIncorrectFormatError, util.DefaultWindowSize); rdeErr != nil {
				klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.RdeIncorrectFormatError, diskManager.ClusterID, rdeErr)
			}
			klog.Errorf("failed to continue RDE upgrade for RDE with ID [%s]: [%v]",
				diskManager.ClusterID, err)
			return nil
		}
		updatedMap := statusMap
		var diskQueryErrorList []vcdsdk.BackendError
		if len(oldPvs) > 0 {
			//b. get PV details
			PVDetailList := make([]vcdsdk.VCDResource, len(oldPvs))
			for idx, oldPVId := range oldPvs {
				diskName, diskId := "", oldPVId
				disk, err := diskManager.GetDiskByNameOrId(oldPVId, diskManager.ZoneMap, diskManager.VCDClient.ClusterOVDCIdentifier)
				if err != nil {
					// We hold the diskQuery and choose not to update RDE here. Because we don't want etag outdated
					diskQueryErrorList = append(diskQueryErrorList, vcdsdk.BackendError{
						Name:              "DiskQueryError",
						OccurredAt:        time.Now(),
						VcdResourceId:     oldPVId,
						VcdResourceName:   "",
						AdditionalDetails: map[string]interface{}{"Detailed Error": fmt.Sprintf("fail to execute query using disk id: [%s], %s", oldPVId, err.Error())},
					})
					klog.Errorf("error when conducting disk query with id [%s]: %v", oldPVId, err)
				} else {
					diskName, diskId = disk.Name, disk.Id
				}
				PVDetailList[idx] = vcdsdk.VCDResource{
					Type:              util.ResourcePersistentVolume,
					ID:                diskId,
					Name:              diskName,
					AdditionalDetails: nil,
				}
			}
			//c.1 update local RDE data structure where we add newPVs to resourceSet
			updatedMap, err = util.UpgradeStatusMapOfRdeToLatestFormat(statusMap, PVDetailList, diskManager.ClusterID)
			if err != nil {
				rdeIncorrectFormatError := vcdsdk.BackendError{
					Name:              util.RdeIncorrectFormatError,
					OccurredAt:        time.Now(),
					VcdResourceId:     "",
					VcdResourceName:   "",
					AdditionalDetails: nil,
				}
				if rdeErr := rdeManager.AddToErrorSet(context.Background(), vcdsdk.ComponentCSI, rdeIncorrectFormatError, util.DefaultWindowSize); rdeErr != nil {
					klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.RdeIncorrectFormatError, diskManager.ClusterID, rdeErr)
				}
				klog.Errorf("error occurred when updating VCDResource set of CSI status in RDE [%s]: [%v]; skipping upgrade of CSI section in RDE status", diskManager.ClusterID, err)
				return nil
			}
		}
		//c.2 update local RDE data structure where we remove persistentVolumes section
		delete(updatedMap, util.OldPersistentVolumeKey)
		rde.Entity["status"] = updatedMap
		//d. update RDE in VCD with PV details
		_, httpResponse, err := diskManager.VCDClient.APIClient.DefinedEntityApi.UpdateDefinedEntity(context.Background(), rde, etag, diskManager.ClusterID, clusterOrg.Org.ID, nil)
		// TODO: Optimize the diskQuery process, try to make those happen in one time upgrade operation. Also might do extra sorting
		for _, diskQueryError := range diskQueryErrorList {
			if rdeErr := rdeManager.AddToErrorSet(context.Background(), vcdsdk.ComponentCSI, diskQueryError, util.DefaultWindowSize); rdeErr != nil {
				klog.Errorf("unable to add error [%s]into [CSI.Errors] in RDE [%s], %v", diskQueryError.Name, diskManager.ClusterID, rdeErr)
			}
		}
		if httpResponse != nil && httpResponse.StatusCode != http.StatusOK {
			var responseMessageBytes []byte
			if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
				responseMessageBytes = gsErr.Body()
			}
			klog.Warningf(
				"failed to upgrade CSI Persistent Volumes section for RDE [%s];expected http response [%v], obtained [%v]: resp: [%#v]: [%v]; Record Etag: [%s]. Remaining retry attempts: [%d]",
				diskManager.ClusterID, http.StatusOK, httpResponse.StatusCode, string(responseMessageBytes), err, etag, vcdsdk.MaxRDEUpdateRetries-retries+1)
			continue
		} else if err != nil {
			klog.Errorf("error while getting the RDE [%s]: [%v]. Remaining retry attempts: [%d]", diskManager.ClusterID, err, vcdsdk.MaxRDEUpdateRetries-retries+1)
		}
		klog.Infof("successfully upgraded CSI Persistent Volumes section of the RDE [%s]", diskManager.ClusterID)
		return nil
	}
	return fmt.Errorf("failed to upgrade CSI Persistent Volumes section of the RDE [%s] after [%d] retries", diskManager.ClusterID, vcdsdk.MaxRDEUpdateRetries)
}

// ConvertToLatestCSIVersionFormat provides an automatic conversion from current CSI status to the latest CSI version in use
// Call an API call (GET) to get the CAPVCD entity.
// Provide an automatic conversion of the content in srcCapvcdEntity.entity.status.csi content to the latest CSI version format (vcdtypes.CSIStatus)
// Add the placeholder for any special conversion logic inside vcdtypes.CSIStatus (for developers)
// Call an API call (PUT) to update CAPVCD entity and persist data into VCD
// Return dstCapvcdEntity as output.
func (diskManager *DiskManager) ConvertToLatestCSIVersionFormat(dstVersion string) (*swaggerClient.DefinedEntity, error) {
	clusterOrg, err := diskManager.VCDClient.VCDClient.GetOrgByName(diskManager.VCDClient.ClusterOrgName)
	if err != nil {
		return nil, fmt.Errorf("error when getting cluster org [%s] from VCD: [%v]; skipping upgrade of CSI status section to Version [%s]", diskManager.ClusterID, err, dstVersion)
	}

	if clusterOrg == nil || clusterOrg.Org == nil {
		return nil, fmt.Errorf("obtained nil org when getting org by name [%s]; skipping upgrade of CSI status section to Version [%s]", diskManager.VCDClient.ClusterOrgName, dstVersion)
	}
	var srcVersion string
	for retries := 0; retries < vcdsdk.MaxRDEUpdateRetries; retries++ {
		srcCapvcdEntity, resp, etag, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.Background(),
			diskManager.ClusterID, clusterOrg.Org.ID, nil)
		if err != nil {
			var responseMessageBytes []byte
			if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
				responseMessageBytes = gsErr.Body()
				klog.Errorf("error occurred when getting defined entity [%s]; skipping upgrade of CSI status section: [%s]",
					diskManager.ClusterID, string(responseMessageBytes))
			}
			return nil, fmt.Errorf("error when getting defined entity [%s] from VCD: [%v]; skipping upgrade of CSI status section to Version [%s]", diskManager.ClusterID, err, dstVersion)
		} else if resp == nil {
			return nil, fmt.Errorf("unexpected response when fetching the defined entity [%s]; skipping upgrade of CSI status section to Version [%s]",
				diskManager.ClusterID, dstVersion)
		} else {
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("obtained unexpected response status code [%d] when fetching the defined entity [%s]. expected [%d]",
					resp.StatusCode, diskManager.ClusterID, http.StatusOK)
			}
		}

		newStatusMap := make(map[string]interface{})
		// ******************  upgrade status.capvcd  ******************
		statusEntryIf, statusEntryOk := srcCapvcdEntity.Entity["status"]
		if !statusEntryOk {
			return nil, fmt.Errorf("key 'Status' is missing in the RDE [%s]; skipping upgrade of CSI status section to Version [%s]", diskManager.ClusterID, dstVersion)
		}

		srcStatusMapIf, srcStatusMapOk := statusEntryIf.(map[string]interface{})
		if !srcStatusMapOk {
			return nil, fmt.Errorf("failed to convert RDE [%s] Status from [%T] to map[string]interface{}; skipping upgrade of CSI status section to Version [%s]", diskManager.ClusterID, statusEntryIf, dstVersion)
		}

		srcEntityCSIStatusIf, srcCSIStatusEntityOk := srcStatusMapIf[vcdsdk.ComponentCSI]
		if !srcCSIStatusEntityOk {
			klog.V(4).Infof("key 'CSI' is missing in the RDE [%s]; skipping upgrade of CSI status section to Version [%s]", diskManager.ClusterID, dstVersion)
			return &srcCapvcdEntity, nil
		}

		srcCSIStatusMapIf, srcCSIStatusMapOk := srcEntityCSIStatusIf.(map[string]interface{})
		if !srcCSIStatusMapOk {
			klog.V(4).Infof("failed to convert RDE [%s] CSI status from [%T] to map[string]interface{}; skipping upgrade of CSI status section from Version [%s] to Version [%s]", diskManager.ClusterID, srcEntityCSIStatusIf, srcVersion, dstVersion)
			return &srcCapvcdEntity, nil
		}
		newStatusMap = srcStatusMapIf
		CSIStatus, err := util.ConvertMapToCSIStatus(srcCSIStatusMapIf)
		if err != nil {
			return nil, fmt.Errorf("failed to convert RDE [%s(%s)] CSI status map [%T] to CAPVCDStatus: [%v]; skipping upgrade of CSI status section to Version [%s]", srcCapvcdEntity.Name, srcCapvcdEntity.Id, srcCSIStatusMapIf, err, dstVersion)
		}
		srcVersion = CSIStatus.Version
		// ******************  placeHolder: add any special conversion logic for CSIStatus  ******************
		// Say CSI 1.3 has properties {X, Y} and CSI 1.4 introduces new property Z; {X, Y, Z}
		// CSIStatus should update with {X, Y, Z=default}. Developers should update property Z at this place.
		//PUT RDE.status.CSI should update with {X, Y, Z=updatedValue}
		CSIStatus.Version = dstVersion
		dstCSIStatusMap, err := util.ConvertCSIStatusToMap(CSIStatus)
		if err != nil {
			return nil, fmt.Errorf("failed to convert upgraded RDE [%s(%s)] CAPVCD Status from [%T] to map[string]interface{}; skipping upgrade of CSI status section from Version [%s] to Version [%s]", srcCapvcdEntity.Name, srcCapvcdEntity.Id, CSIStatus, srcVersion, dstVersion)
		}
		newStatusMap[vcdsdk.ComponentCSI] = dstCSIStatusMap
		srcCapvcdEntity.Entity["status"] = newStatusMap

		updatedRde, resp, err := diskManager.VCDClient.APIClient.DefinedEntityApi.UpdateDefinedEntity(
			context.Background(), srcCapvcdEntity, etag, diskManager.ClusterID, clusterOrg.Org.ID, nil)
		if err != nil {
			var responseMessageBytes []byte
			if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
				responseMessageBytes = gsErr.Body()
				klog.V(5).Infof("error occurred when upgrading CSI of defined entity [%s] from version [%s] to version [%s]: [%s]",
					diskManager.ClusterID, srcVersion, dstVersion, string(responseMessageBytes))
			}
			return nil, fmt.Errorf("error when upgrading CSI of defined entity [%s] from Version [%s] to Version [%s]: [%v]",
				diskManager.ClusterID, srcVersion, dstVersion, err)
		} else if resp == nil {
			return nil, fmt.Errorf("unexpected response when upgrading CSI of defined entity [%s] from Version [%s] to Version [%s]; obtained nil response",
				diskManager.ClusterID, srcVersion, dstVersion)
		} else {
			if resp.StatusCode == http.StatusPreconditionFailed {
				klog.V(5).Infof("wrong etag [%s] while upgrading CSI of defined entity [%s] from Version [%s] to Version [%s]. Retries remaining: [%d]",
					etag, diskManager.ClusterID, srcVersion, dstVersion, vcdsdk.MaxRDEUpdateRetries-retries-1)
				continue
			} else if resp.StatusCode != http.StatusOK {
				klog.Errorf("unexpected response status code when upgrading CSI of defined entity [%s] from Version [%s] to Version [%s]. Expected response [%d] obtained [%d]",
					diskManager.ClusterID, srcVersion, dstVersion, http.StatusOK, resp.StatusCode)
				continue
			}
		}
		klog.V(4).Infof("successfully upgraded CSI of defined entity [%s] from Version [%s] to Version [%s]", diskManager.ClusterID, srcVersion, dstVersion)
		return &updatedRde, nil
	}
	return nil, fmt.Errorf("failed to upgrade CSI of RDE [%s] from Version [%s] to Version [%s] after [%d] retries",
		diskManager.ClusterID, srcVersion, dstVersion, vcdsdk.MaxRDEUpdateRetries)
}

func (diskManager *DiskManager) AddToErrorSet(errorType string, vcdResourceId string, vcdResourceName string, detailMap map[string]interface{}) error {
	rdeManager := vcdsdk.NewRDEManager(diskManager.VCDClient, diskManager.ClusterID, util.CSIName, version.Version)
	newError := vcdsdk.BackendError{
		Name:              errorType,
		OccurredAt:        time.Now(),
		VcdResourceId:     vcdResourceId,
		VcdResourceName:   vcdResourceName,
		AdditionalDetails: detailMap,
	}
	return rdeManager.AddToErrorSet(context.Background(), vcdsdk.ComponentCSI, newError, util.DefaultWindowSize)
}

func (diskManager *DiskManager) RemoveFromErrorSet(errorType string, vcdResourceId string, vcdResourceName string) error {
	rdeManager := vcdsdk.NewRDEManager(diskManager.VCDClient, diskManager.ClusterID, util.CSIName, version.Version)
	return rdeManager.RemoveErrorByNameOrIdFromErrorSet(context.Background(), vcdsdk.ComponentCSI, errorType, vcdResourceId, vcdResourceName)
}

func (diskManager *DiskManager) AddToEventSet(eventType string, vcdResourceId string, vcdResourceName string, detailMap map[string]interface{}) error {
	rdeManager := vcdsdk.NewRDEManager(diskManager.VCDClient, diskManager.ClusterID, util.CSIName, version.Version)
	newEvent := vcdsdk.BackendEvent{
		Name:              eventType,
		OccurredAt:        time.Now(),
		VcdResourceId:     vcdResourceId,
		VcdResourceName:   vcdResourceName,
		AdditionalDetails: detailMap,
	}
	return rdeManager.AddToEventSet(context.Background(), vcdsdk.ComponentCSI, newEvent, util.DefaultWindowSize)
}
