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

package validating

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	feature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
	d8commonapi "github.com/deckhouse/sds-common-lib/api/v1alpha1"
)

func ValidateNFSStorageClass(nfsModuleConfig *d8commonapi.ModuleConfig, nsc *cn.NFSStorageClass) error {
	var logPostfix = "Such a combination of parameters is not allowed"

	if nsc.Spec.Connection.NFSVersion == "3" {
		if value, ok := nfsModuleConfig.Spec.Settings["v3support"]; !ok {
			return fmt.Errorf(
				"ModuleConfig: %s (the v3support parameter is missing); NFSStorageClass: %s (nfsVersion is set to 3); %s",
				nfsModuleConfig.Name, nsc.Name, logPostfix,
			)
		} else if value == false {
			return fmt.Errorf(
				"ModuleConfig: %s (the v3support parameter is disabled); NFSStorageClass: %s (nfsVersion is set to 3); %s",
				nfsModuleConfig.Name, nsc.Name, logPostfix,
			)
		}
	}

	if feature.TLSEnabled() {
		if nsc.Spec.Connection.Tls || nsc.Spec.Connection.Mtls {
			tlsParameters, _ := nfsModuleConfig.Spec.Settings["tlsParameters"].(map[string]any)
			moduleCA, _ := tlsParameters["ca"].(string)

			var classCA string
			if nsc.Spec.TLSParameters != nil {
				classCA = nsc.Spec.TLSParameters.CA
				if err := ValidateCA(classCA); err != nil {
					return fmt.Errorf("NFSStorageClass: %s (tlsParameters.ca: %v); %s", nsc.Name, err, logPostfix)
				}
			}

			if moduleCA == "" && classCA == "" {
				return fmt.Errorf(
					"ModuleConfig: %s (the tlsParameters.ca parameter is either missing or has a zero length); NFSStorageClass: %s (tls or mtls is enabled, and the class has no tlsParameters.ca of its own); %s",
					nfsModuleConfig.Name, nsc.Name, logPostfix,
				)
			}

			if nsc.Spec.Connection.Mtls {
				// The nodes present the client certificate the module issues unless
				// tlsParameters.mtls replaces it, which takes the certificate and the
				// key together.
				mtls, _ := tlsParameters["mtls"].(map[string]any)
				cert, _ := mtls["clientCert"].(string)
				key, _ := mtls["clientKey"].(string)
				if (cert == "") != (key == "") {
					return fmt.Errorf(
						"ModuleConfig: %s (tlsParameters.mtls sets only one of clientCert and clientKey); NFSStorageClass: %s (mtls is enabled); %s",
						nfsModuleConfig.Name, nsc.Name, logPostfix,
					)
				}
			}
		}
	} else {
		_, ok := nfsModuleConfig.Spec.Settings["tlsParameters"]
		if nsc.Spec.Connection.Tls || nsc.Spec.Connection.Mtls || nsc.Spec.TLSParameters != nil || ok {
			return fmt.Errorf("RPC-with-TLS related parameters are not allowed because the TLS feature is not available in your edition")
		}
	}

	return nil
}

// ValidateCA checks that a Base64-encoded value holds PEM certificates and
// nothing else, as tlshd reads the truststore.
func ValidateCA(encoded string) error {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("not Base64: %w", err)
	}
	found := false
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			if len(bytes.TrimSpace(rest)) > 0 {
				return errors.New("holds data that is not a PEM block")
			}
			break
		}
		if block.Type != "CERTIFICATE" {
			return fmt.Errorf("holds a PEM block of type %q, not a certificate", block.Type)
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("holds a certificate that does not parse: %w", err)
		}
		found = true
	}
	if !found {
		return errors.New("holds no PEM certificate")
	}
	return nil
}
