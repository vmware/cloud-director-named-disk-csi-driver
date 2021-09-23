/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package csi

import (
	"context"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdclient"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	"math"
)

const (
	MbToBytes           = int64(1024 * 1024)
	GbToBytes           = int64(1024 * 1024 * 1024)
	DefaultDiskSizeInGb = int64(1)
)

const (
	BusTypeParameter        = "busType"
	BusSubTypeParameter     = "busSubType"
	StorageProfileParameter = "storageProfile"
	FileSystemParameter     = "filesystem"
	EphemeralVolumeContext  = "csi.storage.k8s.io/ephemeral"

	DiskIDAttribute     = "diskID"
	VMFullNameAttribute = "vmID"
	DiskUUIDAttribute   = "diskUUID"
	FileSystemAttribute = "filesystem"
)

var (
	// BusTypesFromValues is a map of different possible BusTypes from id to string
	BusTypesFromValues = map[string]string{
		"5":  "IDE",
		"6":  "SCSI",
		"20": "SATA",
	}
)


type controllerServer struct {
	Driver *VCDDriver
	VCDClient  *vcdclient.Client
}

// NewControllerService creates a controllerService
func NewControllerService(driver *VCDDriver, vcdClient *vcdclient.Client) csi.ControllerServer {
	return &controllerServer{
		Driver: driver,
		VCDClient:  vcdClient,
	}
}

func (cs *controllerServer) isDiskShareable(volumeCapabilities []*csi.VolumeCapability) bool {
	// no set operation, need to use hashmap instead of set, but there are only 3 comparisons, so
	// just compare directly
	for _, volumeCapability := range volumeCapabilities {
		if volumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY ||
			volumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER ||
			volumeCapability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER {
			return true
		}
	}

	return false
}

func (cs *controllerServer) CreateVolume(ctx context.Context,
	req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: req should not be nil")
	}

	klog.Infof("CreateVolume: called with req [%#v]", *req)

	if err := cs.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}

	diskName := req.GetName()
	if len(diskName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume name not provided")
	}

	volumeCapabilities := req.GetVolumeCapabilities()
	if volumeCapabilities == nil || len(volumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume: VolumeCapabilities should be provided")
	}
	for _, volumeCapability := range volumeCapabilities {
		if _, ok := VolumeCapabilityAccessModesStringMap[volumeCapability.AccessMode.Mode.String()]; !ok {
			return nil, status.Errorf(codes.Unavailable, "CreateVolume: volume capability [%s] not supported",
				volumeCapability.String())
		}
	}

	shareable := cs.isDiskShareable(volumeCapabilities)

	var volSizeBytes int64 = DefaultDiskSizeInGb * GbToBytes
	if req.GetCapacityRange() != nil && req.GetCapacityRange().RequiredBytes != 0 {
		volSizeBytes = req.GetCapacityRange().GetRequiredBytes()
	}
	sizeMB := int64(math.Ceil(float64(volSizeBytes) / float64(MbToBytes)))
	klog.Infof("CreateVolume: requesting volume [%s] with size [%d] MiB, shareable [%v]",
		diskName, sizeMB, shareable)

	busType := vcdclient.VCDBusTypeSCSI
	busSubType := vcdclient.VCDBusSubTypeVirtualSCSI

	storageProfile, _ := req.Parameters[StorageProfileParameter]

	disk, err := cs.VCDClient.CreateDisk(diskName, sizeMB, busType,
		busSubType, "", storageProfile, shareable)
	if err != nil {
		return nil, fmt.Errorf("unable to create disk [%s] with sise [%d]MB: [%v]",
			diskName, sizeMB, err)
	}
	klog.Infof("Successfully created disk [%s] of size [%d]MB", diskName, sizeMB)

	attributes := make(map[string]string)
	attributes[BusTypeParameter] = BusTypesFromValues[disk.BusType]
	attributes[BusSubTypeParameter] = disk.BusSubType
	attributes[StorageProfileParameter] = disk.StorageProfile.Name
	attributes[DiskIDAttribute] = disk.Id

	fsType := ""
	ok := false
	if fsType, ok = req.Parameters[FileSystemParameter]; !ok {
		fsType = "ext4"
		klog.Infof("No FS specified for raw disk [%s]. Hence defaulting to [%s].", diskName, fsType)
	}
	attributes[FileSystemParameter] = fsType

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      disk.Name,
			CapacityBytes: sizeMB * MbToBytes,
			VolumeContext: attributes,
		},
	}
	return resp, nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("req should not be nil")
	}

	klog.Infof("DeleteVolume: called with req [%#v]", *req)
	volumeID := req.GetVolumeId()

	if err := cs.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}

	err := cs.VCDClient.DeleteDisk(volumeID)
	if err != nil {
		if err == govcd.ErrorEntityNotFound {
			klog.Infof("Volume [%s] is already deleted.", volumeID)
			return &csi.DeleteVolumeResponse{}, nil
		}

		return nil, status.Errorf(codes.Internal, "DeleteVolume failed: [%v]", err)
	}

	klog.Infof("Volume %s deleted successfully", req.VolumeId)
	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerPublishVolume(ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {

	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"ControllerPublishVolume: req should not be nil")
	}
	klog.Infof("ControllerPublishVolume: called with req [%#v]", *req)

	if err := cs.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}

	nodeID := req.GetNodeId()
	if len(nodeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume: NodeId must be provided")
	}

	diskName := req.GetVolumeId()
	if len(diskName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume: VolumeId must be provided")
	}

	// Get basic params from volumeContext and add it to publishContext, so that it can be used for static PV
	// provisioned volumes
	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"ControllerPublishVolume: Volume capability not provided")
	}
	mountDetails := volumeCapability.GetMount()
	if mountDetails == nil {
		return nil, status.Error(codes.InvalidArgument,
			"ControllerPublishVolume: Volume capability does not have mount capabilities set")
	}

	klog.Infof("Getting node details for [%s]", nodeID)
	vm, err := cs.VCDClient.FindVMByName(nodeID)
	if err != nil {
		return nil, fmt.Errorf("unable to find VM for node [%s]: [%v]", nodeID, err)
	}

	klog.Infof("Getting disk details for [%s]", diskName)
	disk, err := cs.VCDClient.GetDiskByName(diskName)
	if err != nil {
		return nil, fmt.Errorf("unable to find disk [%s]: [%v]", diskName, err)
	}
	klog.Infof("Obtained disk: [%#v]\n", disk)

	klog.Infof("Attaching volume [%s] to node [%s]", diskName, nodeID)
	err = cs.VCDClient.AttachVolume(vm, disk)
	if err != nil {
		if err == govcd.ErrorEntityNotFound {
			return nil, status.Errorf(codes.NotFound, "could not provision disk [%s] in vcd", diskName)
		}
		return nil, err
	}
	klog.Infof("Successfully attached volume %s to node %s ", diskName, nodeID)

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			VMFullNameAttribute: vm.VM.Name,
			DiskIDAttribute:     diskName,
			DiskUUIDAttribute:   disk.UUID,
			FileSystemAttribute: mountDetails.FsType,
		},
	}, nil
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {

	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "ControllerUnpublishVolume: req should not be nil")
	}
	klog.Infof("ControllerUnpublishVolume: called with req [%#v]", *req)

	if err := cs.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}

	nodeID := req.GetNodeId()
	if len(nodeID) == 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"ControllerUnpublishVolume: Node ID must be provided")
	}

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"ControllerUnpublishVolume: Volume ID must be provided")
	}

	vm, err := cs.VCDClient.FindVMByName(nodeID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound,
			"Could not find VM with nodeID [%s] from which to detach [%s]", nodeID, volumeID)
	}

	err = cs.VCDClient.DetachVolume(vm, volumeID)
	if err != nil {
		if err == govcd.ErrorEntityNotFound {
			return nil, status.Errorf(codes.NotFound, "Volume [%s] does not exist", volumeID)
		}

		return nil, err
	}
	klog.Infof("Volume [%s] unpublished successfully", volumeID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context,
	req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ValidateVolumeCapabilities not implemented")
}

func (cs *controllerServer) ListVolumes(ctx context.Context,
	req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListVolumes not implemented")
}

func (cs *controllerServer) GetCapacity(ctx context.Context,
	req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCapacity not implemented")
}

func (cs *controllerServer) ControllerGetCapabilities(ctx context.Context,
	req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	klog.Infof("ControllerGetCapabilities: called with args [%#v]", *req)
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.Driver.controllerServiceCapabilities,
	}, nil
}

func (cs *controllerServer) CreateSnapshot(ctx context.Context,
	req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.InvalidArgument, "CreateSnapshot not implemented")
}

func (cs *controllerServer) DeleteSnapshot(ctx context.Context,
	req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.InvalidArgument, "DeleteSnapshot not implemented")
}

func (cs *controllerServer) ListSnapshots(ctx context.Context,
	req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.InvalidArgument, "ListSnapshots not implemented")
}

func (cs *controllerServer) ControllerExpandVolume(ctx context.Context,
	req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.InvalidArgument, "ControllerExpandVolume not implemented")
}

func (cs *controllerServer) ControllerGetVolume(ctx context.Context,
	req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.InvalidArgument, "ControllerGetVolume not implemented")
}
