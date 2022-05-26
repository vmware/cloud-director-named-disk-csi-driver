/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package csi

import (
	"context"
	"fmt"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/util"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdcsiclient"
	"github.com/vmware/cloud-director-named-disk-csi-driver/version"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	"net"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/klog"
)

// VCDDriver is the main controller of the csi-plugin
type VCDDriver struct {
	name     string
	nodeID   string
	version  string
	endpoint string

	cs  csi.ControllerServer
	ns  csi.NodeServer
	ids csi.IdentityServer

	srv *grpc.Server

	volumeCapabilityAccessModes   []*csi.VolumeCapability_AccessMode
	controllerServiceCapabilities []*csi.ControllerServiceCapability
	nodeServiceCapabilities       []*csi.NodeServiceCapability
}

var (
	// VolumeCapabilityAccessModesList is used in CreateVolume as well
	VolumeCapabilityAccessModesList = []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	}

	VolumeCapabilityAccessModesStringMap = make(map[string]bool)
)

// NewDriver creates new VCDDriver
func NewDriver(nodeID string, endpoint string) (*VCDDriver, error) {

	klog.Infof("Driver: [%s] Version: [%s]", Name, version.Version)

	d := &VCDDriver{
		name:     Name,
		nodeID:   nodeID,
		version:  version.Version,
		endpoint: endpoint,
	}

	d.volumeCapabilityAccessModes = make([]*csi.VolumeCapability_AccessMode, len(VolumeCapabilityAccessModesList))
	for idx, volumeCapabilityAccessMode := range VolumeCapabilityAccessModesList {
		klog.Infof("Adding volume capability [%s]\n", volumeCapabilityAccessMode.String())
		VolumeCapabilityAccessModesStringMap[volumeCapabilityAccessMode.String()] = true
		d.volumeCapabilityAccessModes[idx] = &csi.VolumeCapability_AccessMode{
			Mode: volumeCapabilityAccessMode,
		}
	}

	nodeServiceCapabilityList := []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
	}
	d.nodeServiceCapabilities = make([]*csi.NodeServiceCapability, len(nodeServiceCapabilityList))
	for idx, nodeServiceCapability := range nodeServiceCapabilityList {
		klog.Infof("Enabling node service capability: %v", nodeServiceCapability.String())
		d.nodeServiceCapabilities[idx] = &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: nodeServiceCapability,
				},
			},
		}
	}

	controllerServerCapabilitiesRPCList := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
	}
	d.controllerServiceCapabilities = make([]*csi.ControllerServiceCapability, len(controllerServerCapabilitiesRPCList))
	for idx, controllerServiceCapabilityRPC := range controllerServerCapabilitiesRPCList {
		klog.Infof("Enabling controller service capability: [%s]", controllerServiceCapabilityRPC.String())
		d.controllerServiceCapabilities[idx] = &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: controllerServiceCapabilityRPC,
				},
			},
		}
	}

	return d, nil
}

// Setup will setup the driver and add controller, node and identity servers
func (d *VCDDriver) Setup(diskManager *vcdcsiclient.DiskManager, VAppName string, nodeID string, upgradeRde bool) {
	klog.Infof("Driver setup called")
	d.ns = NewNodeService(d, nodeID)
	d.cs = NewControllerService(d, diskManager.VCDClient, diskManager.ClusterID, VAppName)
	d.ids = NewIdentityServer(d)
	if !upgradeRde {
		klog.Infof("Skipping RDE CSI section upgrade as upgradeRde flag is invalid")
		return
	}
	if !util.IsValidEntityId(diskManager.ClusterID) {
		klog.Infof("Skipping RDE CSI section upgrade as invalid RDE: [%s]", diskManager.ClusterID)
		return
	}
	if vcdsdk.IsNativeClusterEntityType(diskManager.ClusterID) {
		klog.Infof("Skipping RDE CSI section upgrade as native cluster: [%s]", diskManager.ClusterID)
		return
	}
	if vcdsdk.IsCAPVCDEntityType(diskManager.ClusterID) {
		err := diskManager.UpgradeRDEPersistentVolumes()
		if err != nil {
			//Todo add csi.errors: hard failure
			klog.Infof(err.Error())
		}
	}

}

// Run will start driver gRPC server to communicated with Kubernetes
// and register controller, node and identity servers
func (d *VCDDriver) Run() error {

	scheme, addr, err := util.ParseEndpoint(d.endpoint)
	if err != nil {
		return fmt.Errorf("unable to parse endpoint [%s]: [%v]", d.endpoint, err)
	}

	if scheme == "unix" {
		addr = "/" + addr
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			klog.Fatalf("Failed to remove [%s]: [%v]", addr, err)
		}
	}

	listener, err := net.Listen(scheme, addr)
	if err != nil {
		klog.Fatalf("Failed to listen on [%s]: [%v]", addr, err)
		return err
	}

	logGRPC := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {

		klog.Infof("GRPC call: [%s]: [%#v]", info.FullMethod, req)

		resp, err := handler(ctx, req)
		if err != nil {
			klog.Errorf("GRPC error: function [%s] req [%#v]: [%v]",
				info.FullMethod, req, err)
		}

		return resp, err
	}

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logGRPC),
	}
	d.srv = grpc.NewServer(opts...)

	if d.ids != nil {
		csi.RegisterIdentityServer(d.srv, d.ids)
	}
	if d.cs != nil {
		csi.RegisterControllerServer(d.srv, d.cs)
	}
	if d.ns != nil {
		csi.RegisterNodeServer(d.srv, d.ns)
	}

	klog.Infof("Listening for connections on address: %#v", listener.Addr().String())
	return d.srv.Serve(listener)
}

// Stop will stop the grpc server
func (d *VCDDriver) Stop() {
	klog.Infof("Stopping server")
	d.srv.Stop()
}
