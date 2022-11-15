package reader

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReadK8sResources(t *testing.T) {
	type args struct {
		r io.Reader
	}
	tests := []struct {
		name    string
		args    args
		want    []*unstructured.Unstructured
		wantErr bool
	}{
		{
			name: "test with valid input objects",
			args: args{r: strings.NewReader("---\n# Source: some/templates/file.yaml\nname: object1\nattrs:\n  - attr1\n  - attr2\n---\n# Source: some/templates/file.yaml\nname: object2\nlabels:\n  - label1\n  - label2\n")},
			want: []*unstructured.Unstructured{
				{Object: map[string]interface{}{
					"name": "object1",
					"attrs": []interface{}{
						"attr1",
						"attr2",
					},
				}},
				{Object: map[string]interface{}{
					"name": "object2",
					"labels": []interface{}{
						"label1",
						"label2",
					},
				}},
			},
			wantErr: false,
		},
		{
			name: "test with valid and also empty input objects",
			args: args{r: strings.NewReader("---\n# Source: some/templates/file.yaml\nname: object1\nattrs:\n  - attr1\n  - attr2\n---\n# Source: some/templates/file.yaml\n# only containing some comment\n---\n# Source: some/templates/file.yaml\nname: object2\nlabels:\n  - label1\n  - label2\n")},
			want: []*unstructured.Unstructured{
				{Object: map[string]interface{}{
					"name": "object1",
					"attrs": []interface{}{
						"attr1",
						"attr2",
					},
				}},
				{Object: map[string]interface{}{
					"name": "object2",
					"labels": []interface{}{
						"label1",
						"label2",
					},
				}},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadK8sResources(tt.args.r)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadK8sResources() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReadK8sResources() got = %v, want %v", got, tt.want)
			}
		})
	}
}
