package util

import (
	"encoding/json"
	"fmt"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
)

const (
	ResourcePersistentVolume = "named-disk"
)

func convertMapToComponentStatus(componentStatusMap map[string]interface{}) (*vcdsdk.ComponentStatus, error) {
	componentStatusBytes, err := json.Marshal(componentStatusMap)
	if err != nil {
		return nil, fmt.Errorf("failed to convert componentStatusMap to byte array: [%v]", err)
	}

	var cs vcdsdk.ComponentStatus
	err = json.Unmarshal(componentStatusBytes, &cs)
	if err != nil {
		return nil, fmt.Errorf("failed to read bytes from componentStatus [%#v] to ComponentStatus object: [%v]", componentStatusMap, err)
	}

	return &cs, nil
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
	case vcdsdk.IsCAPVCDEntityType(rde.EntityType):
		csiEntry, ok := statusMap[vcdsdk.ComponentCSI]
		if !ok {
			return make([]string, 0), nil
		}
		csiMap, ok := csiEntry.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unable to convert [%T] to map[string]interface{}", csiEntry)
		}
		componentStatus, _ := convertMapToComponentStatus(csiMap)
		pvNameArray := make([]string, len(componentStatus.VCDResourceSet))
		namedDiskCount := 0
		for _, rs := range componentStatus.VCDResourceSet {
			if rs.Type == ResourcePersistentVolume {
				pvNameArray[namedDiskCount] = rs.Name
				namedDiskCount++
			}
		}
		return pvNameArray[0:namedDiskCount], nil
	case vcdsdk.IsNativeClusterEntityType(rde.EntityType):
		pvInterfaces = statusMap["persistentVolumes"]
	default:
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
