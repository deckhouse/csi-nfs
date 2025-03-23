package hooks_common

import (
	"fmt"

	consts "github.com/deckhouse/csi-nfs/hooks/go/consts"
	tlscertificate "github.com/deckhouse/module-sdk/common-hooks/tls-certificate"
)

var _ = tlscertificate.RegisterInternalTLSHookEM(tlscertificate.GenSelfSignedTLSHookConf{
	CN:            consts.WebhookCertCn,
	TLSSecretName: fmt.Sprintf("%s-webhook-cert", consts.WebhookCertCn),
	Namespace:     consts.ModuleNamespace,
	SANs: tlscertificate.DefaultSANs([]string{
		consts.WebhookCertCn,
		fmt.Sprintf("%s.%s", consts.WebhookCertCn, consts.ModuleNamespace),
		fmt.Sprintf("%s.%s.svc", consts.WebhookCertCn, consts.ModuleNamespace),
		// %CLUSTER_DOMAIN%:// is a special value to generate SAN like 'svc_name.svc_namespace.svc.cluster.local'
		fmt.Sprintf("%%CLUSTER_DOMAIN%%://%s.%s.svc", consts.WebhookCertCn, consts.ModuleNamespace),
	}),
	FullValuesPathPrefix: fmt.Sprintf("%s.internal.customWebhookCert", consts.ModuleName),
})
