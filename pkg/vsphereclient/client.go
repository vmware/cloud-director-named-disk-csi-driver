/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package vsphereclient

import (
	"context"
	"fmt"
	"github.com/vmware/govmomi/session/cache"
	"github.com/vmware/govmomi/session/keepalive"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
	"k8s.io/klog"
	"net/url"
	"sync"
	"time"
)

var (
	clientCreatorLock sync.Mutex
	clientSingleton   *Client = nil
)

// Client :
type Client struct {
	vsphereAuthConfig *VSphereAuthConfig
	govcClient         *vim25.Client
}

// NewVSphereClientFromSecrets :
func NewVSphereClientFromSecrets(host string, user string, password string,
	insecure bool) (*Client, error) {

	clientCreatorLock.Lock()
	defer clientCreatorLock.Unlock()
	if clientSingleton != nil {
		return clientSingleton, nil
	}

	vsphereAuthConfig := NewVSphereAuthConfigFromSecrets(host, user, password, insecure)
	// Parse URL from string
	u, err := soap.ParseURL(host)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL [%s]: [%v]", host, err)
	}

	u.User = url.UserPassword(user, password)

	// Share govc's session cache
	s := &cache.Session{
		URL:      u,
		Insecure: insecure,
	}

	ctx := context.Background()
	govcClient := &vim25.Client{}
	govcClient.RoundTripper = keepalive.NewHandlerSOAP(govcClient.RoundTripper, time.Minute,
		func()error{
			err = s.Login(ctx, govcClient, nil)
			if err != nil {
				return fmt.Errorf("unable to login to vsphere host [%s]: [%v]",
					host, err)
			}
			klog.Infof("Refreshed session with vsphere successfully")
			return nil
		})

	// Login once
	err = s.Login(ctx, govcClient, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to login to vsphere host [%s] for the first time: [%v]",
			host, err)
	}

	return &Client{
		govcClient: govcClient,
		vsphereAuthConfig: vsphereAuthConfig,
	}, nil
}
