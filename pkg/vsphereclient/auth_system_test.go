/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

// +build integration

package vsphereclient

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/config"
	"os"
	"path/filepath"
	"testing"
)

var (
	gitRoot string = ""
)

func init() {
	gitRoot = os.Getenv("GITROOT")
	if gitRoot == "" {
		// It is okay to panic here as this will be caught during dev
		panic("GITROOT should be set")
	}
}

func TestNewVSphereAuthConfigFromSecrets(t *testing.T) {

	testConfigFilePath := filepath.Join(gitRoot, "testdata/config_test.yaml")
	configReader, err := os.Open(testConfigFilePath)
	assert.NoError(t, err, "unable to open file [%s]", testConfigFilePath)
	defer func(){
		err = configReader.Close()
		assert.NoError(t, err, "Unable to close file [%s]", testConfigFilePath)
	}()

	cloudConfig, err := config.ParseCloudConfig(configReader)
	assert.NoError(t, err, "unable to parse cloud config")

	vsphereClient, err := NewVSphereClientFromSecrets(
		cloudConfig.VSphere.Host,
		cloudConfig.VSphere.User,
		cloudConfig.VSphere.Secret,
		true)
	assert.NoError(t, err, "unable to get vsphere client")
	assert.NotNil(t, vsphereClient, "vsphere client should not be nil")

	ctx := context.Background()
	vms, err := vsphereClient.GetAllVMs(ctx)
	assert.NoError(t, err, "unable to get all vms")
	assert.NotNil(t, vms, "there should be some VMs at least")

	diskUUID, err := vsphereClient.GetDiskUuid(ctx, "vm1-Lt22", "test123-321")
	assert.NoError(t, err, "should not error out while getting disk uuid")
	assert.NotEmpty(t, diskUUID, "disk uuid should not be empty")

	return
}
