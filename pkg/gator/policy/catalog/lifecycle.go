package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
)

// This file implements deriving a policy's Kubernetes version compatibility
// range from the built-in API resources it targets, using the API lifecycle
// metadata baked into k8s.io/api types (APILifecycleIntroduced /
// APILifecycleRemoved). It is used as a fallback when a ConstraintTemplate does
// not carry explicit metadata.gatekeeper.sh/{min,max}KubernetesVersion
// annotations.
type apiLifecycleIntroduced interface {
	APILifecycleIntroduced() (major, minor int)
}

// apiLifecycleRemoved is implemented by generated k8s.io/api types (pre-release
// APIs) that record the release in which the API is removed.
type apiLifecycleRemoved interface {
	APILifecycleRemoved() (major, minor int)
}

// groupKind identifies an API resource by group and kind, ignoring version
// (constraint match.kinds entries do not carry a version).
type groupKind struct {
	Group string
	Kind  string
}

// versionFloor is the earliest Kubernetes release. A resource introduced at or
// before it has effectively always existed, so it imposes no meaningful lower
// bound and is ignored when deriving a policy's min version.
var versionFloor = version.MajorMinor(1, 0)

// kindLifecycle is the aggregated lifecycle of a single group+kind across all
// of its registered API versions.
type kindLifecycle struct {
	// introduced is the earliest release any registered version of this
	// group+kind appeared (the release from which a cluster can serve it).
	introduced *version.Version
	// removed is the release in which the last remaining version of this
	// group+kind is removed. It is only meaningful when hasStableVersion is
	// false; otherwise the resource lives on under a non-removed version.
	removed *version.Version
	// hasStableVersion is true when at least one registered version of this
	// group+kind is not scheduled for removal.
	hasStableVersion bool
}

var (
	lifecycleIndexOnce sync.Once
	lifecycleIndex     map[groupKind]*kindLifecycle
)

// buildLifecycleIndex scans every type registered in the client-go scheme (all
// built-in Kubernetes API groups) and aggregates their lifecycle metadata by
// group+kind.
func buildLifecycleIndex() map[groupKind]*kindLifecycle {
	index := make(map[groupKind]*kindLifecycle)

	for gvk := range clientgoscheme.Scheme.AllKnownTypes() {
		// Skip the internal (non-served) version; it carries no lifecycle data.
		if gvk.Version == runtime.APIVersionInternal {
			continue
		}

		obj, err := clientgoscheme.Scheme.New(gvk)
		if err != nil {
			continue
		}

		gk := groupKind{Group: gvk.Group, Kind: gvk.Kind}
		lc := index[gk]
		if lc == nil {
			lc = &kindLifecycle{}
			index[gk] = lc
		}

		if intro, ok := obj.(apiLifecycleIntroduced); ok {
			maj, min := intro.APILifecycleIntroduced()
			v := version.MajorMinor(uint(maj), uint(min)) //nolint:gosec // Kubernetes version components are always non-negative
			if lc.introduced == nil || v.LessThan(lc.introduced) {
				lc.introduced = v
			}
		}

		rem, ok := obj.(apiLifecycleRemoved)
		if !ok {
			// No removal method: this version is stable and keeps the resource
			// available indefinitely.
			lc.hasStableVersion = true
			continue
		}
		remMaj, remMin := rem.APILifecycleRemoved()
		v := version.MajorMinor(uint(remMaj), uint(remMin)) //nolint:gosec // Kubernetes version components are always non-negative
		if v.Major() == 0 && v.Minor() == 0 {
			// (0,0) means "not scheduled for removal".
			lc.hasStableVersion = true
			continue
		}
		if lc.removed == nil || v.GreaterThan(lc.removed) {
			lc.removed = v
		}
	}

	return index
}

// lifecycleIdx returns the lazily-built, cached lifecycle index.
func lifecycleIdx() map[groupKind]*kindLifecycle {
	lifecycleIndexOnce.Do(func() {
		lifecycleIndex = buildLifecycleIndex()
	})
	return lifecycleIndex
}

// deriveK8sVersionRange computes the Kubernetes version compatibility range for
// a policy from the built-in resources its constraints target.
//
//   - min = the maximum, over all targeted resources, of the release each
//     resource was introduced. The cluster must be new enough to serve every
//     targeted API, so the latest-introduced one is the binding lower bound. A
//     resource present since the version floor imposes no bound and is ignored.
//   - max = the minimum, over all targeted resources that are fully removed, of
//     the last release before removal. The policy stops working as soon as any
//     targeted API disappears, so the earliest removal is the binding upper
//     bound.
//
// The minimum is a normalized floor ("vX.Y.0"); the maximum is a whole-minor
// ceiling ("vX.Y", no patch component) because lifecycle metadata only knows
// the minor an API was removed in, so every patch of the prior minor still
// serves it. Either is empty when no bound applies.
//
// A contradictory derived range (min > max) is returned as an error rather than
// as empty bounds: the targeted APIs have no overlapping supported release, so
// no single cluster can serve them all. Emitting empty bounds would make
// K8sVersionInRange treat the policy as compatible with every cluster (two empty
// bounds are unbounded) — the opposite of the truth — so the caller must fail
// generation and require explicit metadata instead.
func deriveK8sVersionRange(kinds []groupKind) (minVersion, maxVersion string, err error) {
	idx := lifecycleIdx()

	var minV, maxV *version.Version

	for _, gk := range kinds {
		lc, ok := idx[gk]
		if !ok {
			// Custom resource or an API already fully removed from k8s.io/api:
			// no lifecycle data to reason about.
			continue
		}

		// Lower bound: latest introduction across targeted resources. Resources
		// present since the version floor impose no meaningful bound.
		if lc.introduced != nil && lc.introduced.GreaterThan(versionFloor) {
			if minV == nil || lc.introduced.GreaterThan(minV) {
				minV = lc.introduced
			}
		}

		// Upper bound: only when the resource is fully removed (no stable
		// version remains). The last supported release is one minor before the
		// removal release.
		//
		// A removal at a major boundary (minor 0, e.g. 2.0) would make the last
		// supported release the final minor of the previous major, which the
		// lifecycle metadata does not record, so no reliable bound can be derived
		// and we skip it. Kubernetes only ships major version 1, so in practice
		// removals always occur at a nonzero minor and this branch is not hit.
		if lc.removed != nil && !lc.hasStableVersion && lc.removed.Minor() > 0 {
			last := lc.removed.OffsetMinor(-1)
			if maxV == nil || last.LessThan(maxV) {
				maxV = last
			}
		}
	}

	// A contradictory range means the policy targets an API introduced only
	// after another it targets was removed, which no single cluster can satisfy.
	// Surface it so the caller fails generation and requires explicit metadata,
	// rather than dropping both bounds and marking the policy compatible with
	// every cluster.
	if minV != nil && maxV != nil && minV.GreaterThan(maxV) {
		return "", "", fmt.Errorf("derived Kubernetes version range is contradictory (min %s > max %s): the targeted APIs have no overlapping supported release; add explicit metadata.gatekeeper.sh/{min,max}KubernetesVersion annotations",
			formatDerivedVersion(minV), formatDerivedCeiling(maxV))
	}

	return formatDerivedVersion(minV), formatDerivedCeiling(maxV), nil
}

// formatDerivedVersion renders a derived lower bound as a normalized floor
// "vX.Y.0", or "" when no bound applies. The patch is always 0 because
// lifecycle metadata has only major/minor granularity, and a floor at the start
// of a minor correctly admits every later patch of that minor.
func formatDerivedVersion(v *version.Version) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("v%d.%d.0", v.Major(), v.Minor())
}

// formatDerivedCeiling renders a derived upper bound as a whole-minor ceiling
// "vX.Y" (no patch component), or "" when no bound applies. Lifecycle metadata
// only knows the minor in which an API was removed, so the bound must cover the
// entire prior minor including all its patch releases. K8sVersionInRange treats
// a patch-less ceiling as minor-granular, so a derived "v1.21" admits v1.21.8,
// unlike an explicit patch-level "v1.21.0".
func formatDerivedCeiling(v *version.Version) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("v%d.%d", v.Major(), v.Minor())
}

// targetGroupKinds scans a policy's sample constraint files and returns the
// deduplicated set of built-in resources (group+kind) they match on via
// spec.match.kinds. Wildcards are skipped since they carry no lifecycle signal.
func targetGroupKinds(templateDir string) []groupKind {
	samplesDir := filepath.Join(templateDir, "samples")
	entries, err := os.ReadDir(samplesDir)
	if err != nil {
		return nil
	}

	// A sample constraint can live either directly under samples/
	// (samples/constraint.yaml, the layout used by the generator test fixture
	// and some library policies) or inside a per-sample subdirectory
	// (samples/<name>/constraint.yaml). Collect both so directly-stored
	// constraints still contribute derived bounds.
	var constraintPaths []string
	if direct := findConstraintFile(samplesDir); direct != "" {
		constraintPaths = append(constraintPaths, direct)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if path := findConstraintFile(filepath.Join(samplesDir, entry.Name())); path != "" {
			constraintPaths = append(constraintPaths, path)
		}
	}

	seen := make(map[groupKind]bool)
	var result []groupKind

	for _, path := range constraintPaths {
		data, readErr := os.ReadFile(path)
		if readErr != nil || !isConstraintFile(data) {
			continue
		}
		for _, gk := range parseMatchKinds(data) {
			if !seen[gk] {
				seen[gk] = true
				result = append(result, gk)
			}
		}
	}

	return result
}

// parseMatchKinds extracts the group+kind pairs from a constraint's
// spec.match.kinds, expanding the apiGroups × kinds cross product and skipping
// wildcard entries.
func parseMatchKinds(data []byte) []groupKind {
	var c struct {
		Spec struct {
			Match match.Match `json:"match"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil
	}

	var result []groupKind
	for _, entry := range c.Spec.Match.Kinds {
		// An omitted apiGroups matches every group (wildcard), just like "*"
		// (see the matcher at pkg/mutation/match/match.go: len(APIGroups) == 0
		// matches any group). It therefore carries no usable lifecycle signal,
		// so skip the entry rather than treating it as the core ("") group.
		if len(entry.APIGroups) == 0 {
			continue
		}
		for _, g := range entry.APIGroups {
			if g == "*" {
				continue
			}
			for _, k := range entry.Kinds {
				if k == "" || k == "*" {
					continue
				}
				result = append(result, groupKind{Group: g, Kind: k})
			}
		}
	}
	return result
}
