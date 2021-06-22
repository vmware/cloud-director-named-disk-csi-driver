/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package vcdtypes

import (
	"encoding/xml"
	"github.com/vmware/go-vcloud-director/v2/types/v56"
)

// LinkList represents a list of links
type LinkList []*types.Link

// Type for a single disk query result in records format.
// Reference: vCloud API 35.0 - QueryResultDiskRecordType
// https://code.vmware.com/apis/1046/vmware-cloud-director/doc/doc/types/QueryResultDiskRecordType.html
type DiskRecordType struct {
	HREF               string          `xml:"href,attr,omitempty"`
	Id                 string          `xml:"id,attr,omitempty"`
	Type               string          `xml:"type,attr,omitempty"`
	Name               string          `xml:"name,attr,omitempty"`
	Vdc                string          `xml:"vdc,attr,omitempty"`
	Description        string          `xml:"description,attr,omitempty"`
	SizeMB             int64           `xml:"sizeMb,attr,omitempty"`
	Iops               int64           `xml:"iops,attr,omitempty"`
	Encrypted          bool            `xml:"encrypted,attr,omitempty"`
	DataStore          string          `xml:"dataStore,attr,omitempty"`
	DataStoreName      string          `xml:"datastoreName,attr,omitempty"`
	OwnerName          string          `xml:"ownerName,attr,omitempty"`
	VdcName            string          `xml:"vdcName,attr,omitempty"`
	Task               string          `xml:"task,attr,omitempty"`
	StorageProfile     string          `xml:"storageProfile,attr,omitempty"`
	StorageProfileName string          `xml:"storageProfileName,attr,omitempty"`
	Status             string          `xml:"status,attr,omitempty"`
	BusType            string          `xml:"busType,attr,omitempty"`
	BusTypeDesc        string          `xml:"busTypeDesc,attr,omitempty"`
	BusSubType         string          `xml:"busSubType,attr,omitempty"`
	AttachedVmCount    int32           `xml:"attachedVmCount,attr,omitempty"`
	IsAttached         bool            `xml:"isAttached,attr,omitempty"`
	IsShareable        bool            `xml:"isShareable,attr,omitempty"`
	Link               []*types.Link   `xml:"Link,omitempty"`
	Metadata           *types.Metadata `xml:"Metadata,omitempty"`
}

// Represents an independent disk
// Reference: vCloud API 35.0 - DiskType
// https://code.vmware.com/apis/287/vcloud?h=Director#/doc/doc/types/DiskType.html
type Disk struct {
	HREF         string `xml:"href,attr,omitempty"`
	Type         string `xml:"type,attr,omitempty"`
	Id           string `xml:"id,attr,omitempty"`
	OperationKey string `xml:"operationKey,attr,omitempty"`
	Name         string `xml:"name,attr"`
	Status       int    `xml:"status,attr,omitempty"`
	SizeMb       int64  `xml:"sizeMb,attr"`
	Iops         int64  `xml:"iops,attr,omitempty"`
	Encrypted    bool   `xml:"encrypted,attr,omitempty"`
	BusType      string `xml:"busType,attr,omitempty"`
	BusSubType   string `xml:"busSubType,attr,omitempty"`
	Shareable    bool   `xml:"shareable,attr,omitempty"`
	SharingType  string `xml:"sharingType,attr,omitempty"`
	UUID         string `xml:"uuid,attr,omitempty"`

	Description     string                 `xml:"Description,omitempty"`
	Files           *types.FilesList       `xml:"Files,omitempty"`
	Link            []*types.Link          `xml:"Link,omitempty"`
	Owner           *types.Owner           `xml:"Owner,omitempty"`
	StorageProfile  *types.Reference       `xml:"StorageProfile,omitempty"`
	Tasks           *types.TasksInProgress `xml:"Tasks,omitempty"`
	VCloudExtension *types.VCloudExtension `xml:"VCloudExtension,omitempty"`
}

// DiskCreateParams Parameters for creating or updating an independent disk.
// Reference: vCloud API 35.0 - DiskCreateParamsType
// https://code.vmware.com/apis/1046/vmware-cloud-director/doc/doc/types/DiskCreateParamsType.html
type DiskCreateParams struct {
	XMLName         xml.Name               `xml:"DiskCreateParams"`
	Xmlns           string                 `xml:"xmlns,attr,omitempty"`
	Disk            *Disk                  `xml:"Disk"`
	Locality        *types.Reference       `xml:"Locality,omitempty"`
	VCloudExtension *types.VCloudExtension `xml:"VCloudExtension,omitempty"`
}

// Represents a list of virtual machines
// Reference: vCloud API 35.0 - VmsType
// https://code.vmware.com/apis/1046/vmware-cloud-director/doc/doc/types/VmsType.html
type Vms struct {
	XMLName     xml.Name   `xml:"Vms"`
	Xmlns       string     `xml:"xmlns,attr,omitempty"`

	HREF        string     `xml:"href,attr"`
	Type        string     `xml:"type,attr"`

	// Elements
	Link            []*types.Link          `xml:"Link,omitempty"`
	VCloudExtension *types.VCloudExtension `xml:"VCloudExtension,omitempty"`
	VmReference     []*types.Reference      `xml:"VmReference,omitempty"`
}
