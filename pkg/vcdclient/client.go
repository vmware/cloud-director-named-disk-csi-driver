/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdclient

import (
	"fmt"
	swaggerClient "github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdswaggerclient"
	"sync"

	"github.com/vmware/go-vcloud-director/v2/govcd"
)

var (
	clientCreatorLock sync.Mutex
	clientSingleton   *Client = nil
)

// Client :
type Client struct {
	vcdAuthConfig *VCDAuthConfig
	vcdClient     *govcd.VCDClient
	vdc           *govcd.Vdc
	ClusterID     string
	rwLock        sync.RWMutex
	apiClient     *swaggerClient.APIClient
}

func (client *Client) RefreshBearerToken() error {
	href := fmt.Sprintf("%s/api", client.vcdAuthConfig.Host)
	if client.vcdAuthConfig.User != "" && client.vcdAuthConfig.Password != "" && client.vcdAuthConfig.RefreshToken == "" {
		resp, err := client.vcdClient.GetAuthResponse(client.vcdAuthConfig.User, client.vcdAuthConfig.Password, client.vcdAuthConfig.Org)
		if err != nil {
			return fmt.Errorf("unable to authenticate [%s/%s] for url [%s]: [%+v] : [%v]",
				client.vcdAuthConfig.Org, client.vcdAuthConfig.User, href, resp, err)
		}
		return nil
	}
	accessTokenResponse, _, err := client.vcdAuthConfig.getAccessTokenFromRefreshToken(client.vcdClient.Client.IsSysAdmin)
	if err != nil {
		return fmt.Errorf("failed to get access token from refresh token for user [%s/%s] for url [%s]: [%v]", client.vcdAuthConfig.Org, client.vcdAuthConfig.User, href, err)
	}
	err = client.vcdClient.SetToken(client.vcdAuthConfig.Org, "Authorization", fmt.Sprintf("Bearer %s", accessTokenResponse.AccessToken))
	if err != nil {
		return fmt.Errorf("failed to set authorization header: [%v]", err)
	}
	return nil
}

// NewVCDClientFromSecrets :
func NewVCDClientFromSecrets(host string, orgName string, vdcName string,
	user string, password string, refreshToken string, insecure bool, clusterID string, getVdcClient bool) (*Client, error) {

	// TODO: validation of parameters

	clientCreatorLock.Lock()
	defer clientCreatorLock.Unlock()

	// Return old client if everything matches. Else create new one and cache it.
	// This is suboptimal but is not a common case.
	if clientSingleton != nil {
		if clientSingleton.vcdAuthConfig.Host == host &&
			clientSingleton.vcdAuthConfig.Org == orgName &&
			clientSingleton.vcdAuthConfig.VDC == vdcName &&
			clientSingleton.vcdAuthConfig.User == user &&
			clientSingleton.vcdAuthConfig.Password == password &&
			clientSingleton.vcdAuthConfig.Insecure == insecure {
			return clientSingleton, nil
		}
	}

	vcdAuthConfig := NewVCDAuthConfigFromSecrets(host, user, password, refreshToken, orgName, insecure)

	// Get API client
	vcdClient, apiClient, err := vcdAuthConfig.GetSwaggerClientFromSecrets()
	if err != nil {
		return nil, fmt.Errorf("unable to get swagger client from secrets: [%v]", err)
	}

	client := &Client{
		vcdAuthConfig: vcdAuthConfig,
		vcdClient:     vcdClient,
		ClusterID:     clusterID,
		apiClient:     apiClient,
	}

	if getVdcClient {
		org, err := vcdClient.GetOrgByName(orgName)
		if err != nil {
			return nil, fmt.Errorf("unable to get org from name [%s]: [%v]", orgName, err)
		}

		client.vdc, err = org.GetVDCByName(vdcName, true)
		if err != nil {
			return nil, fmt.Errorf("unable to get vdc [%s] from org [%s]: [%v]", vdcName, orgName, err)
		}
	}
	client.vcdClient = vcdClient

	clientSingleton = client

	return clientSingleton, nil
}
