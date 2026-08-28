package target

import (
	"errors"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestMatcherMatchReusesParsedObjectsAcrossCalls(t *testing.T) {
	t.Parallel()

	review := &gkReview{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object:    runtime.RawExtension{Raw: matchedRawData()},
			OldObject: runtime.RawExtension{Raw: unmatchedRawData()},
		},
		namespace: makeNamespace("my-ns", map[string]string{"ns": "label"}),
		source:    types.SourceTypeOriginal,
	}
	m := &Matcher{match: fooMatch(), cache: newNsCache()}

	got, err := m.Match(review)
	if err != nil {
		t.Fatalf("first Match() unexpected error: %v", err)
	}
	if !got {
		t.Fatal("first Match() = false, want true")
	}

	firstObject := review.parsedObjects.object
	firstOldObject := review.parsedObjects.oldObject
	if firstObject == nil {
		t.Fatal("object was not cached")
	}
	if firstOldObject == nil {
		t.Fatal("oldObject was not cached")
	}

	// If the second Match reparses, these invalid raw payloads will fail. Keeping
	// the first parsed values proves that per-constraint matching reuses the
	// request-local parse cache and leaves the match view immutable for the
	// lifetime of this gkReview.
	review.Object.Raw = []byte(`{`)
	review.OldObject.Raw = []byte(`{`)

	got, err = m.Match(review)
	if err != nil {
		t.Fatalf("second Match() unexpected error: %v", err)
	}
	if !got {
		t.Fatal("second Match() = false, want true")
	}
	if review.parsedObjects.object != firstObject {
		t.Fatal("object cache was replaced between Match calls")
	}
	if review.parsedObjects.oldObject != firstOldObject {
		t.Fatal("oldObject cache was replaced between Match calls")
	}
}

func TestMatcherMatchCachesNamespaceLookupAcrossCalls(t *testing.T) {
	t.Parallel()

	ns := makeNamespace("foo", map[string]string{"ns": "label"})
	review := &gkReview{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "foo",
			Object:    runtime.RawExtension{Raw: namespacedRawData("foo")},
		},
		source: types.SourceTypeOriginal,
	}
	m := &Matcher{match: namespaceSelectorMatch(), cache: newNsCache()}
	key := clusterScopedKey(corev1.SchemeGroupVersion.WithKind("Namespace"), ns.Name)
	m.cache.AddNamespace(toKey(key), ns)

	got, err := m.Match(review)
	if err != nil {
		t.Fatalf("first Match() unexpected error: %v", err)
	}
	if !got {
		t.Fatal("first Match() = false, want true")
	}
	if review.cachedNamespace != ns {
		t.Fatal("namespace lookup was not cached on the review")
	}

	m.cache.RemoveNamespace(toKey(key))
	got, err = m.Match(review)
	if err != nil {
		t.Fatalf("second Match() unexpected error: %v", err)
	}
	if !got {
		t.Fatal("second Match() = false, want true from cached namespace")
	}
	if review.cachedNamespace != ns {
		t.Fatal("cached namespace was replaced between Match calls")
	}
}

func TestMatcherMatchDeleteUsesCachedOldObjectSemantics(t *testing.T) {
	t.Parallel()

	review := &gkReview{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Delete,
			OldObject: runtime.RawExtension{Raw: matchedRawData()},
		},
		namespace: makeNamespace("my-ns", map[string]string{"ns": "label"}),
		source:    types.SourceTypeOriginal,
	}
	if err := setObjectOnDelete(review); err != nil {
		t.Fatalf("setObjectOnDelete() unexpected error: %v", err)
	}
	if review.Object.Raw == nil {
		t.Fatal("DELETE review object was not populated from oldObject")
	}

	m := &Matcher{match: fooMatch(), cache: newNsCache()}
	got, err := m.Match(review)
	if err != nil {
		t.Fatalf("first Match() unexpected error: %v", err)
	}
	if !got {
		t.Fatal("first Match() = false, want true")
	}
	if review.parsedObjects.object == nil {
		t.Fatal("DELETE object was not parsed from oldObject")
	}
	if review.parsedObjects.oldObject == nil {
		t.Fatal("DELETE oldObject was not parsed")
	}
	if review.parsedObjects.object.GetName() != review.parsedObjects.oldObject.GetName() {
		t.Fatalf("DELETE object name = %q, oldObject name = %q, want same", review.parsedObjects.object.GetName(), review.parsedObjects.oldObject.GetName())
	}

	review.Object.Raw = []byte(`{`)
	review.OldObject.Raw = []byte(`{`)
	got, err = m.Match(review)
	if err != nil {
		t.Fatalf("second Match() unexpected error: %v", err)
	}
	if !got {
		t.Fatal("second Match() = false, want true")
	}
}

func TestMatcherMatchCachesUnmarshalError(t *testing.T) {
	t.Parallel()

	review := &gkReview{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: runtime.RawExtension{Raw: []byte(`{`)},
		},
	}
	m := &Matcher{match: fooMatch(), cache: newNsCache()}

	_, err := m.Match(review)
	if !errors.Is(err, ErrRequestObject) {
		t.Fatalf("first Match() error = %v, want %v", err, ErrRequestObject)
	}
	firstErr := review.parsedObjects.err
	if firstErr == nil {
		t.Fatal("unmarshal error was not cached")
	}

	review.Object.Raw = matchedRawData()
	_, err = m.Match(review)
	if !errors.Is(err, ErrRequestObject) {
		t.Fatalf("second Match() error = %v, want cached %v", err, ErrRequestObject)
	}
	if !errors.Is(review.parsedObjects.err, firstErr) {
		t.Fatal("cached unmarshal error was replaced between Match calls")
	}
}

func BenchmarkMatcherMatchRequestLocalParseCache(b *testing.B) {
	const matchesPerReview = 8

	ns := makeNamespace("my-ns", map[string]string{"ns": "label"})
	objectRaw := matchedRawData()
	oldObjectRaw := unmatchedRawData()
	matchers := make([]*Matcher, matchesPerReview)
	for i := range matchers {
		matchers[i] = &Matcher{match: fooMatch(), cache: newNsCache()}
	}

	newReview := func() *gkReview {
		return &gkReview{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Object:    runtime.RawExtension{Raw: objectRaw},
				OldObject: runtime.RawExtension{Raw: oldObjectRaw},
			},
			namespace: ns,
			source:    types.SourceTypeOriginal,
		}
	}

	b.Run("cached_shared_review", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			review := newReview()
			for _, matcher := range matchers {
				got, err := matcher.Match(review)
				if err != nil {
					b.Fatal(err)
				}
				if !got {
					b.Fatal("Match() = false, want true")
				}
			}
		}
	})

	b.Run("uncached_same_review", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			review := newReview()
			for _, matcher := range matchers {
				got, err := matchWithoutParseCacheForBenchmark(matcher, review)
				if err != nil {
					b.Fatal(err)
				}
				if !got {
					b.Fatal("Match() = false, want true")
				}
			}
		}
	})
}

func matchWithoutParseCacheForBenchmark(m *Matcher, review *gkReview) (bool, error) {
	obj, oldObj, ns, err := gkReviewToObjectWithoutCacheForBenchmark(review)
	if err != nil {
		return false, err
	}
	return matchAny(m, ns, review.source, obj, oldObj)
}

func gkReviewToObjectWithoutCacheForBenchmark(req *gkReview) (*unstructured.Unstructured, *unstructured.Unstructured, *corev1.Namespace, error) {
	obj, err := unmarshalReviewObject("object", req.Object.Raw)
	if err != nil {
		return nil, nil, nil, err
	}

	oldObj, err := unmarshalReviewObject("oldObject", req.OldObject.Raw)
	if err != nil {
		return nil, nil, nil, err
	}

	return obj, oldObj, req.namespace, nil
}
