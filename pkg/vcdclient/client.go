/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package vcdclient

import (
	"crypto/tls"
	"fmt"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/util"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdtypes"
	"github.com/vmware/go-vcloud-director/v2/types/v56"
	"net/http"
	"net/url"
	"sync"

	"github.com/vmware/go-vcloud-director/v2/govcd"
	"k8s.io/klog"
)

var (
	clientCreatorLock sync.Mutex
	clientSingleton   *Client = nil
)

// Client :
type Client struct {
	vcdAuthConfig      *VCDAuthConfig
	vcdClient          *govcd.VCDClient
	vdc                *govcd.Vdc
	ClusterID          string
	rwLock             sync.RWMutex
}

// RefreshToken will check if can authenticate and rebuild clients if needed
func (client *Client) RefreshToken() error {
	_, r, err := client.vcdAuthConfig.GetBearerTokenFromSecrets()
	if r == nil && err != nil {
		return fmt.Errorf("error while getting bearer token from secrets: [%v]", err)
	} else if r != nil && r.StatusCode == 401 {
		klog.Info("Refreshing tokens as previous one has expired")
		client.vcdClient.Client.APIVersion = "35.0"
		err := client.vcdClient.Authenticate(client.vcdAuthConfig.User,
			client.vcdAuthConfig.Password, client.vcdAuthConfig.Org)
		if err != nil {
			return fmt.Errorf("unable to Authenticate user [%s]: [%v]",
				client.vcdAuthConfig.User, err)
		}

		org, err := client.vcdClient.GetOrgByNameOrId(client.vcdAuthConfig.Org)
		if err != nil {
			return fmt.Errorf("unable to get vcd organization [%s]: [%v]",
				client.vcdAuthConfig.Org, err)
		}

		vdc, err := org.GetVDCByName(client.vcdAuthConfig.VDC, true)
		if err != nil {
			return fmt.Errorf("unable to get vdc from org [%s], vdc [%s]: [%v]",
				client.vcdAuthConfig.Org, client.vcdAuthConfig.VDC, err)
		}

		client.vdc = vdc
	}

	return nil
}

func (client *Client) GetOrgVDCByName(orgName string, vdcName string) (*govcd.Vdc, error) {

	org, err := client.vcdClient.GetOrgByName(orgName)
	if err != nil {
		return nil, fmt.Errorf("unable to get vcd organization [%s]: [%v]", orgName, err)
	}

	err = org.Refresh()
	if err != nil {
		return nil, err
	}

	recordType := "application/vnd.vmware.vcloud.query.records+xml"
	queryLinks := make([]types.Link, 0)
	for _, link := range org.Org.Link {
		if link.Name == "" || link.Name == vdcName {
			if link.Rel == "down" && link.Type == recordType {
				queryLinks = append(queryLinks, *link)
			}
		}
	}

	outLinks := make([]types.Link, 0)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{Transport: tr}
	for _, queryLink := range queryLinks {
		href := queryLink.HREF
		urlStr, err := url.Parse(href)
		if err != nil {
			return nil, fmt.Errorf("unable to parse url [%s]: [%v]", href, err)
		}
		if urlStr.Query().Get("type") != "orgVdc" {
			continue
		}

		nextPageURI := href
		for nextPageURI != "" {
			req, err := http.NewRequest("GET", nextPageURI, nil)
			if err != nil {
				return nil, fmt.Errorf("unable to create request for url [%v]: [%v]",
					nextPageURI, err)
			}
			req.Header.Add("Accept",
				fmt.Sprintf("application/*+xml;version=%s", client.vcdClient.Client.APIVersion))
			req.Header.Add("X-Vmware-Vcloud-Token-Type", "Bearer")
			req.Header.Add("Authorization", fmt.Sprintf("bearer %s", client.vcdClient.Client.VCDToken))
			resp, err := httpClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("unable to query link [%v]: resp: [%v]: [%v]",
					nextPageURI, resp, err)
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("received status [%v] for req [%v]: resp: [%v]",
					resp.StatusCode, req, resp)
			}

			queryOvdcResults := vcdtypes.QueryResultRecordsType{}
			if err = util.DecodeXMLBody(types.BodyTypeXML, resp, &queryOvdcResults); err != nil {
				return nil, fmt.Errorf("error decoding OVDC response: %s", err)
			}

			if queryOvdcResults.OrgVdcRecord != nil {
				for _, queryOvdcResult := range queryOvdcResults.OrgVdcRecord {
					outLinks = append(outLinks, types.Link{
						HREF: queryOvdcResult.HREF,
						Name: queryOvdcResult.Name,
					})
				}
			}

			nextPageURI = ""
			for _, link := range queryOvdcResults.Link {
				if link.Rel == "nextPage" {
					// assuming that there is only one of these
					nextPageURI = link.HREF
					continue
				}
				outLinks = append(outLinks, *link)
			}
		}
	}

	for _, link := range outLinks {
		if vdcName == link.Name {
			return org.GetVDCByHref(link.HREF)
		}
	}

	return nil, fmt.Errorf("unable to find vdc [%s:%s]", org.Org.Name, vdcName)
}

// NewVCDClientFromSecrets :
func NewVCDClientFromSecrets(host string, orgName string, vdcName string,
	user string, password string, insecure bool, clusterID string, getVdcClient bool) (*Client, error) {

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

	vcdAuthConfig := NewVCDAuthConfigFromSecrets(host, user, password, orgName, insecure)

	vcdClient, err := vcdAuthConfig.GetVCDClientFromSecrets()
	if err != nil {
		return nil, fmt.Errorf("unable to get swagger client from secrets: [%v]", err)
	}

	client := &Client{
		vcdAuthConfig:    vcdAuthConfig,
		vcdClient:        vcdClient,
		ClusterID:        clusterID,
	}

	if getVdcClient {
		// this new client is only needed to get the vdc pointer
		vcdClient, err = vcdAuthConfig.GetPlainClientFromSecrets()
		if err != nil {
			return nil, fmt.Errorf("unable to get plain client from secrets: [%v]", err)
		}

		client.vdc, err = client.GetOrgVDCByName(orgName, vdcName)
		if err != nil {
			return nil, fmt.Errorf("unable to get orgVdc ID for [%s:%s]: [%v]", orgName, vdcName, err)
		}
	}
	client.vcdClient = vcdClient

	clientSingleton = client

	return clientSingleton, nil
}
