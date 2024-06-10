/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package csi

import (
	"context"

	"github.com/vmware/cloud-director-named-disk-csi-driver/version"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog"
)

type identityServer struct {
	Driver *VCDDriver
}

// NewIdentityServer will create a new csi.IdentityServer with driver
func NewIdentityServer(driver *VCDDriver) csi.IdentityServer {
	return &identityServer{
		Driver: driver,
	}
}

func (ids *identityServer) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	klog.Infof("Probe: called with args [%+v]", *req)
	return &csi.ProbeResponse{}, nil
}

func (ids *identityServer) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	klog.Infof("GetPluginInfo: called with args [%+v]", *req)
	resp := &csi.GetPluginInfoResponse{
		Name:          ids.Driver.name,
		VendorVersion: version.Version,
	}

	return resp, nil
}

func (ids *identityServer) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	klog.Infof("GetPluginCapabilities: called with args [%+v]", *req)
	resp := &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
			{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_OFFLINE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_ONLINE,
					},
				},
			},
		},
	}

	return resp, nil
}
