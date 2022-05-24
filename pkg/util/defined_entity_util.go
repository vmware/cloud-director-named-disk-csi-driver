package util

import (
	"fmt"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
	"strings"
)

const (
	ResourcePersistentVolume = "named-disk"
	OldPersistentVolumeKey   = "persistentVolumes"
)

func IsValidEntityId(rdeId string) bool {
	if rdeId == "" || strings.HasPrefix(rdeId, vcdsdk.NoRdePrefix) {
		return false
	}
	return true
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
	switch {
	case vcdsdk.IsNativeClusterEntityType(rde.EntityType):
		pvInterfaces = statusMap["persistentVolumes"]
	default:
		return nil, fmt.Errorf("only native cluster is supported here, entity type %s not supported", rde.EntityType)
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

// AddPVsInRDE function only used for Native Cluster
func AddPVsInRDE(rde *swaggerClient.DefinedEntity, updatedPvs []string) (*swaggerClient.DefinedEntity, error) {
	if !vcdsdk.IsNativeClusterEntityType(rde.EntityType) {
		return nil, fmt.Errorf("entity type %s not supported by CSI", rde.EntityType)
	}
	statusEntry, ok := rde.Entity["status"]
	if !ok {
		return nil, fmt.Errorf("could not find 'status' entry in defined entity")
	}
	statusMap, ok := statusEntry.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to map", statusEntry)
	}
	statusMap["persistentVolumes"] = updatedPvs
	return rde, nil
}

// RemovePVInRDE function only used for Native Cluster
func RemovePVInRDE(rde *swaggerClient.DefinedEntity, updatedPvs []string) (*swaggerClient.DefinedEntity, error) {
	if !vcdsdk.IsNativeClusterEntityType(rde.EntityType) {
		return nil, fmt.Errorf("entity type %s not supported by CSI", rde.EntityType)
	}
	statusEntry, ok := rde.Entity["status"]
	if !ok {
		return nil, fmt.Errorf("could not find 'status' entry in defined entity")
	}
	statusMap, ok := statusEntry.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to map", statusEntry)
	}
	statusMap["persistentVolumes"] = updatedPvs
	return rde, nil
}

func GetOldPVsFromRDE(rde *swaggerClient.DefinedEntity) (*swaggerClient.DefinedEntity, []string, error) {
	statusEntry, ok := rde.Entity["status"]
	if !ok {
		return nil, nil, fmt.Errorf("could not find 'status' entry in defined entity")
	}
	statusMap, ok := statusEntry.(map[string]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("unable to convert [%T] to map", statusEntry)
	}

	pvInterfaces, ok := statusMap[OldPersistentVolumeKey]

	if !ok {
		return nil, make([]string, 0), fmt.Errorf("no entity [%s] found", OldPersistentVolumeKey)
	}
	if pvInterfaces == nil {
		return nil, make([]string, 0), nil
	}

	pvInterfacesSlice, ok := pvInterfaces.([]interface{})
	if !ok {
		return nil, nil, fmt.Errorf("unable to convert [%T] to slice of interface", pvInterfaces)
	}
	pvIdStrs := make([]string, len(pvInterfacesSlice))
	for idx, pvInterface := range pvInterfacesSlice {
		currPv, ok := pvInterface.(string)
		if !ok {
			return nil, nil, fmt.Errorf("unable to convert [%T] to string", pvInterface)
		}
		pvIdStrs[idx] = currPv
	}

	delete(statusMap, OldPersistentVolumeKey)

	return rde, pvIdStrs, nil

}
