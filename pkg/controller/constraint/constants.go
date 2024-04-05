package constraint

const (
	// VapGenerationLabel indicates opting in and out preference for generating VAP objects.
	VapGenerationLabel = "gatekeeper.sh/use-vap"
	// VapFlagNone: do not generate.
	VapFlagNone = "NONE"
	// VapFlagGatekeeperDefault: do not generate unless label gatekeeper.sh/use-vap: yes is added to policy explicitly.
	VapFlagGatekeeperDefault = "GATEKEEPER_DEFAULT"
	// VapFlagVapDefault: generate unless label gatekeeper.sh/use-vap: no is added to policy explicitly.
	VapFlagVapDefault = "VAP_DEFAULT"
	// no value.
	No = "no"
	// yes value.
	Yes = "yes"
)
