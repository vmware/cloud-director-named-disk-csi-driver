/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"fmt"
	"io"

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

// Ports :
type Ports struct {
	HTTP  int32 `yaml:"http" default:"80"`
	HTTPS int32 `yaml:"https" default:"443"`
}

// OneArm :
type OneArm struct {
	StartIP string `yaml:"startIP"`
	EndIP   string `yaml:"endIP"`
}

// LBConfig :
type LBConfig struct {
	OneArm           *OneArm `yaml:"oneArm,omitempty"`
	Ports            Ports   `yaml:"ports"`
	CertificateAlias string  `yaml:"certAlias"`
}

// VSphereConfig :
type VSphereConfig struct {
	Host   string `yaml:"host"`
	User   string `yaml:"user" default:""`
	Secret string `yaml:"secret" default:""`
}

// CloudConfig contains the config that will be read from the secret
type CloudConfig struct {
	VCD       VCDConfig     `yaml:"vcd"`
	LB        LBConfig      `yaml:"loadbalancer"`
	ClusterID string        `yaml:"clusterid"`
	VSphere   VSphereConfig `yaml:"vsphere"`
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

func validateCloudConfig(config *CloudConfig) error {
	// TODO: needs more validation
	if config == nil {
		return fmt.Errorf("nil config passed")
	}

	if config.VCD.Host == "" {
		return fmt.Errorf("need a valid vCloud Host")
	}

	return nil
}
