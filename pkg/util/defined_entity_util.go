package util

import (
	 "fmt"
	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
)

const (
	EntityTypePrefix = "urn:vcloud:type"
	CAPVCDEntityTypeVendor = "vmware"
	CAPVCDEntityTypeNss = "capvcdCluster"
	CAPVCDEntityTypeVersion = "1.0.0"

	NativeClusterEntityTypeVendor = "cse"
	NativeClusterEntityTypeNss = "nativeCluster"
	NativeClusterEntityTypeVersion = "2.0.0"
)

var (
	CAPVCDEntityTypeID = fmt.Sprintf("%s:%s:%s:%s", EntityTypePrefix, CAPVCDEntityTypeVendor, CAPVCDEntityTypeNss, CAPVCDEntityTypeVersion)
	NativeEntityTypeID = fmt.Sprintf("%s:%s:%s:%s", EntityTypePrefix, NativeClusterEntityTypeVendor, NativeClusterEntityTypeNss, NativeClusterEntityTypeVersion)
)

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
	if rde.EntityType == CAPVCDEntityTypeID {
		pvInterfaces = statusMap["persistentVolumes"]
	} else if rde.EntityType == NativeEntityTypeID {
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

	if rde.EntityType == CAPVCDEntityTypeID {
		statusMap["persistentVolumes"] = updatedPvs
	} else if rde.EntityType == NativeEntityTypeID {
		statusMap["persistentVolumes"] = updatedPvs
	} else {
		return nil, fmt.Errorf("entity type %s not supported by CSI", rde.EntityType)
	}
	return rde, nil
}
