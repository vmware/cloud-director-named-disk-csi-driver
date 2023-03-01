/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdcsiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/util"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdtypes"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
	"gopkg.in/yaml.v3"

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

func convertMapToComponentStatus(componentStatusMap map[string]interface{}) (*vcdsdk.ComponentStatus, error) {
	componentStatusBytes, err := json.Marshal(componentStatusMap)
	if err != nil {
		return nil, fmt.Errorf("failed to convert componentStatusMap to byte array: [%v]", err)
	}

	var cs vcdsdk.ComponentStatus
	err = json.Unmarshal(componentStatusBytes, &cs)
	if err != nil {
		return nil, fmt.Errorf("failed to read bytes from componentStatus [%#v] to ComponentStatus object: [%v]", componentStatusMap, err)
	}

	return &cs, nil
}

func TestUpdateRDE(t *testing.T) {
	diskManager := new(DiskManager)

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
	diskManager.VCDClient = vcdClient
	diskManager.ClusterID = cloudConfig.ClusterID
	assert.NoError(t, err, "Unable to get VCD client")
	require.NotNil(t, vcdClient, "VCD DiskManager should not be nil")
	diskManager.UpgradeRDEPersistentVolumes()
}

func TestDiskCreateAttach(t *testing.T) {

	diskManager := new(DiskManager)

	authFile := filepath.Join(gitRoot, "testdata/auth_test.yaml")
	authFileContent, err := ioutil.ReadFile(authFile)
	assert.NoError(t, err, "There should be no error reading the auth file contents.")

	var authDetails authorizationDetails
	err = yaml.Unmarshal(authFileContent, &authDetails)
	assert.NoError(t, err, "There should be no error parsing auth file content.")

	cloudConfig, err := getTestConfig()
	assert.NoError(t, err, "There should be no error opening and parsing cloud config file contents.")

	type args struct {
		storageProfile string
		nodeID         string
		busTuple       BusTuple
	}
	tests := []struct {
		name    string
		args    args
		want    *vcdtypes.Disk
		wantErr bool
	}{
		{
			name: "SATA",
			args: args{
				storageProfile: "*",
				nodeID:         "capi-cluster-2-md0-85c8585c96-8bqj2",
				busTuple:       BusTypesSet["sata"],
			},
		},
		{
			name: "Paravirtual(SCSI)",
			args: args{
				storageProfile: "*",
				nodeID:         "capi-cluster-2-md0-85c8585c96-8bqj2",
				busTuple:       BusTypesSet["scsi_paravirtual"],
			},
		},
		{
			name: "NVME",
			args: args{
				storageProfile: "*",
				nodeID:         "capi-cluster-2-md0-85c8585c96-8bqj2",
				busTuple:       BusTypesSet["nvme"],
			},
		},
		{
			name: "LSI Logic Parallel (SCSI)",
			args: args{
				storageProfile: "*",
				nodeID:         "capi-cluster-2-md0-85c8585c96-8bqj2",
				busTuple:       BusTypesSet["scsi_lsi_logic_parallel"],
			},
		},
		{
			name: "LSI Logic SAS (SCSI)",
			args: args{
				storageProfile: "*",
				nodeID:         "capi-cluster-2-md0-85c8585c96-8bqj2",
				busTuple:       BusTypesSet["scsi_lsi_logic_sas"],
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// get client
			vcdClient, err := getTestVCDClient(cloudConfig, map[string]interface{}{
				"getVdcClient": true,
				"user":         authDetails.Username,
				"secret":       authDetails.Password,
				"userOrg":      authDetails.UserOrg,
			})
			diskManager.VCDClient = vcdClient
			vAppName := cloudConfig.VCD.VAppName
			diskManager.ClusterID = cloudConfig.ClusterID
			assert.NoError(t, err, "Unable to get VCD client")
			require.NotNil(t, vcdClient, "VCD DiskManager should not be nil")

			_, err = vcdClient.VDC.FindStorageProfileReference("dev")
			assert.Errorf(t, err, "unable to find storage profile reference")

			_, err = vcdClient.VDC.FindStorageProfileReference(tt.args.storageProfile)
			assert.NoErrorf(t, err, "unable to find storage profile reference")

			diskName := fmt.Sprintf("test-pvc-%s", uuid.New().String())

			// create disk with bad storage profile: should not succeed
			disk, err := diskManager.CreateDisk(diskName, 100, tt.args.busTuple.BusType, tt.args.busTuple.BusSubType,
				"", "dev", true)
			assert.Errorf(t, err, "should not be able to create disk with storage profile [dev]")
			assert.Nil(t, disk, "disk created should be nil")

			// create disk
			diskName = fmt.Sprintf("test-pvc-%s", uuid.New().String())

			disk, err = diskManager.CreateDisk(diskName, 100, tt.args.busTuple.BusType, tt.args.busTuple.BusSubType, "", tt.args.storageProfile, true)
			assert.NoErrorf(t, err, "unable to create disk with name [%s]", diskName)
			require.NotNil(t, disk, "disk created should not be nil")
			assert.NotNil(t, disk.UUID, "disk UUID should not be nil")

			// try to create same disk with same parameters: should succeed
			disk, err = diskManager.CreateDisk(diskName, 100, tt.args.busTuple.BusType, tt.args.busTuple.BusSubType, "", tt.args.storageProfile, true)
			assert.NoError(t, err, "unable to create disk again with name [%s]", diskName)
			require.NotNil(t, disk, "disk created should not be nil")

			// Check RDE was updated with PV
			clusterOrg, err := diskManager.VCDClient.VCDClient.GetOrgByName(diskManager.VCDClient.ClusterOrgName)
			assert.NoError(t, err, "unable to get org by name [%s]", diskManager.VCDClient.ClusterOrgName)
			assert.NotNil(t, clusterOrg, "retrieved org is nil for org name [%s]", diskManager.VCDClient.ClusterOrgName)
			assert.NotNil(t, clusterOrg.Org, "retrieved org is nil for org name [%s]", diskManager.VCDClient.ClusterOrgName)

			defEnt, _, _, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
				diskManager.ClusterID, clusterOrg.Org.ID)
			assert.NoError(t, err, "unable to get RDE")

			currRDEPvs, err := GetCAPVCDRDEPersistentVolumes(&defEnt)
			assert.NoError(t, err, "unable to get RDE PVs after creating disk")
			assert.Equal(t, true, foundStringInSlice(disk.Name, currRDEPvs), "Disk Id should be found in RDE")

			// try to create same disk with different parameters; should not succeed
			disk1, err := diskManager.CreateDisk(diskName, 1000, tt.args.busTuple.BusType, tt.args.busTuple.BusSubType, "", "", true)
			assert.Error(t, err, "should not be able to create same disk with different parameters")
			assert.Nil(t, disk1, "disk should not be created")

			// get VM nodeID should be the existing VM name
			vdcManager, err := vcdsdk.NewVDCManager(diskManager.VCDClient, diskManager.VCDClient.ClusterOrgName, diskManager.VCDClient.ClusterOVDCName)
			assert.NoError(t, err, "unable to get vdcManager")
			// Todo find a suitable way to handle cluster
			vm, err := vdcManager.FindVMByName(vAppName, tt.args.nodeID)
			require.NoError(t, err, "unable to find VM [%s] by name", tt.args.nodeID)
			require.NotNil(t, vm, "vm should not be nil")

			// attach to VM
			err = diskManager.AttachVolume(vm, disk)
			assert.NoError(t, err, "unable to attach disk [%s] to vm [%#v]", disk.Name, vm)

			attachedVMs, err := diskManager.govcdAttachedVM(disk)
			assert.NoError(t, err, "unable to get VMs attached to disk [%#v]", disk)
			assert.NotNil(t, attachedVMs, "VM [%s] should be returned", tt.args.nodeID)
			assert.EqualValues(t, len(attachedVMs), 1, "[%d] VM(s) should be returned", 1)
			assert.EqualValues(t, attachedVMs[0].Name, tt.args.nodeID, "VM Name should be [%s]", tt.args.nodeID)

			err = diskManager.DetachVolume(vm, disk.Name)
			assert.NoError(t, err, "unable to detach disk [%s] from vm [%#v]", disk.Name, vm)

			attachedVMs, err = diskManager.govcdAttachedVM(disk)
			assert.NoError(t, err, "unable to get VMs attached to disk [%#v]", disk)
			assert.Nil(t, attachedVMs, "no VM should be returned", tt.args.nodeID)

			err = diskManager.DeleteDisk(diskName)
			assert.NoError(t, err, "unable to delete disk [%s]", disk.Name)

			// Check PV was removed from RDE
			defEnt, _, _, err = diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
				diskManager.ClusterID, clusterOrg.Org.ID)
			assert.NoError(t, err, "unable to get RDE")
			currRDEPvs, err = GetCAPVCDRDEPersistentVolumes(&defEnt)
			assert.NoError(t, err, "unable to get RDE PVs after deleting disk")
			assert.False(t, foundStringInSlice(disk.Name, currRDEPvs), "Disk Id should not be found in RDE")
		})
	}
}

func GetCAPVCDRDEPersistentVolumes(rde *swaggerClient.DefinedEntity) ([]string, error) {
	statusEntry, ok := rde.Entity["status"]
	if !ok {
		return nil, fmt.Errorf("could not find 'status' entry in defined entity")
	}
	statusMap, ok := statusEntry.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to map", statusEntry)
	}
	csiEntry, ok := statusMap[vcdsdk.ComponentCSI]
	if !ok {
		return make([]string, 0), nil
	}
	csiMap, ok := csiEntry.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to map[string]interface{}", csiEntry)
	}
	componentStatus, _ := convertMapToComponentStatus(csiMap)
	pvNameArray := make([]string, len(componentStatus.VCDResourceSet))
	namedDiskCount := 0
	for _, rs := range componentStatus.VCDResourceSet {
		if rs.Type == util.ResourcePersistentVolume {
			pvNameArray[namedDiskCount] = rs.Name
			namedDiskCount++
		}
	}
	return pvNameArray[0:namedDiskCount], nil
}

func TestRdeEtag(t *testing.T) {

	diskManager := new(DiskManager)

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
	diskManager.VCDClient = vcdClient
	diskManager.ClusterID = cloudConfig.ClusterID
	assert.NoError(t, err, "Unable to get VCD client")
	require.NotNil(t, vcdClient, "VCD DiskManager should not be nil")

	clusterOrg, err := diskManager.VCDClient.VCDClient.GetOrgByName(diskManager.VCDClient.ClusterOrgName)
	assert.NoError(t, err, "unable to get org by name [%s]", diskManager.VCDClient.ClusterOrgName)
	assert.NotNil(t, clusterOrg, "retrieved org is nil for org name [%s]", diskManager.VCDClient.ClusterOrgName)
	assert.NotNil(t, clusterOrg.Org, "retrieved org is nil for org name [%s]", diskManager.VCDClient.ClusterOrgName)

	defEnt1, _, etag1, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
		diskManager.ClusterID, clusterOrg.Org.ID)
	assert.NoError(t, err, "unable to get RDE")
	rdePvs1, err := GetCAPVCDRDEPersistentVolumes(&defEnt1)
	assert.NoError(t, err, "unable to get RDE PVs")

	defEnt2, _, etag2, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
		diskManager.ClusterID, clusterOrg.Org.ID)
	assert.NoError(t, err, "unable to get RDE")
	rdePvs2, err := GetCAPVCDRDEPersistentVolumes(&defEnt2)
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.NoError(t, err, "unable to get RDE PVs on second attempt")
	assert.Equal(t, etag1, etag2, "etags from consecutive GETs should be equal")
	origRdePvs := make([]string, len(rdePvs1))
	copy(origRdePvs, rdePvs1)

	// try updating RDE PVs
	addPv1Name := "pv1"
	addPv2Name := "pv2"
	updatedRdePvs1 := append(rdePvs1, addPv1Name)
	httpResponse1, err := diskManager.addRDEPersistentVolumes(updatedRdePvs1, etag1, &defEnt1)
	assert.NoError(t, err, "should have no error when update RDE on first attempt")
	assert.Equal(t, http.StatusOK, httpResponse1.StatusCode, "first RDE update should have an OK (200) response")
	defEnt3, _, _, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
		diskManager.ClusterID, clusterOrg.Org.ID)
	assert.NoError(t, err, "unable to get RDE")
	rdePvs3, err := GetCAPVCDRDEPersistentVolumes(&defEnt3)
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.Equal(t, true, foundStringInSlice(addPv1Name, rdePvs3), "pv [%s] should be found in rde pvs", addPv1Name)

	// try updating RDE PVs with outdated etag
	updatedRdePvs2 := append(rdePvs2, addPv2Name)
	httpResponse2, err := diskManager.addRDEPersistentVolumes(updatedRdePvs2, etag2, &defEnt2)
	assert.Error(t, err, "updating RDE with outdated etag should have an error")
	assert.Equal(t, http.StatusPreconditionFailed, httpResponse2.StatusCode, "updating RDE does not have precondition failed (412) status code")
	defEnt3, _, etag3, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
		diskManager.ClusterID, clusterOrg.Org.ID)
	assert.NoError(t, err, "unable to get RDE")
	rdePvs3, err = GetCAPVCDRDEPersistentVolumes(&defEnt3)
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.False(t, foundStringInSlice(addPv2Name, rdePvs3), "pv [%s] should not be in rde pvs", addPv2Name)

	// try updating RDE PVs with current etag
	updatedRdePvs3 := append(rdePvs3, addPv2Name)
	httpResponse3, err := diskManager.addRDEPersistentVolumes(updatedRdePvs3, etag3, &defEnt3)
	assert.NoError(t, err, "should have no error updating RDE with current etag")
	assert.Equal(t, http.StatusOK, httpResponse3.StatusCode, "updating PV had status code [%d] instead of 200 (OK)", httpResponse3.StatusCode)
	defEnt4, _, etag4, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
		diskManager.ClusterID, clusterOrg.Org.ID)
	assert.NoError(t, err, "unable to get RDE")
	rdePvs4, err := GetCAPVCDRDEPersistentVolumes(&defEnt4)
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.Equal(t, true, foundStringInSlice(addPv2Name, rdePvs4), "pv [%s] should be found in RDE PVs", addPv2Name)
	httpResponse5, err := diskManager.removeRDEPersistentVolumes(origRdePvs, etag4, &defEnt4)
	assert.NoError(t, err, "should have no error updating RDE with current etag")
	assert.Equal(t, http.StatusOK, httpResponse5.StatusCode, "updating PV had status code 200 (OK)")
	defEnt5, _, _, err := diskManager.VCDClient.APIClient.DefinedEntityApi.GetDefinedEntity(context.TODO(),
		diskManager.ClusterID, clusterOrg.Org.ID)
	rdePvs5, err := GetCAPVCDRDEPersistentVolumes(&defEnt5)
	assert.NoError(t, err, "unable to get RDE PVs")
	assert.False(t, foundStringInSlice(addPv1Name, rdePvs5), "pv [%s] should not be found in rde pvs", addPv1Name)
	assert.False(t, foundStringInSlice(addPv2Name, rdePvs5), "pv [%s] should not be found in rde pvs", addPv2Name)
}
