/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdclient

import (
	"context"
	"fmt"
	"github.com/antihax/optional"
	"github.com/apparentlymart/go-cidr/cidr"
	"github.com/peterhellberg/link"
	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"github.com/vmware/go-vcloud-director/v2/types/v56"
	"k8s.io/klog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (client *Client) getOVDCNetwork(ctx context.Context, networkName string) (*swaggerClient.VdcNetwork, error) {
	if networkName == "" {
		return nil, fmt.Errorf("network name should not be empty")
	}

	ovdcNetworksAPI := client.APIClient.OrgVdcNetworksApi
	pageNum := int32(1)
	ovdcNetworkID := ""
	for {
		ovdcNetworks, resp, err := ovdcNetworksAPI.GetAllVdcNetworks(ctx, pageNum, 32, nil)
		if err != nil {
			// TODO: log resp in debug mode only
			return nil, fmt.Errorf("unable to get all ovdc networks: [%+v]: [%v]", resp, err)
		}

		if len(ovdcNetworks.Values) == 0 {
			break
		}

		for _, ovdcNetwork := range ovdcNetworks.Values {
			if ovdcNetwork.Name == client.networkName {
				ovdcNetworkID = ovdcNetwork.Id
				break
			}
		}

		if ovdcNetworkID != "" {
			break
		}
		pageNum++
	}
	if ovdcNetworkID == "" {
		return nil, fmt.Errorf("unable to obtain ID for ovdc network name [%s]",
			client.networkName)
	}

	ovdcNetworkAPI := client.APIClient.OrgVdcNetworkApi
	ovdcNetwork, resp, err := ovdcNetworkAPI.GetOrgVdcNetwork(ctx, ovdcNetworkID)
	if err != nil {
		return nil, fmt.Errorf("unable to get network for id [%s]: [%+v]: [%v]", ovdcNetworkID, resp, err)
	}

	return &ovdcNetwork, nil
}

// CacheGatewayDetails : get gateway reference and cache some details in client object
func (client *Client) CacheGatewayDetails(ctx context.Context) error {

	if client.networkName == "" {
		return fmt.Errorf("network name should not be empty")
	}

	ovdcNetwork, err := client.getOVDCNetwork(ctx, client.networkName)
	if err != nil {
		return fmt.Errorf("unable to get OVDC network [%s]: [%v]", client.networkName, err)
	}

	// Cache backing type
	if ovdcNetwork.BackingNetworkType != nil {
		client.networkBackingType = *ovdcNetwork.BackingNetworkType
	}

	// Cache gateway reference
	if ovdcNetwork.Connection == nil ||
		ovdcNetwork.Connection.RouterRef == nil {
		klog.Infof("Gateway for Network Name [%s] is of type [%v]\n",
			client.networkName, client.networkBackingType)
		return nil
	}

	client.gatewayRef = &swaggerClient.EntityReference{
		Name: ovdcNetwork.Connection.RouterRef.Name,
		Id:   ovdcNetwork.Connection.RouterRef.Id,
	}

	klog.Infof("Obtained Gateway [%s] for Network Name [%s] of type [%v]\n",
		client.gatewayRef.Name, client.networkName, client.networkBackingType)

	return nil
}

func getUnusedIPAddressInRange(startIPAddress string, endIPAddress string,
	usedIPAddresses map[string]bool) string {

	// This is not the best approach and can be optimized further by skipping ranges.
	freeIP := ""
	startIP, _, _ := net.ParseCIDR(fmt.Sprintf("%s/32", startIPAddress))
	endIP, _, _ := net.ParseCIDR(fmt.Sprintf("%s/32", endIPAddress))
	for !startIP.Equal(endIP) && usedIPAddresses[startIP.String()] {
		startIP = cidr.Inc(startIP)
	}
	// either the last IP is free or an intermediate IP is not yet used
	if !startIP.Equal(endIP) || startIP.Equal(endIP) && !usedIPAddresses[startIP.String()] {
		freeIP = startIP.String()
	}

	if freeIP != "" {
		klog.Infof("Obtained unused IP [%s] in range [%s-%s]\n", freeIP, startIPAddress, endIPAddress)
	}

	return freeIP
}

func (client *Client) getUnusedInternalIPAddress(ctx context.Context) (string, error) {

	if client.gatewayRef == nil {
		return "", fmt.Errorf("gateway reference should not be nil")
	}

	usedIPAddress := make(map[string]bool)
	pageNum := int32(1)
	for {
		lbVSSummaries, resp, err := client.APIClient.EdgeGatewayLoadBalancerVirtualServicesApi.GetVirtualServiceSummariesForGateway(
			ctx, pageNum, 25, client.gatewayRef.Id, nil)
		if err != nil {
			return "", fmt.Errorf("unable to get virtual service summaries for gateway [%s]: resp: [%v]: [%v]",
				client.gatewayRef.Name, resp, err)
		}
		if len(lbVSSummaries.Values) == 0 {
			break
		}
		for _, lbVSSummary := range lbVSSummaries.Values {
			usedIPAddress[lbVSSummary.VirtualIpAddress] = true
		}

		pageNum++
	}

	freeIP := getUnusedIPAddressInRange(client.OneArm.StartIPAddress,
		client.OneArm.EndIPAddress, usedIPAddress)
	if freeIP == "" {
		return "", fmt.Errorf("unable to find unused IP address in range [%s-%s]",
			client.OneArm.StartIPAddress, client.OneArm.EndIPAddress)
	}

	return freeIP, nil
}

// There are races here since there is no 'acquisition' of an IP. However, since k8s retries, it will
// be correct.
func (client *Client) getUnusedExternalIPAddress(ctx context.Context, ipamSubnet string) (string, error) {
	if client.gatewayRef == nil {
		return "", fmt.Errorf("gateway reference should not be nil")
	}

	// First, get list of ip ranges for the IPAMSubnet subnet mask
	edgeGW, resp, err := client.APIClient.EdgeGatewayApi.GetEdgeGateway(ctx, client.gatewayRef.Id)
	if err != nil {
		return "", fmt.Errorf("unable to retrieve edge gateway details for [%s]: resp [%+v]: [%v]",
			client.gatewayRef.Name, resp, err)
	}

	ipRangesList := make([]*swaggerClient.IpRanges, 0)
	for _, edgeGWUplink := range edgeGW.EdgeGatewayUplinks {
		for _, subnet := range edgeGWUplink.Subnets.Values {
			subnetMask := fmt.Sprintf("%s/%d", subnet.Gateway, subnet.PrefixLength)
			// if there is no specified subnet, look at all ranges
			if ipamSubnet == "" {
				ipRangesList = append(ipRangesList, subnet.IpRanges)
			} else if subnetMask == ipamSubnet {
				ipRangesList = append(ipRangesList, subnet.IpRanges)
				break
			}
		}
	}
	if len(ipRangesList) == 0 {
		return "", fmt.Errorf(
			"unable to get appropriate ipRange corresponding to IPAM subnet mask [%s]",
			ipamSubnet)
	}

	// Next, get the list of used IP addresses for this gateway
	usedIPs := make(map[string]bool)
	pageNum := int32(1)
	for {
		gwUsedIPAddresses, resp, err := client.APIClient.EdgeGatewayApi.GetUsedIpAddresses(ctx, pageNum, 25,
			client.gatewayRef.Id, nil)
		if err != nil {
			return "", fmt.Errorf("unable to get used IP addresses of gateway [%s]: [%+v]: [%v]",
				client.gatewayRef.Name, resp, err)
		}
		if len(gwUsedIPAddresses.Values) == 0 {
			break
		}

		for _, gwUsedIPAddress := range gwUsedIPAddresses.Values {
			usedIPs[gwUsedIPAddress.IpAddress] = true
		}

		pageNum++
	}

	// Now get a free IP that is not used.
	freeIP := ""
	for _, ipRanges := range ipRangesList {
		for _, ipRange := range ipRanges.Values {
			freeIP = getUnusedIPAddressInRange(ipRange.StartAddress,
				ipRange.EndAddress, usedIPs)
			if freeIP != "" {
				break
			}
		}
	}
	if freeIP == "" {
		return "", fmt.Errorf("unable to obtain free IP from gateway [%s]; all are used",
			client.gatewayRef.Name)
	}
	klog.Infof("Using unused IP [%s] on gateway [%v]\n", freeIP, client.gatewayRef.Name)

	return freeIP, nil
}

// TODO: There could be a race here as we don't book a slot. Retry repeatedly to get a LB Segment.
func (client *Client) getLoadBalancerSEG(ctx context.Context) (*swaggerClient.EntityReference, error) {
	if client.gatewayRef == nil {
		return nil, fmt.Errorf("gateway reference should not be nil")
	}

	pageNum := int32(1)
	var chosenSEGAssignment *swaggerClient.LoadBalancerServiceEngineGroupAssignment = nil
	for {
		segAssignments, resp, err := client.APIClient.LoadBalancerServiceEngineGroupAssignmentsApi.GetServiceEngineGroupAssignments(
			ctx, pageNum, 25,
			&swaggerClient.LoadBalancerServiceEngineGroupAssignmentsApiGetServiceEngineGroupAssignmentsOpts{
				Filter: optional.NewString(fmt.Sprintf("gatewayRef.id==%s", client.gatewayRef.Id)),
			},
		)
		if err != nil {
			return nil, fmt.Errorf("unable to get service engine group for gateway [%s]: resp: [%v]: [%v]",
				client.gatewayRef.Name, resp, err)
		}
		if len(segAssignments.Values) == 0 {
			return nil, fmt.Errorf("obtained no service engine group assignment for gateway [%s]: [%v]", client.gatewayRef.Name, err)
		}

		for _, segAssignment := range segAssignments.Values {
			if segAssignment.NumDeployedVirtualServices < segAssignment.MaxVirtualServices {
				chosenSEGAssignment = &segAssignment
				break
			}
		}
		if chosenSEGAssignment != nil {
			break
		}

		pageNum++
	}

	if chosenSEGAssignment == nil {
		return nil, fmt.Errorf("unable to find service engine group with free instances")
	}

	klog.Infof("Using service engine group [%v] on gateway [%v]\n", chosenSEGAssignment.ServiceEngineGroupRef, client.gatewayRef.Name)

	return chosenSEGAssignment.ServiceEngineGroupRef, nil
}

func getCursor(resp *http.Response) (string, error) {
	cursorURI := ""
	for _, linklet := range resp.Header["Link"] {
		for _, l := range link.Parse(linklet) {
			if l.Rel == "nextPage" {
				cursorURI = l.URI
				break
			}
		}
		if cursorURI != "" {
			break
		}
	}
	if cursorURI == "" {
		return "", nil
	}

	u, err := url.Parse(cursorURI)
	if err != nil {
		return "", fmt.Errorf("unable to parse cursor URI [%s]: [%v]", cursorURI, err)
	}

	cursorStr := ""
	keyMap, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", fmt.Errorf("unable to parse raw query [%s]: [%v]", u.RawQuery, err)
	}

	if cursorStrList, ok := keyMap["cursor"]; ok {
		cursorStr = cursorStrList[0]
	}

	return cursorStr, nil
}

type NatRuleRef struct {
	Name         string
	ID           string
	ExternalIP   string
	InternalIP   string
	ExternalPort int
	InternalPort int
}

// getNATRuleRef: returns nil if the rule is not found;
func (client *Client) getNATRuleRef(ctx context.Context, natRuleName string) (*NatRuleRef, error) {

	if client.gatewayRef == nil {
		return nil, fmt.Errorf("gateway reference should not be nil")
	}

	var natRuleRef *NatRuleRef = nil
	cursor := optional.EmptyString()
	for {
		natRules, resp, err := client.APIClient.EdgeGatewayNatRulesApi.GetNatRules(
			ctx, 128, client.gatewayRef.Id,
			&swaggerClient.EdgeGatewayNatRulesApiGetNatRulesOpts{
				Cursor: cursor,
			})
		if err != nil {
			return nil, fmt.Errorf("unable to get nat rules: resp: [%+v]: [%v]", resp, err)
		}
		if len(natRules.Values) == 0 {
			break
		}

		for _, rule := range natRules.Values {
			if rule.Name == natRuleName {
				externalPort := 0
				if rule.DnatExternalPort != "" {
					externalPort, err = strconv.Atoi(rule.DnatExternalPort)
					if err != nil {
						return nil, fmt.Errorf("unable to convert external port [%s] to int: [%v]",
							rule.DnatExternalPort, err)
					}
				}

				internalPort := 0
				if rule.InternalPort != "" {
					internalPort, err = strconv.Atoi(rule.InternalPort)
					if err != nil {
						return nil, fmt.Errorf("unable to convert internal port [%s] to int: [%v]",
							rule.InternalPort, err)
					}
				}

				natRuleRef = &NatRuleRef{
					ID:           rule.Id,
					Name:         rule.Name,
					ExternalIP:   rule.ExternalAddresses,
					InternalIP:   rule.InternalAddresses,
					ExternalPort: externalPort,
					InternalPort: internalPort,
				}
				break
			}
		}
		if natRuleRef != nil {
			break
		}

		cursorStr, err := getCursor(resp)
		if err != nil {
			return nil, fmt.Errorf("error while parsing response [%+v]: [%v]", resp, err)
		}
		if cursorStr == "" {
			break
		}
		cursor = optional.NewString(cursorStr)
	}

	if natRuleRef == nil {
		return nil, nil // this is not an error
	}

	return natRuleRef, nil
}

func getDNATRuleName(virtualServiceName string) string {
	return fmt.Sprintf("dnat-%s", virtualServiceName)
}

func getAppPortProfileName(dnatRuleName string) string {
	return fmt.Sprintf("appPort_%s", dnatRuleName)
}

func (client *Client) createDNATRule(ctx context.Context, dnatRuleName string,
	externalIP string, internalIP string, externalPort int32, internalPort int32) error {

	if client.gatewayRef == nil {
		return fmt.Errorf("gateway reference should not be nil")
	}

	dnatRuleRef, err := client.getNATRuleRef(ctx, dnatRuleName)
	if err != nil {
		return fmt.Errorf("unexpected error while looking for nat rule [%s] in gateway [%s]: [%v]",
			dnatRuleName, client.gatewayRef.Name, err)
	}
	if dnatRuleRef != nil {
		klog.Infof("DNAT Rule [%s] already exists", dnatRuleName)
		return nil
	}

	org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
	if err != nil {
		return fmt.Errorf("unable to find org [%s] by name: [%v]", client.ClusterOrgName, err)
	}

	appPortProfileName := getAppPortProfileName(dnatRuleName)
	klog.Infof("Verifying if app port profile [%s] exists in org [%s]...", appPortProfileName,
		client.ClusterOrgName)
	// we always use tenant scoped profiles
	contextEntityID := client.VDC.Vdc.ID
	scope := types.ApplicationPortProfileScopeTenant
	appPortProfile, err := org.GetNsxtAppPortProfileByName(appPortProfileName, scope)
	if err != nil && !strings.Contains(err.Error(), govcd.ErrorEntityNotFound.Error()) {
		return fmt.Errorf("unable to search for Application Port Profile [%s]: [%v]",
			appPortProfileName, err)
	}
	if appPortProfile == nil {
		if err == nil {
			// this should not arise
			return fmt.Errorf("obtained empty Application Port Profile even though there was no error")
		}

		klog.Infof("App Port Profile [%s] in org [%s] does not exist.", appPortProfileName,
			client.ClusterOrgName)

		appPortProfileConfig := &types.NsxtAppPortProfile{
			Name:             appPortProfileName,
			Description:      fmt.Sprintf("App Port Profile for DNAT rule [%s]", dnatRuleName),
			ApplicationPorts: []types.NsxtAppPortProfilePort{
				{
					Protocol: "TCP",
					// We use the externalPort itself, since the LB does the ExternalPort=>InternalPort
					// translation.
					DestinationPorts: []string{fmt.Sprintf("%d", externalPort)},
				},
			},
			OrgRef:           &types.OpenApiReference{
				Name: org.Org.Name,
				ID:   org.Org.ID,
			},
			ContextEntityId:  contextEntityID,
			Scope:            scope,
		}

		klog.Infof("Creating App Port Profile [%s] in org [%s]...", appPortProfileName,
			client.ClusterOrgName)
		appPortProfile, err = org.CreateNsxtAppPortProfile(appPortProfileConfig)
		if err != nil {
			return fmt.Errorf("unable to create nsxt app port profile with config [%#v]: [%v]",
				appPortProfileConfig, err)
		}
		klog.Infof("Created App Port Profile [%s] in org [%s].", appPortProfileName,
			client.ClusterOrgName)
	}

	ruleType := swaggerClient.DNAT_NatRuleType
	edgeNatRule := swaggerClient.EdgeNatRule{
		Name:              dnatRuleName,
		Enabled:           true,
		RuleType:          &ruleType,
		ExternalAddresses: externalIP,
		InternalAddresses: internalIP,
		DnatExternalPort:  fmt.Sprintf("%d", externalPort),
		ApplicationPortProfile: &swaggerClient.EntityReference{
			Name: appPortProfile.NsxtAppPortProfile.Name,
			Id:   appPortProfile.NsxtAppPortProfile.ID,
		},
	}
	resp, err := client.APIClient.EdgeGatewayNatRulesApi.CreateNatRule(ctx, edgeNatRule, client.gatewayRef.Id)
	if err != nil {
		return fmt.Errorf("unable to create dnat rule [%s]: [%s:%d]=>[%s:%d]: [%v]", dnatRuleName,
			externalIP, externalPort, internalIP, internalPort, err)
	}
	if resp != nil && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf(
			"unable to create dnat rule [%s]: [%s]=>[%s]; expected http response [%v], obtained [%v]: [%v]",
			dnatRuleName, externalIP, internalIP, http.StatusAccepted, resp.StatusCode, err)
	} else if err != nil {
		return fmt.Errorf("unable to create dnat rule [%s]: [%s:%d]=>[%s:%d]: [%v]", dnatRuleName,
			externalIP, externalPort, internalIP, internalPort, err)
	}

	taskURL := resp.Header.Get("Location")
	task := govcd.NewTask(&client.VCDClient.Client)
	task.Task.HREF = taskURL
	if err = task.WaitTaskCompletion(); err != nil {
		return fmt.Errorf("unable to create dnat rule [%s]: [%s]=>[%s]; creation task [%s] did not complete: [%v]",
			dnatRuleName, externalIP, internalIP, taskURL, err)
	}

	klog.Infof("Created DNAT rule [%s]: [%s:%d] => [%s:%d] on gateway [%s]\n", dnatRuleName,
		externalIP, externalPort, internalIP, internalPort, client.gatewayRef.Name)

	return nil
}

func (client *Client) updateAppPortProfile(appPortProfileName string, externalPort int32) error {
	org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
	if err != nil {
		return fmt.Errorf("unable to find org [%s] by name: [%v]", client.ClusterOrgName, err)
	}
	appPortProfile, err := org.GetNsxtAppPortProfileByName(appPortProfileName, types.ApplicationPortProfileScopeTenant)
	if err != nil {
		return fmt.Errorf("failed to get application port profile by name [%s]: [%v]", appPortProfileName, err)
	}
	if appPortProfile == nil || appPortProfile.NsxtAppPortProfile == nil || len(appPortProfile.NsxtAppPortProfile.ApplicationPorts) == 0 || len(appPortProfile.NsxtAppPortProfile.ApplicationPorts[0].DestinationPorts) == 0  {
		return fmt.Errorf("invalid app port profile [%s]", appPortProfileName)
	}
	appPortProfile.NsxtAppPortProfile.ApplicationPorts[0].DestinationPorts[0] = fmt.Sprintf("%d", externalPort)
	_, err = appPortProfile.Update(appPortProfile.NsxtAppPortProfile)
	if err != nil {
		return fmt.Errorf("failed to update application port profile")
	}
	klog.Infof("successfully updated app port profile [%s]", appPortProfileName)
	return nil
}

func (client *Client) updateDNATRule(ctx context.Context, dnatRuleName string, externalIP string, internalIP string, externalPort int32) error {
	if err := client.checkIfGatewayIsReady(ctx); err != nil {
		klog.Errorf("failed to update DNAT rule; gateway [%s] is busy", client.gatewayRef.Name)
		return err
	}
	dnatRuleRef, err := client.getNATRuleRef(ctx, dnatRuleName)
	if err != nil {
		return fmt.Errorf("unexpected error while looking for nat rule [%s] in gateway [%s]: [%v]",
			dnatRuleName, client.gatewayRef.Name, err)
	}
	if dnatRuleRef == nil {
		return fmt.Errorf("failed to get DNAT rule name [%s]", dnatRuleName)
	}
	dnatRule, resp, err := client.APIClient.EdgeGatewayNatRuleApi.GetNatRule(ctx, client.gatewayRef.Id, dnatRuleRef.ID)
	if resp != nil && resp.StatusCode != http.StatusOK {
		var responseMessageBytes []byte
		if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
			responseMessageBytes = gsErr.Body()
		}
		return fmt.Errorf(
			"unable to get DNAT rule [%s]; expected http response [%v], obtained [%v]: resp: [%#v]: [%v]",
			dnatRuleRef.Name, http.StatusOK, resp.StatusCode, string(responseMessageBytes), err)
	} else if err != nil {
		return fmt.Errorf("error while getting DNAT rule [%s]: [%v]", dnatRuleRef.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get DNAT rule with ID [%s] from gateway with ID [%s]; unexpected status code [%d]. Expected status code: [%d]", dnatRuleRef.ID, client.gatewayRef.Id, resp.StatusCode, http.StatusOK)
	}
	// update DNAT rule
	dnatRule.ExternalAddresses = externalIP
	dnatRule.InternalAddresses = internalIP
	dnatRule.DnatExternalPort = fmt.Sprintf("%d", externalPort)
	resp, err = client.APIClient.EdgeGatewayNatRuleApi.UpdateNatRule(ctx, dnatRule, client.gatewayRef.Id, dnatRuleRef.ID)
	if resp != nil && resp.StatusCode != http.StatusAccepted {
		var responseMessageBytes []byte
		if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
			responseMessageBytes = gsErr.Body()
		}
		return fmt.Errorf(
			"unable to update DNAT rule [%s]; expected http response [%v], obtained [%v]: resp: [%#v]: [%v]",
			dnatRuleRef.Name, http.StatusAccepted, resp.StatusCode, string(responseMessageBytes), err)
	} else if err != nil {
		return fmt.Errorf("error while updating DNAT rule [%s]: [%v]", dnatRuleRef.Name, err)
	}
	klog.Infof("successfully updated DNAT rule [%s]", dnatRuleRef.Name)
	return nil
}

// Note that this also deletes App Port Profile Config. So we always need to call this
// even if we don't find a DNAT rule, to ensure that everything is cleaned up.
func (client *Client) deleteDNATRule(ctx context.Context, dnatRuleName string,
	failIfAbsent bool) error {

	if err := client.checkIfGatewayIsReady(ctx); err != nil {
		klog.Errorf("failed to update DNAT rule; gateway [%s] is busy", client.gatewayRef.Name)
		return err
	}

	if client.gatewayRef == nil {
		return fmt.Errorf("gateway reference should not be nil")
	}

	dnatRuleRef, err := client.getNATRuleRef(ctx, dnatRuleName)
	if err != nil {
		return fmt.Errorf("unexpected error while finding dnat rule [%s]: [%v]", dnatRuleName, err)
	}
	if dnatRuleRef == nil {
		if failIfAbsent {
			return fmt.Errorf("dnat rule [%s] does not exist", dnatRuleName)
		}

		klog.Infof("DNAT rule [%s] does not exist", dnatRuleName)
	} else {
		resp, err := client.APIClient.EdgeGatewayNatRuleApi.DeleteNatRule(ctx,
			client.gatewayRef.Id, dnatRuleRef.ID)
		if resp.StatusCode != http.StatusAccepted {
			var responseMessageBytes []byte
			if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
				responseMessageBytes = gsErr.Body()
			}
			return fmt.Errorf("unable to delete dnat rule [%s]: expected http response [%v], obtained [%v], response: [%v]",
				dnatRuleName, http.StatusAccepted, resp.StatusCode, string(responseMessageBytes))
		}

		taskURL := resp.Header.Get("Location")
		task := govcd.NewTask(&client.VCDClient.Client)
		task.Task.HREF = taskURL
		if err = task.WaitTaskCompletion(); err != nil {
			return fmt.Errorf("unable to delete dnat rule [%s]: deletion task [%s] did not complete: [%v]",
				dnatRuleName, taskURL, err)
		}
		klog.Infof("Deleted DNAT rule [%s] on gateway [%s]\n", dnatRuleName, client.gatewayRef.Name)
	}

	appPortProfileName := getAppPortProfileName(dnatRuleName)
	klog.Infof("Checking if App Port Profile [%s] in org [%s] exists", appPortProfileName,
		client.ClusterOrgName)

	org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
	if err != nil {
		return fmt.Errorf("unable to find org [%s] by name: [%v]", client.ClusterOrgName, err)
	}

	// we always use tenant scoped profiles
	scope := types.ApplicationPortProfileScopeTenant
	appPortProfile, err := org.GetNsxtAppPortProfileByName(appPortProfileName, scope)
	if err != nil {
		if strings.Contains(err.Error(), govcd.ErrorEntityNotFound.Error()) {
			// things to delete are done
			return nil
		}

		return fmt.Errorf("unable to search for Application Port Profile [%s]: [%v]",
			appPortProfileName, err)
	}

	if appPortProfile == nil {
		if failIfAbsent {
			return fmt.Errorf("app port profile [%s] does not exist in org [%s]",
				appPortProfileName, client.ClusterOrgName)
		}

		klog.Infof("App Port Profile [%s] does not exist", appPortProfileName)
	} else {
		klog.Infof("Deleting App Port Profile [%s] in org [%s]", appPortProfileName,
			client.ClusterOrgName)
		if err = appPortProfile.Delete(); err != nil {
			return fmt.Errorf("unable to delete application port profile [%s]: [%v]", appPortProfileName, err)
		}
	}


	return nil
}

func (client *Client) getLoadBalancerPoolSummary(ctx context.Context,
	lbPoolName string) (*swaggerClient.EdgeLoadBalancerPoolSummary, error) {
	if client.gatewayRef == nil {
		return nil, fmt.Errorf("gateway reference should not be nil")
	}

	// This should return exactly one result, so no need to accumulate results
	lbPoolSummaries, resp, err := client.APIClient.EdgeGatewayLoadBalancerPoolsApi.GetPoolSummariesForGateway(
		ctx, 1, 25, client.gatewayRef.Id,
		&swaggerClient.EdgeGatewayLoadBalancerPoolsApiGetPoolSummariesForGatewayOpts{
			Filter: optional.NewString(fmt.Sprintf("name==%s", lbPoolName)),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get reference for LB pool [%s]: [%+v]: [%v]",
			lbPoolName, resp, err)
	}
	if len(lbPoolSummaries.Values) != 1 {
		return nil, nil // this is not an error
	}

	return &lbPoolSummaries.Values[0], nil
}

func (client *Client) getLoadBalancerPool(ctx context.Context,
	lbPoolName string) (*swaggerClient.EntityReference, error) {

	lbPoolSummary, err := client.getLoadBalancerPoolSummary(ctx, lbPoolName)
	if err != nil {
		return nil, fmt.Errorf("error when getting LB Pool: [%v]", err)
	}
	if lbPoolSummary == nil {
		return nil, nil // this is not an error
	}

	return &swaggerClient.EntityReference{
		Name: lbPoolSummary.Name,
		Id:   lbPoolSummary.Id,
	}, nil
}

func (client *Client) formLoadBalancerPool(lbPoolName string, ips []string,
	internalPort int32) (swaggerClient.EdgeLoadBalancerPool, []swaggerClient.EdgeLoadBalancerPoolMember) {
	lbPoolMembers := make([]swaggerClient.EdgeLoadBalancerPoolMember, len(ips))
	for i, ip := range ips {
		lbPoolMembers[i].IpAddress = ip
		lbPoolMembers[i].Port = internalPort
		lbPoolMembers[i].Ratio = 1
		lbPoolMembers[i].Enabled = true
	}

	lbPool := swaggerClient.EdgeLoadBalancerPool{
		Enabled:               true,
		Name:                  lbPoolName,
		DefaultPort:           internalPort,
		GracefulTimeoutPeriod: 0, // when service outage occurs, immediately mark as bad
		Members:               lbPoolMembers,
		GatewayRef:            client.gatewayRef,
	}
	return lbPool, lbPoolMembers
}

func (client *Client) createLoadBalancerPool(ctx context.Context, lbPoolName string,
	ips []string, internalPort int32) (*swaggerClient.EntityReference, error) {

	if client.gatewayRef == nil {
		return nil, fmt.Errorf("gateway reference should not be nil")
	}

	lbPoolRef, err := client.getLoadBalancerPool(ctx, lbPoolName)
	if err != nil {
		return nil, fmt.Errorf("unexpected error when querying for pool [%s]: [%v]",
			lbPoolName, err)
	}
	if lbPoolRef != nil {
		klog.Infof("LoadBalancer Pool [%s] already exists", lbPoolName)
		return lbPoolRef, nil
	}

	lbPool, lbPoolMembers := client.formLoadBalancerPool(lbPoolName, ips, internalPort)
	resp, err := client.APIClient.EdgeGatewayLoadBalancerPoolsApi.CreateLoadBalancerPool(ctx, lbPool)
	if err != nil {
		return nil, fmt.Errorf("unable to create loadbalancer pool with name [%s], members [%+v]: resp [%+v]: [%v]",
			lbPoolName, lbPoolMembers, resp, err)
	}
	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unable to create loadbalancer pool; expected http response [%v], obtained [%v]",
			http.StatusAccepted, resp.StatusCode)
	}

	taskURL := resp.Header.Get("Location")
	task := govcd.NewTask(&client.VCDClient.Client)
	task.Task.HREF = taskURL
	if err = task.WaitTaskCompletion(); err != nil {
		return nil, fmt.Errorf("unable to create loadbalancer pool; creation task [%s] did not complete: [%v]",
			taskURL, err)
	}

	// Get the pool to return it
	lbPoolRef, err = client.getLoadBalancerPool(ctx, lbPoolName)
	if err != nil {
		return nil, fmt.Errorf("unexpected error when querying for pool [%s]: [%v]",
			lbPoolName, err)
	}
	if lbPoolRef == nil {
		return nil, fmt.Errorf("unable to query for loadbalancer pool [%s] that was freshly created: [%v]",
			lbPoolName, err)
	}
	klog.Infof("Created lb pool [%v] on gateway [%v]\n", lbPoolRef, client.gatewayRef.Name)

	return lbPoolRef, nil
}

func (client *Client) deleteLoadBalancerPool(ctx context.Context, lbPoolName string,
	failIfAbsent bool) error {

	if client.gatewayRef == nil {
		return fmt.Errorf("gateway reference should not be nil")
	}

	lbPoolRef, err := client.getLoadBalancerPool(ctx, lbPoolName)
	if err != nil {
		return fmt.Errorf("unexpected error in retrieving loadbalancer pool [%s]: [%v]",
			lbPoolName, err)
	}
	if lbPoolRef == nil {
		if failIfAbsent {
			return fmt.Errorf("LoadBalancer pool [%s] does not exist", lbPoolName)
		}

		return nil
	}

	if err = client.checkIfLBPoolIsReady(ctx, lbPoolName); err != nil {
		return err
	}

	resp, err := client.APIClient.EdgeGatewayLoadBalancerPoolApi.DeleteLoadBalancerPool(ctx, lbPoolRef.Id)
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unable to delete lb pool; expected http response [%v], obtained [%v]",
			http.StatusAccepted, resp.StatusCode)
	}

	taskURL := resp.Header.Get("Location")
	task := govcd.NewTask(&client.VCDClient.Client)
	task.Task.HREF = taskURL
	if err = task.WaitTaskCompletion(); err != nil {
		return fmt.Errorf("unable to delete lb pool; deletion task [%s] did not complete: [%v]",
			taskURL, err)
	}
	klog.Infof("Deleted loadbalancer pool [%s]\n", lbPoolName)

	return nil
}

func hasSameLBPoolMembers(array1 []swaggerClient.EdgeLoadBalancerPoolMember, array2 []string) bool {
	if array1 == nil || array2 == nil || len(array1) != len(array2) {
		return false
	}
	elementsMap := make(map[string]int)
	for _, e := range array1 {
		elementsMap[e.IpAddress] = 1
	}
	for _, e := range array2 {
		if _, ok := elementsMap[e]; !ok {
			return false
		}
	}
	return false
}

func (client *Client) updateLoadBalancerPool(ctx context.Context, lbPoolName string, ips []string,
	internalPort int32) (*swaggerClient.EntityReference, error) {
	lbPoolRef, err := client.getLoadBalancerPool(ctx, lbPoolName)
	if err != nil {
		return nil, fmt.Errorf("unexpected error when querying for pool [%s]: [%v]", lbPoolName, err)
	}
	if lbPoolRef == nil {
		return nil, fmt.Errorf("no lb pool found with name [%s]: [%v]", lbPoolName, err)
	}

	lbPool, resp, err := client.APIClient.EdgeGatewayLoadBalancerPoolApi.GetLoadBalancerPool(ctx, lbPoolRef.Id)
	if err != nil {
		return nil, fmt.Errorf("unable to get loadbalancer pool with id [%s]: [%v]", lbPoolRef.Id, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unable to get loadbalancer pool with id [%s], expected http response [%v], obtained [%v]", lbPoolRef.Id, http.StatusOK, resp.StatusCode)
	}

	if hasSameLBPoolMembers(lbPool.Members, ips) && lbPool.Members[0].Port == internalPort {
		klog.Infof("No updates needed for the loadbalancer pool [%s]", lbPool.Name)
		return lbPoolRef, nil
	}
	if err = client.checkIfLBPoolIsReady(ctx, lbPoolName); err != nil {
		return nil, fmt.Errorf("unable to update loadbalancer pool [%s]; loadbalancer pool is busy: [%v]",lbPoolName, err)
	}
	lbPool, resp, err = client.APIClient.EdgeGatewayLoadBalancerPoolApi.GetLoadBalancerPool(ctx, lbPoolRef.Id)
	if err != nil {
		return nil, fmt.Errorf("unable to get loadbalancer pool with id [%s]: [%v]", lbPoolRef.Id, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unable to get loadbalancer pool with id [%s], expected http response [%v], obtained [%v]", lbPoolRef.Id, http.StatusOK, resp.StatusCode)
	}
	updatedLBPool, lbPoolMembers := client.formLoadBalancerPool(lbPoolName, ips, internalPort)
	resp, err = client.APIClient.EdgeGatewayLoadBalancerPoolApi.UpdateLoadBalancerPool(ctx, updatedLBPool, lbPoolRef.Id)
	if err != nil {
		return nil, fmt.Errorf("unable to update loadbalancer pool with name [%s], members [%+v]: resp [%+v]: [%v]",
			lbPoolName, lbPoolMembers, resp, err)
	}
	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unable to update loadbalancer pool; expected http response [%v], obtained [%v]",
			http.StatusAccepted, resp.StatusCode)
	}

	taskURL := resp.Header.Get("Location")
	task := govcd.NewTask(&client.VCDClient.Client)
	task.Task.HREF = taskURL
	if err = task.WaitTaskCompletion(); err != nil {
		return nil, fmt.Errorf("unable to update loadbalancer pool; update task [%s] did not complete: [%v]",
			taskURL, err)
	}

	// Get the pool to return it
	lbPoolRef, err = client.getLoadBalancerPool(ctx, lbPoolName)
	if err != nil {
		return nil, fmt.Errorf("unexpected error when querying for pool [%s]: [%v]",
			lbPoolName, err)
	}
	if lbPoolRef == nil {
		return nil, fmt.Errorf("unable to query for loadbalancer pool [%s] that was updated: [%v]",
			lbPoolName, err)
	}
	klog.Infof("Updated lb pool [%v] on gateway [%v]\n", lbPoolRef, client.gatewayRef.Name)

	return lbPoolRef, nil
}

func (client *Client) getVirtualService(ctx context.Context,
	virtualServiceName string) (*swaggerClient.EdgeLoadBalancerVirtualServiceSummary, error) {

	if client.gatewayRef == nil {
		return nil, fmt.Errorf("gateway reference should not be nil")
	}

	// This should return exactly one result, so no need to accumulate results
	lbVSSummaries, resp, err := client.APIClient.EdgeGatewayLoadBalancerVirtualServicesApi.GetVirtualServiceSummariesForGateway(
		ctx, 1, 25, client.gatewayRef.Id,
		&swaggerClient.EdgeGatewayLoadBalancerVirtualServicesApiGetVirtualServiceSummariesForGatewayOpts{
			Filter: optional.NewString(fmt.Sprintf("name==%s", virtualServiceName)),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get reference for LB VS [%s]: resp: [%v]: [%v]",
			virtualServiceName, resp, err)
	}
	if len(lbVSSummaries.Values) != 1 {
		return nil, nil // this is not an error
	}

	return &lbVSSummaries.Values[0], nil
}

func (client *Client) checkIfVirtualServiceIsPending(ctx context.Context, virtualServiceName string) error {
	if client.gatewayRef == nil {
		return fmt.Errorf("gateway reference should not be nil")
	}

	klog.V(3).Infof("Checking if virtual service [%s] is still pending", virtualServiceName)
	vsSummary, err := client.getVirtualService(ctx, virtualServiceName)
	if err != nil {
		return fmt.Errorf("unable to get summary for LB VS [%s]: [%v]", virtualServiceName, err)
	}
	if vsSummary == nil {
		return fmt.Errorf("unable to get summary of virtual service [%s]: [%v]", virtualServiceName, err)
	}
	if vsSummary.HealthStatus == "UP" || vsSummary.HealthStatus == "DOWN" {
		klog.V(3).Infof("Completed waiting for [%s] since healthStatus is [%s]",
			virtualServiceName, vsSummary.HealthStatus)
		return nil
	}

	klog.Errorf("Virtual service [%s] is still pending. Virtual service status: [%s]", virtualServiceName, vsSummary.HealthStatus)
	return NewVirtualServicePendingError(virtualServiceName)
}

func (client *Client) checkIfVirtualServiceIsReady(ctx context.Context, virtualServiceName string) error {
	if client.gatewayRef == nil {
		return fmt.Errorf("gateway reference should not be nil")
	}

	klog.V(3).Infof("Checking if virtual service [%s] is busy", virtualServiceName)
	vsSummary, err := client.getVirtualService(ctx, virtualServiceName)
	if err != nil {
		return fmt.Errorf("unable to get summary for LB VS [%s]: [%v]", virtualServiceName, err)
	}
	if vsSummary == nil {
		return fmt.Errorf("unable to get summary of virtual service [%s]: [%v]", virtualServiceName, err)
	}
	if *vsSummary.Status == "REALIZED" {
		klog.V(3).Infof("Completed waiting for [%s] to be configured since current status is [%s]",
			virtualServiceName, *vsSummary.Status)
		return nil
	}

	klog.Errorf("Virtual service [%s] is still being configured. Virtual service status: [%s]", virtualServiceName, *vsSummary.Status)
	return NewVirtualServiceBusyError(virtualServiceName)
}

func (client *Client) checkIfLBPoolIsReady(ctx context.Context, lbPoolName string) error {
	if client.gatewayRef == nil {
		return fmt.Errorf("gateway reference should not be nil")
	}

	klog.V(3).Infof("Checking if loadbalancer pool [%s] is busy", lbPoolName)
	lbPoolSummary, err := client.getLoadBalancerPoolSummary(ctx, lbPoolName)
	if err != nil {
		return fmt.Errorf("unable to get summary for LB VS [%s]: [%v]", lbPoolName, err)
	}
	if lbPoolSummary == nil {
		return fmt.Errorf("unable to get summary of virtual service [%s]: [%v]", lbPoolName, err)
	}
	if *lbPoolSummary.Status == "REALIZED" {
		klog.V(3).Infof("Completed waiting for [%s] to be configured since load balancer pool status is [%s]",
			lbPoolName, *lbPoolSummary.Status)
		return nil
	}

	klog.Errorf("Load balancer pool [%s] is still being configured. load balancer pool status: [%s]", lbPoolName, *lbPoolSummary.Status)
	return NewLBPoolBusyError(lbPoolName)
}

func (client *Client) checkIfGatewayIsReady(ctx context.Context) error {
	edgeGateway, resp, err := client.APIClient.EdgeGatewayApi.GetEdgeGateway(ctx, client.gatewayRef.Id)
	if resp != nil && resp.StatusCode != http.StatusOK {
		var responseMessageBytes []byte
		if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
			responseMessageBytes = gsErr.Body()
		}
		return fmt.Errorf(
			"unable to get gateway details; expected http response [%v], obtained [%v]: resp: [%#v]: [%v]",
			http.StatusOK, resp.StatusCode, string(responseMessageBytes), err)
	} else if err != nil {
		return fmt.Errorf("error while checking gateway status for [%s]: [%v]", client.gatewayRef.Name, err)
	}
	if *edgeGateway.Status == "REALIZED" {
		klog.V(3).Infof("Completed waiting for [%s] to be configured since gateway status is [%s]",
			client.gatewayRef.Name, *edgeGateway.Status)
		return nil
	}
	klog.Errorf("gateway [%s] is still being configured. Gateway status: [%s]", client.gatewayRef.Name, *edgeGateway.Status)
	return NewGatewayBusyError(client.gatewayRef.Name)
}

func (client *Client) updateVirtualServicePort(ctx context.Context, virtualServiceName string, externalPort int32) error {
	vsSummary, err := client.getVirtualService(ctx, virtualServiceName)
	if err != nil {
		return fmt.Errorf("failed to get virtual service summary for virtual service [%s]: [%v]", virtualServiceName, err)
	}
	if vsSummary == nil {
		return fmt.Errorf("virtual service [%s] doesn't exist", virtualServiceName)
	}
	if len(vsSummary.ServicePorts) == 0 {
		return fmt.Errorf("virtual service [%s] has no service ports", virtualServiceName)
	}
	if vsSummary.ServicePorts[0].PortStart == externalPort {
		klog.Infof("virtual service [%s] is already configured with port [%d]", virtualServiceName, externalPort)
		return nil
	}
	if err = client.checkIfVirtualServiceIsReady(ctx, virtualServiceName); err != nil {
		return err
	}
	vs, _, err := client.APIClient.EdgeGatewayLoadBalancerVirtualServiceApi.GetVirtualService(ctx, vsSummary.Id)
	if err != nil {
		return fmt.Errorf("failed to get virtual service with ID [%s]", vsSummary.Id)
	}
	if externalPort != vsSummary.ServicePorts[0].PortStart {
		// update both port start and port end to be the same.
		vs.ServicePorts[0].PortStart = externalPort
		vs.ServicePorts[0].PortEnd = externalPort
	}
	resp, err := client.APIClient.EdgeGatewayLoadBalancerVirtualServiceApi.UpdateVirtualService(ctx, vs, vsSummary.Id)
	if resp != nil && resp.StatusCode != http.StatusAccepted {
		var responseMessageBytes []byte
		if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
			responseMessageBytes = gsErr.Body()
		}
		return fmt.Errorf(
			"unable to update virtual service; expected http response [%v], obtained [%v]: resp: [%#v]: [%v]",
			http.StatusAccepted, resp.StatusCode, string(responseMessageBytes), err)
	} else if err != nil {
		return fmt.Errorf("error while updating virtual service [%s]: [%v]", virtualServiceName, err)
	}
	return nil
}

func (client *Client) createVirtualService(ctx context.Context, virtualServiceName string,
	lbPoolRef *swaggerClient.EntityReference, segRef *swaggerClient.EntityReference,
	freeIP string, rdeVIP string, vsType string, externalPort int32,
	useSSL bool, certificateAlias string) (*swaggerClient.EntityReference, error) {

	if client.gatewayRef == nil {
		return nil, fmt.Errorf("gateway reference should not be nil")
	}

	vsSummary, err := client.getVirtualService(ctx, virtualServiceName)
	if err != nil {
		return nil, fmt.Errorf("unexpected error while getting summary for LB VS [%s]: [%v]",
			virtualServiceName, err)
	}
	if vsSummary != nil {
		klog.V(3).Infof("LoadBalancer Virtual Service [%s] already exists", virtualServiceName)
		if err = client.checkIfVirtualServiceIsPending(ctx, virtualServiceName); err != nil {
			return nil, err
		}

		return &swaggerClient.EntityReference{
			Name: vsSummary.Name,
			Id:   vsSummary.Id,
		}, nil
	}

	if useSSL {
		klog.Infof("Creating SSL-enabled service with certificate [%s]", certificateAlias)
	}

	virtualServiceConfig := &swaggerClient.EdgeLoadBalancerVirtualService{
		Name:                  virtualServiceName,
		Enabled:               true,
		VirtualIpAddress:      freeIP,
		LoadBalancerPoolRef:   lbPoolRef,
		GatewayRef:            client.gatewayRef,
		ServiceEngineGroupRef: segRef,
		ServicePorts: []swaggerClient.EdgeLoadBalancerServicePort{
			{
				TcpUdpProfile: &swaggerClient.EdgeLoadBalancerTcpUdpProfile{
					Type_: "TCP_PROXY",
				},
				PortStart:  externalPort,
				SslEnabled: useSSL,
			},
		},
		ApplicationProfile: &swaggerClient.EdgeLoadBalancerApplicationProfile{
			SystemDefined: true,
		},
	}
	switch vsType {
	case "TCP":
		virtualServiceConfig.ApplicationProfile.Name = "System-L4-Application"
		virtualServiceConfig.ApplicationProfile.Type_ = "L4"
		if useSSL {
			virtualServiceConfig.ApplicationProfile.Name = "System-SSL-Application"
			virtualServiceConfig.ApplicationProfile.Type_ = "L4_TLS" // this needs Enterprise License
		}
		break

	case "HTTP":
		virtualServiceConfig.ApplicationProfile.Name = "System-HTTP"
		virtualServiceConfig.ApplicationProfile.Type_ = "HTTP"
		break

	case "HTTPS":
		virtualServiceConfig.ApplicationProfile.Name = "System-Secure-HTTP"
		virtualServiceConfig.ApplicationProfile.Type_ = "HTTPS"
		break

	default:
		return nil, fmt.Errorf("unhandled virtual service type [%s]", vsType)
	}

	if useSSL {
		clusterOrg, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
		if err != nil {
			return nil, fmt.Errorf("unable to get org for org [%s]: [%v]", client.ClusterOrgName, err)
		}
		if clusterOrg == nil || clusterOrg.Org == nil {
			return nil, fmt.Errorf("obtained nil org for name [%s]", client.ClusterOrgName)
		}

		certLibItems, resp, err := client.APIClient.CertificateLibraryApi.QueryCertificateLibrary(ctx,
			1, 128,
			&swaggerClient.CertificateLibraryApiQueryCertificateLibraryOpts{
				Filter: optional.NewString(fmt.Sprintf("alias==%s", certificateAlias)),
			},
			clusterOrg.Org.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to get cert with alias [%s] in org [%s]: resp: [%v]: [%v]",
				certificateAlias, client.ClusterOrgName, resp, err)
		}
		if len(certLibItems.Values) != 1 {
			return nil, fmt.Errorf("expected 1 cert with alias [%s], obtained [%d]",
				certificateAlias, len(certLibItems.Values))
		}
		virtualServiceConfig.CertificateRef =  &swaggerClient.EntityReference{
			Name: certLibItems.Values[0].Alias,
			Id:   certLibItems.Values[0].Id,
		}
	}

	resp, gsErr := client.APIClient.EdgeGatewayLoadBalancerVirtualServicesApi.CreateVirtualService(ctx, *virtualServiceConfig)
	if resp != nil && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf(
			"unable to create virtual service; expected http response [%v], obtained [%v]: resp: [%#v]: [%v]: [%v]",
			http.StatusAccepted, resp.StatusCode, resp, gsErr, string(gsErr.Body()))
	} else if gsErr != nil {
		return nil, fmt.Errorf("error while creating virtual service [%s]: [%v]", virtualServiceName, gsErr)
	}

	taskURL := resp.Header.Get("Location")
	task := govcd.NewTask(&client.VCDClient.Client)
	task.Task.HREF = taskURL
	if err = task.WaitTaskCompletion(); err != nil {
		return nil, fmt.Errorf("unable to create virtual service; creation task [%s] did not complete: [%v]",
			taskURL, err)
	}

	// update RDE with freeIp
	err = client.addVirtualIpToRDE(ctx, rdeVIP)
	if err != nil {
		klog.Errorf("error when adding virtual IP to RDE: [%v]", err)
	}

	vsSummary, err = client.getVirtualService(ctx, virtualServiceName)
	if err != nil {
		return nil, fmt.Errorf("unable to get summary for freshly created LB VS [%s]: [%v]",
			virtualServiceName, err)
	}
	if vsSummary == nil {
		return nil, fmt.Errorf("unable to get summary of freshly created virtual service [%s]: [%v]",
			virtualServiceName, err)
	}

	if err = client.checkIfVirtualServiceIsPending(ctx, virtualServiceName); err != nil {
		return nil, err
	}

	virtualServiceRef := &swaggerClient.EntityReference{
		Name: vsSummary.Name,
		Id:   vsSummary.Id,
	}
	klog.Infof("Created virtual service [%v] on gateway [%v]\n", virtualServiceRef, client.gatewayRef.Name)

	return virtualServiceRef, nil
}

func (client *Client) deleteVirtualService(ctx context.Context, virtualServiceName string,
	failIfAbsent bool, rdeVIP string) error {

	if client.gatewayRef == nil {
		return fmt.Errorf("gateway reference should not be nil")
	}

	vsSummary, err := client.getVirtualService(ctx, virtualServiceName)
	if err != nil {
		return fmt.Errorf("unable to get summary for LB Virtual Service [%s]: [%v]",
			virtualServiceName, err)
	}
	if vsSummary == nil {
		if failIfAbsent {
			return fmt.Errorf("virtual Service [%s] does not exist", virtualServiceName)
		}

		return nil
	}

	err = client.checkIfVirtualServiceIsReady(ctx, virtualServiceName)
	if err != nil {
			// virtual service is busy
			return err
	}

	resp, err := client.APIClient.EdgeGatewayLoadBalancerVirtualServiceApi.DeleteVirtualService(
		ctx, vsSummary.Id)
	if resp != nil && resp.StatusCode != http.StatusAccepted {
		var responseMessageBytes []byte
		if gsErr, ok := err.(swaggerClient.GenericSwaggerError); ok {
			responseMessageBytes = gsErr.Body()
		}
		return fmt.Errorf("unable to delete virtual service [%s]; expected http response [%v], obtained [%v] with response [%v]",
			vsSummary.Name, http.StatusAccepted, resp.StatusCode, string(responseMessageBytes))
	} else if err != nil {
		return fmt.Errorf("failed to delete virtual service [%s]: [%v]", vsSummary.Name, err)
	}

	taskURL := resp.Header.Get("Location")
	task := govcd.NewTask(&client.VCDClient.Client)
	task.Task.HREF = taskURL
	if err = task.WaitTaskCompletion(); err != nil {
		return fmt.Errorf("unable to delete virtual service; deletion task [%s] did not complete: [%v]",
			taskURL, err)
	}
	klog.Infof("Deleted virtual service [%s]\n", virtualServiceName)

	// remove virtual ip from RDE
	err = client.removeVirtualIpFromRDE(ctx, rdeVIP)
	if err != nil {
		return fmt.Errorf("error when removing vip from RDE: [%v]", err)
	}

	return nil
}

type PortDetails struct {
	Protocol     string
	PortSuffix   string
	ExternalPort int32
	InternalPort int32
	UseSSL       bool
	CertAlias    string
}

// CreateLoadBalancer : create a new load balancer pool and virtual service pointing to it
func (client *Client) CreateLoadBalancer(ctx context.Context, virtualServiceNamePrefix string,
	lbPoolNamePrefix string, ips []string, portDetailsList []PortDetails) (string, error) {

	client.RWLock.Lock()
	defer client.RWLock.Unlock()

	if len(portDetailsList) == 0 {
		// nothing to do here
		klog.Infof("There is no port specified. Hence nothing to do.")
		return "", fmt.Errorf("nothing to do since http and https ports are not specified")
	}

	if client.gatewayRef == nil {
		return "", fmt.Errorf("gateway reference should not be nil")
	}

	// Separately loop through all DNAT rules to see if any exist, so that we can reuse the external IP in case a
	// partial creation of load-balancer is continued and an externalIP was claimed earlier by a dnat rule
	externalIP := ""
	var err error
	if client.OneArm != nil {
		for _, portDetails := range portDetailsList {
			if portDetails.InternalPort == 0 {
				continue
			}

			virtualServiceName := fmt.Sprintf("%s-%s", virtualServiceNamePrefix, portDetails.PortSuffix)
			dnatRuleName := getDNATRuleName(virtualServiceName)
			dnatRuleRef, err := client.getNATRuleRef(ctx, dnatRuleName)
			if err != nil {
				return "", fmt.Errorf("unable to retrieve created dnat rule [%s]: [%v]", dnatRuleName, err)
			}
			if dnatRuleRef == nil {
				continue // ths implies that the rule does not exist
			}

			if externalIP != "" && externalIP != dnatRuleRef.ExternalIP {
				return "", fmt.Errorf("as per dnat there are two external IP rules for the same service: [%s], [%s]",
					externalIP, dnatRuleRef.ExternalIP)
			}

			externalIP = dnatRuleRef.ExternalIP
		}
	}

	if externalIP == "" {
		externalIP, err = client.getUnusedExternalIPAddress(ctx, client.IPAMSubnet)
		if err != nil {
			return "", fmt.Errorf("unable to get unused IP address from subnet [%s]: [%v]",
				client.IPAMSubnet, err)
		}
	}
	klog.Infof("Using external IP [%s] for virtual service\n", externalIP)

	for _, portDetails := range portDetailsList {
		if portDetails.InternalPort == 0 {
			klog.Infof("No internal port specified for [%s], hence loadbalancer not created\n",
				portDetails.PortSuffix)
			continue
		}

		virtualServiceName := fmt.Sprintf("%s-%s", virtualServiceNamePrefix, portDetails.PortSuffix)
		lbPoolName := fmt.Sprintf("%s-%s", lbPoolNamePrefix, portDetails.PortSuffix)

		vsSummary, err := client.getVirtualService(ctx, virtualServiceName)
		if err != nil {
			return "", fmt.Errorf("unexpected error while querying for virtual service [%s]: [%v]",
				virtualServiceName, err)
		}
		if vsSummary != nil {
			if vsSummary.LoadBalancerPoolRef.Name != lbPoolName {
				return "", fmt.Errorf("virtual Service [%s] found with unexpected loadbalancer pool [%s]",
					virtualServiceName, lbPoolName)
			}

			klog.V(3).Infof("LoadBalancer Virtual Service [%s] already exists", virtualServiceName)
			if err = client.checkIfVirtualServiceIsPending(ctx, virtualServiceName); err != nil {
				return "", err
			}

			continue
		}

		virtualServiceIP := externalIP
		if client.OneArm != nil {
			internalIP, err := client.getUnusedInternalIPAddress(ctx)
			if err != nil {
				return "", fmt.Errorf("unable to get internal IP address for one-arm mode: [%v]", err)
			}

			dnatRuleName := getDNATRuleName(virtualServiceName)
			if err = client.createDNATRule(ctx, dnatRuleName, externalIP, internalIP,
				portDetails.ExternalPort, portDetails.InternalPort); err != nil {
				return "", fmt.Errorf("unable to create dnat rule [%s:%d] => [%s:%d]: [%v]",
					externalIP, portDetails.ExternalPort, internalIP, portDetails.InternalPort, err)
			}
			// use the internal IP to create virtual service
			virtualServiceIP = internalIP

			// We get an IP address above and try to get-or-create a DNAT rule from external IP => internal IP.
			// If the rule already existed, the old DNAT rule will remain unchanged. Hence we get the old externalIP
			// from the old rule and use it. What happens to the new externalIP that we selected above? It just remains
			// unused and hence does not get allocated and disappears. Since there is no IPAM based resource
			// _acquisition_, the new externalIP can just be forgotten about.
			dnatRuleRef, err := client.getNATRuleRef(ctx, dnatRuleName)
			if err != nil {
				return "", fmt.Errorf("unable to retrieve created dnat rule [%s]: [%v]", dnatRuleName, err)
			}
			if dnatRuleRef == nil {
				return "", fmt.Errorf("retrieved dnat rule ref is nil")
			}

			externalIP = dnatRuleRef.ExternalIP
		}

		segRef, err := client.getLoadBalancerSEG(ctx)
		if err != nil {
			return "", fmt.Errorf("unable to get service engine group from edge [%s]: [%v]",
				client.gatewayRef.Name, err)
		}

		lbPoolRef, err := client.createLoadBalancerPool(ctx, lbPoolName, ips, portDetails.InternalPort)
		if err != nil {
			return "", fmt.Errorf("unable to create load balancer pool [%s]: [%v]", lbPoolName, err)
		}

		virtualServiceRef, err := client.createVirtualService(ctx, virtualServiceName, lbPoolRef, segRef,
			virtualServiceIP, externalIP, portDetails.Protocol, portDetails.ExternalPort,
			portDetails.UseSSL, portDetails.CertAlias)
		if err != nil {
			return "", err
		}
		klog.Infof("Created Load Balancer with virtual service [%v], pool [%v] on gateway [%s]\n",
			virtualServiceRef, lbPoolRef, client.gatewayRef.Name)
	}

	return externalIP, nil
}

func (client *Client) UpdateLoadBalancer(ctx context.Context, lbPoolName string, virtualServiceName string,
	ips []string, internalPort int32, externalPort int32) error {

	client.RWLock.Lock()
	defer client.RWLock.Unlock()
	_, err := client.updateLoadBalancerPool(ctx, lbPoolName, ips, internalPort)
	if err != nil {
		if lbPoolBusyErr, ok := err.(*LoadBalancerPoolBusyError); ok {
			klog.Errorf("update loadbalancer pool failed; loadbalancer pool [%s] is busy: [%v]", lbPoolName, err)
			return lbPoolBusyErr
		}
		return fmt.Errorf("unable to update load balancer pool [%s]: [%v]", lbPoolName, err)
	}
	err = client.updateVirtualServicePort(ctx, virtualServiceName, externalPort)
	if err != nil {
		if vsBusyErr, ok := err.(*VirtualServiceBusyError); ok {
			klog.Errorf("update virtual service failed; virtual service [%s] is busy: [%v]", virtualServiceName, err)
			return vsBusyErr
		}
		return fmt.Errorf("unable to update virtual service [%s] with port [%d]: [%v]", virtualServiceName, externalPort, err)
	}
	// update app port profile
	dnatRuleName := getDNATRuleName(virtualServiceName)
	appPortProfileName := getAppPortProfileName(dnatRuleName)
	err = client.updateAppPortProfile(appPortProfileName, externalPort)
	if err != nil {
		return fmt.Errorf("unable to update application port profile [%s] with external port [%d]: [%v]", appPortProfileName, externalPort, err)
	}

	// update DNAT rule
	dnatRuleRef, err := client.getNATRuleRef(ctx, dnatRuleName)
	if err != nil {
		return fmt.Errorf("unable to retrieve created dnat rule [%s]: [%v]", dnatRuleName, err)
	}
	err = client.updateDNATRule(ctx, dnatRuleName, dnatRuleRef.ExternalIP, dnatRuleRef.InternalIP, externalPort)
	if err != nil {
		return fmt.Errorf("unable to update DNAT rule [%s]: [%v]", dnatRuleName, err)
	}
	return nil
}

// DeleteLoadBalancer : create a new load balancer pool and virtual service pointing to it
func (client *Client) DeleteLoadBalancer(ctx context.Context, virtualServiceNamePrefix string,
	lbPoolNamePrefix string, portDetailsList []PortDetails) error {

	client.RWLock.Lock()
	defer client.RWLock.Unlock()

	// TODO: try to continue in case of errors
	var err error

	// Here the principle is to delete what is available; retry in case of failure
	// but do not fail for missing entities, since a retry will always have missing
	// entities.
	for _, portDetails := range portDetailsList {
		if portDetails.InternalPort == 0 {
			klog.Infof("No internal port specified for [%s], hence loadbalancer not created\n",
				portDetails.PortSuffix)
			continue
		}

		virtualServiceName := fmt.Sprintf("%s-%s", virtualServiceNamePrefix, portDetails.PortSuffix)
		lbPoolName := fmt.Sprintf("%s-%s", lbPoolNamePrefix, portDetails.PortSuffix)

		// get external IP
		rdeVIP := ""
		dnatRuleName := ""
		if client.OneArm != nil {
			dnatRuleName = getDNATRuleName(virtualServiceName)
			dnatRuleRef, err := client.getNATRuleRef(ctx, dnatRuleName)
			if err != nil {
				return fmt.Errorf("unable to get dnat rule ref for nat rule [%s]: [%v]", dnatRuleName, err)
			}
			if dnatRuleRef != nil {
				rdeVIP = dnatRuleRef.ExternalIP
			}
		} else {
			vsSummary, err := client.getVirtualService(ctx, virtualServiceName)
			if err != nil {
				return fmt.Errorf("unable to get summary for LB Virtual Service [%s]: [%v]",
					virtualServiceName, err)
			}
			if vsSummary != nil {
				rdeVIP = vsSummary.VirtualIpAddress
			}
		}

		err = client.deleteVirtualService(ctx, virtualServiceName, false, rdeVIP)
		if err != nil {
			if vsBusyErr, ok := err.(*VirtualServiceBusyError); ok {
				klog.Errorf("delete virtual service failed; virtual service [%s] is busy: [%v]", virtualServiceName, err)
				return vsBusyErr
			}
			return fmt.Errorf("unable to delete virtual service [%s]: [%v]", virtualServiceName, err)
		}

		err = client.deleteLoadBalancerPool(ctx, lbPoolName, false)
		if err != nil {
			if lbPoolBusyErr, ok := err.(*LoadBalancerPoolBusyError); ok {
				klog.Errorf("delete loadbalancer pool failed; loadbalancer pool [%s] is busy: [%v]", lbPoolName, err)
				return lbPoolBusyErr
			}
			return fmt.Errorf("unable to delete load balancer pool [%s]: [%v]", lbPoolName, err)
		}

		if client.OneArm != nil {
			err = client.deleteDNATRule(ctx, dnatRuleName, false)
			if err != nil {
				return fmt.Errorf("unable to delete dnat rule [%s]: [%v]", dnatRuleName, err)
			}
		}
	}

	return nil
}

// GetLoadBalancer :
func (client *Client) GetLoadBalancer(ctx context.Context, virtualServiceName string) (string, error) {

	vsSummary, err := client.getVirtualService(ctx, virtualServiceName)
	if err != nil {
		return "", fmt.Errorf("unable to get summary for LB Virtual Service [%s]: [%v]",
			virtualServiceName, err)
	}
	if vsSummary == nil {
		return "", nil // this is not an error
	}

	klog.V(3).Infof("LoadBalancer Virtual Service [%s] exists", virtualServiceName)
	if err = client.checkIfVirtualServiceIsPending(ctx, virtualServiceName); err != nil {
		return "", err
	}

	if client.OneArm == nil {
		return vsSummary.VirtualIpAddress, nil
	}

	dnatRuleName := getDNATRuleName(virtualServiceName)
	dnatRuleRef, err := client.getNATRuleRef(ctx, dnatRuleName)
	if err != nil {
		return "", fmt.Errorf("unable to find dnat rule [%s] for virtual service [%s]: [%v]",
			dnatRuleName, virtualServiceName, err)
	}
	if dnatRuleRef == nil {
		return "", nil // so that a retry creates the DNAT rule
	}

	return dnatRuleRef.ExternalIP, nil
}

// IsNSXTBackedGateway : return true if gateway is backed by NSX-T
func (client *Client) IsNSXTBackedGateway() bool {
	isNSXTBackedGateway :=
		(client.networkBackingType == swaggerClient.NSXT_FIXED_SEGMENT_BackingNetworkType) ||
			(client.networkBackingType == swaggerClient.NSXT_FLEXIBLE_SEGMENT_BackingNetworkType)

	return isNSXTBackedGateway
}
