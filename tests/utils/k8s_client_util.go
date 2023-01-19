package utils

import (
	"context"
	"fmt"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	"k8s.io/apimachinery/pkg/util/wait"
)

func WaitForPVDeleted(ctx context.Context, pvName string, tc *testingsdk.TestClient) error {
	err := wait.PollImmediate(defaultRetryInterval, defaultWaitTimeout, func() (bool, error) {
		pv, pvFoundErr := tc.GetPV(ctx, pvName)
		if pvFoundErr != nil {
			if pvFoundErr == testingsdk.ResourceNotFound && pv == nil {
				return true, nil
			}
			return false, fmt.Errorf("error occurred while getting PV [%s] from Kubernetes: %v", pvName, pvFoundErr)
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return nil
}
