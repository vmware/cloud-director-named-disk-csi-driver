package util

import (
	"encoding/json"
	"fmt"
	"github.com/vmware/cloud-director-named-disk-csi-driver/version"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
)

const (
	ResourcePersistentVolume = "named-disk"
	CSIName                  = "cloud-director-named-disk-csi-driver"
	OldPersistentVolumeKey   = "persistentVolumes"
	PVDetailsNum             = 2
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

func UpgradePVResourceToRDE(statusMap map[string]interface{}, pvDetailList [][]string, rdeId string) (map[string]interface{}, error) {
	if pvDetailList == nil || len(pvDetailList) == 0 || len(pvDetailList[0]) != PVDetailsNum {
		return nil, fmt.Errorf("error occurred when validating pv details list in RDE [%s]", rdeId)
	}
	vcdResourceSet := make([]vcdsdk.VCDResource, len(pvDetailList))
	for idx := range vcdResourceSet {
		vcdResourceSet[idx] = vcdsdk.VCDResource{
			Type:              ResourcePersistentVolume,
			ID:                pvDetailList[idx][1],
			Name:              pvDetailList[idx][0],
			AdditionalDetails: nil,
		}
	}
	updatedStatusMap, err := addToVCDResourceSet(vcdsdk.ComponentCSI, CSIName, version.Version, statusMap, vcdResourceSet)
	if err != nil {
		return nil, fmt.Errorf("error occurred when updating VCDResource set of %s status in RDE [%s]: [%v]", vcdsdk.ComponentCSI, rdeId, err)
	}
	return updatedStatusMap, nil
}

func GetOldPVsFromRDE(statusMap map[string]interface{}, rdeId string) ([]string, error) {
	pvInterfaces, ok := statusMap[OldPersistentVolumeKey]

	if !ok {
		return make([]string, 0), fmt.Errorf("key [%s] found in the status section of RDE [%s]", OldPersistentVolumeKey, rdeId)
	}
	if pvInterfaces == nil {
		return make([]string, 0), nil
	}

	pvInterfacesSlice, ok := pvInterfaces.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unable to convert [%T] to []interface{} in RDE [%s]", pvInterfaces, rdeId)
	}
	pvIdStrs := make([]string, len(pvInterfacesSlice))
	for idx, pvInterface := range pvInterfacesSlice {
		currPv, ok := pvInterface.(string)
		if !ok {
			return nil, fmt.Errorf("unable to convert [%T] to string in RDE [%s]", pvInterface, rdeId)
		}
		pvIdStrs[idx] = currPv
	}
	return pvIdStrs, nil
}

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

func addToVCDResourceSet(component string, componentName string, componentVersion string, statusMap map[string]interface{}, vcdResourceSet []vcdsdk.VCDResource) (map[string]interface{}, error) {
	// get the component info from the status
	componentIf, ok := statusMap[component]
	if !ok {
		// component map not found
		statusMap[component] = map[string]interface{}{
			"name":           componentName,
			"version":        componentVersion,
			"vcdResourceSet": vcdResourceSet,
		}
		//klog.Infof("created component map [%#v] since the component was not found in the status map", statusMap[component])
		return statusMap, nil
	}

	componentMap, ok := componentIf.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("failed to convert the status belonging to component [%s] to map[string]interface{}", component)
	}

	componentStatus, err := convertMapToComponentStatus(componentMap)
	if err != nil {
		return nil, fmt.Errorf("failed to convert component status map to ")
	}

	if componentStatus.VCDResourceSet == nil || len(componentStatus.VCDResourceSet) == 0 {
		// create an array with a single element - vcdResource
		componentMap[vcdsdk.ComponentStatusFieldVCDResourceSet] = vcdResourceSet
		return statusMap, nil
	}
	componentStatus.VCDResourceSet = append(componentStatus.VCDResourceSet, vcdResourceSet...)

	componentMap[vcdsdk.ComponentStatusFieldVCDResourceSet] = componentStatus.VCDResourceSet

	return statusMap, nil
}
