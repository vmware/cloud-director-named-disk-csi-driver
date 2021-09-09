/*
   Copyright 2021 VMware, Inc.
   SPDX-License-Identifier: Apache-2.0
*/

package swagger

// Error type format displayed to users for exceptions emerging from openapi endpoints.
type ModelError struct {
	MinorErrorCode string `json:"minorErrorCode"`
	Message string `json:"message"`
	StackTrace string `json:"stackTrace,omitempty"`
}
