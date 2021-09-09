/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package vcdclient

import (
	"context"
	"encoding/json"
	"fmt"
	swaggerClient "github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdswaggerclient"
	"net/http"
	"strings"

	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdtypes"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"github.com/vmware/go-vcloud-director/v2/types/v56"
	"k8s.io/klog"
)


const (
	VCDBusTypeSCSI           = "6"
	VCDBusSubTypeVirtualSCSI = "VirtualSCSI"
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
func (client *Client) createDisk(diskCreateParams *vcdtypes.DiskCreateParams) (govcd.Task, error) {
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
	for _, vdcLink := range client.vdc.Vdc.Link {
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
	resp, err := client.vcdClient.Client.ExecuteRequestWithApiVersion(createDiskLink.HREF, http.MethodPost,
		createDiskLink.Type, "error creating Disk with params: [%#v]", diskCreateParams, &newDisk,
		client.vcdClient.Client.APIVersion)
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
	newTask := govcd.NewTask(&client.vcdClient.Client)
	newTask.Task = newDisk.Tasks.Task[0]

	return *newTask, nil
}

// CreateDisk will create a new independent disk with params specified
func (client *Client) CreateDisk(diskName string, sizeMB int64, busType string, busSubType string,
	description string, storageProfile string, shareable bool) (*vcdtypes.Disk, error) {
	client.rwLock.Lock()
	defer client.rwLock.Unlock()

	if err := client.RefreshToken(); err != nil {
		return nil, fmt.Errorf("unable to refresh vcd client: [%v]", err)
	}

	klog.Infof("Entered CreateDisk with name [%s] size [%d]MB, storageProfile [%s] shareable[%v]\n",
		diskName, sizeMB, storageProfile, shareable)

	disk, err := client.GetDiskByName(diskName)
	if err != nil && err != govcd.ErrorEntityNotFound {
		return nil, fmt.Errorf("unable to check if disk [%s] already exists: [%v]",
			diskName, err)
	}
	if disk != nil {
		if disk.SizeMb != sizeMB ||
			disk.BusType != busType ||
			disk.BusSubType != busSubType ||
			(storageProfile != "") && (disk.StorageProfile.Name != storageProfile) ||
			disk.Shareable != shareable {
			return nil, fmt.Errorf("Disk [%s] already exists but with different properties: [%v]",
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
		Disk: d,
	}
	if storageProfile != "" {
		storageReference, err := client.vdc.FindStorageProfileReference(storageProfile)
		if err != nil {
			return nil, fmt.Errorf("unable to find storage profile [%s] for disk [%s]",
				storageProfile, diskName)
		}

		diskParams.Disk.StorageProfile = &types.Reference{
			HREF: storageReference.HREF,
		}
	}

	task, err := client.createDisk(diskParams)
	if err != nil {
		return nil,
			fmt.Errorf("unable to create disk with name [%s] size [%d]MB: [%v]",
				diskName, sizeMB, err)
	}

	klog.Infof("START: Waiting for creation of disk [%s] size [%d]MB", diskName, sizeMB)
	err = task.WaitTaskCompletion()
	if err != nil {
		return nil, fmt.Errorf("error waiting to finish creation of independent disk: [%v]", err)
	}
	klog.Infof("END  : Waiting for creation of disk [%s] size [%d]MB", diskName, sizeMB)

	diskHref := task.Task.Owner.HREF
	disk, err = client.govcdGetDiskByHref(diskHref)
	if err != nil {
		return nil, fmt.Errorf("unable to find disk with href [%s]: [%v]", diskHref, err)
	}
	klog.Infof("Disk created: [%#v]", disk)

	// update RDE
	if client.ClusterID != "" {
		if err = client.addPvToRDE(disk.Id); err != nil {
			return nil, fmt.Errorf("unable to add PV Id [%s] to RDE: [%v]", disk.Id, err)
		}
	}

	return disk, nil
}

// GetDiskByHref finds a Disk by HREF
// On success, returns a pointer to the Disk structure and a nil error
// On failure, returns a nil pointer and an error
func (client *Client) govcdGetDiskByHref(diskHref string) (*vcdtypes.Disk, error) {
	klog.Infof("[TRACE] Get Disk By Href: %s\n", diskHref)
	disk := &vcdtypes.Disk{}

	_, err := client.vcdClient.Client.ExecuteRequestWithApiVersion(diskHref, http.MethodGet,
		"", "error retrieving Disk: %#v", nil, disk,
		client.vcdClient.Client.APIVersion)
	if err != nil && strings.Contains(err.Error(), "MajorErrorCode:403") {
		return nil, govcd.ErrorEntityNotFound
	}
	if err != nil {
		return nil, err
	}
	return disk, nil
}

// GetDisksByName finds one or more Disks by Name
// On success, returns a pointer to the Disk list and a nil error
// On failure, returns a nil pointer and an error
func (client *Client) govcdGetDisksByName(diskName string, refresh bool) (*[]vcdtypes.Disk, error) {
	klog.Infof("Get Disk By Name: %s\n", diskName)
	var diskList []vcdtypes.Disk
	if refresh {
		err := client.vdc.Refresh()
		if err != nil {
			return nil, fmt.Errorf("disk name should not be empty")
		}
	}
	for _, resourceEntities := range client.vdc.Vdc.ResourceEntities {
		for _, resourceEntity := range resourceEntities.ResourceEntity {
			if resourceEntity.Name == diskName && resourceEntity.Type == "application/vnd.vmware.vcloud.disk+xml" {
				disk, err := client.govcdGetDiskByHref(resourceEntity.HREF)
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

// GetDiskByName will get disk by name
func (client *Client) GetDiskByName(name string) (*vcdtypes.Disk, error) {
	klog.Infof("Entered GetDiskByName for name [%s]", name)

	if name == "" {
		return nil, fmt.Errorf("disk name should not be empty")
	}

	if err := client.RefreshToken(); err != nil {
		return nil, fmt.Errorf("unable to refresh vcd client: [%v]", err)
	}

	disks, err := client.govcdGetDisksByName(name, true)
	if err != nil  && err != govcd.ErrorEntityNotFound {
		return nil, fmt.Errorf("unable to GetDiskByName for [%s] from vdc: [%v]", name, err)
	}
	if err == govcd.ErrorEntityNotFound || disks == nil || len(*disks) == 0 {
		// disk not found is a useful error code in some scenarios
		return nil, govcd.ErrorEntityNotFound
	}
	if len(*disks) > 1 {
		return nil, fmt.Errorf("found [%d] > 1 disks with name [%s]", len(*disks), name)
	}

	return &(*disks)[0], nil
}

func (client *Client) govcdAttachedVM(disk *vcdtypes.Disk) ([]*types.Reference, error) {
	klog.Infof("[TRACE] Disk attached VM, HREF: %s\n", disk.HREF)

	var attachedVMLink *types.Link

	// Find the proper link for request
	for _, diskLink := range disk.Link {
		if diskLink.Type == types.MimeVMs {
			klog.Infof("[TRACE] Disk attached VM - found the proper link for request, HREF: %s, name: %s, type: %s,id: %s, rel: %s \n",
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

	_, err := client.vcdClient.Client.ExecuteRequestWithApiVersion(attachedVMLink.HREF, http.MethodGet,
		attachedVMLink.Type, "error getting attached vms: %s", nil, &attachedVMs,
		client.vcdClient.Client.APIVersion)
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
func (client *Client) govcdDelete(disk *vcdtypes.Disk) (govcd.Task, error) {
	klog.Infof("[TRACE] Delete disk, HREF: %s \n", disk.HREF)

	var err error

	// Verify the independent disk is not connected to any VM
	vmRefs, err := client.govcdAttachedVM(disk)
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
	return client.vcdClient.Client.ExecuteTaskRequestWithApiVersion(deleteDiskLink.HREF, http.MethodDelete,
		"", "error delete disk: %s", nil,
		client.vcdClient.Client.APIVersion)
}

// DeleteDisk will delete independent disk by its name
func (client *Client) DeleteDisk(name string) error {
	client.rwLock.Lock()
	defer client.rwLock.Unlock()

	klog.Infof("Entered DeleteDisk for disk [%s]\n", name)
	if err := client.RefreshToken(); err != nil {
		return fmt.Errorf("unable to refresh vcd client: [%v]", err)
	}

	disk, err := client.GetDiskByName(name)
	if err != nil {
		if err == govcd.ErrorEntityNotFound {
			// ignore deletes for non-existent entities
			klog.Infof("Unable to find disk with name [%s]: [%v]", name, err)
			return nil
		}

		return fmt.Errorf("unable to find disk with name [%s]: [%v]", name, err)
	}

	attachedVMs, err := client.govcdAttachedVM(disk)
	if err != nil {
		return fmt.Errorf("unable to find if disk [%s] is attached to a VM: [%v]", name, err)
	}
	if attachedVMs != nil && len(attachedVMs) > 0 {
		return fmt.Errorf("unable to delete disk [%s] that is attached to VMs [%#v]", name, attachedVMs)
	}

	task, err := client.govcdDelete(disk)
	if err != nil {
		return fmt.Errorf("unable to issue delete disk call for [%s]: [%v]", name, err)
	}

	err = task.WaitTaskCompletion()
	if err != nil {
		return fmt.Errorf("failed to wait for deletion task of disk [%s]: [%v]", name, err)
	}

	// update RDE
	if client.ClusterID != "" {
		if err = client.removePvFromRDE(disk.Id); err != nil {
			return fmt.Errorf("unable to remove PV Id [%s] from RDE: [%v]", disk.Id, err)
		}
	}

	return nil
}

// Refresh the disk information by disk href
func (client *Client) govcdRefresh(disk *vcdtypes.Disk) error {
	klog.Infof("[TRACE] Disk refresh, HREF: %s\n", disk.HREF)

	if disk == nil || disk.HREF == "" {
		return fmt.Errorf("cannot refresh, Object is empty")
	}

	unmarshalledDisk := &vcdtypes.Disk{}

	_, err := client.vcdClient.Client.ExecuteRequestWithApiVersion(disk.HREF, http.MethodGet,
		"", "error refreshing independent disk: %s", nil, unmarshalledDisk,
		client.vcdClient.Client.APIVersion)
	if err != nil {
		return err
	}

	// shallow-copy content from unmarshalledDisk to Disk
	*disk = *unmarshalledDisk

	// The request was successful
	return nil
}

// AttachVolume will attach diskName to vm
func (client *Client) AttachVolume(vm *govcd.VM, disk *vcdtypes.Disk) error {
	client.rwLock.Lock()
	defer client.rwLock.Unlock()

	if err := client.RefreshToken(); err != nil {
		return fmt.Errorf("unable to refresh vcd client: [%v]", err)
	}

	if disk == nil {
		return fmt.Errorf("disk passed shoulf not be nil")
	}

	klog.Infof("Entered AttachVolume for vm [%v], disk [%s]\n", vm, disk.Name)

	attachedVMs, err := client.govcdAttachedVM(disk)
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
		Disk:      &types.Reference{HREF: disk.HREF},
	}

	klog.Infof("Attaching disk with params [%v]", params)
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

	if err = client.govcdRefresh(disk); err != nil {
		return fmt.Errorf("unable to refresh disk [%s] for verification: [%v]", disk.Name, err)
	}

	return nil
}

// DetachVolume will detach diskName from vm
func (client *Client) DetachVolume(vm *govcd.VM, diskName string) error {
	client.rwLock.Lock()
	defer client.rwLock.Unlock()

	if err := client.RefreshToken(); err != nil {
		return fmt.Errorf("unable to refresh vcd client: [%v]", err)
	}

	klog.Infof("Entered DetachVolume for vm [%v], disk [%s]\n", vm, diskName)

	disk, err := client.GetDiskByName(diskName)
	if err == govcd.ErrorEntityNotFound {
		klog.Warningf("Unable to find disk [%s]. It is probably already deleted.", diskName)
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to get disk details for [%s]: [%v]", diskName, err)
	}

	attachedVMs, err := client.govcdAttachedVM(disk)
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
	klog.Infof("Successfully detached disk [%s] from VM [%s]", disk.Name, vm.VM.Name)

	return nil
}

func (client *Client) GetRDEPersistentVolumes() ([]string, string, *swaggerClient.DefinedEntity, error) {
	defEnt, _, etag, err := client.apiClient.DefinedEntityApi.GetDefinedEntity(context.TODO(), client.ClusterID)
	if err != nil {
		return nil, "", nil, fmt.Errorf("error when getting defined entity: [%v]", err)
	}

	statusEntry, ok := defEnt.Entity["status"]
	if !ok {
		return nil, "", nil, fmt.Errorf("could not find 'status' entry in defined entity")
	}
	statusMap, ok := statusEntry.(map[string]interface{})
	if !ok {
		return nil, "", nil, fmt.Errorf("unable to convert [%T] to map", statusEntry)
	}
	pvInterfaces := statusMap["persistentVolumes"]
	if pvInterfaces == nil {
		return make([]string, 0), etag, &defEnt, nil
	}

	pvInterfacesSlice, ok := pvInterfaces.([]interface{})
	if !ok {
		return nil, "", nil, fmt.Errorf("unable to convert [%T] to slice of interface", pvInterfaces)
	}
	pvIdStrs := make([]string, len(pvInterfacesSlice))
	for ind, pvInterface := range pvInterfacesSlice {
		currPv, ok := pvInterface.(string)
		if !ok {
			return nil, "", nil, fmt.Errorf("unable to convert [%T] to string", pvInterface)
		}
		pvIdStrs[ind] = currPv
	}
	return pvIdStrs, etag, &defEnt, nil
}

// This function will modify the passed in defEnt
func (client *Client) updateRDEPersistentVolumes(updatedPvs []string, etag string,
	defEnt *swaggerClient.DefinedEntity) (*http.Response, error) {
	statusEntry, ok := defEnt.Entity["status"]
	if !ok {
		return nil, fmt.Errorf("could not find 'status' entry in defined entity")
	}
	statusMap, ok := statusEntry.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to map", statusEntry)
	}

	statusMap["persistentVolumes"] = updatedPvs
	_, httpResponse, err := client.apiClient.DefinedEntityApi.UpdateDefinedEntity(context.TODO(), *defEnt, etag, client.ClusterID)
	if err != nil {
		return httpResponse, fmt.Errorf("error when updating defined entity: [%v]", err)
	}
	return nil, nil
}

func (client *Client) addPvToRDE(addPv string) error {
	for {
		currPvs, etag, defEnt, err := client.GetRDEPersistentVolumes()
		if err != nil {
			return fmt.Errorf("error for getting current RDE PVs: [%v]", err)
		}

		// check if need to update RDE
		foundAddPv := false
		for _, pv := range currPvs {
			if pv == addPv {
				foundAddPv = true
				break
			}
		}
		if foundAddPv {
			return nil // no need to update RDE
		}

		updatedPvs := append(currPvs, addPv)
		httpResponse, err := client.updateRDEPersistentVolumes(updatedPvs, etag, defEnt)
		if err != nil {
			if httpResponse.StatusCode == http.StatusPreconditionFailed {
				continue
			}
			return fmt.Errorf("error when adding pv to RDE: [%v]", err)
		}
		break
	}
	return nil
}

func (client *Client) removePvFromRDE(removePv string) error {
	for {
		currPvs, etag, defEnt, err := client.GetRDEPersistentVolumes()
		if err != nil {
			return fmt.Errorf("error for getting current RDE PVs: [%v]", err)
		}
		// currPvs is guaranteed not to be nil by GetRDEPersistentVolumes
		if len(currPvs) == 0 {
			return fmt.Errorf("no RDE PVs to remove")
		}

		// form updated virtual pv list
		updatedPvs := make([]string, len(currPvs)-1)
		updatedInd := 0
		foundRemovePv := false
		for _, pv := range currPvs {
			if pv == removePv {
				foundRemovePv = true
				continue // for inner loop
			}
			updatedPvs[updatedInd] = pv
			updatedInd += 1
		}
		if !foundRemovePv {
			return nil // no need to update RDE
		}

		httpResponse, err := client.updateRDEPersistentVolumes(updatedPvs, etag, defEnt)
		if err != nil {
			if httpResponse.StatusCode == http.StatusPreconditionFailed {
				continue
			}
			return fmt.Errorf("error when removing pv from RDE: [%v]", err)
		}
		break
	}
	return nil
}
