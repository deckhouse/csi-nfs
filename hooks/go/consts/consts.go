package consts

const (
	ModuleName      	string = "csiNfs"
	ModuleNamespace 	string = "d8-csi-nfs"
	ModulePluralName 	string = "csi-nfs"
	WebhookCertCn   	string = "webhooks"
)

var AllowedProvisioners = []string{
	"nfs.csi.k8s.io",
}
