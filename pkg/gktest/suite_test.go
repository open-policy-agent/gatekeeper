package gktest

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
)

const (
	templateAlwaysValidate = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
spec:
  crd:
    spec:
      names:
        kind: AlwaysValidate
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation {
          false
        }
`

	templateUnsupportedVersion = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta2
metadata:
  name: unsupportedversion
spec:
  crd:
    spec:
      names:
        kind: UnsupportedVersion
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation {
          false
        }
`

	templateInvalidYAML = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
  {}: {}
spec:
  crd:
    spec:
      names:
        kind: AlwaysValidate
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation {
          false
        }
`

	templateMarshalError = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
spec: [a, b, c]
`

	templateCompileError = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: compileerror
spec:
  crd:
    spec:
      names:
        kind: CompileError
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation {
          f
        }
`

	constraintAlwaysValidate = `
kind: AlwaysValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-pass
`

	constraintInvalidYAML = `
kind: AlwaysValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-pass
  {}: {}
`

	constraintWrongTemplate = `
kind: Other
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: other
`
)

func TestSuite_Run(t *testing.T) {
	testCases := []struct {
		name  string
		suite Suite
		f     fs.FS
		want  []Result
	}{
		{
			name:  "Suite missing Template",
			suite: Suite{},
			f:     fstest.MapFS{},
			want: []Result{
				errorResult(ErrInvalidSuite),
			},
		},
		{
			name: "Suite with template in nonexistent file",
			suite: Suite{
				Template: "template.yaml",
			},
			f: fstest.MapFS{},
			want: []Result{
				errorResult(fs.ErrNotExist),
			},
		},
		{
			name: "Suite with YAML parsing error",
			suite: Suite{
				Template: "template.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateInvalidYAML),
				},
			},
			want: []Result{
				errorResult(ErrAddingTemplate),
			},
		},
		{
			name: "Suite with template unmarshalling error",
			suite: Suite{
				Template: "template.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateMarshalError),
				},
			},
			want: []Result{
				errorResult(ErrAddingTemplate),
			},
		},
		{
			name: "Suite with rego compilation error",
			suite: Suite{
				Template: "template.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateCompileError),
				},
			},
			want: []Result{
				errorResult(ErrAddingTemplate),
			},
		},
		{
			name: "Suite with unsupported template version",
			suite: Suite{
				Template: "template.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateUnsupportedVersion),
				},
			},
			want: []Result{
				errorResult(ErrAddingTemplate),
			},
		},
		{
			name: "Suite pointing to non-template",
			suite: Suite{
				Template: "template.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(constraintAlwaysValidate),
				},
			},
			want: []Result{
				errorResult(ErrNotATemplate),
			},
		},
		{
			name: "Suite missing Constraint",
			suite: Suite{
				Template: "template.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
			},
			want: []Result{
				errorResult(ErrInvalidSuite),
			},
		},
		{
			name: "valid Suite",
			suite: Suite{
				Template:   "template.yaml",
				Constraint: "constraint.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintAlwaysValidate),
				},
			},
			want: []Result{},
		},
		{
			name: "constraint missing file",
			suite: Suite{
				Template:   "template.yaml",
				Constraint: "constraint.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
			},
			want: []Result{errorResult(fs.ErrNotExist)},
		},
		{
			name: "constraint invalid YAML",
			suite: Suite{
				Template:   "template.yaml",
				Constraint: "constraint.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintInvalidYAML),
				},
			},
			want: []Result{errorResult(ErrAddingConstraint)},
		},
		{
			name: "constraint is not a constraint",
			suite: Suite{
				Template:   "template.yaml",
				Constraint: "constraint.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
			},
			want: []Result{errorResult(ErrNotAConstraint)},
		},
		{
			name: "constraint is for other template",
			suite: Suite{
				Template:   "template.yaml",
				Constraint: "constraint.yaml",
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(templateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(constraintWrongTemplate),
				},
			},
			want: []Result{errorResult(ErrAddingConstraint)},
		},
	}

	for _, tc := range testCases {
		ctx := context.Background()

		c, err := NewOPAClient()
		if err != nil {
			t.Fatal(err)
		}

		t.Run(tc.name, func(t *testing.T) {
			got := tc.suite.Run(ctx, c, tc.f, Filter{})

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}
