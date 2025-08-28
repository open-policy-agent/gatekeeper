package reader

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	gvkmanifestv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/gvkmanifest/v1alpha1"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type versionless interface {
	ToVersionless() (*templates.ConstraintTemplate, error)
}

// jsonLookaheadBytes is the number of bytes the JSON and YAML decoder will
// look into the data it's reading to determine if the document is JSON or
// YAML.  1024 was a guess that's worked so far.
const jsonLookaheadBytes int = 1024

// clean removes the following from yaml:
// 1) Empty lines
// 2) Lines with only space characters
// 3) Lines which are only comments
//
// This prevents us from attempting to parse an empty yaml document and failing.
func clean(yaml string) string {
	lines := strings.Split(yaml, "\n")
	result := strings.Builder{}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 || strings.HasPrefix(trimmed, "#") {
			continue
		}

		result.WriteString(line)
		result.WriteString("\n")
	}

	return result.String()
}

func ReadUnstructureds(bytes []byte) ([]*unstructured.Unstructured, error) {
	splits := strings.Split(string(bytes), "\n---")
	var result []*unstructured.Unstructured

	for _, split := range splits {
		split = clean(split)
		if len(split) == 0 {
			continue
		}

		u, err := ReadUnstructured([]byte(split))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", gator.ErrInvalidYAML, err)
		}

		result = append(result, u)
	}

	return result, nil
}

func ReadUnstructured(bytes []byte) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	err := gator.ParseYaml(bytes, u)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ReadTemplate reads the contents of the path and returns the
// ConstraintTemplate it defines. Returns an error if the file does not define
// a ConstraintTemplate.
func ReadTemplate(scheme *runtime.Scheme, f fs.FS, path string) (*templates.ConstraintTemplate, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading ConstraintTemplate from %q: %w", path, err)
	}

	u, err := ReadUnstructured(bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing ConstraintTemplate YAML from %q: %w", gator.ErrAddingTemplate, path, err)
	}

	template, err := ToTemplate(scheme, u)
	if err != nil {
		return nil, fmt.Errorf("path %q: %w", path, err)
	}
	return template, nil
}

// ToStructured converts an unstructured object into an object with the schema defined
// by u's group, version, and kind.
func ToStructured(scheme *runtime.Scheme, u *unstructured.Unstructured) (runtime.Object, error) {
	gvk := u.GroupVersionKind()
	t, err := scheme.New(gvk)
	if err != nil {
		// The type isn't registered in the scheme.
		return nil, err
	}

	// YAML parsing doesn't properly handle ObjectMeta, so we must
	// marshal/unmashal through JSON.
	jsonBytes, err := u.MarshalJSON()
	if err != nil {
		// Indicates a bug in unstructured.MarshalJSON(). Any Unstructured
		// unmarshalled from YAML should be marshallable to JSON.
		return nil, fmt.Errorf("calling unstructured.MarshalJSON(): %w", err)
	}
	err = json.Unmarshal(jsonBytes, t)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ToTemplate converts an unstructured template into a versionless ConstraintTemplate struct.
func ToTemplate(scheme *runtime.Scheme, u *unstructured.Unstructured) (*templates.ConstraintTemplate, error) {
	if u.GroupVersionKind().Group != templatesv1.SchemeGroupVersion.Group || u.GroupVersionKind().Kind != "ConstraintTemplate" {
		return nil, fmt.Errorf("%w", gator.ErrNotATemplate)
	}

	t, err := ToStructured(scheme, u)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", gator.ErrAddingTemplate, err)
	}

	v, isVersionless := t.(versionless)
	if !isVersionless {
		return nil, fmt.Errorf("%w: %T", gator.ErrConvertingTemplate, t)
	}

	template, err := v.ToVersionless()
	if err != nil {
		// This shouldn't happen unless there's a bug in the conversion functions.
		// Most likely it means the conversion functions weren't generated.
		return nil, fmt.Errorf("%w: %w", gator.ErrConvertingTemplate, err)
	}

	return template, nil
}

// ToSyncSet converts an unstructured SyncSet into a SyncSet struct.
func ToSyncSet(scheme *runtime.Scheme, u *unstructured.Unstructured) (*syncsetv1alpha1.SyncSet, error) {
	if u.GroupVersionKind().Group != syncsetv1alpha1.GroupVersion.Group || u.GroupVersionKind().Kind != "SyncSet" {
		return nil, fmt.Errorf("%w", gator.ErrNotASyncSet)
	}

	s, err := ToStructured(scheme, u)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", gator.ErrAddingSyncSet, err)
	}

	syncSet, isSyncSet := s.(*syncsetv1alpha1.SyncSet)
	if !isSyncSet {
		return nil, fmt.Errorf("%w: %T", gator.ErrAddingSyncSet, syncSet)
	}

	return syncSet, nil
}

// ToConfig converts an unstructured Config into a Config struct.
func ToConfig(scheme *runtime.Scheme, u *unstructured.Unstructured) (*configv1alpha1.Config, error) {
	if u.GroupVersionKind().Group != configv1alpha1.GroupVersion.Group || u.GroupVersionKind().Kind != "Config" {
		return nil, fmt.Errorf("%w", gator.ErrNotAConfig)
	}

	s, err := ToStructured(scheme, u)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", gator.ErrAddingConfig, err)
	}

	config, isConfig := s.(*configv1alpha1.Config)
	if !isConfig {
		return nil, fmt.Errorf("%w: %T", gator.ErrAddingConfig, config)
	}

	return config, nil
}

// ToGVKManifest converts an unstructured GVKManifest into a GVKManifest struct.
func ToGVKManifest(scheme *runtime.Scheme, u *unstructured.Unstructured) (*gvkmanifestv1alpha1.GVKManifest, error) {
	if u.GroupVersionKind().Group != gvkmanifestv1alpha1.GroupVersion.Group || u.GroupVersionKind().Kind != "GVKManifest" {
		return nil, fmt.Errorf("%w", gator.ErrNotAGVKManifest)
	}

	s, err := ToStructured(scheme, u)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", gator.ErrAddingGVKManifest, err)
	}

	gvkManifest, isGVKManifest := s.(*gvkmanifestv1alpha1.GVKManifest)
	if !isGVKManifest {
		return nil, fmt.Errorf("%w: %T", gator.ErrAddingGVKManifest, gvkManifest)
	}

	return gvkManifest, nil
}

// ReadObject reads a file from the filesystem abstraction at the specified
// path, and returns an unstructured.Unstructured object if the file can be
// successfully unmarshalled.
func ReadObject(f fs.FS, path string) (*unstructured.Unstructured, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading Constraint from %q: %w", path, err)
	}

	u, err := ReadUnstructured(bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing Constraint from %q: %w", gator.ErrAddingConstraint, path, err)
	}

	return u, nil
}

func ReadConstraint(f fs.FS, path string) (*unstructured.Unstructured, error) {
	u, err := ReadObject(f, path)
	if err != nil {
		return nil, err
	}

	gvk := u.GroupVersionKind()
	if gvk.Group != "constraints.gatekeeper.sh" {
		return nil, gator.ErrNotAConstraint
	}

	return u, nil
}

func ReadExpansions(f fs.FS, path string) ([]*unstructured.Unstructured, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading ExpansionTemplate from %q: %w", path, err)
	}
	objs, err := ReadUnstructureds(bytes)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	if len(objs) == 0 {
		return nil, fmt.Errorf("%w: %q", gator.ErrNotAnExpansion, path)
	}

	for _, obj := range objs {
		gvk := obj.GroupVersionKind()
		if gvk.Group != "expansion.gatekeeper.sh" || gvk.Kind != "ExpansionTemplate" {
			return nil, fmt.Errorf("%w: %q", gator.ErrNotAnExpansion, path)
		}
	}

	return objs, nil
}

// ReadK8sResources reads JSON or YAML k8s resources from an io.Reader,
// decoding them into Unstructured objects and returning those objects as a
// slice.
func ReadK8sResources(r io.Reader) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured

	decoder := yaml.NewYAMLOrJSONDecoder(r, jsonLookaheadBytes)
	for {
		u := &unstructured.Unstructured{
			Object: make(map[string]interface{}),
		}
		err := decoder.Decode(&u.Object)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading yaml source: %w", err)
		}
		if err = gator.FixYAML(u.Object, &u.Object); err != nil {
			return nil, fmt.Errorf("passing yaml through json: %w", err)
		}

		// skip empty resources
		if len(u.Object) > 0 {
			objs = append(objs, u)
		}
	}

	return objs, nil
}

func IsTemplate(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == templatesv1.SchemeGroupVersion.Group && gvk.Kind == "ConstraintTemplate"
}

func IsConfig(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == configv1alpha1.GroupVersion.Group && gvk.Kind == "Config"
}

func IsSyncSet(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == syncsetv1alpha1.GroupVersion.Group && gvk.Kind == "SyncSet"
}

func IsGVKManifest(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == gvkmanifestv1alpha1.GroupVersion.Group && gvk.Kind == "GVKManifest"
}

func IsConstraint(u *unstructured.Unstructured) bool {
	gvk := u.GroupVersionKind()
	return gvk.Group == "constraints.gatekeeper.sh"
}
