module github.com/vmware/cloud-director-named-disk-csi-driver

go 1.15

require (
	github.com/akutz/gofsutil v0.1.2
	github.com/container-storage-interface/spec v1.4.0
	github.com/go-openapi/errors v0.20.2 // indirect
	github.com/google/uuid v1.2.0
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/spf13/cobra v0.0.0-20180319062004-c439c4fa0937
	github.com/spf13/pflag v1.0.1
	github.com/stretchr/testify v1.7.0
	github.com/thecodeteam/gofsutil v0.1.2 // indirect
	github.com/vmware/cloud-provider-for-cloud-director v0.0.0-20220508221901-1304924dad33
	github.com/vmware/go-vcloud-director/v2 v2.14.0-rc.3
	golang.org/x/net v0.0.0-20201021035429-f5854403a974 // indirect
	golang.org/x/oauth2 v0.0.0-20210819190943-2bc19b11175f // indirect
	golang.org/x/sys v0.0.0-20210314195730-07df6a141424 // indirect
	google.golang.org/grpc v1.31.0
	google.golang.org/protobuf v1.26.0 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.0-20200605160147-a5ece683394c
	k8s.io/component-base v0.22.1
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.60.1 // indirect
)

replace k8s.io/api => k8s.io/api v0.0.0-20190918195907-bd6ac527cfd2

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190918201827-3de75813f604

replace k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190817020851-f2f3a405f61d

replace k8s.io/apiserver => k8s.io/apiserver v0.0.0-20190918200908-1e17798da8c1

replace k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20190918202139-0b14c719ca62

replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190918200256-06eb1244587a

replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20190918203125-ae665f80358a

replace k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20190918202959-c340507a5d48

replace k8s.io/code-generator => k8s.io/code-generator v0.0.0-20190612205613-18da4a14b22b

replace k8s.io/component-base => k8s.io/component-base v0.0.0-20190918200425-ed2f0867c778

replace k8s.io/cri-api => k8s.io/cri-api v0.0.0-20190817025403-3ae76f584e79

replace k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20190918203248-97c07dcbb623

replace k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20190918201136-c3a845f1fbb2

replace k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.0.0-20190918202837-c54ce30c680e

replace k8s.io/kube-proxy => k8s.io/kube-proxy v0.0.0-20190918202429-08c8357f8e2d

replace k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.0.0-20190918202713-c34a54b3ec8e

replace k8s.io/kubelet => k8s.io/kubelet v0.0.0-20190918202550-958285cf3eef

replace k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.0.0-20190918203421-225f0541b3ea

replace k8s.io/metrics => k8s.io/metrics v0.0.0-20190918202012-3c1ca76f5bda

replace k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.0.0-20190918201353-5cc279503896
