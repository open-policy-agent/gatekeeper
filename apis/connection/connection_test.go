package connection_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/apis/connection/unversioned"
	connectionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1alpha1"
	connectionv1beta1 "github.com/open-policy-agent/gatekeeper/v3/apis/connection/v1beta1"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/yaml"
)

func TestConnectionConversionRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		config *types.Anything
	}{
		{name: "nil"},
		{name: "null", config: &types.Anything{}},
		{name: "empty object", config: &types.Anything{Value: map[string]interface{}{}}},
		{name: "nested unknown values", config: nestedConfig()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := connectionScheme(t)
			alpha := connectionFixture(tt.config)
			internal := &unversioned.Connection{}
			require.NoError(t, scheme.Convert(alpha, internal, nil))
			beta := &connectionv1beta1.Connection{}
			require.NoError(t, scheme.Convert(internal, beta, nil))
			require.Equal(t, alpha.ObjectMeta, beta.ObjectMeta)
			require.Equal(t, alpha.Spec.Driver, beta.Spec.Driver)
			require.Equal(t, alpha.Spec.Config, beta.Spec.Config)
			require.Equal(t, alpha.Status.ByPod, beta.Status.ByPod)

			roundTripInternal := &unversioned.Connection{}
			require.NoError(t, scheme.Convert(beta, roundTripInternal, nil))
			roundTripAlpha := &connectionv1alpha1.Connection{}
			require.NoError(t, scheme.Convert(roundTripInternal, roundTripAlpha, nil))
			require.Equal(t, alpha, roundTripAlpha)
		})
	}
}

func TestConnectionListConversionRoundTrip(t *testing.T) {
	for _, items := range [][]connectionv1alpha1.Connection{nil, {}, {*connectionFixture(nestedConfig())}} {
		scheme := connectionScheme(t)
		alpha := &connectionv1alpha1.ConnectionList{
			ListMeta: metav1.ListMeta{ResourceVersion: "42", Continue: "next"},
			Items:    items,
		}
		internal := &unversioned.ConnectionList{}
		require.NoError(t, scheme.Convert(alpha, internal, nil))
		beta := &connectionv1beta1.ConnectionList{}
		require.NoError(t, scheme.Convert(internal, beta, nil))
		require.Equal(t, alpha.ListMeta, beta.ListMeta)
		require.Len(t, beta.Items, len(items))

		roundTripInternal := &unversioned.ConnectionList{}
		require.NoError(t, scheme.Convert(beta, roundTripInternal, nil))
		roundTripAlpha := &connectionv1alpha1.ConnectionList{}
		require.NoError(t, scheme.Convert(roundTripInternal, roundTripAlpha, nil))
		require.Equal(t, alpha, roundTripAlpha)
	}
}

func TestConnectionSchemeAndSerialization(t *testing.T) {
	scheme := connectionScheme(t)
	alpha := connectionFixture(nestedConfig())
	internal := &unversioned.Connection{}
	require.NoError(t, scheme.Convert(alpha, internal, nil))
	beta := &connectionv1beta1.Connection{}
	require.NoError(t, scheme.Convert(internal, beta, nil))

	for _, tt := range []struct {
		object runtime.Object
		gvk    schema.GroupVersionKind
	}{
		{object: alpha, gvk: connectionv1alpha1.GroupVersion.WithKind("Connection")},
		{object: beta, gvk: connectionv1beta1.GroupVersion.WithKind("Connection")},
		{object: &connectionv1alpha1.ConnectionList{Items: []connectionv1alpha1.Connection{*alpha}}, gvk: connectionv1alpha1.GroupVersion.WithKind("ConnectionList")},
		{object: &connectionv1beta1.ConnectionList{Items: []connectionv1beta1.Connection{*beta}}, gvk: connectionv1beta1.GroupVersion.WithKind("ConnectionList")},
	} {
		t.Run(tt.gvk.String(), func(t *testing.T) {
			kinds, unversionedType, err := scheme.ObjectKinds(tt.object)
			require.NoError(t, err)
			require.False(t, unversionedType)
			require.Equal(t, []schema.GroupVersionKind{tt.gvk}, kinds)
			created, err := scheme.New(tt.gvk)
			require.NoError(t, err)
			require.IsType(t, tt.object, created)

			codecs := serializer.NewCodecFactory(scheme)
			encoded, err := runtime.Encode(codecs.LegacyCodec(tt.gvk.GroupVersion()), tt.object)
			require.NoError(t, err)
			decoded, gvk, err := codecs.UniversalDeserializer().Decode(encoded, nil, nil)
			require.NoError(t, err)
			require.Equal(t, tt.gvk, *gvk)
			expected := tt.object.DeepCopyObject()
			expected.GetObjectKind().SetGroupVersionKind(tt.gvk)
			require.IsType(t, expected, decoded)
			require.True(t, apiequality.Semantic.DeepEqual(expected, decoded))
		})
	}
}

func TestConnectionDeepCopy(t *testing.T) {
	const changedValue = "changed"
	scheme := connectionScheme(t)
	internal := &unversioned.Connection{}
	require.NoError(t, scheme.Convert(connectionFixture(nestedConfig()), internal, nil))
	beta := &connectionv1beta1.Connection{}
	require.NoError(t, scheme.Convert(internal, beta, nil))
	copied := beta.DeepCopy()

	copiedConfig, ok := copied.Spec.Config.Value.(map[string]interface{})
	require.True(t, ok)
	copiedNested, ok := copiedConfig["nested"].(map[string]interface{})
	require.True(t, ok)
	copiedArray, ok := copiedNested["array"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, copiedArray)
	copied.Labels["test"] = changedValue
	copiedArray[0] = changedValue
	copied.Status.ByPod[0].Operations[0] = changedValue
	copied.Status.ByPod[0].ConnectionErrors[0].Message = changedValue
	copied.Status.ByPod[0].PublishStatuses[0].Errors[0].Message = changedValue
	copied.Status.ByPod[0].PublishStatuses[0].LastSuccessTime.Time = time.Time{}

	require.Equal(t, "original", beta.Labels["test"])
	require.Equal(t, nestedConfig(), beta.Spec.Config)
	require.Equal(t, connectionFixture(nil).Status.ByPod, beta.Status.ByPod)
}

func TestConnectionCRDVersions(t *testing.T) {
	crdDirectory := filepath.Join("..", "..", "config", "crd", "bases")
	_, err := os.Stat(filepath.Join(crdDirectory, "_connections.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist, "internal Connection types must not generate a CRD")
	data, err := os.ReadFile(filepath.Join(crdDirectory, "connection.gatekeeper.sh_connections.yaml"))
	require.NoError(t, err)
	crd := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(t, yaml.UnmarshalStrict(data, crd))
	require.Equal(t, connectionv1beta1.GroupVersion.Group, crd.Spec.Group)
	require.Equal(t, apiextensionsv1.NamespaceScoped, crd.Spec.Scope)
	require.Equal(t, "Connection", crd.Spec.Names.Kind)
	require.Equal(t, 2, len(crd.Spec.Versions), "regenerate the Connection CRD after changing API versions")

	versions := make(map[string]apiextensionsv1.CustomResourceDefinitionVersion)
	storageVersions := []string{}
	for _, version := range crd.Spec.Versions {
		versions[version.Name] = version
		require.True(t, version.Served, "%s must remain served", version.Name)
		require.NotNil(t, version.Schema)
		require.NotNil(t, version.Schema.OpenAPIV3Schema)
		require.NotNil(t, version.Subresources)
		require.NotNil(t, version.Subresources.Status)
		if version.Storage {
			storageVersions = append(storageVersions, version.Name)
		}
	}
	require.Equal(t, []string{connectionv1beta1.GroupVersion.Version}, storageVersions)
	require.Contains(t, versions, connectionv1alpha1.GroupVersion.Version)
	require.Contains(t, versions, connectionv1beta1.GroupVersion.Version)
	require.Equal(t, versions["v1alpha1"].Schema, versions["v1beta1"].Schema, "both versions must retain the same wire contract")
	if crd.Spec.Conversion != nil {
		require.Equal(t, apiextensionsv1.NoneConverter, crd.Spec.Conversion.Strategy)
		require.Nil(t, crd.Spec.Conversion.Webhook)
	}
	config := versions["v1beta1"].Schema.OpenAPIV3Schema.Properties["spec"].Properties["config"]
	require.NotNil(t, config.XPreserveUnknownFields)
	require.True(t, *config.XPreserveUnknownFields)
}

func connectionScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, apis.AddToScheme(scheme))
	return scheme
}

func nestedConfig() *types.Anything {
	return &types.Anything{Value: map[string]interface{}{
		"path":            "/violations/topics",
		"maxAuditResults": float64(3),
		"nested": map[string]interface{}{
			"array": []interface{}{"value", float64(7), true, nil, map[string]interface{}{"unknown": "kept"}},
			"empty": map[string]interface{}{},
		},
	}}
}

func connectionFixture(config *types.Anything) *connectionv1alpha1.Connection {
	timestamp := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return &connectionv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "audit-connection",
			Namespace:       "gatekeeper-system",
			UID:             "connection-uid",
			ResourceVersion: "42",
			Generation:      3,
			Labels:          map[string]string{"test": "original"},
			Annotations:     map[string]string{"test": "preserved"},
		},
		Spec: connectionv1alpha1.ConnectionSpec{Driver: "disk", Config: config},
		Status: connectionv1alpha1.ConnectionStatus{ByPod: []statusv1alpha1.ConnectionPodStatusStatus{{
			ID:                 "audit-pod",
			ConnectionUID:      "connection-uid",
			Operations:         []string{"audit", "status"},
			ObservedGeneration: 3,
			ConnectionErrors:   []*statusv1alpha1.ConnectionError{{Type: statusv1alpha1.UpsertConnectionError, Message: "configuration error"}},
			PublishStatuses: []statusv1alpha1.ConnectionPublishStatus{
				{
					Source:          statusv1alpha1.AuditPublishSource,
					Active:          true,
					LastAttemptTime: &timestamp,
					LastSuccessTime: &timestamp,
					Errors:          []*statusv1alpha1.ConnectionError{{Type: statusv1alpha1.PublishError, Message: "publish error"}},
				},
				{Source: statusv1alpha1.WebhookPublishSource},
			},
		}}},
	}
}
