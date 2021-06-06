/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package vsphereclient

import (
	"context"
	"fmt"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"strings"
)

func (client *Client) GetAllVMs(ctx context.Context) ([]mo.VirtualMachine, error) {

	if client.govcClient == nil {
		return nil, fmt.Errorf("govc client not initialized")
	}

	// Create view of VirtualMachine objects
	m := view.NewManager(client.govcClient)

	v, err := m.CreateContainerView(ctx, client.govcClient.ServiceContent.RootFolder,
		[]string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("unable to create container view for [VirtualMachine]: [%v]", err)
	}
	defer v.Destroy(ctx)

	// Retrieve summary property for all machines
	// Reference: http://pubs.vmware.com/vsphere-60/topic/com.vmware.wssdk.apiref.doc/vim.VirtualMachine.html
	var vms []mo.VirtualMachine
	err = v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"config", "summary"}, &vms)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve vm summaries: [%v]", err)
	}

	return vms, nil
}

func (client *Client) GetDiskUuid(ctx context.Context, vmFullName string, diskName string) (string, error) {
	if vmFullName == "" {
		return "", fmt.Errorf("vm full name should be passed")
	}
	if diskName == "" {
		return "", fmt.Errorf("disk name should be passed")
	}

	vms, err := client.GetAllVMs(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to get all VMs: [%v]", err)
	}
	if vms == nil || len(vms) == 0 {
		return "", fmt.Errorf("unable to find vm [%s]", vmFullName)
	}

	diskUUID := ""
	for _, vm := range vms {
		if vm.Config == nil /* || vm.Config.Name != vmFullName */ {
			continue
		}

		//if strings.HasPrefix(vm.Config.Name, fmt.Sprintf("%s", vmFullName)) {
		//	continue
		//}

		if vm.Config.Hardware.Device == nil {
			continue
		}

		for _, device := range vm.Config.Hardware.Device {
			if device.GetVirtualDevice() != nil {
				backing, ok := device.GetVirtualDevice().Backing.(*types.VirtualDiskFlatVer2BackingInfo)
				if !ok {
					continue
				}

				fileName := backing.FileName
				parts := strings.Split(fileName, "/")
				if len(parts) != 2 {
					continue
				}
				if strings.Contains(parts[0], fmt.Sprintf(" %s ", diskName)) &&
					strings.HasPrefix(parts[1], fmt.Sprintf("%s ", diskName)) {
					diskUUID = backing.Uuid
					break
				}
			}
		}
		if diskUUID != "" {
			break
		}
	}

	if diskUUID == "" {
		return "", fmt.Errorf("unable to get diskUUID for [%s] on vm [%s]", diskName, vmFullName)
	}

	return diskUUID, nil
}
