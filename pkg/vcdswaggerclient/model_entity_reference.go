/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package swagger

// Entity reference used to describe VCD entities
type EntityReference struct {
	Name string `json:"name,omitempty"`
	Id string `json:"id,omitempty"`
}
