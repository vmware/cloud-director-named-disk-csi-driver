/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"fmt"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"k8s.io/klog"
	"strings"
)

// VCDConfig :
type VCDConfig struct {
	Host     string `yaml:"host"`
	VDC      string `yaml:"vdc"`
	Org      string `yaml:"org"`
	UserOrg  string // this defaults to Org or a prefix of User
	VAppName string `yaml:"vAppName"`

	// The User, Secret and RefreshToken are obtained from a secret mounted to /etc/kubernetes/vcloud/basic-auth
	// with files at username, password and refreshToken respectively.
	// The User could be userOrg/user or just user. In the latter case, we assume
	// that Org is the org in which the user exists.
	User         string
	Secret       string
	RefreshToken string
}

// CloudConfig contains the config that will be read from the secret
type CloudConfig struct {
	VCD       VCDConfig `yaml:"vcd"`
	ClusterID string    `yaml:"clusterid"`
}

func ParseCloudConfig(configReader io.Reader) (*CloudConfig, error) {
	var err error
	config := &CloudConfig{}

	decoder := yaml.NewDecoder(configReader)
	decoder.SetStrict(true)

	if err = decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("Unable to decode yaml file: [%v]", err)
	}
	config.VCD.Host = strings.TrimRight(config.VCD.Host, "/")
	return config, validateCloudConfig(config)
}

func SetAuthorization(config *CloudConfig) error {
	refreshToken, err := ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/refreshToken")
	if err != nil {
		klog.Infof("Unable to get refresh token: [%v]", err)
	} else {
		config.VCD.RefreshToken = strings.TrimSuffix(string(refreshToken), "\n")
	}

	username, err := ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/username")
	if err != nil {
		klog.Infof("Unable to get username: [%v]", err)
	} else {
		trimmedUserName := strings.TrimSuffix(string(username), "\n")
		if string(trimmedUserName) != "" {
			config.VCD.UserOrg, config.VCD.User, err = vcdsdk.GetUserAndOrg(trimmedUserName, config.VCD.Org, config.VCD.UserOrg)
			if err != nil {
				return fmt.Errorf("unable to get user org and name: [%v]", err)
			}
		}
	}
	if config.VCD.UserOrg == "" {
		config.VCD.UserOrg = strings.TrimSuffix(config.VCD.Org, "\n")
	}

	secret, err := ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/password")
	if err != nil {
		klog.Infof("Unable to get password: [%v]", err)
	} else {
		config.VCD.Secret = strings.TrimSuffix(string(secret), "\n")
	}

	if config.VCD.RefreshToken != "" {
		klog.Infof("Using non-empty refresh token.")
		return nil
	}
	if config.VCD.User != "" && config.VCD.UserOrg != "" && config.VCD.Secret != "" {
		klog.Infof("Using username/secret based credentials.")
		return nil
	}

	return fmt.Errorf("unable to get valid set of credentials from secrets")
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

	if config.VCD.VAppName == "" {
		return fmt.Errorf("need a valid vApp name")
	}

	return nil
}
