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
