/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package vcdclient

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	swaggerClient "github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdswaggerclient"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"k8s.io/klog"
)

const (
	VCloudApiVersion = "36.0"
)

// VCDAuthConfig : contains config related to vcd auth
type VCDAuthConfig struct {
	User         string `json:"user"`
	Password     string `json:"password"`
	RefreshToken string `json:"refreshToken"`
	Org          string `json:"org"`
	Host         string `json:"host"`
	CloudAPIHref string `json:"cloudapihref"`
	VDC          string `json:"vdc"`
	Insecure     bool   `json:"insecure"`
	Token        string `json:"token"`
	IsSysAdmin   bool   `json:"isSysAdmin,omitempty"` // will be set by GetBearerToken()
}

func (config *VCDAuthConfig) GetBearerToken() (*govcd.VCDClient, *http.Response, error) {
	href := fmt.Sprintf("%s/api", config.Host)
	u, err := url.ParseRequestURI(href)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to parse url [%s]: %s", href, err)
	}

	vcdClient := govcd.NewVCDClient(*u, config.Insecure)
	vcdClient.Client.APIVersion = VCloudApiVersion
	klog.Infof("Using VCD OpenAPI version [%s]", vcdClient.Client.APIVersion)

	var resp *http.Response
	if config.RefreshToken != "" {
		// Since it is not known if the user is sysadmin, try to get the access token using provider endpoint
		accessTokenResponse, resp, err := config.getAccessTokenFromRefreshToken(true)
		if err != nil {
			// Failed to get token as provider. Try to get token as tenant user
			accessTokenResponse, resp, err = config.getAccessTokenFromRefreshToken(false)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get access token from refresh token: [%v]", err)
			}
		}
		err = vcdClient.SetToken(config.Org, "Authorization", fmt.Sprintf("Bearer %s", accessTokenResponse.AccessToken))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to set authorization header: [%v]", err)
		}
		config.IsSysAdmin, err = isAdminUser(vcdClient)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to determine if the user is a system administrator: [%v]", err)
		}
		vcdClient.Client.IsSysAdmin = config.IsSysAdmin

		klog.Infof("Running CPI as sysadmin [%v]", vcdClient.Client.IsSysAdmin)
		return vcdClient, resp, nil
	}
	resp, err = vcdClient.GetAuthResponse(config.User, config.Password, config.Org)
	if err != nil {
		return nil, resp, fmt.Errorf("unable to authenticate [%s/%s] for url [%s]: [%+v] : [%v]",
			config.Org, config.User, href, resp, err)
	}

	return vcdClient, resp, nil
}

func (config *VCDAuthConfig) GetVCDClientFromSecrets() (*govcd.VCDClient, error) {

	vcdClient, _, err := config.GetBearerToken()
	if err != nil {
		return nil, fmt.Errorf("unable to get bearer token from serets: [%v]", err)
	}

	return vcdClient, nil
}

func (config *VCDAuthConfig) GetSwaggerClientFromSecrets() (*govcd.VCDClient, *swaggerClient.APIClient, error) {

	vcdClient, _, err := config.GetBearerToken()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to get bearer token from serets: [%v]", err)
	}
	var authHeader string
	if config.RefreshToken == "" {
		authHeader = fmt.Sprintf("Bearer %s", vcdClient.Client.VCDToken)
	} else {
		authHeader = vcdClient.Client.VCDToken
	}

	swaggerConfig := swaggerClient.NewConfiguration()
	swaggerConfig.BasePath = fmt.Sprintf("%s/cloudapi", config.Host)
	swaggerConfig.AddDefaultHeader("Authorization", authHeader)
	swaggerConfig.HTTPClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	return vcdClient, swaggerClient.NewAPIClient(swaggerConfig), nil
}

func (config *VCDAuthConfig) GetPlainClientFromSecrets() (*govcd.VCDClient, error) {

	href := fmt.Sprintf("%s/api", config.Host)
	u, err := url.ParseRequestURI(href)
	if err != nil {
		return nil, fmt.Errorf("unable to parse url: [%s]: [%v]", href, err)
	}

	vcdClient := govcd.NewVCDClient(*u, config.Insecure)
	vcdClient.Client.APIVersion = VCloudApiVersion
	klog.Infof("Using VCD XML API version [%s]", vcdClient.Client.APIVersion)
	if err = vcdClient.Authenticate(config.User, config.Password, config.Org); err != nil {
		return nil, fmt.Errorf("cannot authenticate with vcd: [%v]", err)
	}

	return vcdClient, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int32  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func (config *VCDAuthConfig) getAccessTokenFromRefreshToken(isSysadminUser bool) (*tokenResponse, *http.Response, error) {
	accessTokenUrl := fmt.Sprintf("%s/oauth/provider/token", config.Host)
	if !isSysadminUser {
		accessTokenUrl = fmt.Sprintf("%s/oauth/tenant/%s/token", config.Host, config.Org)
	}
	payload := url.Values{}
	payload.Set("grant_type", "refresh_token")
	payload.Set("refresh_token", config.RefreshToken)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: config.Insecure},
		},
	}
	oauthRequest, err := http.NewRequest("POST", accessTokenUrl,
		strings.NewReader(payload.Encode()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get access token from refresh token: [%v]", err)
	}
	oauthRequest.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	oauthResponse, err := client.Do(oauthRequest)
	if err != nil {
		return nil, oauthResponse, fmt.Errorf("request to get access token failed: [%v]", err)
	}
	if oauthResponse.StatusCode != http.StatusOK {
		return nil, oauthResponse, fmt.Errorf("error while getting access token from refresh token: [%v]", err)
	}
	body, err := ioutil.ReadAll(oauthResponse.Body)
	if err != nil {
		return nil, oauthResponse, fmt.Errorf("unable to read response body while getting access token from refresh token: [%v]", err)
	}
	defer func() {
		if err := oauthResponse.Body.Close(); err != nil {
			klog.Errorf("failed to close response body: [%v]", err)
		}
	}()

	var accessTokenResponse tokenResponse
	if err = json.Unmarshal(body, &accessTokenResponse); err != nil {
		return nil, oauthResponse, fmt.Errorf("error unmarshaling the token response: [%v]", err)
	}
	return &accessTokenResponse, oauthResponse, nil
}

func NewVCDAuthConfigFromSecrets(host string, user string, secret string, refreshToken string, org string, insecure bool) *VCDAuthConfig {
	return &VCDAuthConfig{
		Host:         host,
		User:         user,
		Password:     secret,
		RefreshToken: refreshToken,
		Org:          org,
		Insecure:     insecure,
	}
}

type currentSessionsResponse struct {
	Id    string   `json:"id"`
	Roles []string `json:"roles"`
}

func isAdminUser(vcdClient *govcd.VCDClient) (bool, error) {
	currentSessionUrl, err :=
		vcdClient.Client.OpenApiBuildEndpoint("1.0.0/sessions/current")
	if err != nil {
		return false, fmt.Errorf("failed to construct current session url [%v]", err)
	}
	var output currentSessionsResponse
	err = vcdClient.Client.OpenApiGetItem(vcdClient.Client.APIVersion, currentSessionUrl, url.Values{}, &output)
	if err != nil {
		return false, fmt.Errorf("error while getting current session [%v]", err)
	}
	if len(output.Roles) == 0 {
		return false, fmt.Errorf("no roles associated with the user: [%v]", err)
	}
	isSysAdmin := output.Roles[0] == "System Administrator"
	return isSysAdmin, nil
}
