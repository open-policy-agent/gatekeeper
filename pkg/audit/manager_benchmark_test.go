package audit

import (
	"context"
	"os"
	"path"
	"strconv"
	"testing"

	"github.com/go-logr/logr"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	benchmarkAPIVersionKey = "apiVersion"
	benchmarkKindKey       = "kind"
	benchmarkMetadataKey   = "metadata"
	benchmarkNameKey       = "name"
	benchmarkNamespaceKind = "Namespace"
)

func BenchmarkReviewObjectsSpoolFormats(b *testing.B) {
	const objectCount = 500
	objects := benchmarkNamespaceObjects(objectCount)

	b.Run("per_object_files", func(b *testing.B) {
		rootDir := b.TempDir()
		spoolDir := path.Join(rootDir, "Namespace_0")
		require.NoError(b, os.Mkdir(spoolDir, 0o750))
		writeBenchmarkPerObjectFiles(b, spoolDir, objects)
		runReviewObjectsBenchmark(b, rootDir)
	})

	b.Run("batch_file", func(b *testing.B) {
		rootDir := b.TempDir()
		spoolDir := path.Join(rootDir, "Namespace_0")
		require.NoError(b, os.Mkdir(spoolDir, 0o750))
		am := benchmarkAuditManager(b)
		require.NoError(b, am.writeUnstructuredList(spoolDir, objects))
		runReviewObjectsBenchmark(b, rootDir)
	})
}

func BenchmarkWriteObjectsSpoolFormats(b *testing.B) {
	const objectCount = 500
	objects := benchmarkNamespaceObjects(objectCount)

	b.Run("per_object_files", func(b *testing.B) {
		spoolDir := b.TempDir()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			writeBenchmarkPerObjectFiles(b, spoolDir, objects)
		}
	})

	b.Run("batch_file", func(b *testing.B) {
		spoolDir := b.TempDir()
		am := benchmarkAuditManager(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			require.NoError(b, am.writeUnstructuredList(spoolDir, objects))
		}
	})
}

func runReviewObjectsBenchmark(b *testing.B, rootDir string) {
	b.Helper()

	restore := setAuditGlobalsForBenchmark(rootDir)
	defer restore()

	am := benchmarkAuditManager(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		updateLists := map[util.KindVersionName]*LimitQueue{}
		totalViolationsPerConstraint := map[util.KindVersionName]int64{}
		totalViolationsPerEnforcementAction := map[util.EnforcementAction]int64{}
		auditExportPublishingState := &auditExportPublishingState{Errors: map[string]error{}}

		require.NoError(b, am.reviewObjects(context.Background(), "Namespace", 1, newNSCache(), updateLists, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, "benchmark-timestamp", auditExportPublishingState))
	}
}

func setAuditGlobalsForBenchmark(rootDir string) func() {
	oldAPICacheDir := *apiCacheDir
	oldExportEnabled := *exportutil.ExportEnabled
	oldEmitAuditEvents := *emitAuditEvents
	oldLogStatsAudit := *logStatsAudit

	*apiCacheDir = rootDir
	*exportutil.ExportEnabled = false
	*emitAuditEvents = false
	*logStatsAudit = false

	return func() {
		*apiCacheDir = oldAPICacheDir
		*exportutil.ExportEnabled = oldExportEnabled
		*emitAuditEvents = oldEmitAuditEvents
		*logStatsAudit = oldLogStatsAudit
	}
}

func benchmarkAuditManager(tb testing.TB) *Manager {
	tb.Helper()

	driver, err := rego.New()
	require.NoError(tb, err)
	opaClient, err := constraintclient.NewClient(constraintclient.Targets(&target.K8sValidationTarget{}), constraintclient.Driver(driver), constraintclient.EnforcementPoints([]string{util.AuditEnforcementPoint}...))
	require.NoError(tb, err)

	return &Manager{
		opa:             opaClient,
		expansionSystem: expansion.NewSystem(nil),
		log:             logr.Discard(),
	}
}

func benchmarkNamespaceObjects(count int) []unstructured.Unstructured {
	objects := make([]unstructured.Unstructured, count)
	for i := range objects {
		objects[i] = unstructured.Unstructured{Object: map[string]interface{}{
			benchmarkAPIVersionKey: "v1",
			benchmarkKindKey:       benchmarkNamespaceKind,
			benchmarkMetadataKey: map[string]interface{}{
				benchmarkNameKey: "namespace-" + strconv.Itoa(i),
			},
		}}
	}
	return objects
}

func writeBenchmarkPerObjectFiles(tb testing.TB, directory string, objects []unstructured.Unstructured) {
	tb.Helper()

	for i := range objects {
		jsonBytes, err := objects[i].MarshalJSON()
		require.NoError(tb, err)
		require.NoError(tb, os.WriteFile(path.Join(directory, strconv.Itoa(i)), jsonBytes, 0o600))
	}
}

func TestShouldAuditKind(t *testing.T) {
	for _, tc := range []struct {
		name         string
		matchedKinds map[string]bool
		kind         string
		want         bool
	}{
		{name: "explicit match", matchedKinds: map[string]bool{"Pod": true}, kind: "Pod", want: true},
		{name: "explicit mismatch", matchedKinds: map[string]bool{"Pod": true}, kind: "ConfigMap"},
		{name: "wildcard", matchedKinds: map[string]bool{"*": true}, kind: "ConfigMap", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAuditKind(tc.matchedKinds, tc.kind); got != tc.want {
				t.Fatalf("shouldAuditKind(%v, %q) = %t, want %t", tc.matchedKinds, tc.kind, got, tc.want)
			}
		})
	}
}

func BenchmarkAuditSkippedKindAvoidsCacheCleanup(b *testing.B) {
	const staleDirs = 100
	am := benchmarkAuditManager(b)
	matchedKinds := map[string]bool{"Pod": true}

	b.Run("old_order_cleanup_before_skip", func(b *testing.B) {
		root := b.TempDir()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			populateAuditCacheDirs(b, root, staleDirs)
			if err := am.removeAllFromDir(root, *auditChunkSize); err != nil {
				b.Fatal(err)
			}
			if shouldAuditKind(matchedKinds, "ConfigMap") {
				b.Fatal("expected ConfigMap to be skipped")
			}
		}
	})

	b.Run("new_order_skip_before_cleanup", func(b *testing.B) {
		root := b.TempDir()
		populateAuditCacheDirs(b, root, staleDirs)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if !shouldAuditKind(matchedKinds, "ConfigMap") {
				continue
			}
			if err := am.removeAllFromDir(root, *auditChunkSize); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func populateAuditCacheDirs(tb testing.TB, root string, count int) {
	tb.Helper()
	for i := 0; i < count; i++ {
		dir := path.Join(root, "stale-"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(path.Join(dir, auditObjectsFile), []byte("[]"), 0o600); err != nil {
			tb.Fatal(err)
		}
	}
}
