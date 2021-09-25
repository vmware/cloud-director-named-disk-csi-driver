/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdclient

import (
	"fmt"
	"k8s.io/klog"
	"strings"

	"github.com/vmware/go-vcloud-director/v2/govcd"
)

const (
	// VCDVMIDPrefix is a prefix added to VM objects by VCD. This needs
	// to be removed for query operations.
	VCDVMIDPrefix = "urn:vcloud:vm:"
)

func (client *Client) FindVMByName(vmName string) (*govcd.VM, error) {
	if vmName == "" {
		return nil, fmt.Errorf("vmName mandatory for FindVMByName")
	}

	if err := client.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("unable to refresh vcd token: [%v]", err)
	}

	klog.Infof("Trying to find vm [%s] in vApp [%s] by name", vmName, client.ClusterVAppName)

	vApp, err := client.vdc.GetVAppByName(client.ClusterVAppName, true)
	if err != nil {
		return nil, fmt.Errorf("unable to find vApp [%s] by name: [%v]", client.ClusterVAppName, err)
	}

	vm, err := vApp.GetVMByName(vmName, true)
	if err != nil {
		return nil, fmt.Errorf("unable to find vm [%s] in vApp [%s]: [%v]", vmName, client.ClusterVAppName, err)
	}

	return vm, nil
}

func (client *Client) FindVMByUUID(vcdVmUUID string) (*govcd.VM, error) {
	if vcdVmUUID == "" {
		return nil, fmt.Errorf("vmUUID mandatory for FindVMByUUID")
	}

	if err := client.RefreshBearerToken(); err != nil {
		return nil, fmt.Errorf("unable to refresh vcd token: [%v]", err)
	}

	klog.Infof("Trying to find vm [%s] in vApp [%s] by UUID", vcdVmUUID, client.ClusterVAppName)
	vmUUID := strings.TrimPrefix(vcdVmUUID, VCDVMIDPrefix)

	vApp, err := client.vdc.GetVAppByName(client.ClusterVAppName, true)
	if err != nil {
		return nil, fmt.Errorf("unable to find vApp [%s] by name: [%v]", client.ClusterVAppName, err)
	}


	vm, err := vApp.GetVMById(vmUUID, true)
	if err != nil {
		return nil, fmt.Errorf("unable to find vm UUID [%s] in vApp [%s]: [%v]",
			vmUUID, client.ClusterVAppName, err)
	}

	return vm, nil
}

// IsVmNotAvailable : In VCD, if the VM is not available, it can be an access error or the VM may not be present.
// Hence we sometimes get an error different from govcd.ErrorEntityNotFound
func (client *Client) IsVmNotAvailable(err error) bool {

	if strings.Contains(err.Error(), "Either you need some or all of the following rights [Base]") &&
		strings.Contains(err.Error(), "to perform operations [VAPP_VM_VIEW]") &&
		strings.Contains(err.Error(), "target entity is invalid") {
		return true
	}

	if strings.Contains(err.Error(), "error refreshing VM: cannot refresh VM, Object is empty") {
		return true
	}

	return false
}
