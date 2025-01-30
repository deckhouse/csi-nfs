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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"csi-nfs-scheduler-extender/pkg/logger"
)

func (s *scheduler) prioritize(w http.ResponseWriter, r *http.Request) {
	s.log.Debug("[prioritize] starts serving. WARNING: this scheduler does not support prioritizing! It will return the same nodes with 0 score")

	var inputData ExtenderArgs
	reader := http.MaxBytesReader(w, r.Body, 10<<20)
	err := json.NewDecoder(reader).Decode(&inputData)
	if err != nil {
		s.log.Error(err, "[prioritize] unable to decode a request")
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		return
	}
	s.log.Trace(fmt.Sprintf("[prioritize] input data: %+v", inputData))

	if inputData.Pod == nil {
		s.log.Error(errors.New("no pod in the request"), "[prioritize] unable to get a Pod from the request")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	nodeNames, err := getNodeNames(inputData)
	if err != nil {
		s.log.Error(err, "[prioritize] unable to get node names from the request")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	result := scoreNodes(s.log, &nodeNames)
	s.log.Debug(fmt.Sprintf("[prioritize] successfully scored the nodes for Pod %s/%s", inputData.Pod.Namespace, inputData.Pod.Name))

	w.Header().Set("content-type", "application/json")
	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		s.log.Error(err, fmt.Sprintf("[prioritize] unable to encode a response for a Pod %s/%s", inputData.Pod.Namespace, inputData.Pod.Name))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
	s.log.Debug("[prioritize] ends serving")
}

func scoreNodes(
	log logger.Logger,
	nodeNames *[]string,
) []HostPriority {
	result := make([]HostPriority, 0, len(*nodeNames))
	if len(*nodeNames) == 0 {
		log.Warning("[scoreNodes] no nodes to score. Return empty result")
		return result
	}

	for _, nodeName := range *nodeNames {
		log.Trace(fmt.Sprintf("[scoreNodes] node: %s", nodeName))
		result = append(result, HostPriority{
			Host:  nodeName,
			Score: 0,
		})
	}

	log.Trace("[scoreNodes] final result: %+v", result)
	return result
}
