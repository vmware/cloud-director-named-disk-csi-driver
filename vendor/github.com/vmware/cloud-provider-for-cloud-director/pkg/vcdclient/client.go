/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"k8s.io/klog"
	"net/http"
	"sync"

	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
	"github.com/vmware/go-vcloud-director/v2/govcd"
)

var (
	clientCreatorLock sync.Mutex
	clientSingleton   *Client = nil
)

// OneArm : internal struct representing OneArm config details
type OneArm struct {
	StartIPAddress string
	EndIPAddress   string
}

// Client :
type Client struct {
	VCDAuthConfig  *VCDAuthConfig
	ClusterOrgName string
	ClusterOVDCName    string
	ClusterVAppName string
	VCDClient *govcd.VCDClient
	VDC         *govcd.Vdc
	APIClient   *swaggerClient.APIClient
	networkName string
	IPAMSubnet  string
	gatewayRef  *swaggerClient.EntityReference
	networkBackingType swaggerClient.BackingNetworkType
	ClusterID          string
	OneArm             *OneArm
	HTTPPort           int32
	HTTPSPort          int32
	CertificateAlias string
	RWLock           sync.RWMutex
}

func (client *Client) RefreshBearerToken() error {
	klog.Infof("Refreshing vcd client")

	href := fmt.Sprintf("%s/api", client.VCDAuthConfig.Host)
	client.VCDClient.Client.APIVersion = VCloudApiVersion

	klog.Infof("Is user sysadmin: [%v]", client.VCDAuthConfig.IsSysAdmin)
	if client.VCDAuthConfig.RefreshToken != "" {
		userOrg := client.VCDAuthConfig.UserOrg
		if client.VCDAuthConfig.IsSysAdmin {
			userOrg = "system"
		}
		// Refresh vcd client using refresh token as system org user
		err := client.VCDClient.SetToken(userOrg,
			govcd.ApiTokenHeader, client.VCDAuthConfig.RefreshToken)
		if err != nil {
			return fmt.Errorf("failed to refresh VCD client with the refresh token: [%v]", err)
		}
	} else if client.VCDAuthConfig.User != "" && client.VCDAuthConfig.Password != "" {
		// Refresh vcd client using username and password
		resp, err := client.VCDClient.GetAuthResponse(client.VCDAuthConfig.User, client.VCDAuthConfig.Password,
			client.VCDAuthConfig.UserOrg)
		if err != nil {
			return fmt.Errorf("unable to authenticate [%s/%s] for url [%s]: [%+v] : [%v]",
				client.VCDAuthConfig.UserOrg, client.VCDAuthConfig.User, href, resp, err)
		}
	} else {
		return fmt.Errorf(
			"unable to find refresh token or secret to refresh vcd client for user [%s/%s] and url [%s]",
			client.VCDAuthConfig.UserOrg, client.VCDAuthConfig.User, href)
	}

	// reset legacy client
	org, err := client.VCDClient.GetOrgByNameOrId(client.ClusterOrgName)
	if err != nil {
		return fmt.Errorf("unable to get vcd organization [%s]: [%v]",
			client.ClusterOrgName, err)
	}

	vdc, err := org.GetVDCByName(client.ClusterOVDCName, true)
	if err != nil {
		return fmt.Errorf("unable to get VDC from org [%s], VDC [%s]: [%v]",
			client.ClusterOrgName, client.VCDAuthConfig.VDC, err)
	}
	client.VDC = vdc

	// reset swagger client
	swaggerConfig := swaggerClient.NewConfiguration()
	swaggerConfig.BasePath = fmt.Sprintf("%s/cloudapi", client.VCDAuthConfig.Host)
	swaggerConfig.AddDefaultHeader("Authorization", fmt.Sprintf("Bearer %s", client.VCDClient.Client.VCDToken))
	swaggerConfig.HTTPClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: client.VCDAuthConfig.Insecure},
		},
	}
	client.APIClient = swaggerClient.NewAPIClient(swaggerConfig)

	klog.Info("successfully refreshed all clients")
	return nil
}

// NewVCDClientFromSecrets :
func NewVCDClientFromSecrets(host string, orgName string, vdcName string, vAppName string,
	networkName string, ipamSubnet string, userOrg string, user string, password string,
	refreshToken string, insecure bool, clusterID string, oneArm *OneArm,
	httpPort int32, httpsPort int32, certAlias string, getVdcClient bool) (*Client, error) {

	// TODO: validation of parameters

	clientCreatorLock.Lock()
	defer clientCreatorLock.Unlock()

	// Return old client if everything matches. Else create new one and cache it.
	// This is suboptimal but is not a common case.
	if clientSingleton != nil {
		if clientSingleton.VCDAuthConfig.Host == host &&
			clientSingleton.ClusterOrgName == orgName &&
			clientSingleton.ClusterOVDCName == vdcName &&
			clientSingleton.ClusterVAppName == vAppName &&
			clientSingleton.networkName == networkName &&
			clientSingleton.VCDAuthConfig.UserOrg == userOrg &&
			clientSingleton.VCDAuthConfig.User == user &&
			clientSingleton.VCDAuthConfig.Password == password &&
			clientSingleton.VCDAuthConfig.RefreshToken == refreshToken &&
			clientSingleton.VCDAuthConfig.Insecure == insecure {
			return clientSingleton, nil
		}
	}

	vcdAuthConfig := NewVCDAuthConfigFromSecrets(host, user, password, refreshToken, userOrg, insecure)

	vcdClient, apiClient, err := vcdAuthConfig.GetSwaggerClientFromSecrets()
	if err != nil {
		return nil, fmt.Errorf("unable to get swagger client from secrets: [%v]", err)
	}

	client := &Client{
		VCDAuthConfig:    vcdAuthConfig,
		ClusterOrgName:   orgName,
		ClusterOVDCName:  vdcName,
		ClusterVAppName:  vAppName,
		VCDClient:        vcdClient,
		APIClient:        apiClient,
		networkName:      networkName,
		IPAMSubnet:       ipamSubnet,
		gatewayRef:       nil,
		ClusterID:        clusterID,
		OneArm:           oneArm,
		HTTPPort:         httpPort,
		HTTPSPort:        httpsPort,
		CertificateAlias: certAlias,
	}

	if getVdcClient {
		org, err := vcdClient.GetOrgByName(orgName)
		if err != nil {
			return nil, fmt.Errorf("unable to get org from name [%s]: [%v]", orgName, err)
		}

		client.VDC, err = org.GetVDCByName(vdcName, true)
		if err != nil {
			return nil, fmt.Errorf("unable to get VDC [%s] from org [%s]: [%v]", vdcName, orgName, err)
		}
	}
	client.VCDClient = vcdClient
	// We will specifically cache the gateway ID that corresponds to the
	// network name since it is used frequently in the loadbalancer context.
	if networkName != "" {
		ctx := context.Background()
		if err = client.CacheGatewayDetails(ctx); err != nil {
			return nil, fmt.Errorf("unable to get gateway edge from network name [%s]: [%v]",
				client.networkName, err)
		}
	}
	clientSingleton = client

	klog.Infof("Client singleton is sysadmin: [%v]", clientSingleton.VCDClient.Client.IsSysAdmin)
	return clientSingleton, nil
}
