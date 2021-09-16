/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"fmt"
	"io"
	"io/ioutil"
	"k8s.io/klog"

	yaml "gopkg.in/yaml.v2"
)

// VCDConfig :
type VCDConfig struct {
	Host string `yaml:"host"`
	VDC  string `yaml:"vdc"`
	Org  string `yaml:"org"`

	User         string `yaml:"user" default:""`
	Secret       string `yaml:"secret" default:""`
	RefreshToken string `yaml:"refreshToken" default:""`
}

// CloudConfig contains the config that will be read from the secret
type CloudConfig struct {
	VCD       VCDConfig     `yaml:"vcd"`
	ClusterID string        `yaml:"clusterid"`
}

func ParseCloudConfig(configReader io.Reader) (*CloudConfig, error) {
	var err error
	config := &CloudConfig{}

	decoder := yaml.NewDecoder(configReader)
	decoder.SetStrict(true)

	if err = decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("Unable to decode yaml file: [%v]", err)
	}

	return config, validateCloudConfig(config)
}

func SetAuthorization(config *CloudConfig) error {
	var refreshToken, username, secret []byte
	refreshToken, err := ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/refreshToken")
	if err != nil {
		klog.Infof("unable to get refresh token. Looking for username and password")
		// Couldn't find refresh token. See if username and password can be obtained.
		username, err = ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/username")
		if err != nil {
			return fmt.Errorf("unable to get username: [%v]", err)
		}
		secret, err = ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/password")
		if err != nil {
			return fmt.Errorf("unable to get password: [%v]", err)
		}
		config.VCD.User = string(username)
		config.VCD.Secret = string(secret)
	} else {
		config.VCD.RefreshToken = string(refreshToken)
	}
	return nil
}

func validateCloudConfig(config *CloudConfig) error {
	// TODO: needs more validation
	if config == nil {
		return fmt.Errorf("nil config passed")
	}

	if config.VCD.Host == "" {
		return fmt.Errorf("need a valid vCloud Host")
	}

	if config.VCD.Org == "" {
		return fmt.Errorf("need a valid vCloud Organization")
	}

	if config.VCD.VDC == "" {
		return fmt.Errorf("need a valid vCloud Organization VDC")
	}

	return nil
}
