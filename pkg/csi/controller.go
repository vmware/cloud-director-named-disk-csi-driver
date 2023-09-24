/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package csi

import (
	"context"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/util"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdcsiclient"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	"math"
	"strings"
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
	VDCNameParameter        = "ovdcName"
	VDCIDParameter          = "ovdcID"
	FileSystemParameter     = "filesystem"
	EphemeralVolumeContext  = "csi.storage.k8s.io/ephemeral"

	DiskIDAttribute     = "diskID"
	VMFullNameAttribute = "vmID"
	OVDCNameAttribute   = "ovdcName"
	OVDCIDAttribute     = "ovdcID"
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
func NewControllerService(driver *VCDDriver, vcdClient *vcdsdk.Client, clusterID string, vAppName string) csi.ControllerServer {
	return &controllerServer{
		Driver: driver,
		DiskManager: &vcdcsiclient.DiskManager{
			VCDClient: vcdClient,
			ClusterID: clusterID,
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

// AMK multiAZ TODO: single-instance this into common-core
func getOrgByName(client *vcdsdk.Client, orgName string) (*govcd.Org, error) {
	org, err := client.VCDClient.GetOrgByName(orgName)
	if err != nil {
		return nil, fmt.Errorf("failed to get org by name [%s]: [%v]", orgName, err)
	}
	if org == nil || org.Org == nil {
		return nil, fmt.Errorf("found nil org when getting org by name [%s]", orgName)
	}
	return org, nil
}

func getOvdcByID(client *vcdsdk.Client, orgName string, ovdcID string) (*govcd.Vdc, error) {
	org, err := getOrgByName(client, orgName)
	if err != nil {
		return nil, fmt.Errorf("error occurred when getting ovdc by ID [%s]: [%v]", ovdcID, err)
	}
	ovdc, err := org.GetVDCById(ovdcID, true)
	if err != nil {
		if err == govcd.ErrorEntityNotFound {
			return nil, err
		}
		return nil, fmt.Errorf("fail to get ovdc by ID [%s]: [%v]", ovdcID, err)
	}
	if ovdc == nil || ovdc.Vdc == nil {
		return nil, fmt.Errorf("found nil ovdc when getting org by ID [%s]", ovdcID)
	}
	return ovdc, nil
}

func getOvdcByName(client *vcdsdk.Client, orgName string, ovdcName string) (*govcd.Vdc, error) {
	org, err := getOrgByName(client, orgName)
	if err != nil {
		return nil, fmt.Errorf("error occurred when getting ovdc by Name [%s]: [%v]", ovdcName, err)
	}
	ovdc, err := org.GetVDCByName(ovdcName, true)
	if err != nil {
		if err == govcd.ErrorEntityNotFound {
			return nil, err
		}
		return nil, fmt.Errorf("fail to get ovdc by Name [%s]: [%v]", ovdcName, err)
	}
	if ovdc == nil || ovdc.Vdc == nil {
		return nil, fmt.Errorf("found nil ovdc when getting org by Name [%s]", ovdcName)
	}
	return ovdc, nil
}

func (cs *controllerServer) UpdateOVDC(_ context.Context, ovdcName string, ovdcID string) error {
	var ovdc *govcd.Vdc = nil
	var err error
	switch {
	case ovdcID != "":
		klog.Infof("Will use OVDC specified using id [%s]", ovdcID)
		if ovdc, err = getOvdcByID(cs.DiskManager.VCDClient, cs.DiskManager.VCDClient.ClusterOrgName, ovdcID); err != nil {
			return fmt.Errorf("unable to get OVDC from ID [%s] in org [%s]: [%v]",
				ovdcID, cs.DiskManager.VCDClient.ClusterOrgName, err)
		}
		cs.DiskManager.VCDClient.VDC = ovdc
		return nil

	case ovdcName != "":
		klog.Infof("Will use OVDC specified using name [%s]", ovdcName)
		if ovdc, err = getOvdcByName(cs.DiskManager.VCDClient, cs.DiskManager.VCDClient.ClusterOrgName, ovdcName); err != nil {
			return fmt.Errorf("unable to get OVDC from Name [%s] in org [%s]: [%v]",
				ovdcName, cs.DiskManager.VCDClient.ClusterOrgName, err)
		}
		cs.DiskManager.VCDClient.VDC = ovdc
		return nil

	case cs.DiskManager.VCDClient.VDC != nil:
		klog.Infof("Will use OVDC specified in non-AZ mode [%s:%s]", cs.DiskManager.VCDClient.VDC.Vdc.Name,
			cs.DiskManager.VCDClient.VDC.Vdc.ID)
		return nil

	default:
		return fmt.Errorf("VDC should be specified either in single-OVDC or AZ modes")
	}
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

	busType := vcdcsiclient.VCDBusTypeSCSI
	busSubType := vcdcsiclient.VCDBusSubTypeVirtualSCSI

	storageProfile, _ := req.Parameters[StorageProfileParameter]

	ovdcName, _ := req.Parameters[VDCNameParameter]
	ovdcID, _ := req.Parameters[VDCIDParameter]
	if err := cs.UpdateOVDC(ctx, ovdcName, ovdcID); err != nil {
		return nil, fmt.Errorf("error in updating OVDC from parameters [%#v]: [%v]", req.Parameters, err)
	}

	disk, err := cs.DiskManager.CreateDisk(diskName, sizeMB, busType,
		busSubType, cs.DiskManager.ClusterID, storageProfile, shareable)
	if err != nil {
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskCreateError, "", diskName,
			map[string]interface{}{"Detailed Error": err.Error()}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v",
				util.DiskCreateError, cs.DiskManager.ClusterID, rdeErr)
		}
		return nil, fmt.Errorf("unable to create disk [%s] with sise [%d]MB: [%v]",
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
	attributes[VDCNameParameter] = cs.DiskManager.VCDClient.VDC.Vdc.Name
	attributes[VDCIDParameter] = cs.DiskManager.VCDClient.VDC.Vdc.ID
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

	if err := cs.DiskManager.VCDClient.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("error while obtaining access token: [%v]", err)
	}
	//volumeID is a diskName
	err := cs.DiskManager.DeleteDisk(volumeID)
	if err != nil {
		if err == govcd.ErrorEntityNotFound {
			klog.Infof("Volume [%s] is already deleted.", volumeID)
			return &csi.DeleteVolumeResponse{}, nil
		}
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskDeleteError, "", volumeID, map[string]interface{}{"Detailed Error": err.Error()}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.DiskDeleteError, cs.DiskManager.ClusterID, rdeErr)
		}
		return nil, status.Errorf(codes.Internal, "DeleteVolume failed: [%v]", err)
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskDeleteError, "", volumeID); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskDeleteError, cs.DiskManager.ClusterID)
	}
	klog.Infof("Volume %s deleted successfully", req.VolumeId)
	return &csi.DeleteVolumeResponse{}, nil
}

func CreateVAppNamePrefix(clusterName string, ovdcID string) (string, error) {
	parts := strings.Split(ovdcID, ":")
	if len(parts) != 4 {
		// urn:vcloud:org:<uuid>
		return "", fmt.Errorf("invalid URN format for OVDC: [%s]", ovdcID)
	}

	return fmt.Sprintf("%s_%s", clusterName, parts[3]), nil
}

func (cs *controllerServer) SearchVMAcrossVDCs(vmName string, vmId string) (*govcd.VM, string, error) {

	orgName := cs.DiskManager.VCDClient.ClusterOrgName
	org, err := cs.DiskManager.VCDClient.VCDClient.GetOrgByName(orgName)
	if err != nil {
		return nil, "", fmt.Errorf("unable to get org by name [%s]: [%v]", orgName, err)
	}

	ovdcList, err := org.QueryOrgVdcList()
	if err != nil {
		return nil, "", fmt.Errorf("unable to get list of OVDCs from org [%s]: [%v]", orgName, err)
	}

	clusterName := cs.VAppName
	for _, ovdcRecordType := range ovdcList {
		klog.Infof("Looking for VM [name:%s],[ID:%s] of cluster [%s] in OVDC [%s]",
			vmName, vmId, clusterName, ovdcRecordType.Name)
		vdc, err := org.GetVDCByName(ovdcRecordType.Name, false)
		if err != nil {
			klog.Infof("unable to query VDC [%s] in Org [%s] by name: [%v]",
				ovdcRecordType.Name, orgName, err)
			continue
		}
		vAppNamePrefix, err := CreateVAppNamePrefix(clusterName, vdc.Vdc.ID)
		if err != nil {
			klog.Infof("Unable to create a vApp name prefix for cluster [%s] in OVDC [%s] with OVDC ID [%s]: [%v]",
				clusterName, vdc.Vdc.Name, vdc.Vdc.ID, err)
			continue
		}
		klog.Infof("Looking for vApps with a prefix of [%s]", vAppNamePrefix)
		vAppList := vdc.GetVappList()
		// check if the VM exists in any cluster-vApps in this OVDC
		for _, vApp := range vAppList {
			if strings.HasPrefix(vApp.Name, vAppNamePrefix) {
				// check if VM exists
				klog.Infof("Looking for VM [name:%s],[id:%s] in vApp [%s] in OVDC [%s] in cluster [%s]",
					vmName, vmId, vApp.Name, vdc.Vdc.Name, clusterName)
				vdcManager, err := vcdsdk.NewVDCManager(cs.DiskManager.VCDClient, orgName, vdc.Vdc.Name)
				if err != nil {
					return nil, "", fmt.Errorf("error creating VDCManager object for VDC [%s]: [%v]",
						vdc.Vdc.Name, err)
				}
				var vm *govcd.VM = nil
				if vmName != "" {
					vm, err = vdcManager.FindVMByName(vApp.Name, vmName)
				} else if vmId != "" {
					vm, err = vdcManager.FindVMByUUID(vApp.Name, vmId)
				} else {
					return nil, "", fmt.Errorf("either vm name [%s] or ID [%s] should be passed", vmName, vmId)
				}
				if err != nil {
					klog.Infof("Could not find VM [%s] in vApp [%s] of Cluster [%s] in OVDC [%s]: [%v]",
						vmName, vApp.Name, clusterName, vdc.Vdc.Name, err)
					continue
				}

				// If we reach here, we found the VM
				klog.Infof("Found VM [%s] in vApp [%s] of Cluster [%s] in OVDC [%s]: [%v]",
					vmName, vApp.Name, clusterName, vdc.Vdc.Name, err)
				return vm, vdc.Vdc.Name, nil
			}
		}
		klog.Infof("Could not find VM [%s] of cluster [%s] in OVDC [%s]",
			vmName, clusterName, ovdcRecordType.Name)
	}

	return nil, "", govcd.ErrorEntityNotFound
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

	klog.Infof("Getting VM details for [%s]", nodeID)
	vm, ovdcName, err := cs.SearchVMAcrossVDCs(nodeID, "") // nodeID is actually node Name
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "unable to find VM for node [%s]: [%v]", nodeID, err)
	}

	if err := cs.UpdateOVDC(ctx, ovdcName, ""); err != nil {
		return nil, fmt.Errorf("error in updating OVDC from ovdcName [%s]: [%v]", ovdcName, err)
	}

	klog.Infof("Getting disk details for [%s]", diskName)
	disk, err := cs.DiskManager.GetDiskByName(diskName)
	if err != nil {
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskQueryError, "", diskName,
			map[string]interface{}{"Detailed Error": fmt.Errorf("unable query disk [%s]: [%v]",
				diskName, err)}); rdeErr != nil {
			klog.Errorf("unable to unable to add error [%s] into [CSI.Errors] in RDE [%s], %v",
				util.DiskQueryError, cs.DiskManager.ClusterID, rdeErr)
		}
		return nil, fmt.Errorf("unable to find disk [%s]: [%v]", diskName, err)
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskQueryError, "", diskName); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskQueryError, cs.DiskManager.ClusterID)
	}
	klog.Infof("Obtained disk: [%#v]\n", disk)

	klog.Infof("Attaching volume [%s] to node [%s]", diskName, nodeID)
	err = cs.DiskManager.AttachVolume(vm, disk)
	if err != nil {
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskAttachError, "", diskName,
			map[string]interface{}{"Detailed Error": err.Error(), "VM Info": nodeID}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.DiskAttachError,
				cs.DiskManager.ClusterID, rdeErr)
		}
		if err == govcd.ErrorEntityNotFound {
			return nil, status.Errorf(codes.NotFound, "could not provision disk [%s] in vcd", diskName)
		}
		return nil, err
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskAttachError, "",
		diskName); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskAttachError, cs.DiskManager.ClusterID)
	}
	klog.Infof("Successfully attached volume %s to node %s ", diskName, nodeID)

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			VMFullNameAttribute: vm.VM.Name,
			DiskIDAttribute:     diskName,
			DiskUUIDAttribute:   disk.UUID,
			FileSystemAttribute: mountDetails.FsType,
			OVDCNameAttribute:   ovdcName,
			OVDCIDAttribute:     cs.DiskManager.VCDClient.VDC.Vdc.ID,
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

	klog.Infof("Getting VM details for [%s]", nodeID)
	vm, ovdcName, err := cs.SearchVMAcrossVDCs(nodeID, "") // nodeID is actually node Name
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "unable to find VM for node [%s]: [%v]", nodeID, err)
	}

	if err := cs.UpdateOVDC(ctx, ovdcName, ""); err != nil {
		return nil, fmt.Errorf("error in updating OVDC from ovdcName [%s]: [%v]", ovdcName, err)
	}

	err = cs.DiskManager.DetachVolume(vm, volumeID)
	if err != nil {
		if rdeErr := cs.DiskManager.AddToErrorSet(util.DiskDetachError, "", volumeID,
			map[string]interface{}{"Detailed Error": err.Error(), "VM Info": nodeID}); rdeErr != nil {
			klog.Errorf("unable to add error [%s] into [CSI.Errors] in RDE [%s], %v", util.DiskDetachError,
				cs.DiskManager.ClusterID, rdeErr)
		}
		if err == govcd.ErrorEntityNotFound {
			return nil, status.Errorf(codes.NotFound, "Volume [%s] does not exist", volumeID)
		}

		return nil, err
	}
	if removeErrorRdeErr := cs.DiskManager.RemoveFromErrorSet(util.DiskDetachError, "",
		volumeID); removeErrorRdeErr != nil {
		klog.Errorf("unable to remove error [%s] from [CSI.Errors] in RDE [%s]", util.DiskDetachError,
			cs.DiskManager.ClusterID)
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
