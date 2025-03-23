package main

import (
	"github.com/deckhouse/module-sdk/pkg/app"

	_ "github.com/deckhouse/csi-nfs/hooks/go/020-webhook-certs"
	_ "github.com/deckhouse/csi-nfs/hooks/go/030-remove-sc-and-secrets-on-module-delete"
)

func main() {
	app.Run()
}
