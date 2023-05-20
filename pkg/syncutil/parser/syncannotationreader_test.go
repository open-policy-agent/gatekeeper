package parser

import (
	"reflect"
	"testing"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReadSyncRequirements(t *testing.T) {
	tests := []struct {
		name     string
		template *templates.ConstraintTemplate
		want     SyncRequirements
		wantErr  bool
	}{
		{
			name: "test with basic valid annotation",
			template: &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"metadata.gatekeeper.sh/requires-sync-data": "\n\"[[{\"groups\": [\"group1\"], \"versions\": [\"version1\"], \"kinds\": [\"kind1\"]}]]\"",
					},
				},
			},
			want: SyncRequirements{
				{
					{
						Group:   "group1",
						Version: "version1",
						Kind:    "kind1",
					}: struct{}{},
				},
			},
		},
		{
			name: "test with valid annotation with multiple groups, versions, and kinds",
			template: &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"metadata.gatekeeper.sh/requires-sync-data": "\n\"[[{\"groups\": [\"group1\", \"group2\"], \"versions\": [\"version1\", \"version2\"], \"kinds\": [\"kind1\", \"kind2\"]}]]\"",
					},
				},
			},
			want: SyncRequirements{
				{
					{
						Group:   "group1",
						Version: "version1",
						Kind:    "kind1",
					}: struct{}{},
					{
						Group:   "group1",
						Version: "version1",
						Kind:    "kind2",
					}: struct{}{},
					{
						Group:   "group1",
						Version: "version2",
						Kind:    "kind1",
					}: struct{}{},
					{
						Group:   "group1",
						Version: "version2",
						Kind:    "kind2",
					}: struct{}{},
					{
						Group:   "group2",
						Version: "version1",
						Kind:    "kind1",
					}: struct{}{},
					{
						Group:   "group2",
						Version: "version1",
						Kind:    "kind2",
					}: struct{}{},
					{
						Group:   "group2",
						Version: "version2",
						Kind:    "kind1",
					}: struct{}{},
					{
						Group:   "group2",
						Version: "version2",
						Kind:    "kind2",
					}: struct{}{},
				},
			},
		},
		{
			name: "test with valid annotation with multiple equivalence sets",
			template: &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"metadata.gatekeeper.sh/requires-sync-data": "\n\"[[{\"groups\": [\"group1\"], \"versions\": [\"version1\"], \"kinds\": [\"kind1\"]}, {\"groups\": [\"group2\"], \"versions\": [\"version2\"], \"kinds\": [\"kind2\"]}]]\"",
					},
				},
			},
			want: SyncRequirements{
				{
					{
						Group:   "group1",
						Version: "version1",
						Kind:    "kind1",
					}: struct{}{},
					{
						Group:   "group2",
						Version: "version2",
						Kind:    "kind2",
					}: struct{}{},
				},
			},
		},
		{
			name: "test with valid annotation with multiple requirements",
			template: &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"metadata.gatekeeper.sh/requires-sync-data": "\n\"[[{\"groups\": [\"group1\"], \"versions\": [\"version1\"], \"kinds\": [\"kind1\"]}], [{\"groups\": [\"group2\"], \"versions\": [\"version2\"], \"kinds\": [\"kind2\"]}]]\"",
					},
				},
			},
			want: SyncRequirements{
				{
					{
						Group:   "group1",
						Version: "version1",
						Kind:    "kind1",
					}: struct{}{},
				},
				{
					{
						Group:   "group2",
						Version: "version2",
						Kind:    "kind2",
					}: struct{}{},
				},
			},
		},
		{
			name: "test with no requires-sync-data annotation",
			template: &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			want: SyncRequirements{},
		},
		{
			name: "test with empty requires-sync-data annotation",
			template: &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"metadata.gatekeeper.sh/requires-sync-data": "",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "test with invalid requires-sync-data annotation",
			template: &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"metadata.gatekeeper.sh/requires-sync-data": "invalid",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "test with requires-sync-data annotation with invalid keys",
			template: &templates.ConstraintTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"metadata.gatekeeper.sh/requires-sync-data": "\n\"[[{\"group\": [\"group1\"], \"version\": [\"version1\"], \"kind\": [\"kind1\"]}]]\"",
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadSyncRequirements(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadSyncRequirements() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReadSyncRequirements() got = %v, want %v", got, tt.want)
			}
		})
	}
}
