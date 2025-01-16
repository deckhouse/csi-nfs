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

package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"csi-nfs-scheduler-extender/pkg/consts"
	"csi-nfs-scheduler-extender/pkg/logger"
)

func (s *scheduler) filter(w http.ResponseWriter, r *http.Request) {
	s.log.Debug("[filter] starts the serving")
	var inputData ExtenderArgs
	reader := http.MaxBytesReader(w, r.Body, 10<<20)
	err := json.NewDecoder(reader).Decode(&inputData)
	if err != nil {
		s.log.Error(err, "[filter] unable to decode a request")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.log.Trace(fmt.Sprintf("[filter] input data: %+v", inputData))

	if inputData.Pod == nil {
		s.log.Error(errors.New("no pod in the request"), "[filter] unable to get a Pod from the request")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	nodeNames, err := getNodeNames(inputData)
	if err != nil {
		s.log.Error(err, "[filter] unable to get node names from the request")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.log.Debug(fmt.Sprintf("[filter] starts the filtering for Pod %s/%s", inputData.Pod.Namespace, inputData.Pod.Name))
	s.log.Trace(fmt.Sprintf("[filter] Pod from the request: %+v", inputData.Pod))
	s.log.Trace(fmt.Sprintf("[filter] Node names from the request: %+v", nodeNames))

	s.log.Debug(fmt.Sprintf("[filter] Find out if the Pod %s/%s should be processed", inputData.Pod.Namespace, inputData.Pod.Name))
	shouldProcess, err := shouldProcessPod(s.ctx, s.client, s.log, inputData.Pod, consts.CSINFSProvisioner)
	if err != nil {
		s.log.Error(err, "[filter] unable to check if the Pod should be processed")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !shouldProcess {
		s.log.Debug(fmt.Sprintf("[filter] Pod %s/%s should not be processed. Return the same nodes", inputData.Pod.Namespace, inputData.Pod.Name))
		filteredNodes := &ExtenderFilterResult{
			NodeNames: &nodeNames,
		}
		s.log.Trace(fmt.Sprintf("[filter] filtered nodes: %+v", filteredNodes))

		w.Header().Set("content-type", "application/json")
		err = json.NewEncoder(w).Encode(filteredNodes)
		if err != nil {
			s.log.Error(err, "[filter] unable to encode a response")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		return
	}
	s.log.Debug(fmt.Sprintf("[filter] Pod %s/%s should be processed", inputData.Pod.Namespace, inputData.Pod.Name))

	s.log.Debug(fmt.Sprintf("[filter] starts to filter the nodes from the request for a Pod %s/%s", inputData.Pod.Namespace, inputData.Pod.Name))
	filteredNodes, err := filterNodes(s.ctx, s.client, s.log, &nodeNames)
	if err != nil {
		s.log.Error(err, "[filter] unable to filter the nodes")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.log.Debug(fmt.Sprintf("[filter] successfully filtered the nodes from the request for a Pod %s/%s", inputData.Pod.Namespace, inputData.Pod.Name))

	w.Header().Set("content-type", "application/json")
	err = json.NewEncoder(w).Encode(filteredNodes)
	if err != nil {
		s.log.Error(err, "[filter] unable to encode a response")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.log.Debug(fmt.Sprintf("[filter] ends the serving the request for a Pod %s/%s", inputData.Pod.Namespace, inputData.Pod.Name))
}

func filterNodes(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	nodeNames *[]string,
) (*ExtenderFilterResult, error) {
	log.Debug("[filterNodes] Get user selectors")

	namespace := os.Getenv("NAMESPACE")
	configSecret := &corev1.Secret{}
	err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: consts.ConfigSecretName}, configSecret)
	if err != nil {
		log.Error(err, fmt.Sprintf("[filterNodes] Failed to get config secret %s/%s", namespace, consts.ConfigSecretName))
		return nil, err
	}

	configNodeSelector, err := GetNodeSelectorFromConfig(*configSecret)
	if err != nil {
		log.Error(err, fmt.Sprintf("[filterNodes] Failed get node selector from secret %s/%s", namespace, consts.ConfigSecretName))
		return nil, err
	}
	log.Debug(fmt.Sprintf("[filterNodes] User selectors: %+v", configNodeSelector))

	selectedKubernetesNodeNames, err := GetKubernetesNodeNamesBySelector(ctx, cl, configNodeSelector)
	if err != nil {
		log.Error(err, fmt.Sprintf("[filterNodes] Failed get nodes from Kubernetes by selector: %+v", configNodeSelector))
		return nil, err
	}
	log.Debug(fmt.Sprintf("[filterNodes] Kubernetes nodes selected by user selectors: %+v", selectedKubernetesNodeNames))

	result := &ExtenderFilterResult{
		NodeNames:   &[]string{},
		FailedNodes: FailedNodesMap{},
	}

	for _, nodeName := range *nodeNames {
		if slices.Contains(selectedKubernetesNodeNames, nodeName) {
			*result.NodeNames = append(*result.NodeNames, nodeName)
		} else {
			result.FailedNodes[nodeName] = "node is not selected by user selectors"
		}
	}

	log.Trace(fmt.Sprintf("[filterNodes] suitable nodes: %+v", *result.NodeNames))
	log.Trace(fmt.Sprintf("[filterNodes] failed nodes: %+v", result.FailedNodes))

	return result, nil
}
