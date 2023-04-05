package v1beta1

// Label keys used for internal gatekeeper operations.
const (
	ExpansionTemplateNameLabel  = "internal.gatekeeper.sh/expansiontemplate-name"
	ConstraintNameLabel         = "internal.gatekeeper.sh/constraint-name"
	ConstraintKindLabel         = "internal.gatekeeper.sh/constraint-kind"
	ConstraintTemplateNameLabel = "internal.gatekeeper.sh/constrainttemplate-name"
	MutatorNameLabel            = "internal.gatekeeper.sh/mutator-name"
	MutatorKindLabel            = "internal.gatekeeper.sh/mutator-kind"
	PodLabel                    = "internal.gatekeeper.sh/pod"
)
