package gktest

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type versionless interface {
	ToVersionless() (*templates.ConstraintTemplate, error)
}

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

// TODO(willbeason): Remove once ToVersionless() is threadsafe.
var versionlessMtx sync.Mutex

// readTemplate reads the contents of the path and returns the
// ConstraintTemplate it defines. Returns an error if the file does not define
// a ConstraintTemplate.
func readTemplate(scheme *runtime.Scheme, f fs.FS, path string) (*templates.ConstraintTemplate, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading ConstraintTemplate from %q: %w", path, err)
	}

	u, err := readUnstructured(bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing ConstraintTemplate YAML from %q: %v", ErrAddingTemplate, path, err)
	}

	gvk := u.GroupVersionKind()
	if gvk.Group != templatesv1.SchemeGroupVersion.Group || gvk.Kind != "ConstraintTemplate" {
		return nil, fmt.Errorf("%w: %q", ErrNotATemplate, path)
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

	versionlessMtx.Lock()
	template, err := v.ToVersionless()
	versionlessMtx.Unlock()
	if err != nil {
		// This shouldn't happen unless there's a bug in the conversion functions.
		// Most likely it means the conversion functions weren't generated.
		return nil, fmt.Errorf("%w: %v", ErrConvertingTemplate, err)
	}

	return template, nil
}

func readObject(f fs.FS, path string) (*unstructured.Unstructured, error) {
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
	u, err := readObject(f, path)
	if err != nil {
		return nil, err
	}

	gvk := u.GroupVersionKind()
	if gvk.Group != "constraints.gatekeeper.sh" {
		return nil, ErrNotAConstraint
	}

	return u, nil
}
