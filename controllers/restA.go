/*
Copyright 2023.

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

package controllers

import (
	"bytes"
	"fmt"
	"io"
	//yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	yaml "gopkg.in/yaml.v3"
	"net/http"
)

type DevState struct {
	//inlineAcclrs	acclrv1bet1.InlineAcclr
	PciAddr        string   `json:"pciAddr,omitempty"`
	NumVfs         string   `json:"numvfs,omitempty"`
	FwImage        string   `json:"fwImage,omitempty"`
	FwTag          string   `json:"fwTag,omitempty"`
	ResourcePrefix string   `json:"resourcePrefix,omitempty"`
	ResourceName   []string `json:"resourceName,omitempty"`

	Status   string `json:"status,omitempty"`
	PfDriver string `json:"pfdriver,omitempty"`
}

func getAcclrState(PodIP, PciAddr string) (DevState, error) {

	devstate := DevState{}

	getUrl := "http://" + PodIP + ":4004/" + PciAddr
	resp, err := http.Get(getUrl)
	if err != nil {
		fmt.Printf("http.Get Failed: %s\n", err)
		return devstate, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	err = yaml.Unmarshal(data, &devstate)
	if err != nil {
		return devstate, err
	}
	fmt.Printf("-----> %d, %+v\n", len(data), devstate)

	return devstate, err
}

var CONTENTTYPE string = "application/json; charset=utf-8"

func postAcclrUnbind(PodIP string, d DevState) (DevState, error) {

	postUrl := "http://" + PodIP + ":4004/UnbindDevice"

	Req, err := yaml.Marshal(d)
	if err != nil {
		fmt.Printf("Failed to Marshal: %s\n", err)
		return DevState{}, err
	}
	resp, err := http.NewRequest("POST", postUrl, bytes.NewBuffer(Req))
	if err != nil {
		fmt.Printf("http.Get Failed: %s\n", err)
		return DevState{}, err
	}
	resp.Header.Add("Content-Type", CONTENTTYPE)
	client := &http.Client{}
	r, err := client.Do(resp)
	if err != nil {
		fmt.Printf("http.NewRequestFailed: %s\n", err)
		return DevState{}, err
	}

	ds := DevState{}
	defer r.Body.Close()
	data, err := io.ReadAll(resp.Body)
	err = yaml.Unmarshal(data, &ds)
	if err != nil {
		return DevState{}, err
	}
	fmt.Printf("-----> %d, %+v\n", len(data), ds)

	return ds, err
}
