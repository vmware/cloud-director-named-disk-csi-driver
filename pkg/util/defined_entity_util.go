package util

import (
	"fmt"
	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
	"strings"
)

const (
	CAPVCDEntityTypeVendor = "vmware"
	CAPVCDEntityTypeNss    = "capvcdCluster"

	NativeClusterEntityTypeVendor = "cse"
	NativeClusterEntityTypeNss    = "nativeCluster"
)

type VCDResource struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}
type CSIStatus struct {
	Name              string        `json:"name,omitempty"`
	Version           string        `json:"version,omitempty"`
	VCDResourceSet    []VCDResource `json:"vcdResourceSet,omitempty"`
	Errors            []string      `json:"errors,omitempty"`
	PersistentVolumes []string      `json:"persistentVolumes,omitempty"`
}

func isCAPVCDEntityType(entityTypeID string) bool {
	entityTypeIDSplit := strings.Split(entityTypeID, ":")
	// format is urn:vcloud:type:<vendor>:<nss>:<version>
	if len(entityTypeIDSplit) != 6 {
		return false
	}
	return entityTypeIDSplit[3] == CAPVCDEntityTypeVendor && entityTypeIDSplit[4] == CAPVCDEntityTypeNss
}

func isNativeClusterEntityType(entityTypeID string) bool {
	entityTypeIDSplit := strings.Split(entityTypeID, ":")
	// format is urn:vcloud:type:<vendor>:<nss>:<version>
	if len(entityTypeIDSplit) != 6 {
		return false
	}
	return entityTypeIDSplit[3] == NativeClusterEntityTypeVendor && entityTypeIDSplit[4] == NativeClusterEntityTypeNss
}

func GetPVsFromRDE(rde *swaggerClient.DefinedEntity) ([]string, error) {
	statusEntry, ok := rde.Entity["status"]
	if !ok {
		return nil, fmt.Errorf("could not find 'status' entry in defined entity")
	}
	statusMap, ok := statusEntry.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to map", statusEntry)
	}

	var pvInterfaces interface{}
	if isCAPVCDEntityType(rde.EntityType) {
		// TODO: Upgrade CSI section in CAPVCD RDE
		csiStatusInterface, ok := statusMap["csi"]
		if !ok {
			return nil, fmt.Errorf("RDE [%s] is missing CSI status", rde.Id)
		}
		csiStatusMap, ok := csiStatusInterface.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("failed to convert CSI status in RDE [%s] to map[string]interface{}", rde.Id)
		}
		pvInterfaces = csiStatusMap["persistentVolumes"]
	} else if isNativeClusterEntityType(rde.EntityType) {
		pvInterfaces = statusMap["persistentVolumes"]
	} else {
		return nil, fmt.Errorf("entity type %s not supported by CSI", rde.EntityType)
	}
	if pvInterfaces == nil {
		return make([]string, 0), nil
	}

	pvInterfacesSlice, ok := pvInterfaces.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to slice of interface", pvInterfaces)
	}
	pvIdStrs := make([]string, len(pvInterfacesSlice))
	for idx, pvInterface := range pvInterfacesSlice {
		currPv, ok := pvInterface.(string)
		if !ok {
			return nil, fmt.Errorf("unable to convert [%T] to string", pvInterface)
		}
		pvIdStrs[idx] = currPv
	}
	return pvIdStrs, nil
}

func ReplacePVsInRDE(rde *swaggerClient.DefinedEntity, updatedPvs []string) (*swaggerClient.DefinedEntity, error) {
	statusEntry, ok := rde.Entity["status"]
	if !ok {
		return nil, fmt.Errorf("could not find 'status' entry in defined entity")
	}
	statusMap, ok := statusEntry.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to map", statusEntry)
	}

	if isCAPVCDEntityType(rde.EntityType) {
		// TODO: Upgrade capvcdCluster RDE to 1.1.0
		csiStatusInterface, ok := statusMap["csi"]
		if !ok {
			statusMap["csi"] = make(map[string]interface{})
			csiStatusInterface = statusMap["csi"]
		}
		csiStatusMap, ok := csiStatusInterface.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("failed to parse 'csi' section in the status of RDE [%s]", rde.Id)
		}
		csiStatusMap["persistentVolumes"] = updatedPvs
	} else if isNativeClusterEntityType(rde.EntityType) {
		statusMap["persistentVolumes"] = updatedPvs
	} else {
		return nil, fmt.Errorf("entity type %s not supported by CSI", rde.EntityType)
	}
	return rde, nil
}
