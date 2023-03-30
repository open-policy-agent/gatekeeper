package expansion

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/gatekeeper/apis/expansion/unversioned"
	"github.com/open-policy-agent/gatekeeper/pkg/expansion/fixtures"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type gvkEdge struct {
	x schema.GroupVersionKind
	y schema.GroupVersionKind
}

func buildGraph(gvks []gvkEdge) gvkGraph {
	m := make(gvkGraph)
	for _, gvk := range gvks {
		m.addEdge(gvk.x, gvk.y)
	}
	return m
}

func TestAddRemoveEdge(t *testing.T) {
	tests := []struct {
		name   string
		start  gvkGraph
		add    []gvkEdge
		remove []gvkEdge
		want   gvkGraph
	}{
		{
			name:  "addEdge 1 edge to empty graph",
			start: make(gvkGraph),
			add: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 1},
			},
		},
		{
			name:  "addEdge 2 edges to empty graph",
			start: make(gvkGraph),
			add: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "daps", Version: "v2", Kind: "Pedloyment"},
					y: schema.GroupVersionKind{Group: "more", Version: "traps", Kind: "Dop"},
				},
			},
			want: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 1},
				schema.GroupVersionKind{Group: "daps", Version: "v2", Kind: "Pedloyment"}: map[schema.GroupVersionKind]int{{Group: "more", Version: "traps", Kind: "Dop"}: 1},
			},
		},
		{
			name:  "addEdge same edge to empty graph",
			start: make(gvkGraph),
			add: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 2},
			},
		},
		{
			name: "removeEdge 1 edge from graph",
			start: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 1},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: make(gvkGraph),
		},
		{
			name: "removeEdge path of 2 from graph, tail first",
			start: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 1},
				schema.GroupVersionKind{Version: "v1", Kind: "Pod"}:                       map[schema.GroupVersionKind]int{{Version: "v1", Kind: "MiniPod"}: 1},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Deployment", Group: "apps"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: make(gvkGraph),
		},
		{
			name: "removeEdge path of 2 from graph, head first",
			start: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 1},
				schema.GroupVersionKind{Version: "v1", Kind: "Pod"}:                       map[schema.GroupVersionKind]int{{Version: "v1", Kind: "MiniPod"}: 1},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Deployment", Group: "apps"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
				},
			},
			want: make(gvkGraph),
		},
		{
			name: "removeEdge 1 edge from graph with double edge",
			start: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 2},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 1},
			},
		},
		{
			name: "removeEdge 2 final edges in inner map properly prunes",
			start: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{
					{Version: "v1", Kind: "Pod"}:      1,
					{Version: "v2", Kind: "SuperPod"}: 1,
				},
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}: map[schema.GroupVersionKind]int{
					{Version: "v1", Kind: "Pod"}: 1,
				},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v2", Kind: "SuperPod"},
				},
			},
			want: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 1},
			},
		},
		{
			name: "removing non-final edge in inner map does not prune outer",
			start: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{
					{Version: "v1", Kind: "Pod"}:      1,
					{Version: "v2", Kind: "SuperPod"}: 1,
				},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{{Version: "v2", Kind: "SuperPod"}: 1},
			},
		},
		{
			name: "removeEdge same edge twice prunes double edge",
			start: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{
					{Version: "v1", Kind: "Pod"}: 2,
				},
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}: map[schema.GroupVersionKind]int{
					{Version: "v1", Kind: "Pod"}: 1,
				},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}: map[schema.GroupVersionKind]int{{Version: "v1", Kind: "Pod"}: 1},
			},
		},
		{
			name:  "addEdge then removeEdge same edge results in empty graph",
			start: make(gvkGraph),
			add: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: make(gvkGraph),
		},
		{
			name:  "removeEdge edge from empty graph does nothing",
			start: make(gvkGraph),
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			},
			want: make(gvkGraph),
		},
		{
			name: "removeEdge non-existing path from graph does nothing",
			start: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{
					{Version: "v1", Kind: "Pod"}: 1,
				},
			},
			remove: []gvkEdge{
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
				},
			},
			want: gvkGraph{
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}: map[schema.GroupVersionKind]int{
					{Version: "v1", Kind: "Pod"}: 1,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.start

			for _, edges := range tc.add {
				m.addEdge(edges.x, edges.y)
			}
			for _, edges := range tc.remove {
				m.removeEdge(edges.x, edges.y)
			}

			if diff := cmp.Diff(m, tc.want); diff != "" {
				t.Errorf("got: %v, want: %v\ndiff: %v", m, tc.want, diff)
			}
		})
	}
}

func TestCheckCycle(t *testing.T) {
	tests := []struct {
		name      string
		graph     gvkGraph
		checkEdge gvkEdge
		want      bool
	}{
		{
			name:  "adding to empty graph does not return cycle",
			graph: make(gvkGraph),
			checkEdge: gvkEdge{
				x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
			},
			want: false,
		},
		{
			name: "adding non-cyclic edge to non-empty graph does not return cycle",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			}),
			checkEdge: gvkEdge{
				x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
			},
			want: false,
		},
		{
			name: "adding cylic edge at end of x path returns true",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "NanoPod"},
				},
			}),
			checkEdge: gvkEdge{
				x: schema.GroupVersionKind{Version: "v1", Kind: "NanoPod"},
				y: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			},
			want: true,
		},
		{
			name: "adding bi-directional edge returns cycle",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
				},
			}),
			checkEdge: gvkEdge{
				x: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
				y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
			},
			want: true,
		},
		{
			name: "adding non-cyclic edge in middle of path does not return cycle",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "MiniPod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "NanoPod"},
				},
			}),
			checkEdge: gvkEdge{
				x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				y: schema.GroupVersionKind{Version: "v1", Kind: "MediumPod"},
			},
			want: false,
		},
		{
			name: "adding non-cyclic edge in middle of path does not return cycle",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPodA"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPodB"},
				},
			}),
			checkEdge: gvkEdge{
				x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				y: schema.GroupVersionKind{Version: "v1", Kind: "MediumPod"},
			},
			want: false,
		},
		{
			name: "adding cyclic edge to tree creates cycle",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPodA"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "MiniPodB"},
				},
			}),
			checkEdge: gvkEdge{
				x: schema.GroupVersionKind{Version: "v1", Kind: "MiniPodB"},
				y: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.graph.checkCycle(tc.checkEdge.x, tc.checkEdge.y)
			if got != tc.want {
				t.Errorf("got %t, want %t", got, tc.want)
			}
		})
	}
}

func TestAddRemoveTemplate(t *testing.T) {
	tests := []struct {
		name            string
		graph           gvkGraph
		addTemplate     *unversioned.ExpansionTemplate
		removeTemplates []*unversioned.ExpansionTemplate
		wantCycle       bool
		wantGraph       gvkGraph
	}{
		{
			name:        "upsert template to empty graph",
			graph:       make(gvkGraph),
			addTemplate: loadTemplate(fixtures.TempExpReplicaDeploymentExpandsPods, t),
			wantCycle:   false,
			wantGraph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			}),
		},
		{
			name:            "remove template from empty graph",
			graph:           make(gvkGraph),
			removeTemplates: []*unversioned.ExpansionTemplate{loadTemplate(fixtures.TempExpReplicaDeploymentExpandsPods, t)},
			wantCycle:       false,
			wantGraph:       make(gvkGraph),
		},
		{
			name:            "upsert then remove same template produces empty graph",
			graph:           make(gvkGraph),
			addTemplate:     loadTemplate(fixtures.TempExpReplicaDeploymentExpandsPods, t),
			removeTemplates: []*unversioned.ExpansionTemplate{loadTemplate(fixtures.TempExpReplicaDeploymentExpandsPods, t)},
			wantCycle:       false,
			wantGraph:       make(gvkGraph),
		},
		{
			name:        "upsert template with multiple applyTo",
			graph:       make(gvkGraph),
			addTemplate: loadTemplate(fixtures.TempExpMultipleApplyTo, t),
			wantCycle:   false,
			wantGraph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "traps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "traps", Version: "v1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "traps", Version: "v1beta1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "traps", Version: "v1beta1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "CoreKind"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			}),
		},
		{
			name: "remove template with multiple applyTo",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "traps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "traps", Version: "v1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "traps", Version: "v1beta1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "traps", Version: "v1beta1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "CoreKind"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			}),
			removeTemplates: []*unversioned.ExpansionTemplate{loadTemplate(fixtures.TempExpMultipleApplyTo, t)},
			wantCycle:       false,
			wantGraph:       make(gvkGraph),
		},
		{
			name: "upsert template to non-empty graph",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			}),
			addTemplate: loadTemplate(fixtures.TempExpReplicaDeploymentExpandsPods, t),
			wantCycle:   false,
			wantGraph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
				{
					x: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
				},
			}),
		},
		{
			name: "upsert template to non-empty graph produces cycle",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Foo"},
					y: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Foo"},
				},
			}),
			addTemplate: loadTemplate(fixtures.TempExpReplicaDeploymentExpandsPods, t),
			wantCycle:   true,
			wantGraph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Foo"},
					y: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Foo"},
				},
			}),
		},
		{
			name: "upsert template that creates self-edge produces cycle",
			graph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Foo"},
				},
			}),
			addTemplate: loadTemplate(fixtures.TempExpReplicaDeploymentExpandsPods, t),
			wantCycle:   true,
			wantGraph: buildGraph([]gvkEdge{
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				},
				{
					x: schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
					y: schema.GroupVersionKind{Version: "v1", Kind: "Foo"},
				},
			}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.addTemplate != nil {
				gotCycle := tc.graph.addTemplate(tc.addTemplate) != nil
				if tc.wantCycle != gotCycle {
					t.Errorf("want cycle: %t, but got: %t", tc.wantCycle, gotCycle)
				}
			}

			for _, t := range tc.removeTemplates {
				tc.graph.removeTemplate(t)
			}

			if diff := cmp.Diff(tc.wantGraph, tc.graph); diff != "" {
				t.Errorf("want graph:%v\nbut got:%v\ndiff:\n%v", tc.wantGraph, tc.graph, diff)
			}
		})
	}
}
