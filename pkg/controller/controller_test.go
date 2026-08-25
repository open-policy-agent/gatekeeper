/*

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

package controller

import (
	"testing"

	cm "github.com/open-policy-agent/gatekeeper/v3/pkg/cachemanager"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/operations"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestDependenciesValidate(t *testing.T) {
	dataSyncDeps := func() *Dependencies {
		return &Dependencies{
			CacheMgr:        &cm.CacheManager{},
			SyncEventsCh:    make(chan event.GenericEvent, 1),
			WatchManger:     &watch.Manager{},
			ProcessExcluder: process.New(),
		}
	}

	tests := []struct {
		name     string
		assigned []operations.Operation
		deps     func() *Dependencies
		wantErr  bool
	}{
		{
			name:     "data sync operations with a complete plan",
			assigned: []operations.Operation{operations.Audit},
			deps:     dataSyncDeps,
		},
		{
			name:     "data sync operations without a cache manager",
			assigned: []operations.Operation{operations.Audit},
			deps: func() *Dependencies {
				deps := dataSyncDeps()
				deps.CacheMgr = nil
				return deps
			},
			wantErr: true,
		},
		{
			name:     "data sync operations without sync events",
			assigned: []operations.Operation{operations.Webhook},
			deps: func() *Dependencies {
				deps := dataSyncDeps()
				deps.SyncEventsCh = nil
				return deps
			},
			wantErr: true,
		},
		{
			name:     "data sync operations without a watch manager",
			assigned: []operations.Operation{operations.Webhook},
			deps: func() *Dependencies {
				deps := dataSyncDeps()
				deps.WatchManger = nil
				return deps
			},
			wantErr: true,
		},
		{
			name:     "mutation webhook keeps process exclusions without data sync",
			assigned: []operations.Operation{operations.MutationWebhook},
			deps: func() *Dependencies {
				return &Dependencies{ProcessExcluder: process.New()}
			},
		},
		{
			name:     "mutation webhook without a process excluder",
			assigned: []operations.Operation{operations.MutationWebhook},
			deps:     func() *Dependencies { return &Dependencies{} },
			wantErr:  true,
		},
		{
			name:     "cache manager built for operations that do not sync data",
			assigned: []operations.Operation{operations.MutationWebhook},
			deps: func() *Dependencies {
				return &Dependencies{
					CacheMgr:        &cm.CacheManager{},
					ProcessExcluder: process.New(),
				}
			},
			wantErr: true,
		},
		{
			name:     "mutation status needs nothing",
			assigned: []operations.Operation{operations.MutationStatus},
			deps:     func() *Dependencies { return &Dependencies{} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(operations.AssignForTest(tt.assigned...))

			err := tt.deps().Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}
