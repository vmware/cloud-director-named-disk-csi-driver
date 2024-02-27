/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package csi

import (
	"context"
	"errors"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/util"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdcsiclient"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"github.com/vmware/go-vcloud-director/v2/types/v56"
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
	ZoneNameParameter       = "zoneName"
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
	Driver      *VCDDriver
	DiskManager *vcdcsiclient.DiskManager
	VAppName    string
}

// NewControllerService creates a controllerService
func NewControllerService(driver *VCDDriver, vcdClient *vcdsdk.Client, clusterID string, vAppName string,
	isZoneEnabledCluster bool, zm *vcdsdk.ZoneMap) csi.ControllerServer {

	org, err := vcdClient.VCDClient.GetOrgByName(vcdClient.ClusterOrgName)
	if err != nil {
		panic(fmt.Errorf("unable to find org [%s]: [%v]", vcdClient.ClusterOrgName, err))
	}

	return &controllerServer{
		Driver: driver,
		DiskManager: &vcdcsiclient.DiskManager{
			VCDClient:            vcdClient,
			ClusterID:            clusterID,
			Org:                  org,
			IsZoneEnabledCluster: isZoneEnabledCluster,
			ZoneMap:              zm,
		},
		VAppName: vAppName,
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

func IsBlockVolume(volumeCapabilities []*csi.VolumeCapability) (bool, error) {
	for _, volumeCapability := range volumeCapabilities {
		if _, ok := VolumeCapabilityAccessModesStringMap[volumeCapability.AccessMode.Mode.String()]; !ok {
			return false, fmt.Errorf("unknown volume capability [%s] or not supported", volumeCapability.String())
		}

		if volumeCapability.GetBlock() != nil {
			return true, nil
		}
	}

	return false, nil
}

func (cs *controllerServer) CreateVolume(ctx context.Context,
	req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume: req should not be nil")
	}
	klog.Infof("CreateVolume: called with req [%#v]", *req)

	if err := cs.DiskManager.VCDClient.RefreshBearerToken(); err != nil {
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

	isBlock, err := IsBlockVolume(volumeCapabilities)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"CreateVolume: Unable to determine if Volume is a block volume from capabilities [%v]", volumeCapabilities)
	}

	shareable := cs.isDiskShareable(volumeCapabilities)

	var volSizeBytes int64 = DefaultDiskSizeInGb * GbToBytes
	if req.GetCapacityRange() != nil && req.GetCapacityRange().RequiredBytes != 0 {
		volSizeBytes = req.GetCapacityRange().GetRequiredBytes()
	}
	sizeMB := int64(math.Ceil(float64(volSizeBytes) / float64(MbToBytes)))
	klog.Infof("CreateVolume: requesting volume [%s] with size [%d] MiB, shareable [%v]",
		diskName, sizeMB, shareable)

	busType := vcdcsiclient.VCDBusTypeSCSI
	busSubType := vcdcsiclient.VCDBusSubTypeVirtualSCSI

	storageProfile, _ := req.Parameters[StorageProfileParameter]

	vdcIdentifierSpecified := ""
	if cs.DiskManager.IsZoneEnabledCluster {
		zoneNameSpecified, ok := req.Parameters[ZoneNameParameter]
		if !ok || zoneNameSpecified == "" {
			return nil, status.Errorf(codes.InvalidArgument,
				"zone enabled clusters should specify a zone where the storage is to be created")
		}
		if zoneNameSpecified != "" && cs.DiskManager.ZoneMap != nil {
			for ovdcName, zoneName := range cs.DiskManager.ZoneMap.VdcToZoneMap {
				if zoneName == zoneNameSpecified {
					vdcIdentifierSpecified = ovdcName
				}
			}
		}
		if vdcIdentifierSpecified == "" {
			return nil, status.Errorf(codes.InvalidArgument,
				"zone specified [%s] does not have an ovdc in the zonemap [%v]", zoneNameSpecified,
				cs.DiskManager.ZoneMap)
		}
	} else {
		vdcIdentifierSpecified = cs.DiskManager.VCDClient.ClusterOVDCName
	}
	klog.Infof("OVDC where disk will be created is [%s] when zone-enabled is [%s]",
		vdcIdentifierSpecified, cs.DiskManager.IsZoneEnabledCluster)

	vdc, err := cs.DiskManager.Org.GetVDCByNameOrId(vdcIdentifierSpecified, false)
	if err != nil {
		return nil, fmt.Errorf("unable to get ovdc by identifier [%s] in org [%s]: [%v]", vdcIdentifierSpecified,
			cs.DiskManager.Org.Org.Name, err)
	}

	disk, err := cs.DiskManager.CreateDisk(diskName, vdc, sizeMB, busType, busSubType, cs.DiskManager.ClusterID,
		storageProfile, shareable, cs.DiskManager.ZoneMap)
	if err != nil {
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskCreateError, "", diskName, map[string]interface{}{"Detailed Error": err.Error()}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.DiskCreateError, cs.DiskManager.ClusterID, rdeErr)
		}
		return nil, fmt.Errorf("unable to create disk [%s] with size [%d]MB: [%v]",
			diskName, sizeMB, err)
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskCreateError, "", diskName); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskCreateError, cs.DiskManager.ClusterID)
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
		if isBlock {
			klog.Infof("Not setting a default FS for raw disk [%s] since it is a block mount", diskName)
		} else {
			fsType = "ext4"
			klog.Infof("No FS specified for raw disk [%s]. Hence defaulting to [%s].", diskName, fsType)
		}
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

	if err := cs.DiskManager.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"DeleteVolume: Volume ID must be provided")
	}

	//volumeID is a diskName
	err := cs.DiskManager.DeleteDisk(volumeID, cs.DiskManager.ZoneMap)
	if err != nil {
		if errors.Is(err, govcd.ErrorEntityNotFound) {
			klog.Infof("Volume [%s] is already deleted.", volumeID)
			return &csi.DeleteVolumeResponse{}, nil
		}
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskDeleteError, "", volumeID,
			map[string]interface{}{"Detailed Error": err}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.DiskDeleteError,
				cs.DiskManager.ClusterID, rdeErr)
		}
		return nil, status.Errorf(codes.Internal, "DeleteVolume failed: [%v]", err)
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskDeleteError, "", volumeID); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskDeleteError, cs.DiskManager.ClusterID)
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

	if err := cs.DiskManager.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}

	vmName := req.GetNodeId()
	if len(vmName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume: NodeId must be provided")
	}

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume: VolumeId must be provided")
	}

	// Get basic params from volumeContext and add it to publishContext, so that it can be used for static PV
	// provisioned volumes
	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"ControllerPublishVolume: Volume capability not provided")
	}

	isBlock := volumeCapability.GetBlock() != nil

	fsType := ""
	if !isBlock {
		mountDetails := volumeCapability.GetMount()
		if mountDetails == nil {
			return nil, status.Error(codes.InvalidArgument,
				"ControllerPublishVolume: Volume capability does not have mount capabilities set")
		}
		fsType = mountDetails.FsType
	}

	klog.Infof("Getting node details for [%s]", vmName)
	orgManager := &vcdsdk.OrgManager{
		Client:  cs.DiskManager.VCDClient,
		OrgName: cs.DiskManager.VCDClient.ClusterOrgName,
	}

	ovdcNameList := make([]string, 0)
	if cs.DiskManager.ZoneMap != nil {
		for ovdcName, _ := range cs.DiskManager.ZoneMap.VdcToZoneMap {
			ovdcNameList = append(ovdcNameList, ovdcName)
		}
	} else {
		ovdcNameList = append(ovdcNameList, cs.DiskManager.VCDClient.ClusterOVDCName)
	}

	vm, _, err := orgManager.SearchVMAcrossVDCs(vmName, cs.VAppName, "", ovdcNameList)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to find VM for node [%s] in vdc list [%v]: [%v]",
			vmName, ovdcNameList, err)
	}

	klog.Infof("Getting disk details for [%s]", volumeID)
	disk, err := cs.DiskManager.GetDiskByNameOrId(volumeID, cs.DiskManager.ZoneMap, nil)
	if err != nil {
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskQueryError, "", volumeID,
			map[string]interface{}{"Detailed Error": fmt.Errorf("unable query disk [%s]: [%v]", volumeID, err)}); rdeErr != nil {
			klog.Errorf("unable to unable to add error [%s] into [CSI.Errors] in RDE [%s], %v",
				util.DiskQueryError, cs.DiskManager.ClusterID, rdeErr)
		}

		return nil, fmt.Errorf("unable to find disk [%s]: [%v]", volumeID, err)
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskQueryError, "", volumeID); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskQueryError, cs.DiskManager.ClusterID)
	}
	klog.Infof("Obtained disk: [%#v]\n", disk)

	klog.Infof("Attaching volume [%s] to node [%s]", volumeID, vmName)
	err = cs.DiskManager.AttachVolume(vm, disk)
	if err != nil {
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskAttachError, "", volumeID,
			map[string]interface{}{"Detailed Error": err.Error(), "VM Info": vmName}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v",
				util.DiskAttachError, cs.DiskManager.ClusterID, rdeErr)
		}
		if errors.Is(err, govcd.ErrorEntityNotFound) {
			return nil, status.Errorf(codes.NotFound, "could not provision disk [%s] in vcd", volumeID)
		}
		return nil, err
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskAttachError, "", volumeID); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskAttachError, cs.DiskManager.ClusterID)
	}
	klog.Infof("Successfully attached volume %s to node %s ", volumeID, vmName)

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			VMFullNameAttribute: vm.VM.Name,
			DiskIDAttribute:     volumeID,
			DiskUUIDAttribute:   disk.UUID,
			FileSystemAttribute: fsType,
		},
	}, nil
}

func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {

	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "ControllerUnpublishVolume: req should not be nil")
	}
	klog.Infof("ControllerUnpublishVolume: called with req [%#v]", *req)

	if err := cs.DiskManager.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Errorf(codes.InvalidArgument,
			"ControllerUnpublishVolume: Volume ID must be provided")
	}

	disk, err := cs.DiskManager.GetDiskByNameOrId(volumeID, cs.DiskManager.ZoneMap, nil)
	if err != nil {
		if errors.Is(err, govcd.ErrorEntityNotFound) {
			klog.Infof("Volume [%s] is not available. Hence will mark as unpublished.", volumeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.NotFound, "Unable to find disk [%s] by name: [%v]", volumeID, err)
	}
	attachedVMs, err := cs.DiskManager.GovcdAttachedVM(disk)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to find VMs attached to disk named [%s]: [%v]",
			volumeID, err)
	}
	if len(attachedVMs) == 0 {
		klog.Infof("Volume [%s] is not attached to any node. Hence will not run the unpublish.", volumeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	vmName := req.GetNodeId()
	if vmName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "ControllerUnpublishVolume: Node ID must be provided")
	}
	orgManager := &vcdsdk.OrgManager{
		Client:  cs.DiskManager.VCDClient,
		OrgName: cs.DiskManager.VCDClient.ClusterOrgName,
	}

	ovdcNameList := make([]string, 0)
	if cs.DiskManager.ZoneMap != nil {
		for ovdcName, _ := range cs.DiskManager.ZoneMap.VdcToZoneMap {
			ovdcNameList = append(ovdcNameList, ovdcName)
		}
	}
	vm, _, err := orgManager.SearchVMAcrossVDCs(vmName, cs.VAppName, "", ovdcNameList)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to find VM for node [%s] in vdc list [%v]: [%v]",
			vmName, ovdcNameList, err)
	}

	err = cs.DiskManager.DetachVolume(vm, volumeID, cs.DiskManager.ZoneMap)
	if err != nil {
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskDetachError, "",
			volumeID, map[string]interface{}{"Detailed Error": err.Error(), "VM Info": vmName}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v",
				util.DiskDetachError, cs.DiskManager.ClusterID, rdeErr)
		}
		if errors.Is(err, govcd.ErrorEntityNotFound) {
			return nil, status.Errorf(codes.NotFound, "Volume [%s] does not exist", volumeID)
		}

		return nil, err
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskDetachError, "", volumeID); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskDetachError, cs.DiskManager.ClusterID)
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

func (cs *controllerServer) ControllerGetCapabilities(ctx context.Context,
	req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {

	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "ControllerGetCapabilities: req should not be nil")
	}
	klog.Infof("ControllerGetCapabilities: called with args [%#v]", *req)

	klog.Infof("Returning controller capabilities [%#v]", cs.Driver.controllerServiceCapabilities)
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

func (cs *controllerServer) GetCapacity(ctx context.Context,
	req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "GetCapacity not implemented")
}

func (cs *controllerServer) ControllerExpandVolume(ctx context.Context,
	req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {

	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "ControllerExpandVolume: req should not be nil")
	}
	klog.Infof("ControllerExpandVolume: called with req [%#v]", *req)

	volumeID := req.VolumeId
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume Id not provided")
	}

	if req.CapacityRange.LimitBytes > 0 && req.CapacityRange.RequiredBytes > req.CapacityRange.LimitBytes {
		return nil, status.Errorf(codes.InvalidArgument,
			"required bytes [%d] should be lesser than limit bytes [%d]",
			req.CapacityRange.RequiredBytes, req.CapacityRange.LimitBytes)
	}

	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "Volume Capability is nil")
	}
	volumeAccessMode := volumeCapability.GetAccessMode()
	if volumeAccessMode == nil {
		return nil, status.Errorf(codes.InvalidArgument, "Volume Access Mode is nil")
	}

	if err := cs.DiskManager.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}

	disk, err := cs.DiskManager.GetDiskByNameOrId(volumeID, cs.DiskManager.ZoneMap, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to get disk by ID [%s]: [%v]", volumeID, err)
	}

	// The required bytes is a minimum and need something >= it. Hence, use ceil.
	newSizeMb := int64(math.Ceil(float64(req.CapacityRange.RequiredBytes / MbToBytes)))
	if disk.SizeMb == newSizeMb {
		klog.Infof("Volume [%s] already at requested size [%d]Mb", volumeID, newSizeMb)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         newSizeMb * MbToBytes,
			NodeExpansionRequired: false,
		}, nil
	}
	if disk.SizeMb > newSizeMb {
		return nil, status.Errorf(codes.InvalidArgument,
			"Disk [%s] new size [%d]Mb cannot be less than previous value [%d]",
			volumeID, newSizeMb, disk.SizeMb)
	}

	attachedVMs, err := cs.DiskManager.GovcdAttachedVM(disk)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Unable to find VM attached to disk [%s]: [%v]", volumeID, err)
	}
	if len(attachedVMs) > 1 {
		// If there are multiple (>1) VMs attached to a disk, the disk cannot be expanded ONLINE. For OFFLINE expansion
		// also there cannot be any disks attached.
		return nil, status.Errorf(codes.FailedPrecondition,
			"There should be at most 1 VM attached to a disk for Volume Expansion (ONLINE OR OFFLINE). Found [%d]",
			len(attachedVMs))
	}

	expansionMode := csi.PluginCapability_VolumeExpansion_UNKNOWN
	if volumeAccessMode.Mode != csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER &&
		volumeCapability.GetBlock() == nil {
		expansionMode = csi.PluginCapability_VolumeExpansion_ONLINE
		if len(attachedVMs) == 0 {
			// Though ONLINE is allowed, if there is no VM attached, we cannot make use of an api call using the VM.
			// Hence, use OFFLINE mode here. Note that, in the race-condition where we choose OFFLINE mode, but the disk
			// gets attached in the interim, the disk expansion API call will fail. Then CSI will retry and then find
			// an attached VM. Then it will use the ONLINE route using the VM API.
			expansionMode = csi.PluginCapability_VolumeExpansion_OFFLINE
		}
	} else {
		expansionMode = csi.PluginCapability_VolumeExpansion_OFFLINE
	}
	klog.Infof("Using [%s] expansion mode since volume Access Mode is [%s], attached VM count is [%d], and block mode is [%v]",
		expansionMode.String(), volumeAccessMode.Mode.String(), len(attachedVMs), volumeCapability.GetBlock() != nil)

	switch expansionMode {
	case csi.PluginCapability_VolumeExpansion_OFFLINE:
		if len(attachedVMs) > 0 {
			return nil, status.Errorf(codes.FailedPrecondition,
				"Disk [%s] cannot be expanded in [%s] mode since VMs [%v] are attached", volumeID,
				expansionMode.String(), attachedVMs)
		}
		newDisk := &types.Disk{ // note that this is an SDK type
			Description: disk.Description,
			SizeMb:      newSizeMb,
			Name:        disk.Name,
			Owner:       disk.Owner,
		}

		klog.Infof("Expanding volume [%s] from [%d]Mb to [%d]Mb...", disk.Name, disk.SizeMb, newSizeMb)
		task, err := cs.DiskManager.UpdateDisk(disk, newDisk)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "unable to update disk [%s]: [%v]", disk.Name, err)
		}
		if err = task.WaitTaskCompletion(); err != nil {
			return nil, status.Errorf(codes.Internal, "unable to wait for task [%v] for expansion of disk [%s]: [%v]",
				task, disk.Name, err)
		}
		klog.Infof("Volume [%s] expanded to [%d]Mb successfully.", disk.Name, newSizeMb)

		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         newSizeMb * MbToBytes,
			NodeExpansionRequired: false,
		}, nil
		break

	case csi.PluginCapability_VolumeExpansion_ONLINE:
		if len(attachedVMs) != 1 {
			return nil, status.Errorf(codes.FailedPrecondition,
				"Disk [%s] cannot be expanded in [%s] mode since zero or more than one VMs [%v] are attached",
				volumeID, expansionMode.String(), attachedVMs)
		}

		attachedVMName := attachedVMs[0].Name
		klog.Infof("Getting node details for attached VM [%s]", attachedVMName)

		orgManager := &vcdsdk.OrgManager{
			Client:  cs.DiskManager.VCDClient,
			OrgName: cs.DiskManager.VCDClient.ClusterOrgName,
		}

		ovdcNameList := make([]string, 0)
		if cs.DiskManager.ZoneMap != nil {
			for ovdcName, _ := range cs.DiskManager.ZoneMap.VdcToZoneMap {
				ovdcNameList = append(ovdcNameList, ovdcName)
			}
		}
		vm, _, err := orgManager.SearchVMAcrossVDCs(attachedVMName, cs.VAppName, "", ovdcNameList)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Unable to find VM for node [%s] in vdc list [%v]: [%v]",
				attachedVMName, ovdcNameList, err)
		}

		// we need the disk from the VM since we need to send all disk settings while updating the disk
		vmSpecSection := vm.VM.VmSpecSection
		newDiskSettings := vmSpecSection.DiskSection.DiskSettings
		diskFound := false
		for idx, _ := range newDiskSettings {
			if newDiskSettings[idx].Disk == nil {
				continue
			}
			klog.Infof("Comparing [%s] and [%s]", newDiskSettings[idx].Disk.ID, disk.Id)
			if newDiskSettings[idx].Disk.ID == disk.Id {
				newDiskSettings[idx].SizeMb = newSizeMb
				diskFound = true
				break
			}
		}
		if !diskFound {
			return nil, status.Errorf(codes.Internal, "Unable to find disk [%s] from node [%s]: [%v]",
				volumeID, attachedVMName, err)
		}
		vmSpecSection.DiskSection.DiskSettings = newDiskSettings
		if _, err = vm.UpdateInternalDisks(vmSpecSection); err != nil {
			return nil, status.Errorf(codes.Internal, "Unable to increase size of disk [%s] from [%d]MB to [%d]MB: [%v]",
				volumeID, disk.SizeMb, newSizeMb, err)
		}
		klog.Infof("Size of disk [%s] increased from [%d]MB to [%d]MB successfully. "+
			"File system will be increased by the node later.",
			volumeID, disk.SizeMb, newSizeMb)

		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         newSizeMb * MbToBytes,
			NodeExpansionRequired: true,
		}, nil

	default:
		return nil, status.Errorf(codes.Internal, "Unknown volume expansion mode [%s]", expansionMode.String())
	}

	return nil, status.Errorf(codes.Internal, "Unexpected location reached")
}

func (cs *controllerServer) ControllerGetVolume(ctx context.Context,
	req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.InvalidArgument, "ControllerGetVolume not implemented")
}
