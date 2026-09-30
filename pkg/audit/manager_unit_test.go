package audit

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
)

func Test_firstNonSpaceByte(t *testing.T) {
	tcs := []struct {
		name    string
		input   string
		want    byte
		wantErr error
	}{
		{name: "returns the first byte when it is not whitespace", input: "abc", want: 'a'},
		{name: "skips leading whitespace", input: " \n\t\rX", want: 'X'},
		{name: "returns EOF on empty input", input: "", wantErr: io.EOF},
		{name: "returns EOF on whitespace-only input", input: " \n\t\r", wantErr: io.EOF},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			b, err := firstNonSpaceByte(bufio.NewReader(strings.NewReader(tc.input)))
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, b)
		})
	}
}

func Test_violationMsg(t *testing.T) {
	constraint := &unstructured.Unstructured{}
	constraint.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"})
	constraint.SetName("must-have-owner")
	constraint.SetNamespace("")
	constraint.SetAnnotations(map[string]string{
		"kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"..."}`,
		"keep-me": "yes",
	})

	resourceGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	details := map[string]interface{}{"missing_labels": []string{"owner"}}
	rlabels := map[string]string{"app": "web"}

	got := violationMsg(constraint, util.Deny, []string{"deny"}, resourceGVK, "default", "my-pod", "message", details, rlabels, "2026-01-01T00:00:00Z")

	msg, ok := got.(exportutil.ExportMsg)
	require.True(t, ok)

	require.Equal(t, "message", msg.Message)
	require.Equal(t, details, msg.Details)
	require.Equal(t, "2026-01-01T00:00:00Z", msg.ID)
	require.Equal(t, "violation_audited", msg.EventType)
	require.Equal(t, "constraints.gatekeeper.sh", msg.Group)
	require.Equal(t, "v1beta1", msg.Version)
	require.Equal(t, "K8sRequiredLabels", msg.Kind)
	require.Equal(t, "must-have-owner", msg.Name)
	require.Equal(t, "", msg.Namespace)
	require.Equal(t, string(util.Deny), msg.EnforcementAction)
	require.Equal(t, []string{"deny"}, msg.EnforcementActions)
	require.Equal(t, "", msg.ResourceGroup)
	require.Equal(t, "v1", msg.ResourceAPIVersion)
	require.Equal(t, "Pod", msg.ResourceKind)
	require.Equal(t, "default", msg.ResourceNamespace)
	require.Equal(t, "my-pod", msg.ResourceName)
	require.Equal(t, rlabels, msg.ResourceLabels)

	// The last-applied-configuration annotation is noisy and must be stripped
	// from the exported message, but other annotations should be preserved.
	// GetAnnotations returns a copy, so this doesn't mutate the constraint.
	require.Equal(t, map[string]string{"keep-me": "yes"}, msg.ConstraintAnnotations)
}

func Test_emitEvent(t *testing.T) {
	constraint := &unstructured.Unstructured{}
	constraint.SetGroupVersionKind(schema.GroupVersionKind{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels"})
	constraint.SetName("must-have-owner")
	resourceGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}

	tcs := []struct {
		name             string
		involvedNS       bool
		rnamespace       string
		wantEventNS      string
		wantMessageParts []string
	}{
		{
			name:             "events default to gatekeeper's namespace",
			involvedNS:       false,
			rnamespace:       "app-ns",
			wantEventNS:      "gatekeeper-system",
			wantMessageParts: []string{"Resource Namespace: app-ns", "Constraint: must-have-owner", "Message: some violation"},
		},
		{
			name:             "events can be emitted in the resource's namespace",
			involvedNS:       true,
			rnamespace:       "app-ns",
			wantEventNS:      "app-ns",
			wantMessageParts: []string{"Constraint: must-have-owner", "Message: some violation"},
		},
	}

	orig := *auditEventsInvolvedNamespace
	defer func() { *auditEventsInvolvedNamespace = orig }()

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			*auditEventsInvolvedNamespace = tc.involvedNS
			recorder := record.NewFakeRecorder(1)

			emitEvent(constraint, "2026-01-01T00:00:00Z", util.Deny, "deny", resourceGVK,
				tc.rnamespace, "my-pod", "1", "some violation", "gatekeeper-system", types.UID("abc-123"), recorder)

			select {
			case event := <-recorder.Events:
				require.Contains(t, event, "Warning AuditViolation")
				for _, part := range tc.wantMessageParts {
					require.Contains(t, event, part)
				}
			default:
				t.Fatal("expected an event to be recorded")
			}
		})
	}
}

func Test_logStart(t *testing.T) {
	var got string
	l := funcr.New(func(_, args string) { got = args }, funcr.Options{})

	logStart(l)

	require.Contains(t, got, `"event_type"="audit_started"`)
	require.Contains(t, got, `"semantic"=true`)
}

func Test_logFinish(t *testing.T) {
	var got string
	l := funcr.New(func(_, args string) { got = args }, funcr.Options{})

	logFinish(l, 2*time.Second)

	require.Contains(t, got, `"event_type"="audit_finished"`)
	require.Contains(t, got, `"duration"="2s"`)
}

func Test_logConstraint(t *testing.T) {
	var got string
	l := funcr.New(func(_, args string) { got = args }, funcr.Options{})

	logConstraint(l, &util.KindVersionName{Group: "constraints.gatekeeper.sh", Version: "v1beta1", Kind: "K8sRequiredLabels", Name: "must-have-owner", Namespace: "default"}, "deny", 3)

	require.Contains(t, got, `"constraint_group"="constraints.gatekeeper.sh"`)
	require.Contains(t, got, `"constraint_kind"="K8sRequiredLabels"`)
	require.Contains(t, got, `"constraint_name"="must-have-owner"`)
	require.Contains(t, got, `"constraint_namespace"="default"`)
	require.Contains(t, got, `"constraint_action"="deny"`)
	require.Contains(t, got, `"constraint_violations"="3"`)
}

func Test_SVQueue_Less(t *testing.T) {
	base := StatusViolation{
		Group:             "a",
		Version:           "a",
		Kind:              "a",
		Namespace:         "a",
		Name:              "a",
		Message:           "a",
		EnforcementAction: "a",
	}

	// Less is a greater-than comparison on each field in turn, falling
	// through to the next field only when the current one is equal.
	withHigher := func(mutate func(sv *StatusViolation)) StatusViolation {
		sv := base
		mutate(&sv)
		return sv
	}

	tcs := []struct {
		name string
		i    StatusViolation
		j    StatusViolation
		want bool
	}{
		{name: "equal is not less", i: base, j: base, want: false},
		{name: "group breaks the tie", i: withHigher(func(sv *StatusViolation) { sv.Group = "b" }), j: base, want: true},
		{name: "version breaks the tie when group is equal", i: withHigher(func(sv *StatusViolation) { sv.Version = "b" }), j: base, want: true},
		{name: "kind breaks the tie when group and version are equal", i: withHigher(func(sv *StatusViolation) { sv.Kind = "b" }), j: base, want: true},
		{name: "namespace breaks the tie next", i: withHigher(func(sv *StatusViolation) { sv.Namespace = "b" }), j: base, want: true},
		{name: "name breaks the tie next", i: withHigher(func(sv *StatusViolation) { sv.Name = "b" }), j: base, want: true},
		{name: "message breaks the tie next", i: withHigher(func(sv *StatusViolation) { sv.Message = "b" }), j: base, want: true},
		{name: "enforcement action is the final tiebreaker", i: withHigher(func(sv *StatusViolation) { sv.EnforcementAction = "b" }), j: base, want: true},
		{name: "reverse of a true case is false", i: base, j: withHigher(func(sv *StatusViolation) { sv.EnforcementAction = "b" }), want: false},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			svq := SVQueue{&tc.i, &tc.j}
			require.Equal(t, tc.want, svq.Less(0, 1))
		})
	}
}

func Test_SVQueue_Push(t *testing.T) {
	svq := make(SVQueue, 0)

	svq.Push("not a *StatusViolation")
	require.Empty(t, svq, "Push must ignore values that aren't *StatusViolation")

	sv := &StatusViolation{Kind: "Pod"}
	svq.Push(sv)
	require.Equal(t, SVQueue{sv}, svq)
}
