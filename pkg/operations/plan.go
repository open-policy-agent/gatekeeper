package operations

import (
	"errors"
	"fmt"
)

// Features captures the feature flags that affect controller wiring.
// Values should match the corresponding command-line flags.
type Features struct {
	ExpansionEnabled          bool
	ExternalDataEnabled       bool
	ViolationExportEnabled    bool
	AdmissionExportEnabled    bool
	SyncVAPEnforcementScope   bool
	EnableK8sNativeValidation bool
}

// DefaultFeatures returns the feature-flag defaults used when Gatekeeper is
// started without overriding those flags.
func DefaultFeatures() Features {
	return Features{
		ExpansionEnabled:          true,
		ExternalDataEnabled:       true,
		ViolationExportEnabled:    false,
		AdmissionExportEnabled:    false,
		SyncVAPEnforcementScope:   true,
		EnableK8sNativeValidation: true,
	}
}

// Set is the collection of operations assigned to a process.
type Set map[Operation]bool

// Plan is the operation-to-capability mapping consumed by setupControllers.
// It is the single source of truth for which clients, systems, runnables,
// webhooks, and controllers a process constructs for a given --operation set
// and feature flags.
type Plan struct {
	Clients     ClientPlan
	Systems     SystemPlan
	Runnables   RunnablePlan
	Webhooks    WebhookPlan
	Controllers ControllerPlan
}

// ClientPlan describes constraint-client construction.
type ClientPlan struct {
	ConstraintClient   bool
	K8sCelDriver       bool
	AuditEnforcement   bool
	WebhookEnforcement bool
}

// SystemPlan describes in-process systems and shared objects.
type SystemPlan struct {
	ProviderCache            bool
	MutationSystem           bool
	ExpansionSystem          bool
	ExportSystem             bool
	ProcessExcluder          bool
	WebhookConfigCache       bool
	ConstraintTemplateEvents bool
}

// RunnablePlan describes manager.Runnable registrations.
type RunnablePlan struct {
	WatchManager          bool
	CacheManager          bool
	ExpectationsPruner    bool
	Metrics               bool
	Upgrade               bool
	Audit                 bool
	ClientCertWatcher     bool
	MutatorConflictRouter bool
}

// WebhookPlan describes admission webhook servers.
type WebhookPlan struct {
	Validation     bool
	Mutation       bool
	NamespaceLabel bool
}

// ControllerPlan describes controller registration.
type ControllerPlan struct {
	ConstraintTemplate       bool
	Constraint               bool
	Config                   bool
	Sync                     bool
	SyncSet                  bool
	ExpansionTemplate        bool
	ExpansionStatus          bool
	ExternalData             bool
	ExternalDataStatus       bool
	ExportConnection         bool
	Mutators                 bool
	MutatorStatus            bool
	WebhookConfig            bool
	ConfigStatus             bool
	ConnectionStatus         bool
	ConstraintStatus         bool
	ConstraintTemplateStatus bool
}

// NewPlan returns the controller/dependency plan for ops and f.
// Production wiring in setupControllers must call this function (via
// CurrentPlan) so tests and process startup cannot drift.
func NewPlan(ops Set, f Features) Plan {
	if ops == nil {
		ops = Set{}
	}

	hasValidation := hasValidationOperations(ops)
	hasMutation := hasMutationOperations(ops)
	exportEnabled := f.ViolationExportEnabled || f.AdmissionExportEnabled

	p := Plan{
		Clients: ClientPlan{
			ConstraintClient:   hasValidation,
			K8sCelDriver:       hasValidation && f.EnableK8sNativeValidation,
			AuditEnforcement:   ops[Audit],
			WebhookEnforcement: ops[Webhook],
		},
		Systems: SystemPlan{
			ProviderCache: f.ExternalDataEnabled,
			// Mutation, expansion, export, and process exclusion are constructed
			// for every operation set today. Later operation-alignment work can
			// narrow these; setupControllers follows this plan.
			MutationSystem:           true,
			ExpansionSystem:          true,
			ExportSystem:             true,
			ProcessExcluder:          true,
			WebhookConfigCache:       ops[Generate],
			ConstraintTemplateEvents: ops[Generate],
		},
		Runnables: RunnablePlan{
			WatchManager:       true,
			CacheManager:       true,
			ExpectationsPruner: true,
			Metrics:            true,
			Upgrade:            true,
			Audit:              ops[Audit],
			ClientCertWatcher:  f.ExternalDataEnabled,
			// routeConflictEvents is registered before mutation.Enabled() is checked.
			MutatorConflictRouter: true,
		},
		Webhooks: WebhookPlan{
			Validation:     ops[Webhook],
			Mutation:       ops[MutationWebhook],
			NamespaceLabel: ops[Webhook] || ops[MutationWebhook],
		},
		Controllers: ControllerPlan{
			ConstraintTemplate:       hasValidation,
			Constraint:               hasValidation,
			Config:                   true,
			Sync:                     hasValidation,
			SyncSet:                  hasValidation,
			ExpansionTemplate:        f.ExpansionEnabled,
			ExpansionStatus:          ops[Status] && f.ExpansionEnabled,
			ExternalData:             f.ExternalDataEnabled,
			ExternalDataStatus:       ops[Status],
			ExportConnection:         exportEnabled,
			Mutators:                 hasMutation,
			MutatorStatus:            ops[MutationStatus],
			WebhookConfig:            ops[Generate] && f.SyncVAPEnforcementScope,
			ConfigStatus:             ops[Status],
			ConnectionStatus:         ops[Status],
			ConstraintStatus:         ops[Status] && hasValidation,
			ConstraintTemplateStatus: ops[Status] && hasValidation,
		},
	}
	return p
}

// CurrentPlan returns NewPlan for the operations assigned to this process.
func CurrentPlan(f Features) Plan {
	return NewPlan(AssignedSet(), f)
}

type namedDep struct {
	name    string
	present bool
}

// Validate reports registered consumers that are missing a required dependency.
func (p Plan) Validate() error {
	var errs []error
	require := func(consumer string, registered bool, deps ...namedDep) {
		if !registered {
			return
		}
		for _, d := range deps {
			if !d.present {
				errs = append(errs, fmt.Errorf("%s requires %s", consumer, d.name))
			}
		}
	}

	constraintClient := namedDep{name: "ConstraintClient", present: p.Clients.ConstraintClient}
	processExcluder := namedDep{name: "ProcessExcluder", present: p.Systems.ProcessExcluder}
	cacheManager := namedDep{name: "CacheManager", present: p.Runnables.CacheManager}
	watchManager := namedDep{name: "WatchManager", present: p.Runnables.WatchManager}
	expansionSystem := namedDep{name: "ExpansionSystem", present: p.Systems.ExpansionSystem}
	mutationSystem := namedDep{name: "MutationSystem", present: p.Systems.MutationSystem}
	exportSystem := namedDep{name: "ExportSystem", present: p.Systems.ExportSystem}
	providerCache := namedDep{name: "ProviderCache", present: p.Systems.ProviderCache}
	webhookConfigCache := namedDep{name: "WebhookConfigCache", present: p.Systems.WebhookConfigCache}
	ctEvents := namedDep{name: "ConstraintTemplateEvents", present: p.Systems.ConstraintTemplateEvents}

	require("audit", p.Runnables.Audit, constraintClient, processExcluder, cacheManager, expansionSystem)
	require("validationWebhook", p.Webhooks.Validation, constraintClient, processExcluder, expansionSystem)
	require("mutationWebhook", p.Webhooks.Mutation, mutationSystem)
	require("constraintTemplate", p.Controllers.ConstraintTemplate, constraintClient, watchManager, processExcluder)
	require("constraint", p.Controllers.Constraint, constraintClient)
	require("config", p.Controllers.Config, cacheManager)
	require("sync", p.Controllers.Sync, cacheManager)
	require("syncSet", p.Controllers.SyncSet, cacheManager)
	require("expansionTemplate", p.Controllers.ExpansionTemplate, expansionSystem)
	require("externalData", p.Controllers.ExternalData, providerCache)
	require("exportConnection", p.Controllers.ExportConnection, exportSystem)
	require("mutators", p.Controllers.Mutators, mutationSystem)
	require("webhookConfig", p.Controllers.WebhookConfig, webhookConfigCache, ctEvents)
	require("cacheManager", p.Runnables.CacheManager, watchManager)
	require("expectationsPruner", p.Runnables.ExpectationsPruner, cacheManager)
	require("clientCertWatcher", p.Runnables.ClientCertWatcher, providerCache)
	require("k8sCelDriver", p.Clients.K8sCelDriver, constraintClient)
	if p.Controllers.ExportConnection {
		if p.Runnables.Audit {
			require("auditExport", true, exportSystem)
		}
		if p.Webhooks.Validation {
			require("admissionExport", true, exportSystem)
		}
	}

	return errors.Join(errs...)
}
