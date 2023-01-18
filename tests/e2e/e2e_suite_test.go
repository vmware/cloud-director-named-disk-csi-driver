package e2e

import (
	"flag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"testing"
)

var (
	rdeId        string
	host         string
	org          string
	ovdc         string
	userName     string
	refreshToken string
)

func init() {
	//Inputs needed: VCD site, org, ovdc, username, refreshToken, clusterId
	flag.StringVar(&host, "host", "", "VCD host site to generate client")
	flag.StringVar(&org, "org", "", "Cluster Org to generate client")
	flag.StringVar(&ovdc, "ovdc", "", "Ovdc Name to generate client")
	flag.StringVar(&userName, "userName", "", "Username for login to generate client")
	flag.StringVar(&refreshToken, "refreshToken", "", "Refresh token of user to generate client")
	flag.StringVar(&rdeId, "rdeId", "", "Cluster ID to fetch cluster RDE")
}

var _ = BeforeSuite(func() {
	// We should validate that all credentials are present for generating a TestClient
	//Todo: modify the description
	Expect(host).NotTo(BeZero(), "Please make sure --host is set correctly.")
	Expect(org).NotTo(BeZero(), "Please make sure --org is set correctly.")
	Expect(ovdc).NotTo(BeZero(), "Please make sure --vdcName is set correctly.")
	Expect(userName).NotTo(BeZero(), "Please make sure --username is set correctly.")
	Expect(refreshToken).NotTo(BeZero(), "Please make sure --token is set correctly.")
	Expect(rdeId).NotTo(BeZero(), "Please make sure --clusterId is set correctly.")
})

func TestCSIAutomation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CSI Testing Suite")
}
