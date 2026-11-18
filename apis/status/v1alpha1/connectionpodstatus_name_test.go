package v1alpha1_test

import (
	"strings"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/apis/status/v1beta1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
)

func TestKeyForConnectionNameBoundaries(t *testing.T) {
	tests := []struct {
		name         string
		pod          string
		namespace    string
		connection   string
		legacyLength int
		want         string
	}{
		{
			name: "ordinary legacy key",
			pod:  "some-gk-pod", namespace: "a-gk-ns", connection: "audit",
			legacyLength: 29,
			want:         "some--gk--pod-a--gk--ns-audit",
		},
		{
			name: "maximum unescaped components",
			pod:  strings.Repeat("p", 63), namespace: strings.Repeat("n", 63), connection: strings.Repeat("c", 63),
			legacyLength: 191,
		},
		{
			name: "252 byte legacy key",
			pod:  strings.Repeat("p", 63), namespace: "n" + strings.Repeat("-", 61) + "n", connection: strings.Repeat("c", 63),
			legacyLength: 252,
		},
		{
			name: "253 byte legacy key",
			pod:  "p-" + strings.Repeat("p", 61), namespace: "n" + strings.Repeat("-", 61) + "n", connection: strings.Repeat("c", 63),
			legacyLength: 253,
		},
		{
			name: "254 byte legacy key",
			pod:  "p--" + strings.Repeat("p", 60), namespace: "n" + strings.Repeat("-", 61) + "n", connection: strings.Repeat("c", 63),
			legacyLength: 254,
		},
		{
			name: "dash expansion in the default namespace",
			pod:  "p" + strings.Repeat("-", 61) + "p", namespace: "gatekeeper-system", connection: "c" + strings.Repeat("-", 61) + "c",
			legacyLength: 268,
			want:         "connection-29868b8ad5a78129ec364439add044673db70114c7ca1143501f5ea7fe46cf01",
		},
		{
			name: "dash expansion in a custom namespace",
			pod:  "p" + strings.Repeat("-", 61) + "p", namespace: "n" + strings.Repeat("-", 61) + "n", connection: "c" + strings.Repeat("-", 61) + "c",
			legacyLength: 374,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legacy, err := v1beta1.DashPacker(tt.pod, tt.namespace, tt.connection)
			require.NoError(t, err)
			require.Len(t, legacy, tt.legacyLength)

			key, err := v1alpha1.KeyForConnection(tt.pod, tt.namespace, tt.connection)
			require.NoError(t, err)
			require.LessOrEqual(t, len(key), validation.DNS1123SubdomainMaxLength)
			require.Empty(t, validation.IsDNS1123Subdomain(key))
			if tt.legacyLength <= validation.DNS1123SubdomainMaxLength {
				require.Equal(t, legacy, key, "valid legacy keys must not change")
			} else {
				require.Len(t, key, len("connection-")+64)
				// A legacy three-component key has at least two separators.
				require.Equal(t, 1, strings.Count(key, "-"), "hashed keys must not overlap the legacy format")
			}
			if tt.want != "" {
				require.Equal(t, tt.want, key)
			}
			again, err := v1alpha1.KeyForConnection(tt.pod, tt.namespace, tt.connection)
			require.NoError(t, err)
			require.Equal(t, key, again)

			t.Setenv("POD_NAMESPACE", tt.namespace)
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: tt.pod, Namespace: tt.namespace, UID: "pod-uid"}}
			status, err := v1alpha1.NewConnectionStatusForPod(pod, tt.namespace, tt.connection, scheme)
			require.NoError(t, err)
			require.Equal(t, key, status.Name)
			require.Equal(t, tt.namespace, status.Namespace)
			require.Equal(t, tt.connection, status.Labels[v1beta1.ConnectionNameLabel])
			require.Equal(t, tt.pod, status.Labels[v1beta1.PodLabel])
			require.Equal(t, tt.pod, status.Status.ID)
		})
	}
}

func TestKeyForConnectionDistinctNames(t *testing.T) {
	pod := "p" + strings.Repeat("-", 61) + "p"
	namespace := "n" + strings.Repeat("-", 61) + "n"
	connection := "c" + strings.Repeat("-", 61) + "c"
	shortPod := "p" + strings.Repeat("-", 59) + "p"
	shortNamespace := "n" + strings.Repeat("-", 57) + "n"
	tuples := [][3]string{
		{pod, namespace, connection},
		{pod[:62] + "q", namespace, connection},
		{pod, namespace[:62] + "o", connection},
		{pod, namespace, connection[:62] + "d"},
		{shortPod + "-a", shortNamespace, connection},
		{shortPod, "a-" + shortNamespace, connection},
	}
	keys := make(map[string]bool)
	for _, tuple := range tuples {
		key, err := v1alpha1.KeyForConnection(tuple[0], tuple[1], tuple[2])
		require.NoError(t, err)
		require.LessOrEqual(t, len(key), validation.DNS1123SubdomainMaxLength)
		require.False(t, keys[key], "different pod/namespace/connection tuples must have different keys")
		keys[key] = true
	}
}

func TestKeyForConnectionInvalidComponents(t *testing.T) {
	for _, invalid := range []string{"", "-invalid", "invalid-"} {
		for i := range 3 {
			tuple := []string{"pod", "namespace", "connection"}
			tuple[i] = invalid
			_, err := v1alpha1.KeyForConnection(tuple[0], tuple[1], tuple[2])
			require.Error(t, err)
		}
	}
}
