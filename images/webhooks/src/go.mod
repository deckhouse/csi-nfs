module webhooks

go 1.23.4

require (
	github.com/deckhouse/csi-nfs/api v0.0.0-20250116103144-d23aedd591a3
	github.com/deckhouse/csi-nfs/lib/go/common v0.0.0-20250116103144-d23aedd591a3
	github.com/sirupsen/logrus v1.9.3
	github.com/slok/kubewebhook/v2 v2.7.0
	k8s.io/api v0.32.0
	k8s.io/apimachinery v0.32.0
	k8s.io/klog/v2 v2.130.1
)

// Do not combine multiple replacements into a single block,
// as this will break the CI workflow "Check Go module version."
replace github.com/deckhouse/csi-nfs/api => ../../../api

replace github.com/deckhouse/csi-nfs/lib/go/common => ../../../lib/go/common

require (
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/client-go v0.32.0 // indirect
	k8s.io/utils v0.0.0-20241210054802-24370beab758 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.5.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
