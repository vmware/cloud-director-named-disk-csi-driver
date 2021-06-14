/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

// +build integration

package vcdclient

import (
	"fmt"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/csi"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	disk, err := vcdClient.CreateDisk(diskName, 100, csi.VCDBusTypeSCSI, csi.VCDBusSubTypeVirtualSCSI,
		"", "dev", true)
	assert.Errorf(t, err, "should not be able to create disk with storage profile [dev]")
	assert.Nil(t, disk, "disk created should be nil")

	// create disk
	diskName = fmt.Sprintf("test-pvc-%s", uuid.New().String())
	disk, err = vcdClient.CreateDisk(diskName, 100, csi.VCDBusTypeSCSI, csi.VCDBusSubTypeVirtualSCSI,
		"", "*", true)
	assert.NoErrorf(t, err, "unable to create disk with name [%s]", diskName)
	assert.NotNil(t, disk, "disk created should not be nil")

	// try to create same disk with same parameters: should succeed
	disk, err = vcdClient.CreateDisk(diskName, 100, csi.VCDBusTypeSCSI, csi.VCDBusSubTypeVirtualSCSI,
		"", "*", true)
	assert.NoError(t, err, "unable to create disk again with name [%s]", diskName)
	assert.NotNil(t, disk, "disk created should not be nil")

	// try to create same disk with different parameters; should not succeed
	disk1, err := vcdClient.CreateDisk(diskName, 1000, csi.VCDBusTypeSCSI, csi.VCDBusSubTypeVirtualSCSI,
		"", "", true)
	assert.Error(t, err, "should not be able to create same disk with different parameters")
	assert.Nil(t, disk1, "disk should not be created")


	// get VM
	nodeID := "vm1"
	vm, err := vcdClient.FindVMByName(nodeID)
	assert.NoError(t, err, "unable to find VM [%s] by name", nodeID)
	assert.NotNil(t, vm, "vm should not be nil")

	// attach to VM
	err = vcdClient.AttachVolume(vm, disk.Name)
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

	return
}
