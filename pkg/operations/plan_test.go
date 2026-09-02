package operations

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// wantPlan returns the capabilities that setupControllers always constructs,
// then applies modify for operation- and feature-specific fields.
func wantPlan(modify func(*Plan)) Plan {
	p := Plan{
		Systems: SystemPlan{
			MutationSystem:  true,
			ExpansionSystem: true,
			ExportSystem:    true,
			ProcessExcluder: true,
		},
		Runnables: RunnablePlan{
			WatchManager:          true,
			CacheManager:          true,
			ExpectationsPruner:    true,
			Metrics:               true,
			Upgrade:               true,
			MutatorConflictRouter: true,
		},
		Controllers: ControllerPlan{
			Config: true,
		},
	}
	modify(&p)
	return p
}

func withDefaultFeatures(p *Plan) {
	p.Systems.ProviderCache = true
	p.Runnables.ClientCertWatcher = true
	p.Controllers.ExpansionTemplate = true
	p.Controllers.ExternalData = true
}

func withValidationIngestion(p *Plan) {
	p.Clients.ConstraintClient = true
	p.Clients.K8sCelDriver = true
	p.Controllers.ConstraintTemplate = true
	p.Controllers.Constraint = true
	p.Controllers.Sync = true
	p.Controllers.SyncSet = true
}

func TestNewPlan(t *testing.T) {
	tests := []struct {
		name     string
		ops      Set
		features Features
		want     Plan
	}{
		{
			name:     "audit",
			ops:      Set{Audit: true},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				withValidationIngestion(p)
				p.Clients.AuditEnforcement = true
				p.Runnables.Audit = true
			}),
		},
		{
			name:     "webhook",
			ops:      Set{Webhook: true},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				withValidationIngestion(p)
				p.Clients.WebhookEnforcement = true
				p.Webhooks.Validation = true
				p.Webhooks.NamespaceLabel = true
			}),
		},
		{
			name:     "mutation-webhook",
			ops:      Set{MutationWebhook: true},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				p.Webhooks.Mutation = true
				p.Webhooks.NamespaceLabel = true
				p.Controllers.Mutators = true
			}),
		},
		{
			name:     "mutation-controller",
			ops:      Set{MutationController: true},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				p.Controllers.Mutators = true
			}),
		},
		{
			name:     "mutation-status",
			ops:      Set{MutationStatus: true},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				p.Controllers.Mutators = true
				p.Controllers.MutatorStatus = true
			}),
		},
		{
			// Status currently initializes a constraint client (HasValidationOperations
			// includes Status) but adds no enforcement points. See #4770.
			name:     "status",
			ops:      Set{Status: true},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				withValidationIngestion(p)
				p.Controllers.ExpansionStatus = true
				p.Controllers.ExternalDataStatus = true
				p.Controllers.ConfigStatus = true
				p.Controllers.ConnectionStatus = true
				p.Controllers.ConstraintStatus = true
				p.Controllers.ConstraintTemplateStatus = true
			}),
		},
		{
			// Generate currently does not start ConstraintTemplate/Constraint
			// reconcilers because HasValidationOperations excludes Generate. See #4771.
			name:     "generate",
			ops:      Set{Generate: true},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				p.Systems.WebhookConfigCache = true
				p.Systems.ConstraintTemplateEvents = true
				p.Controllers.WebhookConfig = true
			}),
		},
		{
			name: "audit+status+mutation-status+generate",
			ops: Set{
				Audit:          true,
				Status:         true,
				MutationStatus: true,
				Generate:       true,
			},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				withValidationIngestion(p)
				p.Clients.AuditEnforcement = true
				p.Systems.WebhookConfigCache = true
				p.Systems.ConstraintTemplateEvents = true
				p.Runnables.Audit = true
				p.Controllers.ExpansionStatus = true
				p.Controllers.ExternalDataStatus = true
				p.Controllers.Mutators = true
				p.Controllers.MutatorStatus = true
				p.Controllers.WebhookConfig = true
				p.Controllers.ConfigStatus = true
				p.Controllers.ConnectionStatus = true
				p.Controllers.ConstraintStatus = true
				p.Controllers.ConstraintTemplateStatus = true
			}),
		},
		{
			name: "webhook+mutation-webhook",
			ops: Set{
				Webhook:         true,
				MutationWebhook: true,
			},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				withValidationIngestion(p)
				p.Clients.WebhookEnforcement = true
				p.Webhooks.Validation = true
				p.Webhooks.Mutation = true
				p.Webhooks.NamespaceLabel = true
				p.Controllers.Mutators = true
			}),
		},
		{
			name: "all operations",
			ops: Set{
				Audit:              true,
				Webhook:            true,
				Status:             true,
				MutationStatus:     true,
				MutationWebhook:    true,
				MutationController: true,
				Generate:           true,
			},
			features: DefaultFeatures(),
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				withValidationIngestion(p)
				p.Clients.AuditEnforcement = true
				p.Clients.WebhookEnforcement = true
				p.Systems.WebhookConfigCache = true
				p.Systems.ConstraintTemplateEvents = true
				p.Runnables.Audit = true
				p.Webhooks.Validation = true
				p.Webhooks.Mutation = true
				p.Webhooks.NamespaceLabel = true
				p.Controllers.ExpansionStatus = true
				p.Controllers.ExternalDataStatus = true
				p.Controllers.Mutators = true
				p.Controllers.MutatorStatus = true
				p.Controllers.WebhookConfig = true
				p.Controllers.ConfigStatus = true
				p.Controllers.ConnectionStatus = true
				p.Controllers.ConstraintStatus = true
				p.Controllers.ConstraintTemplateStatus = true
			}),
		},
		{
			name: "webhook expansion disabled",
			ops:  Set{Webhook: true},
			features: Features{
				ExpansionEnabled:          false,
				ExternalDataEnabled:       true,
				SyncVAPEnforcementScope:   true,
				EnableK8sNativeValidation: true,
			},
			want: wantPlan(func(p *Plan) {
				p.Systems.ProviderCache = true
				p.Runnables.ClientCertWatcher = true
				p.Controllers.ExternalData = true
				withValidationIngestion(p)
				p.Clients.WebhookEnforcement = true
				p.Webhooks.Validation = true
				p.Webhooks.NamespaceLabel = true
			}),
		},
		{
			name: "webhook external data disabled",
			ops:  Set{Webhook: true},
			features: Features{
				ExpansionEnabled:          true,
				ExternalDataEnabled:       false,
				SyncVAPEnforcementScope:   true,
				EnableK8sNativeValidation: true,
			},
			want: wantPlan(func(p *Plan) {
				p.Controllers.ExpansionTemplate = true
				withValidationIngestion(p)
				p.Clients.WebhookEnforcement = true
				p.Webhooks.Validation = true
				p.Webhooks.NamespaceLabel = true
			}),
		},
		{
			name: "audit violation export enabled",
			ops:  Set{Audit: true},
			features: Features{
				ExpansionEnabled:          true,
				ExternalDataEnabled:       true,
				ViolationExportEnabled:    true,
				SyncVAPEnforcementScope:   true,
				EnableK8sNativeValidation: true,
			},
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				withValidationIngestion(p)
				p.Clients.AuditEnforcement = true
				p.Runnables.Audit = true
				p.Controllers.ExportConnection = true
			}),
		},
		{
			name: "webhook admission export enabled",
			ops:  Set{Webhook: true},
			features: Features{
				ExpansionEnabled:          true,
				ExternalDataEnabled:       true,
				AdmissionExportEnabled:    true,
				SyncVAPEnforcementScope:   true,
				EnableK8sNativeValidation: true,
			},
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				withValidationIngestion(p)
				p.Clients.WebhookEnforcement = true
				p.Webhooks.Validation = true
				p.Webhooks.NamespaceLabel = true
				p.Controllers.ExportConnection = true
			}),
		},
		{
			name: "generate vap scope sync disabled",
			ops:  Set{Generate: true},
			features: Features{
				ExpansionEnabled:          true,
				ExternalDataEnabled:       true,
				SyncVAPEnforcementScope:   false,
				EnableK8sNativeValidation: true,
			},
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				p.Systems.WebhookConfigCache = true
				p.Systems.ConstraintTemplateEvents = true
			}),
		},
		{
			name: "status external data disabled",
			ops:  Set{Status: true},
			features: Features{
				ExpansionEnabled:          true,
				ExternalDataEnabled:       false,
				SyncVAPEnforcementScope:   true,
				EnableK8sNativeValidation: true,
			},
			want: wantPlan(func(p *Plan) {
				p.Controllers.ExpansionTemplate = true
				withValidationIngestion(p)
				p.Controllers.ExpansionStatus = true
				p.Controllers.ExternalDataStatus = true
				p.Controllers.ConfigStatus = true
				p.Controllers.ConnectionStatus = true
				p.Controllers.ConstraintStatus = true
				p.Controllers.ConstraintTemplateStatus = true
			}),
		},
		{
			name: "status expansion disabled",
			ops:  Set{Status: true},
			features: Features{
				ExpansionEnabled:          false,
				ExternalDataEnabled:       true,
				SyncVAPEnforcementScope:   true,
				EnableK8sNativeValidation: true,
			},
			want: wantPlan(func(p *Plan) {
				p.Systems.ProviderCache = true
				p.Runnables.ClientCertWatcher = true
				p.Controllers.ExternalData = true
				withValidationIngestion(p)
				p.Controllers.ExternalDataStatus = true
				p.Controllers.ConfigStatus = true
				p.Controllers.ConnectionStatus = true
				p.Controllers.ConstraintStatus = true
				p.Controllers.ConstraintTemplateStatus = true
			}),
		},
		{
			name: "webhook k8s native validation disabled",
			ops:  Set{Webhook: true},
			features: Features{
				ExpansionEnabled:          true,
				ExternalDataEnabled:       true,
				SyncVAPEnforcementScope:   true,
				EnableK8sNativeValidation: false,
			},
			want: wantPlan(func(p *Plan) {
				withDefaultFeatures(p)
				p.Clients.ConstraintClient = true
				p.Clients.WebhookEnforcement = true
				p.Webhooks.Validation = true
				p.Webhooks.NamespaceLabel = true
				p.Controllers.ConstraintTemplate = true
				p.Controllers.Constraint = true
				p.Controllers.Sync = true
				p.Controllers.SyncSet = true
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewPlan(tc.ops, tc.features)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("NewPlan() mismatch (-want +got):\n%s", diff)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("Validate() registered consumers missing dependencies: %v", err)
			}
		})
	}
}

func TestPlanValidateRejectsIncompleteConsumer(t *testing.T) {
	p := NewPlan(Set{Audit: true}, DefaultFeatures())
	p.Clients.ConstraintClient = false
	if err := p.Validate(); err == nil {
		t.Fatal("expected Validate() error when audit is registered without a constraint client")
	}
}

func TestCurrentPlanUsesAssignedSet(t *testing.T) {
	got := CurrentPlan(DefaultFeatures())
	want := NewPlan(AssignedSet(), DefaultFeatures())
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("CurrentPlan() mismatch (-want +got):\n%s", diff)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("Validate() registered consumers missing dependencies: %v", err)
	}
}
