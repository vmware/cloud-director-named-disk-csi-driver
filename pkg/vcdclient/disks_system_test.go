/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/


package vcdclient

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func foundStringInSlice(find string, slice []string) bool {
	for _, currElement := range slice {
		if currElement == find {
			return true
		}
	}
	return  false
}

func TestDiskCreateAttach(t *testing.T) {

	// get client
	vcdClient, err := getTestVCDClient(map[string]interface{}{
		"getVdcClient": true,
	})
	assert.NoError(t, err, "Unable to get VCD client")
	require.NotNil(t, vcdClient, "VCD Client should not be nil")

	_, err = vcdClient.vdc.FindStorageProfileReference("dev")
	assert.Errorf(t, err, "unable to find storage profile reference")

	_, err = vcdClient.vdc.FindStorageProfileReference("*")
	assert.NoErrorf(t, err, "unable to find storage profile reference")

	// create disk with bad storage profile: should not succeed
	diskName := fmt.Sprintf("test-pvc-%s", uuid.New().String())
	disk, err := vcdClient.CreateDisk(diskName, 100, VCDBusTypeSCSI, VCDBusSubTypeVirtualSCSI,
		"", "dev", true)
	assert.Errorf(t, err, "should not be able to create disk with storage profile [dev]")
	assert.Nil(t, disk, "disk created should be nil")

	// create disk
	diskName = fmt.Sprintf("test-pvc-%s", uuid.New().String())
	disk, err = vcdClient.CreateDisk(diskName, 100, VCDBusTypeSCSI, VCDBusSubTypeVirtualSCSI,
		"", "*", true)
	assert.NoErrorf(t, err, "unable to create disk with name [%s]", diskName)
	require.NotNil(t, disk, "disk created should not be nil")
	assert.NotNil(t, disk.UUID, "disk UUID should not be nil")

	// try to create same disk with same parameters: should succeed
	disk, err = vcdClient.CreateDisk(diskName, 100, VCDBusTypeSCSI, VCDBusSubTypeVirtualSCSI,
		"", "*", true)
	assert.NoError(t, err, "unable to create disk again with name [%s]", diskName)
	require.NotNil(t, disk, "disk created should not be nil")

	// Check RDE was updated with PV
	currRDEPvs, _, _, err := vcdClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs after creating disk")
	assert.Equal(t, true, foundStringInSlice(disk.Id, currRDEPvs), "Disk Id should be found in RDE")


	// try to create same disk with different parameters; should not succeed
	disk1, err := vcdClient.CreateDisk(diskName, 1000, VCDBusTypeSCSI, VCDBusSubTypeVirtualSCSI,
		"", "", true)
	assert.Error(t, err, "should not be able to create same disk with different parameters")
	assert.Nil(t, disk1, "disk should not be created")


	// get VM
	nodeID := "vm1"
	vm, err := vcdClient.FindVMByName(nodeID)
	require.NoError(t, err, "unable to find VM [%s] by name", nodeID)
	require.NotNil(t, vm, "vm should not be nil")

	// attach to VM
	err = vcdClient.AttachVolume(vm, disk)
	assert.NoError(t, err, "unable to attach disk [%s] to vm [%#v]", disk.Name, vm)

	attachedVMs, err := vcdClient.govcdAttachedVM(disk)
	assert.NoError(t, err, "unable to get VMs attached to disk [%#v]", disk)
	assert.NotNil(t, attachedVMs, "VM [%s] should be returned", nodeID)
	assert.EqualValues(t, len(attachedVMs), 1, "[%d] VM(s) should be returned", 1)
	assert.EqualValues(t, attachedVMs[0].Name, nodeID, "VM Name should be [%s]", nodeID)

	err = vcdClient.DetachVolume(vm, disk.Name)
	assert.NoError(t, err, "unable to detach disk [%s] from vm [%#v]", disk.Name, vm)

	attachedVMs, err = vcdClient.govcdAttachedVM(disk)
	assert.NoError(t, err, "unable to get VMs attached to disk [%#v]", disk)
	assert.Nil(t, attachedVMs, "no VM should be returned", nodeID)

	err = vcdClient.DeleteDisk(diskName)
	assert.NoError(t, err, "unable to delete disk [%s]", disk.Name)

	// Check PV was removed from RDE
	currRDEPvs, _, _, err = vcdClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs after deleting disk")
	assert.Equal(t, false, foundStringInSlice(disk.Id, currRDEPvs), "Disk Id should not be found in RDE")

	return
}
