/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdcsiclient

import (
	"fmt"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/config"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdclient"
	"os"
	"path/filepath"
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

// Ports :
//type Ports struct {
//	HTTP  int32 `yaml:"http" default:"80"`
//	HTTPS int32 `yaml:"https" default:"443"`
//}

// OneArm :
//type OneArm struct {
//	StartIP string `yaml:"startIP"`
//	EndIP   string `yaml:"endIP"`
//}

// LBConfig :
//type LBConfig struct {
//	OneArm           *vcdclient.OneArm `yaml:"oneArm,omitempty"`
//	Ports            Ports             `yaml:"ports"`
//	CertificateAlias string            `yaml:"certAlias"`
//}

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

//func getOneArmValStrict(val interface{}, defaultVal *vcdclient.OneArm) *vcdclient.OneArm {
//	if oneArmVal, ok := val.(*vcdclient.OneArm); ok {
//		return oneArmVal
//	}
//
//	return defaultVal
//}

//func getInt32ValStrict(val interface{}, defaultVal int32) int32 {
//	if int32Val, ok := val.(int32); ok {
//		return int32Val
//	}
//
//	return defaultVal
//}

func getTestVCDClient(inputMap map[string]interface{}) (*vcdclient.Client, error) {

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

	insecure := true
	//oneArm := &vcdclient.OneArm{
	//	StartIPAddress: cloudConfig.LB.OneArm.StartIP,
	//	EndIPAddress:   cloudConfig.LB.OneArm.EndIP,
	//}
	cloudConfig.VCD.RefreshToken = "cUb5vlHzTJvkki47yIAI8qH1JWXI29ek"
	getVdcClient := false
	if inputMap != nil {
		for key, val := range inputMap {
			switch key {
			case "host":
				cloudConfig.VCD.Host = getStrValStrict(val, cloudConfig.VCD.Host)
			case "org":
				cloudConfig.VCD.Org = getStrValStrict(val, cloudConfig.VCD.Org)
			//case "network":
			//	cloudConfig.VCD.VDCNetwork = getStrValStrict(val, cloudConfig.VCD.VDCNetwork)
			//case "subnet":
			//	cloudConfig.VCD.VIPSubnet = getStrValStrict(val, cloudConfig.VCD.VIPSubnet)
			case "user":
				cloudConfig.VCD.User = getStrValStrict(val, cloudConfig.VCD.User)
			case "secret":
				cloudConfig.VCD.Secret = getStrValStrict(val, cloudConfig.VCD.Secret)
			case "insecure":
				insecure = getBoolValStrict(val, true)
			case "clusterID":
				cloudConfig.ClusterID = getStrValStrict(val, cloudConfig.ClusterID)
			//case "oneArm":
			//	oneArm = getOneArmValStrict(val, oneArm)
			//case "httpPort":
			//	cloudConfig.LB.Ports.HTTP = getInt32ValStrict(val, cloudConfig.LB.Ports.HTTP)
			//case "httpsPort":
			//	cloudConfig.LB.Ports.HTTPS = getInt32ValStrict(val, cloudConfig.LB.Ports.HTTPS)
			//case "certAlias":
			//	cloudConfig.LB.CertificateAlias = getStrValStrict(val, cloudConfig.LB.CertificateAlias)
			case "getVdcClient":
				getVdcClient = getBoolValStrict(val, false)
			case "refreshToken":
				cloudConfig.VCD.RefreshToken = getStrValStrict(val, cloudConfig.VCD.RefreshToken)
			case "userOrg":
				cloudConfig.VCD.UserOrg = getStrValStrict(val, cloudConfig.VCD.UserOrg)
			}
		}
	}

	return vcdclient.NewVCDClientFromSecrets(
		cloudConfig.VCD.Host,
		cloudConfig.VCD.Org,
		cloudConfig.VCD.VDC,
		cloudConfig.VCD.VAppName,
		"",
		"",
		cloudConfig.VCD.UserOrg,
		cloudConfig.VCD.User,
		cloudConfig.VCD.Secret,
		cloudConfig.VCD.RefreshToken,
		insecure,
		cloudConfig.ClusterID,
		nil,
		0,
		0,
		"",
		getVdcClient,
	)
}
