package main

import (
	_ "github.com/deckhouse/csi-nfs/hooks/go/020-webhook-certs"
	_ "github.com/deckhouse/csi-nfs/hooks/go/030-remove-sc-and-secrets-on-module-delete"
	"github.com/deckhouse/module-sdk/pkg/app"
)

func main() {
	app.Run()
}
