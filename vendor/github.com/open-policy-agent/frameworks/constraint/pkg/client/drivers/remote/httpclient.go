// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package remote

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/pkg/errors"
)

// Error contains the standard error fields returned by OPA.
type Error struct {
	Status  int
	Message string
}

func (err *Error) Error() string {
	return fmt.Sprintf("code %v: %v", err.Status, err.Message)
}

// Undefined represents an undefined response from OPA.
type Undefined struct{}

func (Undefined) Error() string {
	return fmt.Sprintf("undefined")
}

// IsUndefinedErr returns true if the err represents an undefined result from
// OPA.
func IsUndefinedErr(err error) bool {
	_, ok := err.(Undefined)
	return ok
}

// Client defines the OPA client interface.
type client interface {
	Policies
	Data
}

// Policies defines the policy management interface in OPA.
type Policies interface {
	InsertPolicy(id string, bs []byte) error
	DeletePolicy(id string) error
	ListPolicies() (*QueryResult, error)
}

// Data defines the interface for pushing and querying data in OPA.
type Data interface {
	Prefix(path string) Data
	PatchData(path string, op string, value *interface{}) error
	PutData(path string, value interface{}) error
	PostData(path string, value interface{}) (json.RawMessage, error)
	DeleteData(path string) error
	Query(path string, value interface{}) (*QueryResult, error)
}

// New returns a new client object.
func newHttpClient(url string, opaCAs *x509.CertPool, auth string) client {
	return &httpClient{
		strings.TrimRight(url, "/"), "", opaCAs, auth}
}

type httpClient struct {
	url            string
	prefix         string
	opaCAs         *x509.CertPool
	authentication string
}

func (c *httpClient) Prefix(path string) Data {
	cpy := *c
	cpy.prefix = joinPaths("/", c.prefix, path)
	return &cpy
}

func (c *httpClient) PatchData(path string, op string, value *interface{}) error {
	buf, err := c.makePatch(path, op, value)
	if err != nil {
		return errors.Wrap(err, "PatchData")
	}
	resp, err := c.do("PATCH", slashPath("v1", "data"), buf)
	if err != nil {
		return errors.Wrap(err, "PatchData")
	}
	return c.handleErrors(resp)
}

func (c *httpClient) PutData(path string, value interface{}) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(value); err != nil {
		return errors.Wrap(err, "PutData")
	}
	absPath := slashPath("v1", "data", c.prefix, path)
	resp, err := c.do("PUT", absPath, &buf)
	if err != nil {
		return errors.Wrap(err, "PutData")
	}
	return c.handleErrors(resp)
}

func (c *httpClient) PostData(path string, value interface{}) (json.RawMessage, error) {
	var buf bytes.Buffer
	var input struct {
		Input interface{} `json:"input"`
	}
	input.Input = value
	if err := json.NewEncoder(&buf).Encode(input); err != nil {
		return nil, err
	}
	absPath := slashPath("v1", "data", c.prefix, path)
	resp, err := c.do("POST", absPath, &buf)
	if err != nil {
		return nil, err
	}
	var result struct {
		Result json.RawMessage        `json:"result"`
		Error  map[string]interface{} `json:"error"`
	}
	if resp.StatusCode != 200 {
		return nil, c.handleErrors(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Result == nil {
		return nil, Undefined{}
	}
	return result.Result, nil
}

func (c *httpClient) DeleteData(path string) error {
	absPath := slashPath("v1", "data", c.prefix, path)
	resp, err := c.do("DELETE", absPath, nil)
	if err != nil {
		return errors.Wrap(err, "DeleteData")
	}
	return c.handleErrors(resp)
}

type QueryResult struct {
	Explanation json.RawMessage        `json:"explanation,omitempty"`
	Result      json.RawMessage        `json:"result"`
	Error       map[string]interface{} `json:"error"`
}

func (c *httpClient) Query(path string, input interface{}) (*QueryResult, error) {
	var buf bytes.Buffer
	var body struct {
		Input interface{} `json:"input,omitempty"`
	}
	body.Input = input
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, errors.Wrap(err, "Query:body")
	}
	absPath := slashPath("v1", "data", c.prefix, path)
	method := "GET"
	if input != nil {
		method = "POST"
	}
	resp, err := c.do(method, absPath, &buf)
	if err != nil {
		return nil, errors.Wrap(err, "Query:do")
	}
	result := &QueryResult{}
	if resp.StatusCode != 200 {
		return nil, errors.Wrap(c.handleErrors(resp), "Query:handleErrors")
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, errors.Wrapf(err, "Query")
	}
	return result, nil
}

func (c *httpClient) InsertPolicy(id string, bs []byte) error {
	buf := bytes.NewBuffer(bs)
	path := slashPath("v1", "policies", id)
	resp, err := c.do("PUT", path, buf)
	if err != nil {
		return errors.Wrap(err, "InsertPolicy")
	}
	return c.handleErrors(resp)
}

func (c *httpClient) DeletePolicy(id string) error {
	path := slashPath("v1", "policies", id)
	resp, err := c.do("DELETE", path, nil)
	if err != nil {
		return errors.Wrap(err, "DeletePolicy")
	}
	return c.handleErrors(resp)
}

func (c *httpClient) ListPolicies() (*QueryResult, error) {
	absPath := slashPath("v1", "policies", c.prefix)
	resp, err := c.do("GET", absPath, nil)
	if err != nil {
		return nil, errors.Wrap(err, "ListPolicies:do")
	}
	result := &QueryResult{}
	if resp.StatusCode != 200 {
		return nil, errors.Wrap(c.handleErrors(resp), "ListPolicies:handleErrors")
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, errors.Wrapf(err, "ListPolicies")
	}
	return result, nil
}

func (c *httpClient) makePatch(path, op string, value *interface{}) (io.Reader, error) {
	patch := []struct {
		Path  string       `json:"path"`
		Op    string       `json:"op"`
		Value *interface{} `json:"value,omitempty"`
	}{
		{
			Path:  slashPath(c.prefix, path),
			Op:    op,
			Value: value,
		},
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(patch); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (c *httpClient) handleErrors(resp *http.Response) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	msg, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrapf(err, "handleErrors")
	}
	return &Error{Message: string(msg), Status: resp.StatusCode}
}

func (c *httpClient) do(verb, path string, body io.Reader) (*http.Response, error) {
	url := c.url + path
	req, err := http.NewRequest(verb, url, body)
	if err != nil {
		return nil, err
	}

	if c.authentication != "" {
		req.Header.Set("Authorization", "Bearer "+c.authentication)
	}

	client := &http.Client{}
	if strings.HasPrefix(c.url, "https") && c.opaCAs != nil {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: c.opaCAs,
			},
		}
	}

	return client.Do(req)
}

func slashPath(paths ...string) string {
	return makePath("/", paths...)
}

func makePath(join string, paths ...string) string {
	return join + joinPaths(join, paths...)
}

func joinPaths(join string, paths ...string) string {
	parts := []string{}
	for _, path := range paths {
		path = strings.Trim(path, join)
		if path != "" {
			parts = append(parts, path)
		}
	}
	return strings.Join(parts, join)
}
