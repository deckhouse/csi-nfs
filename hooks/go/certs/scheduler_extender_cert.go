package certs

import (
	"fmt"

	kcertificates "k8s.io/api/certificates/v1"

	. "github.com/deckhouse/csi-nfs/hooks/go/consts"
	tlscertificate "github.com/deckhouse/csi-nfs/hooks/go/tls-certificate"
	chcrt "github.com/deckhouse/module-sdk/common-hooks/tls-certificate"
)

func RegisterSchedulerExtenderCertHook() {
	tlscertificate.RegisterManualTLSHookEM(SchedulerExtenderCertConfig)
}

var SchedulerExtenderCertConfig = tlscertificate.MustNewGenSelfSignedTLSGroupHookConf(
	tlscertificate.GenSelfSignedTLSHookConf{
		CN:            "csi-nfs-scheduler-extender",
		Namespace:     ModuleNamespace,
		TLSSecretName: "csi-nfs-scheduler-extender-https-certs",
		SANs: chcrt.DefaultSANs([]string{
			"csi-nfs--scheduler-extender",
			fmt.Sprintf("csi-nfs-scheduler-extender.%s", ModuleNamespace),
			fmt.Sprintf("csi-nfs--scheduler-extender.%s.svc", ModuleNamespace),
			fmt.Sprintf("%%CLUSTER_DOMAIN%%://csi-nfs--scheduler-extender.%s.svc", ModuleNamespace),
		}),
		FullValuesPathPrefix: fmt.Sprintf("%s.internal.customSchedulerExtenderCert", ModuleName),
		Usages: []kcertificates.KeyUsage{
			kcertificates.UsageKeyEncipherment,
			kcertificates.UsageCertSign,
			// ExtKeyUsage
			kcertificates.UsageServerAuth,
		},
		CAExpiryDuration:     tlscertificate.DefaultCAExpiryDuration,
		CertExpiryDuration:   tlscertificate.DefaultCertExpiryDuration,
		CertOutdatedDuration: tlscertificate.DefaultCertOutdatedDuration,
	},
)
