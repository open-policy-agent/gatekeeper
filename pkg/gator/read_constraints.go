package gator

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"

	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
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

func readUnstructureds(bytes []byte) ([]*unstructured.Unstructured, error) {
	splits := strings.Split(string(bytes), "\n---")
	var result []*unstructured.Unstructured

	for _, split := range splits {
		split = clean(split)
		if len(split) == 0 {
			continue
		}

		u, err := readUnstructured([]byte(split))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidYAML, err)
		}

		result = append(result, u)
	}

	return result, nil
}

func readUnstructured(bytes []byte) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	err := parseYAML(bytes, u)
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

	u, err := readUnstructured(bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing ConstraintTemplate YAML from %q: %v", ErrAddingTemplate, path, err)
	}

	template, err := ToTemplate(scheme, u)
	if err != nil {
		return nil, fmt.Errorf("path %q: %w", path, err)
	}
	return template, nil
}

// TODO (https://github.com/open-policy-agent/gatekeeper/issues/1779): Move
// this function into a location that makes it more obviously a shared resource
// between `gator test` and `gator verify`

// ToTemplate converts an unstructured template into a versionless ConstraintTemplate struct.
func ToTemplate(scheme *runtime.Scheme, u *unstructured.Unstructured) (*templates.ConstraintTemplate, error) {
	gvk := u.GroupVersionKind()
	if gvk.Group != templatesv1.SchemeGroupVersion.Group || gvk.Kind != "ConstraintTemplate" {
		return nil, fmt.Errorf("%w", ErrNotATemplate)
	}

	t, err := scheme.New(gvk)
	if err != nil {
		// The type isn't registered in the scheme.
		return nil, fmt.Errorf("%w: %v", ErrAddingTemplate, err)
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
		return nil, fmt.Errorf("%w: %v", ErrAddingTemplate, err)
	}

	v, isVersionless := t.(versionless)
	if !isVersionless {
		return nil, fmt.Errorf("%w: %T", ErrConvertingTemplate, t)
	}

	template, err := v.ToVersionless()
	if err != nil {
		// This shouldn't happen unless there's a bug in the conversion functions.
		// Most likely it means the conversion functions weren't generated.
		return nil, fmt.Errorf("%w: %v", ErrConvertingTemplate, err)
	}

	return template, nil
}

// ReadObject reads a file from the filesystem abstraction at the specified
// path, and returns an unstructured.Unstructured object if the file can be
// successfully unmarshalled.
func ReadObject(f fs.FS, path string) (*unstructured.Unstructured, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading Constraint from %q: %w", path, err)
	}

	u, err := readUnstructured(bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing Constraint from %q: %v", ErrAddingConstraint, path, err)
	}

	return u, nil
}

func readConstraint(f fs.FS, path string) (*unstructured.Unstructured, error) {
	u, err := ReadObject(f, path)
	if err != nil {
		return nil, err
	}

	gvk := u.GroupVersionKind()
	if gvk.Group != "constraints.gatekeeper.sh" {
		return nil, ErrNotAConstraint
	}

	return u, nil
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
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading yaml source: %w", err)
		}

		objs = append(objs, u)
	}

	return objs, nil
}
