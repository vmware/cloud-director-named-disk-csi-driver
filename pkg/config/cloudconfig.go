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
	"os"
	"strings"

	yaml "gopkg.in/yaml.v2"
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

func getUserAndOrg(fullUserName string, clusterOrg string) (userOrg string, userName string, err error) {
	// If the full username is specified as org/user, the scenario is that the user
	// may belong to an org different from the cluster, but still has the
	// necessary rights to view the VMs on this org. Else if the username is
	// specified as just user, the scenario is that the user is in the same org
	// as the cluster.
	parts := strings.Split(string(fullUserName), "/")
	if len(parts) > 2 {
		return "", "", fmt.Errorf(
			"invalid username format; expected at most two fields separated by /, obtained [%d]",
			len(parts))
	}
	if len(parts) == 1 {
		userOrg = clusterOrg
		userName = parts[0]
	} else {
		userOrg = parts[0]
		userName = parts[1]
	}

	return userOrg, userName, nil
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
	// check if refresh token is present.
	if _, err := os.Stat("/etc/kubernetes/vcloud/basic-auth/refreshToken"); err == nil {
		// refresh token is present. Populate only refresh token and keep user and secret empty
		refreshToken, err := ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/refreshToken")
		if err != nil {
			return fmt.Errorf("unable to get refresh token: [%v]", err)
		}
		config.VCD.RefreshToken = string(refreshToken)
		if config.VCD.RefreshToken != "" {
			return nil
		}
	}
	klog.Infof("Unable to get refresh token. Looking for username and password")

	username, err := ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/username")
	if err != nil {
		return fmt.Errorf("unable to get username: [%v]", err)
	}

	secret, err := ioutil.ReadFile("/etc/kubernetes/vcloud/basic-auth/password")
	if err != nil {
		return fmt.Errorf("unable to get password: [%v]", err)
	}

	config.VCD.UserOrg, config.VCD.User, err = getUserAndOrg(string(username), config.VCD.Org)
	if err != nil {
		return fmt.Errorf("unable to get user org and name: [%v]", err)
	}

	config.VCD.Secret = string(secret)

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

	if config.VCD.VAppName == "" {
		return fmt.Errorf("need a valid vApp name")
	}

	return nil
}
