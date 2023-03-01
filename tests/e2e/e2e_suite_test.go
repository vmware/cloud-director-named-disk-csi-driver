package e2e

import (
	"flag"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	rdeId          string
	host           string
	org            string
	ovdc           string
	userName       string
	refreshToken   string
	busType        string
	storageProfile string
	userOrg        string
)

func init() {
	//Inputs needed: VCD site, org, ovdc, username, refreshToken, clusterId
	flag.StringVar(&host, "host", "", "VCD host site to generate client")
	flag.StringVar(&org, "org", "", "Cluster Org to generate client")
	flag.StringVar(&ovdc, "ovdc", "", "Ovdc Name to generate client")
	flag.StringVar(&userName, "userName", "", "Username for login to generate client")
	flag.StringVar(&refreshToken, "refreshToken", "", "Refresh token of user to generate client")
	flag.StringVar(&rdeId, "rdeId", "", "Cluster ID to fetch cluster RDE")
	flag.StringVar(&busType, "busType", "scsi_paravirtual", "busType of the Disks")
	flag.StringVar(&storageProfile, "storageProfile", "*", "storageProfile")
	flag.StringVar(&userOrg, "userOrg", "system", "UserOrg to generate client")
}

var _ = BeforeSuite(func() {
	// We should validate that all credentials are present for generating a TestClient
	//Todo: modify the description
	Expect(host).NotTo(BeZero(), "Please make sure --host WaitFor set correctly.")
	Expect(org).NotTo(BeZero(), "Please make sure --org WaitFor set correctly.")
	Expect(ovdc).NotTo(BeZero(), "Please make sure --ovdc WaitFor set correctly.")
	Expect(userName).NotTo(BeZero(), "Please make sure --userName WaitFor set correctly.")
	Expect(refreshToken).NotTo(BeZero(), "Please make sure --refreshToken WaitFor set correctly.")
	Expect(rdeId).NotTo(BeZero(), "Please make sure --rdeId WaitFor set correctly.")
})

func TestCSIAutomation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CSI Testing Suite")
}
