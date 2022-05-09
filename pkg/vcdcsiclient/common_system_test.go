/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdcsiclient

import (
	"fmt"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/config"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	"os"
	"path/filepath"
)

var (
	gitRoot string = ""
)

type authorizationDetails struct {
	Username               string `yaml:"username"`
	Password               string `yaml:"password"`
	RefreshToken           string `yaml:"refreshToken"`
	UserOrg                string `yaml:"userOrg"`
	SystemUser             string `yaml:"systemUser"`
	SystemUserPassword     string `yaml:"systemUserPassword"`
	SystemUserRefreshToken string `yaml:"systemUserRefreshToken"`
}

func init() {
	gitRoot = os.Getenv("GITROOT")
	if gitRoot == "" {
		// It is okay to panic here as this will be caught during dev
		panic("GITROOT should be set")
	}
}

func getTestConfig() (*config.CloudConfig, error) {
	testConfigFilePath := filepath.Join(gitRoot, "testdata/config_test.yaml")
	configReader, err := os.Open(testConfigFilePath)
	if err != nil {
		return nil, fmt.Errorf("Unable to open file [%s]: [%v]", testConfigFilePath, err)
	}
	defer configReader.Close()

	cloudConfig, err := config.ParseCloudConfig(configReader)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse cloud config file [%s]: [%v]", testConfigFilePath, err)
	}
	return cloudConfig, nil
}

func getStrValStrict(val interface{}, defaultVal string) string {
	if strVal, ok := val.(string); ok {
		return strVal
	}

	return defaultVal
}

func getBoolValStrict(val interface{}, defaultVal bool) bool {
	if boolVal, ok := val.(bool); ok {
		return boolVal
	}

	return defaultVal
}

func getTestVCDClient(config *config.CloudConfig, inputMap map[string]interface{}) (*vcdsdk.Client, error) {
	cloudConfig := *config // Make a copy of cloudConfig so modified inputs don't carry over to next test
	insecure := true
	getVdcClient := false
	if inputMap != nil {
		for key, val := range inputMap {
			switch key {
			case "host":
				cloudConfig.VCD.Host = getStrValStrict(val, cloudConfig.VCD.Host)
			case "org":
				cloudConfig.VCD.Org = getStrValStrict(val, cloudConfig.VCD.Org)
			case "user":
				cloudConfig.VCD.User = getStrValStrict(val, cloudConfig.VCD.User)
			case "secret":
				cloudConfig.VCD.Secret = getStrValStrict(val, cloudConfig.VCD.Secret)
			case "insecure":
				insecure = getBoolValStrict(val, true)
			case "clusterID":
				cloudConfig.ClusterID = getStrValStrict(val, cloudConfig.ClusterID)
			case "getVdcClient":
				getVdcClient = getBoolValStrict(val, false)
			case "refreshToken":
				cloudConfig.VCD.RefreshToken = getStrValStrict(val, cloudConfig.VCD.RefreshToken)
			case "userOrg":
				cloudConfig.VCD.UserOrg = getStrValStrict(val, cloudConfig.VCD.UserOrg)
			}
		}
	}

	return vcdsdk.NewVCDClientFromSecrets(
		cloudConfig.VCD.Host,
		cloudConfig.VCD.Org,
		cloudConfig.VCD.VDC,
		cloudConfig.VCD.UserOrg,
		cloudConfig.VCD.User,
		cloudConfig.VCD.Secret,
		cloudConfig.VCD.RefreshToken,
		insecure,
		getVdcClient,
	)
}
