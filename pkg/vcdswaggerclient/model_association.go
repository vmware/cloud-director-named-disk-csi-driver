/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package swagger

// Site association information for an entity
type Association struct {
	// ID of the entity.
	EntityId string `json:"entityId,omitempty"`
	// ID of the association.
	AssociationId string `json:"associationId,omitempty"`
}
