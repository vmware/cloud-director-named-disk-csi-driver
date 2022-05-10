/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdcsiclient

import (
	"fmt"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"net/http"
	"path/filepath"
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
	return false
}

func TestDiskCreateAttach(t *testing.T) {

	vcdCsiClient := new(DiskManager)

	authFile := filepath.Join(gitRoot, "testdata/auth_test.yaml")
	authFileContent, err := ioutil.ReadFile(authFile)
	assert.NoError(t, err, "There should be no error reading the auth file contents.")

	var authDetails authorizationDetails
	err = yaml.Unmarshal(authFileContent, &authDetails)
	assert.NoError(t, err, "There should be no error parsing auth file content.")

	cloudConfig, err := getTestConfig()
	assert.NoError(t, err, "There should be no error opening and parsing cloud config file contents.")

	// get client
	vcdClient, err := getTestVCDClient(cloudConfig, map[string]interface{}{
		"getVdcClient": true,
		"user":         authDetails.Username,
		"secret":       authDetails.Password,
		"userOrg":      authDetails.UserOrg,
	})
	vcdCsiClient.VCDClient = vcdClient
	vcdCsiClient.VAppName = cloudConfig.VCD.VAppName
	vcdCsiClient.ClusterID = cloudConfig.ClusterID
	assert.NoError(t, err, "Unable to get VCD client")
	require.NotNil(t, vcdClient, "VCD DiskManager should not be nil")

	_, err = vcdClient.VDC.FindStorageProfileReference("dev")
	assert.Errorf(t, err, "unable to find storage profile reference")

	_, err = vcdClient.VDC.FindStorageProfileReference("*")
	assert.NoErrorf(t, err, "unable to find storage profile reference")

	// create disk with bad storage profile: should not succeed
	diskName := fmt.Sprintf("test-pvc-%s", uuid.New().String())
	disk, err := vcdCsiClient.CreateDisk(diskName, 100, VCDBusTypeSCSI, VCDBusSubTypeVirtualSCSI,
		"", "dev", true)
	assert.Errorf(t, err, "should not be able to create disk with storage profile [dev]")
	assert.Nil(t, disk, "disk created should be nil")

	// create disk
	diskName = fmt.Sprintf("test-pvc-%s", uuid.New().String())
	disk, err = vcdCsiClient.CreateDisk(diskName, 100, VCDBusTypeSCSI, VCDBusSubTypeVirtualSCSI,
		"", "*", true)
	assert.NoErrorf(t, err, "unable to create disk with name [%s]", diskName)
	require.NotNil(t, disk, "disk created should not be nil")
	assert.NotNil(t, disk.UUID, "disk UUID should not be nil")

	// try to create same disk with same parameters: should succeed
	disk, err = vcdCsiClient.CreateDisk(diskName, 100, VCDBusTypeSCSI, VCDBusSubTypeVirtualSCSI,
		"", "*", true)
	assert.NoError(t, err, "unable to create disk again with name [%s]", diskName)
	require.NotNil(t, disk, "disk created should not be nil")

	// Check RDE was updated with PV
	currRDEPvs, _, _, err := vcdCsiClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs after creating disk")
	assert.Equal(t, true, foundStringInSlice(disk.Id, currRDEPvs), "Disk Id should be found in RDE")

	// try to create same disk with different parameters; should not succeed
	disk1, err := vcdCsiClient.CreateDisk(diskName, 1000, VCDBusTypeSCSI, VCDBusSubTypeVirtualSCSI,
		"", "", true)
	assert.Error(t, err, "should not be able to create same disk with different parameters")
	assert.Nil(t, disk1, "disk should not be created")

	// get VM nodeID should be the existing VM name
	nodeID := "capi-cluster-md0-86c84dc7f9-ggt6b"

	vdcManager, err := vcdsdk.NewVDCManager(vcdCsiClient.VCDClient, vcdCsiClient.VCDClient.ClusterOrgName, vcdCsiClient.VCDClient.ClusterOVDCName)
	if err != nil {
		assert.NoError(t, err, "unable to get vdcManager")
		//return nil, fmt.Errorf("unable to get vdcManager: [%v]", err)
	}
	//// Todo find a suitable way to handle cluster
	//vApp, err := vdcManager.GetOrCreateVApp(vcdCsiClient.VCDClient.ClusterOVDCName)
	//if err != nil {
	//	assert.NoError(t, err, "unable to get vApp from ovdcNetwork [%s]", vcdCsiClient.VCDClient.ClusterOVDCName)
	//	//return nil, fmt.Errorf("unable to get vApp from ovdcNetwork [%s]: [%v]", vcdCsiClient.VCDClient.ClusterOVDCName, err)
	//}
	//if vApp.VApp == nil || vApp.VApp.Name == "" {
	//	assert.NoError(t, err, "unable to get vApp name from vApp")
	//	//return nil, fmt.Errorf("unable to get vApp name from vApp: [%v]", err)
	//}
	//vdcManager.VAppName = vApp.VApp.Name
	vm, err := vdcManager.FindVMByName(vcdCsiClient.VAppName, nodeID)
	require.NoError(t, err, "unable to find VM [%s] by name", nodeID)
	require.NotNil(t, vm, "vm should not be nil")

	// attach to VM
	err = vcdCsiClient.AttachVolume(vm, disk)
	assert.NoError(t, err, "unable to attach disk [%s] to vm [%#v]", disk.Name, vm)

	attachedVMs, err := vcdCsiClient.govcdAttachedVM(disk)
	assert.NoError(t, err, "unable to get VMs attached to disk [%#v]", disk)
	assert.NotNil(t, attachedVMs, "VM [%s] should be returned", nodeID)
	assert.EqualValues(t, len(attachedVMs), 1, "[%d] VM(s) should be returned", 1)
	assert.EqualValues(t, attachedVMs[0].Name, nodeID, "VM Name should be [%s]", nodeID)

	err = vcdCsiClient.DetachVolume(vm, disk.Name)
	assert.NoError(t, err, "unable to detach disk [%s] from vm [%#v]", disk.Name, vm)

	attachedVMs, err = vcdCsiClient.govcdAttachedVM(disk)
	assert.NoError(t, err, "unable to get VMs attached to disk [%#v]", disk)
	assert.Nil(t, attachedVMs, "no VM should be returned", nodeID)

	err = vcdCsiClient.DeleteDisk(diskName)
	assert.NoError(t, err, "unable to delete disk [%s]", disk.Name)

	// Check PV was removed from RDE
	currRDEPvs, _, _, err = vcdCsiClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs after deleting disk")
	assert.False(t, foundStringInSlice(disk.Id, currRDEPvs), "Disk Id should not be found in RDE")
}

func TestRdeEtag(t *testing.T) {

	vcdCsiClient := new(DiskManager)

	authFile := filepath.Join(gitRoot, "testdata/auth_test.yaml")
	authFileContent, err := ioutil.ReadFile(authFile)
	assert.NoError(t, err, "There should be no error reading the auth file contents.")

	var authDetails authorizationDetails
	err = yaml.Unmarshal(authFileContent, &authDetails)
	assert.NoError(t, err, "There should be no error parsing auth file content.")

	cloudConfig, err := getTestConfig()
	assert.NoError(t, err, "There should be no error opening and parsing cloud config file contents.")

	// get client
	vcdClient, err := getTestVCDClient(cloudConfig, map[string]interface{}{
		"getVdcClient": true,
	})
	vcdCsiClient.VCDClient = vcdClient
	assert.NoError(t, err, "Unable to get VCD client")
	require.NotNil(t, vcdClient, "VCD DiskManager should not be nil")

	rdePvs1, etag1, defEnt1, err := vcdCsiClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs")
	rdePvs2, etag2, defEnt2, err := vcdCsiClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs on second attempt")
	assert.Equal(t, etag1, etag2, "etags from consecutive GETs should be equal")
	origRdePvs := make([]string, len(rdePvs1))
	copy(origRdePvs, rdePvs1)

	// try updating RDE PVs
	addPv1 := "pv1"
	addPv2 := "pv2"
	updatedRdePvs1 := append(rdePvs1, addPv1)
	httpResponse1, err := vcdCsiClient.updateRDEPersistentVolumes(updatedRdePvs1, etag1, defEnt1)
	assert.NoError(t, err, "should have no error when update RDE on first attempt")
	assert.Equal(t, http.StatusOK, httpResponse1.StatusCode, "first RDE update should have an OK (200) response")
	rdePvs3, _, _, err := vcdCsiClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.Equal(t, true, foundStringInSlice(addPv1, rdePvs3), "pv [%s] should be found in rde pvs", addPv1)

	// try updating RDE PVs with outdated etag
	updatedRdePvs2 := append(rdePvs2, addPv2)
	httpResponse2, err := vcdCsiClient.updateRDEPersistentVolumes(updatedRdePvs2, etag2, defEnt2)
	assert.Error(t, err, "updating RDE with outdated etag should have an error")
	assert.Equal(t, http.StatusPreconditionFailed, httpResponse2.StatusCode, "updating RDE does not have precondition failed (412) status code")
	rdePvs3, etag3, defEnt3, err := vcdCsiClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.False(t, foundStringInSlice(addPv2, rdePvs3), "pv [%s] should not be in rde pvs", addPv2)

	// try updating RDE PVs with current etag
	updatedRdePvs3 := append(rdePvs3, addPv2)
	httpResponse3, err := vcdCsiClient.updateRDEPersistentVolumes(updatedRdePvs3, etag3, defEnt3)
	assert.NoError(t, err, "should have no error updating RDE with current etag")
	assert.Equal(t, http.StatusOK, httpResponse3.StatusCode, "updating PV had status code [%d] instead of 200 (OK)", httpResponse3.StatusCode)
	rdePvs4, etag4, defEnt4, err := vcdCsiClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.Equal(t, true, foundStringInSlice(addPv2, rdePvs4), "pv [%s] should be found in RDE PVs", addPv2)

	// clean up: reset RDE PVs
	httpResponse5, err := vcdCsiClient.updateRDEPersistentVolumes(origRdePvs, etag4, defEnt4)
	assert.NoError(t, err, "should have no error updating RDE with current etag")
	assert.Equal(t, http.StatusOK, httpResponse5.StatusCode, "updating PV had status code 200 (OK)")
	rdePvs5, _, _, err := vcdCsiClient.GetRDEPersistentVolumes()
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.False(t, foundStringInSlice(addPv1, rdePvs5), "pv [%s] should not be found in rde pvs", addPv1)
	assert.False(t, foundStringInSlice(addPv2, rdePvs5), "pv [%s] should not be found in rde pvs", addPv2)
}
