/*
Copyright 2025 Flant JSC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/deckhouse/csi-nfs/images/webhooks/handlers"
	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
	"github.com/deckhouse/sds-common-lib/logger"
)

type config struct {
	certFile string
	keyFile  string
}

func httpHandlerHealthz(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, "Ok.")
}

func initFlags() config {
	cfg := config{}

	fl := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fl.StringVar(&cfg.certFile, "tls-cert-file", "", "TLS certificate file")
	fl.StringVar(&cfg.keyFile, "tls-key-file", "", "TLS key file")

	_ = fl.Parse(os.Args[1:])
	return cfg
}

const port = ":8443"

func main() {
	// LOG_LEVEL arrives as a number 0..4 from lib-helm, which is what the
	// controller container in this same pod already reads. This webhook used to
	// ignore it, so its level was whatever main hard-coded no matter what the
	// module setting said.
	log, err := logger.NewLoggerFromEnv(logger.InfoLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create the logger: %s\n", err)
		os.Exit(1)
	}
	ctrllog.SetLogger(log.GetLogger())

	cfg := initFlags()

	hs, err := handlers.Handlers(log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating the webhook handlers: %s", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	for path, h := range hs {
		mux.Handle(path, h)
	}
	mux.HandleFunc("/healthz", httpHandlerHealthz)

	log.Info("feature flags", "tlsEnabled", commonfeature.TLSEnabled())

	log.Info("listening", "address", port)
	err = http.ListenAndServeTLS(port, cfg.certFile, cfg.keyFile, mux)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error serving webhook: %s", err)
		os.Exit(1)
	}
}
