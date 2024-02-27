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

	pluginCapabilityVolumeExpansion []*csi.PluginCapability_VolumeExpansion
}

var (
	VolumeCapabilityAccessModesList = []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	}

	// VolumeCapabilityAccessModesStringMap is a cache for available VolumeCapabilityAccessModesList. This is later
	// referenced in CreateVolume of the controller service.
	VolumeCapabilityAccessModesStringMap = make(map[string]bool)

	nodeServiceCapabilityList = []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
		csi.NodeServiceCapability_RPC_EXPAND_VOLUME, // node needs to expand in ONLINE mode
	}

	controllerServerCapabilitiesRPCList = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME, // controller needs to expand in OFFLINE modes
	}

	// VCD with named independent disks currently supports:
	// 1. ONLINE volume expansion for all modes except RWO/RWX
	// 2. OFFLINE volume expansion for RWO/RWX
	pluginCapabilityVolumeExpansion = []csi.PluginCapability_VolumeExpansion_Type{
		csi.PluginCapability_VolumeExpansion_OFFLINE,
		csi.PluginCapability_VolumeExpansion_ONLINE,
	}
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

	d.nodeServiceCapabilities = make([]*csi.NodeServiceCapability, len(nodeServiceCapabilityList))
	for idx, nodeServiceCapability := range nodeServiceCapabilityList {
		klog.Infof("Enabling node service capability: [%s]", nodeServiceCapability.String())
		d.nodeServiceCapabilities[idx] = &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: nodeServiceCapability,
				},
			},
		}
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

	d.pluginCapabilityVolumeExpansion = make([]*csi.PluginCapability_VolumeExpansion,
		len(pluginCapabilityVolumeExpansion))
	for idx, pluginCapabilityVolumeExpansionType := range pluginCapabilityVolumeExpansion {
		klog.Infof("Enabling plugin capability [%s]", pluginCapabilityVolumeExpansionType.String())
		d.pluginCapabilityVolumeExpansion[idx] = &csi.PluginCapability_VolumeExpansion{
			Type: pluginCapabilityVolumeExpansionType,
		}
	}

	return d, nil
}

// Setup will setup the driver and add controller, node and identity servers
func (d *VCDDriver) Setup(diskManager *vcdcsiclient.DiskManager, VAppName string, nodeID string, upgradeRde bool,
	isZoneEnabledCluster bool, zm *vcdsdk.ZoneMap) error {
	klog.Infof("Driver setup called")
	d.ns = NewNodeService(d, nodeID)
	d.cs = NewControllerService(d, diskManager.VCDClient, diskManager.ClusterID, VAppName, isZoneEnabledCluster, zm)
	d.ids = NewIdentityServer(d)
	if !upgradeRde {
		klog.Infof("Skipping RDE CSI section upgrade as upgradeRde flag is false")
		return nil
	}
	if !vcdsdk.IsValidEntityId(diskManager.ClusterID) {
		klog.Infof("Skipping RDE CSI section upgrade as invalid RDE: [%s]", diskManager.ClusterID)
		return nil
	}
	// ******************   Upgrade PersistentVolume Section in RDE if needed   ******************
	if vcdsdk.IsCAPVCDEntityType(diskManager.ClusterID) {
		err := diskManager.UpgradeRDEPersistentVolumes()
		if err != nil {
			if rdeErr := diskManager.AddToErrorSet(util.RdeUpgradeError, "", "", map[string]interface{}{"Detailed Error": err.Error()}); rdeErr != nil {
				klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.RdeUpgradeError, diskManager.ClusterID, rdeErr)
			}
			return fmt.Errorf("CSI section upgrade failed when CAPVCD RDE is present, [%v]", err)
		}
		if addEventRdeErr := diskManager.AddToEventSet(util.RdeUpgradeEvent, "", "", nil); addEventRdeErr != nil {
			klog.Errorf("unable to add event [%s] into [CSI.Events] in RDE [%s]", util.RdeUpgradeEvent, diskManager.ClusterID)
		}
		if removeErrorRdeErr := diskManager.RemoveFromErrorSet(util.RdeUpgradeError, "", ""); removeErrorRdeErr != nil {
			klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.RdeUpgradeError, diskManager.ClusterID)
		}
	}

	// ******************   Upgrade CSI Section in RDE   ******************
	dstCSIVersion := d.version
	//Todo: Validate the compare srcCSIVersion and dstCSIVersion
	_, err := diskManager.ConvertToLatestCSIVersionFormat(dstCSIVersion)
	if err != nil {
		if rdeErr := diskManager.AddToErrorSet(util.RdeUpgradeError, "", "", map[string]interface{}{"Detailed Error": err.Error()}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.RdeUpgradeError, diskManager.ClusterID, rdeErr)
		}
		return fmt.Errorf("CSI section upgrade failed when CAPVCD RDE is present, [%v]", err)
	}
	if addEventRdeErr := diskManager.AddToEventSet(util.RdeUpgradeEvent, "", "", map[string]interface{}{"Detailed Description": "Upgrade CSI Status to the latest version in use"}); addEventRdeErr != nil {
		klog.Errorf("unable to add event [%s] into [CSI.Events] in RDE [%s]", util.RdeUpgradeEvent, diskManager.ClusterID)
	}
	if removeErrorRdeErr := diskManager.RemoveFromErrorSet(util.RdeUpgradeError, "", ""); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.RdeUpgradeError, diskManager.ClusterID)
	}
	return nil
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
