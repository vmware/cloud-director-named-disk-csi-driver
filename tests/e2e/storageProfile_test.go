package e2e

import (
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vmware/cloud-director-named-disk-csi-driver/tests/utils"
	"github.com/vmware/cloud-provider-for-cloud-director/pkg/testingsdk"
	"github.com/vmware/go-vcloud-director/v2/govcd"
	"github.com/vmware/go-vcloud-director/v2/types/v56"
)

const (
	staticDisk              = "static-test"
	diskType                = "MB"
	smallDiskSizeMB         = 2048
	largeDiskSizeMB         = 4096
	storageProfileWithLimit = "Development2"
)

var _ = Describe("CSI Storage Profile Test", func() {
	var (
		tc       *testingsdk.TestClient
		err      error
		spRecord *types.QueryResultProviderVdcStorageProfileRecordType
		adminVdc *govcd.AdminVdc
	)

	tc, err = testingsdk.NewTestClient(&testingsdk.VCDAuthParams{
		Host:         host,
		OvdcName:     ovdc,
		OrgName:      org,
		Username:     userName,
		RefreshToken: refreshToken,
		UserOrg:      userOrg,
		GetVdcClient: true,
	}, rdeId)
	Expect(err).NotTo(HaveOccurred())
	Expect(tc).NotTo(BeNil())
	Expect(&tc.Cs).NotTo(BeNil())

	BeforeEach(func() {
		if !tc.VcdClient.VCDAuthConfig.IsSysAdmin {
			Skip(fmt.Sprintf("Skipping StorageProfile tests as StorageProfile tests are expected to be ran by sysadmin and [%s:%s] is not a sysadmin user", userName, userOrg))
		}
	})

	It("should find the storage profile", func() {
		By("ensuring that admin org exists")
		adminOrg, err := tc.VcdClient.VCDClient.GetAdminOrgByName(org)
		Expect(err).NotTo(HaveOccurred())
		Expect(adminOrg).NotTo(BeNil())

		By("ensuring that admin vdc exists")
		adminVdc, err = adminOrg.GetAdminVDCByName(ovdc, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(adminVdc).NotTo(BeNil())

		By("ensuring that storage profile list exists")
		rawSpList, err := tc.VcdClient.VCDClient.Client.QueryAllProviderVdcStorageProfiles()
		Expect(err).NotTo(HaveOccurred())
		Expect(rawSpList).NotTo(BeNil())

		By("finding the storage profile according to the name")
		for _, sp := range rawSpList {
			if sp.Name == storageProfileWithLimit {
				spRecord = sp
			}
		}
		Expect(spRecord).NotTo(BeNil())
	})

	It("Should add a storage profile with a small limit to ovdc", func() {
		By("adding a storage profile with a small limit")
		err = adminVdc.AddStorageProfileWait(&types.VdcStorageProfileConfiguration{
			Enabled: true,
			Units:   diskType,
			Limit:   smallDiskSizeMB,
			Default: false,
			ProviderVdcStorageProfile: &types.Reference{
				HREF: spRecord.HREF,
				Name: spRecord.Name,
			},
		}, "storage profile with limit")
		Expect(err).NotTo(HaveOccurred())
	})

	It("should failed to create a PV with a size higher than quota", func() {
		By("creating a disk with a size higher than quota")
		err = utils.CreateDisk(tc.VcdClient, staticDisk, largeDiskSizeMB, storageProfileWithLimit)
		Expect(err).To(HaveOccurred())

		By("verifying the error as being a storage limit error")
		isQuotaError := utils.ValidateDiskQuotaError(err)
		Expect(isQuotaError).To(BeTrue())

		By("removing the storage profile from the ovdc")
		err = adminVdc.RemoveStorageProfileWait(storageProfileWithLimit)
		Expect(err).NotTo(HaveOccurred())
	})

})
