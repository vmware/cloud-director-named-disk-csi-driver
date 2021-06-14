/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package vcdclient

import (
	"fmt"
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

	if err := client.RefreshToken(); err != nil {
		return nil, fmt.Errorf("unable to refresh vcd token: [%v]", err)
	}

	// Query is be delimited to org where user exists. The expectation is that
	// there will be exactly one VM with that name.
	results, err := client.vdc.QueryWithNotEncodedParams(
		map[string]string{
			"type":   "vm",
			"format": "records",
		},
		map[string]string{
			"filter": fmt.Sprintf("(name==%s)", vmName),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to query for VM [%s]: [%v]", vmName, err)
	}

	if results.Results.Total > 1 {
		return nil, fmt.Errorf("obtained [%d] VMs for name [%s], expected only one",
			int(results.Results.Total), vmName)
	}

	if results.Results.Total == 0 {
		return nil, govcd.ErrorEntityNotFound
	}

	href := results.Results.VMRecord[0].HREF
	vm, err := client.vcdClient.Client.GetVMByHref(href)
	if err != nil {
		return nil, fmt.Errorf("unable to find vm by HREF [%s]: [%v]", href, err)
	}

	return vm, nil
}

func (client *Client) FindVMByUUID(vcdVmUUID string) (*govcd.VM, error) {
	if vcdVmUUID == "" {
		return nil, fmt.Errorf("vmUUID mandatory for FindVMByUUID")
	}

	if err := client.RefreshToken(); err != nil {
		return nil, fmt.Errorf("unable to refresh vcd token: [%v]", err)
	}

	vmUUID := strings.TrimPrefix(vcdVmUUID, VCDVMIDPrefix)
	href := fmt.Sprintf("%v/vApp/vm-%s", client.vcdClient.Client.VCDHREF.String(),
		strings.ToLower(vmUUID))
	vm, err := client.vcdClient.Client.GetVMByHref(href)
	if err != nil {
		return nil, fmt.Errorf("unable to find vm by HREF [%s]: [%v]", href, err)
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
