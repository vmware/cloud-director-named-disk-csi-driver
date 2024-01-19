/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package csi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akutz/gofsutil"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	mountutils "k8s.io/mount-utils"
	utilexec "k8s.io/utils/exec"

	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/util"
)

const (
	// The maximum number of volumes that a node can have attached.
	// Since we're using bus 1 only, it allows up-to 16 disks of which one (#7)
	// is pre-allocated for the HBA. Hence, we have only 15 disks.
	maxVolumesPerNode = 15

	DevDiskPath          = "/dev/disk/by-path"
	ScsiHostPath         = "/sys/class/scsi_host"
	HostNameRegexPattern = "^host[0-9]+"

	ScsiBlockPathPrefix = "/sys/class/block"
	ScsiBlockPathSuffix = "device/rescan"
)

type nodeService struct {
	Driver *VCDDriver
	NodeID string
}

// NewNodeService creates and returns a NodeService struct.
func NewNodeService(driver *VCDDriver, nodeID string) csi.NodeServer {
	return &nodeService{
		Driver: driver,
		NodeID: nodeID,
	}
}

// NodeStageVolume mounts the device on a directory on the host. For sharing it with
// pods we need to implement NodePublishVolume
func (ns *nodeService) NodeStageVolume(ctx context.Context,
	req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {

	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: Request is empty")
	}

	klog.Infof("NodeStageVolume: called with args [%#v]", *req)

	// Check for block device and exit early if specified
	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: Volume capability not provided")
	}

	// No staging needed for block device. Must be handled by pod
	if blk := volumeCapability.GetBlock(); blk != nil {
		return &csi.NodeStageVolumeResponse{}, nil
	}

	publishContext := req.GetPublishContext()
	if publishContext == nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: Publish context not provided")
	}

	// parameter fsType is the filesystem type of the storage block to be mounted
	var fsType string
	var ok bool
	volumeContext := req.GetVolumeContext()
	// volumeContext might be optional;
	if volumeContext == nil {
		fsType, ok = publishContext[FileSystemParameter]
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument,
				"NodeStageVolume: PublishContext does not have [%s] set", FileSystemParameter)
		}
	} else {
		ephemeralVolume, ok := volumeContext[EphemeralVolumeContext]
		if ok {
			if ephemeralVolume == "true" {
				return &csi.NodeStageVolumeResponse{}, status.Errorf(codes.Unimplemented,
					"NodeStageVolume: [%s] not supported", EphemeralVolumeContext)
			}
		}
		fsType, ok = volumeContext[FileSystemParameter]
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument,
				"NodeStageVolume: PublishContext does not have [%s] set", FileSystemParameter)
		}
	}

	vmFullName, ok := publishContext[VMFullNameAttribute]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument,
			"NodeStageVolume: PublishContext did not contain full vm name in publish context")
	}

	mountMode := "rw"
	if ns.isVolumeReadOnly(volumeCapability) {
		mountMode = "ro"
	}

	mnt := volumeCapability.GetMount()
	mountFlags := mnt.GetMountFlags()
	if mnt == nil {
		return nil, status.Errorf(codes.InvalidArgument, "volume capability must have mount details")
	}
	if mnt.FsType != fsType {
		// allow fsType passed from the PV or other sources to go through
		klog.Infof("fs type in mount-point [%s] does not match specified fs type [%s]. Using FS [%s] from PV config.",
			mnt.FsType, fsType, fsType)
		mnt.FsType = fsType
	}
	mountFlags = util.CollectMountOptions(mnt.FsType, mountFlags)

	mountFlags = append(mountFlags, mountMode)
	diskUUID, ok := publishContext[DiskUUIDAttribute]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument,
			"NodeStageVolume: PublishContext did not contain disk UUID in publish context")
	}

	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume: Volume Id not provided")
	}

	mountDir := req.GetStagingTargetPath()
	if mountDir == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume: Staging target not provided")
	}

	err := ns.rescanDiskInVM(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeStageVolume: unable to scan SCSI bus for vm [%s]: [%v]",
			vmFullName, err)
	}
	devicePath, err := ns.getDiskPath(ctx, vmFullName, diskUUID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeStageVolume: unable to obtain disk for vm [%s], disk [%s]: [%v]",
			vmFullName, volumeID, err)
	}

	// rescan block devices to get new sizes in case disks were resized
	if err := ns.rescanScsiBlockDiskInVM(ctx, devicePath); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeStageVolume: Unable to rescan disk [%s] in VM: [%v]",
			devicePath, err)
	}
	klog.Infof("Scanned size of disk [%s] successfully", devicePath)

	// Check if already mounted
	isMounted, isMountedAsExpected, err := ns.isVolumeMountedAsExpected(ctx, devicePath, mountDir, mountMode)
	if err != nil {
		return nil, fmt.Errorf("unable to check if device [%s] is mounted on [%s] and mode [%s]: [%v]",
			devicePath, mountDir, mountMode, err)
	}
	if isMounted {
		if !isMountedAsExpected {
			return nil, status.Errorf(codes.Internal,
				"device [%s] not mounted on [%s] and mode [%s] as expected: [%v]",
				devicePath, mountDir, mountMode, err)
		} else {
			// the device is mounted as expected, so nothing to do
			klog.Infof("Device [%s] mounted on [%s] with correct mode [%s]",
				devicePath, mountDir, mountMode)
			return &csi.NodeStageVolumeResponse{}, nil
		}
	}

	// Check if  directory exists
	mountDirExists, err := ns.checkIfDirExists(mountDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not verify that [%s] is a dir: [%v]", mountDir, err)
	}
	if !mountDirExists {
		// Directory doesn't exist
		klog.Infof("Path [%s] does not exist. Make it\n", mountDir)
		if err := os.MkdirAll(mountDir, 0750); err != nil {
			return nil, status.Error(codes.Internal,
				fmt.Sprintf("unable to mkdir at path [%s] - [%s] ",
					mountDir, err.Error()))
		}
	}

	// Mounting as the device is not yet mounted
	klog.Infof("Mounting device [%s] to folder [%s] of type [%s] with flags [%v]",
		devicePath, mountDir, fsType, mountFlags)
	if err = gofsutil.FormatAndMount(ctx, devicePath, mountDir, fsType, mountFlags...); err != nil {
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("unable to format and mount device [%s] at path [%s] with fs [%s] and flags [%v]: [%v]",
				devicePath, mountDir, fsType, mountFlags, err))
	}
	klog.Infof("Mounted device [%s] at path [%s] with fs [%s] and options [%v]",
		devicePath, mountDir, fsType, mountFlags)

	klog.Infof("NodeStageVolume successfully staged at [%s] for device [%s]", mountDir, devicePath)
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume unmounts disk from the host.
func (ns *nodeService) NodeUnstageVolume(ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {

	mountDir := req.GetStagingTargetPath()
	deviceName := req.GetVolumeId()
	if mountDir == "" {
		return nil, status.Error(codes.InvalidArgument,
			"NodeUnstageVolume: Staging Target Path must be provided")
	}

	// Figure out if the target path is present in mounts or not - Unstage is not required for file volumes
	mountDirExists, err := ns.checkIfDirExists(mountDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not verify that [%s] is a dir: [%v]", mountDir, err)
	}
	if !mountDirExists {
		klog.Infof("Path [%s] does not exist. Hence assuming already unmounted.", mountDir)
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	isMountDirMounted, err := ns.checkIfDirMounted(ctx, mountDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to check if [%s] is mounted: [%v]", mountDir, err)
	}
	if !isMountDirMounted {
		klog.Infof("Path [%s] is not mounted. Hence assuming already unmounted.", mountDir)
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	// the directory exists and is mounted, so unmount
	klog.Infof("Attempting to unmount path [%s].", mountDir)
	if err = gofsutil.Unmount(ctx, mountDir); err != nil {
		return nil, status.Errorf(codes.Internal, "unable to unmount [%s]: [%v", mountDir, err)
	}

	klog.Infof("NodeUnstageVolume successful for target [%s] for volume [%s]", mountDir, deviceName)
	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume bind-mounts the host mountDir onto a dir specific to each pod
// requesting the pvc.
func (ns *nodeService) NodePublishVolume(ctx context.Context,
	req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {

	klog.Infof("NodePublishVolume: called with args %#v", *req)

	if volumeContext := req.GetVolumeContext(); volumeContext != nil {
		if ephemeralVolume, ok := volumeContext[EphemeralVolumeContext]; ok {
			if ephemeralVolume == "true" {
				return &csi.NodePublishVolumeResponse{}, status.Errorf(codes.Unimplemented,
					"[%s] not supported", EphemeralVolumeContext)
			}
		}
	}

	diskName := req.GetVolumeId()
	if diskName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: VolumeId not provided")
	}

	podMountDir := req.GetTargetPath()
	if podMountDir == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: TargetPath not provided")
	}

	volumeCapability := req.GetVolumeCapability()
	if volumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: VolumeCapability not provided")
	}

	mountMode := "rw"
	if ns.isVolumeReadOnly(volumeCapability) {
		mountMode = "ro"
	}

	hostMountDir := req.GetStagingTargetPath()
	if hostMountDir == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: StagingTargetPath not provided")
	}

	publishContext := req.GetPublishContext()
	if publishContext == nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume: PublishContext not provided")
	}

	mnt := volumeCapability.GetMount()
	if mnt == nil {
		return nil, status.Errorf(codes.InvalidArgument, "volume capability must have mount details")
	}
	mountFlags := append(mnt.GetMountFlags(), mountMode)

	// verify that host dir exists
	hostMountDirExists, err := ns.checkIfDirExists(hostMountDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to check if host mount dir [%s] exists: [%v]",
			hostMountDir, err)
	}
	if !hostMountDirExists {
		return nil, status.Errorf(codes.Internal, "host mount dir [%s] does not exist", hostMountDir)
	}

	// create target dir if not exists
	if err := ns.mkdir(podMountDir); err != nil {
		return nil, status.Errorf(codes.Internal, "unable to create dir [%s]: [%v]", podMountDir, err)
	}
	klog.Infof("Ensured that dir [%s] exists.", podMountDir)

	// Check if already mounted
	isMounted, isMountedAsExpected, err := ns.isVolumeMountedAsExpected(ctx, hostMountDir, podMountDir, mountMode)
	if err != nil {
		return nil, fmt.Errorf("unable to check if dir [%s] is mounted on [%s] and mode [%s]: [%v]",
			hostMountDir, podMountDir, mountMode, err)
	}
	if isMounted {
		if !isMountedAsExpected {
			return nil, status.Errorf(codes.Internal,
				"dir [%s] not mounted on [%s] and mode [%s] as expected: [%v]",
				hostMountDir, podMountDir, mountMode, err)
		} else {
			// the device is mounted as expected, so nothing to do
			klog.Infof("dir [%s] mounted on [%s] with correct mode [%s]",
				hostMountDir, podMountDir, mountMode)
			return &csi.NodePublishVolumeResponse{}, nil
		}
	}

	// Mounting as the dir is not yet mounted
	klog.Infof("Mounting dir [%s] to folder [%s] with flags [%v]",
		hostMountDir, podMountDir, mountFlags)
	if err = gofsutil.BindMount(ctx, hostMountDir, podMountDir, mountFlags...); err != nil {
		return nil, status.Error(codes.Internal,
			fmt.Sprintf("unable to format and mount dir [%s] at path [%s] with flags [%v]: [%v]",
				hostMountDir, podMountDir, mountFlags, err))
	}
	klog.Infof("Mounted dir [%s] at path [%s] with options [%v]", hostMountDir, podMountDir, mountFlags)

	klog.Infof("NodeStageVolume successfully staged at [%s] for host dir [%s]", podMountDir, hostMountDir)
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume detaches the bind mount on the pod
func (ns *nodeService) NodeUnpublishVolume(ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {

	diskName := req.GetVolumeId()
	if diskName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeUnpublishVolume: volumeID must be provided")
	}

	podMountDir := req.GetTargetPath()
	if podMountDir == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeUnpublishVolume: Target Path must be provided")
	}

	podMountDirExists, err := ns.checkIfDirExists(podMountDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to check if pod mount dir [%s] exists: [%v]",
			podMountDir, err)
	}
	if !podMountDirExists {
		klog.Infof("Pod mount dir [%s] does not exist. Assuming already unmounted.", podMountDir)
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	isDirMounted, err := ns.checkIfDirMounted(ctx, podMountDir)
	if err != nil {
		return nil, fmt.Errorf("unable to check if pod mount dir [%s] is mounted: [%v]", podMountDir, err)
	}

	klog.Infof("Attempting to unmount pod mount dir [%s].", podMountDir)
	if isDirMounted {
		if err = gofsutil.Unmount(ctx, podMountDir); err != nil {
			return nil, fmt.Errorf("unable to unmount pod mount dir [%s]: [%v]", podMountDir, err)
		}
	}

	klog.Infof("Attempting to remove pod mount dir [%s].", podMountDir)
	if err := ns.rmdir(podMountDir); err != nil {
		return nil, fmt.Errorf("failed to remove pod mount dir %v", podMountDir)
	}

	klog.Infof("NodeUnpublishVolume successful for disk [%s] at mount dir [%s]", diskName, podMountDir)
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeService) NodeGetCapabilities(_ context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	klog.Infof("NodeGetCapabilities called with req: %#v", req)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.Driver.nodeServiceCapabilities,
	}, nil
}

func (ns *nodeService) NodeGetInfo(_ context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId:             ns.NodeID,
		AccessibleTopology: nil,
		MaxVolumesPerNode:  maxVolumesPerNode,
	}, nil

}

func (ns *nodeService) NodeGetVolumeStats(_ context.Context,
	req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {

	klog.Infof("NodeGetVolumeStats called with req: %#v", req)

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		klog.Errorf("unable to get volume path from request")
		return nil, fmt.Errorf("unable to get volume path from request")
	}

	var statFS unix.Statfs_t
	if err := unix.Statfs(volumePath, &statFS); err != nil {
		klog.Errorf("unable to get stats of volume [%s]: [%v]", volumePath, err)
		return nil, fmt.Errorf("unable to get stats of volume [%s]: [%v]", volumePath, err)
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: int64(statFS.Bavail) * int64(statFS.Bsize),
				Total:     int64(statFS.Blocks) * int64(statFS.Bsize),
				Used:      (int64(statFS.Blocks) - int64(statFS.Bavail)) * int64(statFS.Bsize),
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: int64(statFS.Ffree),
				Total:     int64(statFS.Files),
				Used:      int64(statFS.Files) - int64(statFS.Ffree),
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}

func (ns *nodeService) NodeExpandVolume(ctx context.Context,
	req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	klog.Infof("NodeGetVolumeStats called with req: [%#v]", req)

	if req.GetVolumeCapability() != nil {
		if req.GetVolumeCapability() != nil && req.GetVolumeCapability().GetBlock() != nil {
			klog.Infof("ONLINE Expansion not supported for Block mount volumes")
			return &csi.NodeExpandVolumeResponse{}, nil
		}
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		klog.Errorf("unable to get volume path from request")
		return nil, fmt.Errorf("unable to get volume path from request")
	}

	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeExpandVolume: Staging Path context not provided")
	}
	klog.Infof("Staging Target Path is [%s]", stagingTargetPath)

	diskUUID := req.GetVolumeId()
	if diskUUID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeExpandVolume: Disk UUID not provided")
	}

	mounter := mountutils.New("")
	devicePath, _, err := mountutils.GetDeviceNameFromMount(mounter, stagingTargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume: Unable to get device path for [%s]: [%v]",
			stagingTargetPath, err)
	}
	if devicePath == "" {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume: Empty device path obtained for [%s]: [%v]",
			stagingTargetPath, err)
	}
	klog.Infof("Obtained device path [%s] from mount path [%s] for disk [%s]", devicePath, stagingTargetPath, volumePath)

	// rescan block devices to get new sizes
	if err := ns.rescanScsiBlockDiskInVM(ctx, devicePath); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume: Unable to rescan disk [%s] in VM: [%v]",
			devicePath, err)
	}
	klog.Infof("Scanned size of disk [%s] successfully", devicePath)

	klog.Infof("Resizing volume [%s] host-mounted at [%s] from node", volumePath, stagingTargetPath)
	r := mountutils.NewResizeFs(utilexec.New())
	if _, err := r.Resize(devicePath, stagingTargetPath); err != nil {
		klog.Errorf("Failed to resize volume [%s] mounted at [%s] from node: [%v]", volumePath, stagingTargetPath, err)
		return nil, status.Errorf(codes.Internal,
			"NodeExpandVolume: Failed to resize volume [%s] host-mounted at [%s] from node: [%v]",
			volumePath, stagingTargetPath, err)
	}
	klog.Infof("Successfully resized volume [%s] host-mounted at [%s] from node", volumePath, stagingTargetPath)

	return &csi.NodeExpandVolumeResponse{}, nil
}

// rescanScsiBlockDiskInVM runs `echo "1" > /sys/class/block/sdX/device/rescan`
func (ns *nodeService) rescanScsiBlockDiskInVM(_ context.Context, blockDevice string) error {
	// block device name is of format /dev/sdX. We need only sdX
	parts := strings.Split(blockDevice, "/")
	if len(parts) != 3 {
		return fmt.Errorf("block device path in invalid format: [%s]", blockDevice)
	}

	filePath := fmt.Sprintf("%s/%s/%s", ScsiBlockPathPrefix, parts[2], ScsiBlockPathSuffix)
	if err := os.WriteFile(filePath, []byte("1"), 0200); err != nil {
		return fmt.Errorf("unable to write '1' into file [%s]: [%v]", filePath, err)
	}

	return nil
}

// rescanDiskInVM re-scans the SCSI bus entirely. CSI runs `echo "- - -" > /sys/class/scsi_host/*/scan` inside VM,
// where "- - -" represent controller channel lun.
func (ns *nodeService) rescanDiskInVM(_ context.Context) error {
	// The filepath.Walk walks the file tree, calling the specified function for each file or directory in the tree, including root.
	//  unnamed function is defined to check and perform rescan.
	err := filepath.Walk(ScsiHostPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			klog.Errorf("Encounter error while walking through the folder: [%v]", err)
			return nil
		}
		reg, regErr := regexp.Compile(HostNameRegexPattern)
		if regErr != nil {
			klog.Errorf("Encounter error while generating regex error: [%v]", regErr)
			return fmt.Errorf("encounter error while generating regex error: [%v]", regErr)
		}
		if fi.IsDir() {
			return nil
		}
		if reg.MatchString(fi.Name()) {
			// file mode for scan: --w-------
			executedErr := os.WriteFile(fmt.Sprintf("%s/scan", path), []byte("- - -"), 0200)
			if executedErr != nil {
				klog.Errorf("Encounter error while rescanning the disk in VM [%s];executing command failed, [%v]", ns.NodeID, executedErr)
				return fmt.Errorf("encounter error while rescanning the disk in VM [%s];executing command failed, [%v]", ns.NodeID, executedErr)
			}
			klog.Infof("CSI node plugin rescanned the scsi host [%s] successfully", fi.Name())
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("CSI node plugin could not rescan SCSI bus for [%s]: [%v]", ScsiHostPath, err)
	}
	return nil
}

// getDiskPath looks for a device corresponding to vmName:diskName as stored in vSphere. It
// enumerates devices in /dev/disk/by-path and returns a device with UUID matching the scsi UUID.
// It needs disk.enableUUID to be set for the VM.
func (ns *nodeService) getDiskPath(_ context.Context, vmFullName string, diskUUID string) (string, error) {

	if diskUUID == "" {
		return "", fmt.Errorf("diskUUID should not be an empty string")
	}

	hexDiskUUID := strings.ReplaceAll(diskUUID, "-", "")

	guestDiskPath := ""
	err := filepath.Walk(DevDiskPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if guestDiskPath != "" {
			return nil
		}
		if fi.IsDir() {
			return nil
		}

		fileToProcess := path
		if fi.Mode()&os.ModeSymlink != 0 {
			dst, err := filepath.EvalSymlinks(path)
			if err != nil {
				klog.Infof("Error accessing file [%s]: [%v]", path, err)
				return nil
			}
			fileToProcess = dst
		}
		if fileToProcess == "" {
			return nil
		}

		klog.Infof("Checking file: [%s] => [%s]\n", path, fileToProcess)
		outBytes, err := exec.Command(
			"/lib/udev/scsi_id",
			"--page=0x83",
			"--whitelisted",
			fmt.Sprintf("--device=%v", fileToProcess)).CombinedOutput()
		if err != nil {
			klog.Infof("Encountered error while processing file [%s]: [%v]", fileToProcess, err)
			klog.Infof("Please check if the `disk.enableUUID` parameter is set to 1 for the VM in VC config.")
			return nil
		}
		out := strings.TrimSpace(string(outBytes))
		if len(out) == 33 {
			out = out[1:]
		} else if len(out) != 32 {
			klog.Infof("Obtained uuid with incorrect length: [%s]", out)
			return nil
		}

		if strings.ToLower(out) == strings.ToLower(hexDiskUUID) {
			guestDiskPath = fileToProcess
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("could not create filepath.Walk for [%s]: [%v]", DevDiskPath, err)
	}

	klog.Infof("Obtained matching disk [%s]", guestDiskPath)
	return guestDiskPath, nil
}

func (ns *nodeService) isVolumeReadOnly(capability *csi.VolumeCapability) bool {
	accessMode := capability.GetAccessMode().GetMode()

	return accessMode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY ||
		accessMode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
}

// returns isMounted, isMountedAsExpected, error in checking
func (ns *nodeService) isVolumeMountedAsExpected(ctx context.Context, devicePath string, mountDir string,
	mountMode string) (bool, bool, error) {

	mountedDevs, err := gofsutil.GetDevMounts(ctx, devicePath)
	if err != nil {
		return false, false, fmt.Errorf("unable to check if [%s] is mounted: [%v]", devicePath, err)
	}
	if len(mountedDevs) == 0 {
		return false, false, nil
	}

	klog.Infof("Device [%s] is already mounted. Checking if it is at [%s].", devicePath, mountDir)
	for _, mountedDev := range mountedDevs {
		if mountedDev.Path == mountDir {
			klog.Infof("Device [%s] mounted at the right path [%s]. Checking for properties...",
				devicePath, mountDir)

			for _, option := range mountedDev.Opts {
				if option == mountMode {
					klog.Infof("Device [%s] mounted on [%s] with correct mode [%s]",
						devicePath, mountDir, mountMode)
					return true, true, nil
				}
			}

			klog.Infof("Device [%s] already mounted on [%s] but with options [%s]",
				devicePath, mountDir, mountedDev.Opts)
			return true, false, nil
		}
	}

	klog.Infof("Device [%s] already mounted but on a different location", devicePath)
	return true, false, nil
}

func (ns *nodeService) checkIfDirExists(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("unable to get stat of [%s]: [%v]", path, err)
	}

	if !fi.IsDir() {
		return false, fmt.Errorf("path [%s] is not a dir", path)
	}

	return true, nil
}

func (ns *nodeService) checkIfDirMounted(ctx context.Context, mountDir string) (bool, error) {
	mountDevices, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return false, fmt.Errorf("unable to get mounts of node")
	}

	for _, mountDevice := range mountDevices {
		if mountDevice.Path == mountDir {
			return true, nil
		}
	}

	return false, nil
}

func (ns *nodeService) mkdir(path string) error {
	fi, err := os.Stat(path)
	if err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("Path [%s] exists but is not a directory", path)
		}

		klog.Infof("Path [%s] already exists and is a directory.", path)
		return nil
	}

	// err != nil here
	if !os.IsNotExist(err) {
		return fmt.Errorf("unable to check stats of path [%s]: [%v]", path, err)
	}

	// os.IsNotExist(err) == true here
	mode := os.FileMode(0755)
	if err = os.Mkdir(path, mode); err != nil {
		return fmt.Errorf("unable to create dir [%s] with mode [%#v]: [%v]",
			path, mode, err)
	}

	return nil
}

func (ns *nodeService) rmdir(path string) error {
	klog.Infof("Deleting path %s", path)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		klog.Infof("%s does not exist", path)
		return nil
	}
	return err
}
