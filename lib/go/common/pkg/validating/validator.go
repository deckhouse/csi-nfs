/*
Copyright 2024 Flant JSC

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
	"fmt"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	feature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
)

func ValidateNFSStorageClass(nfsModuleConfig *cn.ModuleConfig, nsc *cn.NFSStorageClass) error {
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

	if feature.TLSEnabled {
		if nsc.Spec.Connection.Tls || nsc.Spec.Connection.Mtls {
			var tlsParameters map[string]any

			value, ok := nfsModuleConfig.Spec.Settings["tlsParameters"]
			if !ok {
				return fmt.Errorf(
					"ModuleConfig: %s (the tlsParameters parameter is missing); NFSStorageClass: %s (tls or mtls is enabled); %s",
					nfsModuleConfig.Name, nsc.Name, logPostfix,
				)
			}
			tlsParameters = value.(map[string]any)

			if value, ok := tlsParameters["ca"]; !ok || len(value.(string)) == 0 {
				return fmt.Errorf(
					"ModuleConfig: %s (the tlsParameters.ca parameter is either missing or has a zero length); NFSStorageClass: %s (tls or mtls is enabled); %s",
					nfsModuleConfig.Name, nsc.Name, logPostfix,
				)
			}

			if nsc.Spec.Connection.Mtls {
				var mtls map[string]any

				value, ok := tlsParameters["mtls"]
				if !ok {
					return fmt.Errorf(
						"ModuleConfig: %s (the tlsParameters.mtls parameter is missing); NFSStorageClass: %s (mtls is enabled); %s",
						nfsModuleConfig.Name, nsc.Name, logPostfix,
					)
				}
				mtls = value.(map[string]any)

				if value, ok := mtls["clientCert"]; !ok || len(value.(string)) == 0 {
					return fmt.Errorf(
						"ModuleConfig: %s (the tlsParameters.mtls.clientCert parameter is either missing or has a zero length); NFSStorageClass: %s (mtls is enabled); %s",
						nfsModuleConfig.Name, nsc.Name, logPostfix,
					)
				}
				if value, ok := mtls["clientKey"]; !ok || len(value.(string)) == 0 {
					return fmt.Errorf(
						"ModuleConfig: %s (the tlsParameters.mtls.clientKey parameter is either missing or has a zero length); NFSStorageClass: %s (mtls is enabled); %s",
						nfsModuleConfig.Name, nsc.Name, logPostfix,
					)
				}
			}
		}
	} else {
		_, ok := nfsModuleConfig.Spec.Settings["tlsParameters"]
		if nsc.Spec.Connection.Tls || nsc.Spec.Connection.Mtls || ok {
			return fmt.Errorf("RPC-with-TLS related parameters are not allowed because feature TLSEnabled: false")
		}
	}

	return nil
}
