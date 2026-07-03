package catalog

import (
	"os"
	"path/filepath"
	"sync"

	semver "github.com/blang/semver/v4"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/match"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
)

// This file implements deriving a policy's Kubernetes version compatibility
// range from the built-in API resources it targets, using the API lifecycle
// metadata baked into k8s.io/api types (APILifecycleIntroduced /
// APILifecycleRemoved). It is used as a fallback when a ConstraintTemplate does
// not carry explicit metadata.gatekeeper.sh/{min,max}KubernetesVersion
// annotations.
//
// Scope and limitations (resource granularity, not field granularity):
//   - The lower bound (min) is the release in which a targeted resource first
//     appeared. Policies targeting resources present since v1.0 get no bound.
//   - The upper bound (max) can only be derived for resources that are
//     deprecated but still shipped in the linked k8s.io/api (e.g. a *beta1 API
//     scheduled for removal). Once an API is fully removed from Kubernetes its
//     Go type is dropped from k8s.io/api, so its removal cannot be detected here.
//   - Newly added *fields* on an existing resource are invisible to this
//     mechanism: the resource's GVK lifecycle is unchanged when a field is
//     added. Such policies still require an explicit annotation.

// apiLifecycleIntroduced is implemented by generated k8s.io/api types that
// record the release in which the API was introduced. The equivalent upstream
// interfaces (k8s.io/apiserver) are unexported, so we redeclare them here.
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
var versionFloor = semver.Version{Major: 1, Minor: 0}

// kindLifecycle is the aggregated lifecycle of a single group+kind across all
// of its registered API versions.
type kindLifecycle struct {
	// introduced is the earliest release any registered version of this
	// group+kind appeared (the release from which a cluster can serve it).
	introduced *semver.Version
	// removed is the release in which the last remaining version of this
	// group+kind is removed. It is only meaningful when hasStableVersion is
	// false; otherwise the resource lives on under a non-removed version.
	removed *semver.Version
	// hasStableVersion is true when at least one registered version of this
	// group+kind is not scheduled for removal.
	hasStableVersion bool
}

var (
	lifecycleIndexOnce sync.Once
	lifecycleIndex     map[groupKind]*kindLifecycle
)

// majorMinor builds a semver.Version from the (major, minor) pair the lifecycle
// methods return.
func majorMinor(major, minor uint64) semver.Version {
	return semver.Version{Major: major, Minor: minor}
}

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
			v := majorMinor(uint64(maj), uint64(min)) //nolint:gosec // Kubernetes version components are always non-negative
			if lc.introduced == nil || v.LT(*lc.introduced) {
				lc.introduced = &v
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
		v := majorMinor(uint64(remMaj), uint64(remMin)) //nolint:gosec // Kubernetes version components are always non-negative
		if v.Major == 0 && v.Minor == 0 {
			// (0,0) means "not scheduled for removal".
			lc.hasStableVersion = true
			continue
		}
		if lc.removed == nil || v.GT(*lc.removed) {
			lc.removed = &v
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
// Both return values are normalized ("vX.Y.0") or empty when no bound applies.
// A contradictory derived range (min > max) is discarded as unreliable.
func deriveK8sVersionRange(kinds []groupKind) (minVersion, maxVersion string) {
	idx := lifecycleIdx()

	var minV, maxV *semver.Version

	for _, gk := range kinds {
		lc, ok := idx[gk]
		if !ok {
			// Custom resource or an API already fully removed from k8s.io/api:
			// no lifecycle data to reason about.
			continue
		}

		// Lower bound: latest introduction across targeted resources. Resources
		// present since the version floor impose no meaningful bound.
		if lc.introduced != nil && lc.introduced.GT(versionFloor) {
			if minV == nil || lc.introduced.GT(*minV) {
				minV = lc.introduced
			}
		}

		// Upper bound: only when the resource is fully removed (no stable
		// version remains). The last supported release is one minor before the
		// removal release.
		if lc.removed != nil && !lc.hasStableVersion && lc.removed.Minor > 0 {
			last := semver.Version{Major: lc.removed.Major, Minor: lc.removed.Minor - 1}
			if maxV == nil || last.LT(*maxV) {
				maxV = &last
			}
		}
	}

	// Discard a contradictory range: it means the policy targets an API
	// introduced only after another it targets was removed, which no single
	// cluster can satisfy. Better to record nothing than a bogus gate.
	if minV != nil && maxV != nil && minV.GT(*maxV) {
		return "", ""
	}

	return formatDerivedVersion(minV), formatDerivedVersion(maxV)
}

// formatDerivedVersion renders a derived version as a normalized "vX.Y.Z"
// string, or "" when no bound applies.
func formatDerivedVersion(v *semver.Version) string {
	if v == nil {
		return ""
	}
	return "v" + v.String()
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

	seen := make(map[groupKind]bool)
	var result []groupKind

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := findConstraintFile(filepath.Join(samplesDir, entry.Name()))
		if path == "" {
			continue
		}
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
		groups := entry.APIGroups
		if len(groups) == 0 {
			groups = []string{""}
		}
		for _, g := range groups {
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
