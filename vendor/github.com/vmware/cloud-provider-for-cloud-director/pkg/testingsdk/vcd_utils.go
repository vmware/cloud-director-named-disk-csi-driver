package testingsdk

import (
	"context"
	"fmt"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdsdk"
	swagger "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient"
)

func getTestVCDClient(params *VCDAuthParams) (*vcdsdk.Client, error) {
	return vcdsdk.NewVCDClientFromSecrets(
		params.Host,
		params.OrgName,
		params.OvdcName,
		params.UserOrg,
		params.Username,
		"",
		params.RefreshToken,
		true,
		params.GetVdcClient)
}

func getRdeById(ctx context.Context, client *vcdsdk.Client, rdeId string) (*swagger.DefinedEntity, error) {
	clusterOrg, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
	if err != nil {
		return nil, fmt.Errorf("error retrieving org [%s]: [%v]", client.ClusterOrgName, err)
	}
	if clusterOrg != nil || clusterOrg.Org != nil {
		return nil, fmt.Errorf("retrieved org is nil for [%s]", client.ClusterOrgName)
	}
	rde, _, _, err := client.APIClient.DefinedEntityApi.GetDefinedEntity(ctx, rdeId, clusterOrg.Org.ID)
	if err != nil {
		return nil, fmt.Errorf("error retrieving RDE [%s]: [%v]", rdeId, err)
	}
	return &rde, nil
}
