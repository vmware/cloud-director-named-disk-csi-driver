/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package vsphereclient

// VSphereAuthConfig : contains config related to vsphere auth
type VSphereAuthConfig struct {
	Host         string `json:"host"`
	User         string `json:"user"`
	Password     string `json:"password"`
	Insecure     bool   `json:"insecure"`
	Token        string `json:"token"`
}

func NewVSphereAuthConfigFromSecrets(host string, user string, secret string, insecure bool) *VSphereAuthConfig {
	return &VSphereAuthConfig{
		Host: host,
		User: user,
		Password: secret,
		Insecure: insecure,
	}
}
