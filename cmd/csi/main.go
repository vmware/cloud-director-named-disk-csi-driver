/*
    Copyright 2021 VMware, Inc.
    SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"flag"
	"fmt"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/config"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/csi"
	"github.com/vmware/cloud-director-named-disk-csi-driver/pkg/vcdclient"
	"github.com/vmware/cloud-director-named-disk-csi-driver/version"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/component-base/logs"
	"k8s.io/klog"
)

var (
	endpointFlag    string
	nodeIDFlag      string
	cloudConfigFlag string
)

func init() {
	flag.Set("logtostderr", "true")
}

func main() {

	if version.Version == "" {
		panic(fmt.Errorf("the Version should be set by flags during compilation"))
	}

	flag.CommandLine.Parse([]string{})

	cmd := &cobra.Command{
		Use:     os.Args[0],
		Short:   "CSI plugin for VCD",
		Version: version.Version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Glog requires this otherwise it complains.
			flag.CommandLine.Parse(nil)

			// This is a temporary hack to enable proper logging until upstream dependencies
			// are migrated to fully utilize klog instead of glog.
			klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
			klog.InitFlags(klogFlags)

			// Sync the glog and klog flags.
			cmd.Flags().VisitAll(func(f1 *pflag.Flag) {
				f2 := klogFlags.Lookup(f1.Name)
				if f2 != nil {
					value := f1.Value.String()
					f2.Value.Set(value)
				}
			})
		},
		Run: func(cmd *cobra.Command, args []string) {
			runCommand()
		},
	}

	cmd.Flags().AddGoFlagSet(flag.CommandLine)

	cmd.PersistentFlags().StringVar(&nodeIDFlag, "nodeid", "", "node id")

	cmd.PersistentFlags().StringVar(&endpointFlag, "endpoint", "", "CSI endpoint")
	cmd.MarkPersistentFlagRequired("endpoint")

	cmd.PersistentFlags().StringVar(&cloudConfigFlag, "cloud-config", "", "CSI driver cloud config")
	cmd.MarkPersistentFlagRequired("cloud-config")

	logs.InitLogs()
	defer logs.FlushLogs()

	if err := cmd.Execute(); err != nil {
		panic(fmt.Errorf("error in executing command: [%v]", err))
	}

	return
}

func runCommand() {

	d, err := csi.NewDriver(nodeIDFlag, endpointFlag)
	if err != nil {
		panic(fmt.Errorf("unable to create new driver: [%v]", err))
	}

	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		panic(fmt.Errorf("ENV NODE_ID is not set"))
	}

	f, err := os.Open(cloudConfigFlag)
	if err != nil {
		panic(fmt.Errorf("unable to read cloud config: [%v]", err))
	}
	defer f.Close()

	cloudConfig, err := config.ParseCloudConfig(f)
	if err != nil {
		panic(fmt.Errorf("unable to parse configuration: [%v]", err))
	}

	for {
		err = config.SetAuthorization(cloudConfig)
		if err == nil {
			break
		}

		waitTime := 10 * time.Second
		klog.Infof("unable to set authorization in config: [%v]", err)
		klog.Infof("Waiting for [%v] before trying again...", waitTime)
		time.Sleep(waitTime)
	}

	vcdClient, err := vcdclient.NewVCDClientFromSecrets(
		cloudConfig.VCD.Host,
		cloudConfig.VCD.Org,
		cloudConfig.VCD.VDC,
		cloudConfig.VCD.VAppName,
		cloudConfig.VCD.UserOrg,
		cloudConfig.VCD.User,
		cloudConfig.VCD.Secret,
		cloudConfig.VCD.RefreshToken,
		true,
		cloudConfig.ClusterID,
		true,
	)
	if err != nil {
		panic (fmt.Errorf("unable to initiate vcd client: [%v]", err))
	}

	d.Setup(vcdClient, nodeID)

	// blocking call
	if err = d.Run(); err != nil {
		panic(fmt.Errorf("error while running driver: [%v]", err))
	}
}
