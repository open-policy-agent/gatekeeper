// Copyright 2018 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package opa

import (
	"encoding/json"
	"io"
	"reflect"
	"testing"
)

func TestHTTPClientMakePatch(t *testing.T) {

	tests := []struct {
		prefix string
		path   string
		op     string
		value  string
		want   string
	}{
		{
			prefix: "",
			path:   "foo",
			op:     "add",
			value:  "true",
			want: `[{
				"path": "/foo",
				"op": "add",
				"value": true
			}]`,
		},
		{
			prefix: "",
			path:   "default/foo",
			op:     "remove",
			value:  "",
			want: `[{
				"path": "/default/foo",
				"op": "remove"
			}]`,
		},
		{
			prefix: "type",
			path:   "default/foo",
			op:     "remove",
			value:  "",
			want: `[{
				"path": "/type/default/foo",
				"op": "remove"
			}]`,
		},
		{
			prefix: "/type1/subtypeA/",
			path:   "default/foo",
			op:     "remove",
			value:  "",
			want: `[{
				"path": "/type1/subtypeA/default/foo",
				"op": "remove"
			}]`,
		},
	}

	for _, tc := range tests {

		client := &httpClient{url: "URL", prefix: tc.prefix}
		var value *interface{}

		if tc.value != "" {
			var x interface{}
			if err := json.Unmarshal([]byte(tc.value), &x); err != nil {
				panic(err)
			}
			value = &x
		}

		patch := mustMakePatch(client, tc.path, tc.op, value)

		var expected interface{}
		if err := json.Unmarshal([]byte(tc.want), &expected); err != nil {
			panic(err)
		}

		if !reflect.DeepEqual(patch, expected) {
			t.Errorf("Expected %v but got: %v", expected, patch)
		}
	}

}

func mustMakePatch(client *httpClient, path, op string, value *interface{}) interface{} {

	buf, err := client.makePatch(path, op, value)
	if err != nil {
		panic(err)
	}

	return mustUnmarshalJSON(buf)
}

func mustUnmarshalJSON(r io.Reader) interface{} {
	var x interface{}
	err := json.NewDecoder(r).Decode(&x)
	if err != nil {
		panic(err)
	}
	return x
}
