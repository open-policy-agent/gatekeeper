package verify

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"k8s.io/utils/pointer"
)

func TestReadSuites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		target       string
		originalPath string
		recursive    bool
		fileSystem   fs.FS
		want         []*Suite
		wantErr      error
	}{
		{
			name:       "no filesystem",
			target:     "test.yaml",
			recursive:  false,
			fileSystem: nil,
			want:       nil,
			wantErr:    ErrNoFileSystem,
		},
		{
			name:       "empty filesystem",
			target:     "test.yaml",
			recursive:  false,
			fileSystem: fstest.MapFS{},
			want:       nil,
			wantErr:    fs.ErrNotExist,
		},
		{
			name:       "empty target",
			target:     "",
			recursive:  false,
			fileSystem: fstest.MapFS{},
			want:       nil,
			wantErr:    ErrNoTarget,
		},
		{
			name:      "non yaml file",
			target:    "test.txt",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.txt": &fstest.MapFile{
					Data: []byte(""),
				},
			},
			want:    nil,
			wantErr: ErrUnsupportedExtension,
		},
		{
			name:      "single target",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "test.yaml"}},
			wantErr: nil,
		},
		{
			name:         "single target absolute path",
			target:       "test.yaml",
			originalPath: "/test.yaml",
			recursive:    false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "test.yaml", InputPath: "/test.yaml"}},
			wantErr: nil,
		},
		{
			name:         "single target relative path",
			target:       "test.yaml",
			originalPath: "test.yaml",
			recursive:    false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "test.yaml", InputPath: "test.yaml"}},
			wantErr: nil,
		},
		{
			name:         "single target relative path ./",
			target:       "test.yaml",
			originalPath: "./test.yaml",
			recursive:    false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "test.yaml", InputPath: "./test.yaml"}},
			wantErr: nil,
		},
		{
			name:         "single target relative path ../",
			target:       "test.yaml",
			originalPath: "../test.yaml",
			recursive:    false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "test.yaml", InputPath: "../test.yaml"}},
			wantErr: nil,
		},
		{
			name:         "single target relative path ../../",
			target:       "test.yaml",
			originalPath: "../../test.yaml",
			recursive:    false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "test.yaml", InputPath: "../../test.yaml"}},
			wantErr: nil,
		},
		{
			name:      "invalid filepath",
			target:    "test/../test/test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    nil,
			wantErr: fs.ErrNotExist,
		},
		{
			name:      "single target wrong Kind",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Role
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    nil,
			wantErr: nil,
		},
		{
			name:      "single target wrong Group",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: v1alpha1
`),
				},
			},
			want:    nil,
			wantErr: nil,
		},
		{
			name:      "single target invalid yaml",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
{a: b}: 3`),
				},
			},
			want:    nil,
			wantErr: gator.ErrInvalidYAML,
		},
		{
			name:      "directory",
			target:    "tests",
			recursive: false,
			fileSystem: fstest.MapFS{
				"tests/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "tests/test.yaml"}},
			wantErr: nil,
		},
		{
			name:      "directory with tests and non-tests",
			target:    "tests",
			recursive: false,
			fileSystem: fstest.MapFS{
				"tests/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
				"tests/other-file": &fstest.MapFile{
					Data: []byte(`some data`),
				},
			},
			want:    []*Suite{{AbsolutePath: "tests/test.yaml"}},
			wantErr: nil,
		},
		{
			name:      "non-recursive directory with subdirectory",
			target:    "tests",
			recursive: false,
			fileSystem: fstest.MapFS{
				"tests/annotations/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
				"tests/labels/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
				"tests/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "tests/test.yaml"}},
			wantErr: nil,
		},
		{
			name:         "recursive directory with subdirectory",
			target:       "tests",
			originalPath: "./tests",
			recursive:    true,
			fileSystem: fstest.MapFS{
				"tests/annotations/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
				"tests/labels/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
				"tests/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want: []*Suite{
				{AbsolutePath: "tests/annotations/test.yaml", InputPath: "./tests/annotations/test.yaml"},
				{AbsolutePath: "tests/labels/test.yaml", InputPath: "./tests/labels/test.yaml"},
				{AbsolutePath: "tests/test.yaml", InputPath: "./tests/test.yaml"},
			},
			wantErr: nil,
		},
		{
			name:      "recursive directory with subdirectory with '.yaml'",
			target:    "tests",
			recursive: true,
			fileSystem: fstest.MapFS{
				"tests/labels.yaml/test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    []*Suite{{AbsolutePath: "tests/labels.yaml/test.yaml"}},
			wantErr: nil,
		},
		{
			name:      "recursive file is an error",
			target:    "test.yaml",
			recursive: true,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
`),
				},
			},
			want:    nil,
			wantErr: ErrNotADirectory,
		},
		{
			name:      "suite with test and cases",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
tests:
- template: template.yaml
  constraint: constraint.yaml
  cases:
  - object: allow.yaml
  - object: deny.yaml
    assertions:
    - violations: 2
      message: "some message"
`),
				},
			},
			want: []*Suite{{
				AbsolutePath: "test.yaml",
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Object: "allow.yaml",
					}, {
						Object: "deny.yaml",
						Assertions: []Assertion{{
							Violations: gator.IntStrFromInt(2),
							Message:    pointer.String("some message"),
						}},
					}},
				}},
			}},
			wantErr: nil,
		},
		{
			name:      "skip case",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
tests:
- template: template.yaml
  constraint: constraint.yaml
  cases:
  - object: allow.yaml
    skip: true
  - object: deny.yaml
    assertions:
    - violations: 2
      message: "some message"
`),
				},
			},
			want: []*Suite{{
				AbsolutePath: "test.yaml",
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Object: "allow.yaml",
						Skip:   true,
					}, {
						Object: "deny.yaml",
						Assertions: []Assertion{{
							Violations: gator.IntStrFromInt(2),
							Message:    pointer.String("some message"),
						}},
					}},
				}},
			}},
			wantErr: nil,
		},
		{
			name:      "skip test",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
tests:
- template: template.yaml
  constraint: constraint.yaml
  skip: true
  cases:
  - object: allow.yaml
  - object: deny.yaml
    assertions:
    - violations: 2
      message: "some message"
`),
				},
			},
			want: []*Suite{{
				AbsolutePath: "test.yaml",
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Skip:       true,
					Cases: []*Case{{
						Object: "allow.yaml",
					}, {
						Object: "deny.yaml",
						Assertions: []Assertion{{
							Violations: gator.IntStrFromInt(2),
							Message:    pointer.String("some message"),
						}},
					}},
				}},
			}},
			wantErr: nil,
		},
		{
			name:      "skip suite",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
skip: true
tests:
- template: template.yaml
  constraint: constraint.yaml
  cases:
  - object: allow.yaml
  - object: deny.yaml
    assertions:
    - violations: 2
      message: "some message"
`),
				},
			},
			want: []*Suite{{
				AbsolutePath: "test.yaml",
				Skip:         true,
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Object: "allow.yaml",
					}, {
						Object: "deny.yaml",
						Assertions: []Assertion{{
							Violations: gator.IntStrFromInt(2),
							Message:    pointer.String("some message"),
						}},
					}},
				}},
			}},
			wantErr: nil,
		},
		{
			name:      "suite with empty assertions",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
tests:
- template: template.yaml
  constraint: constraint.yaml
  cases:
  - object: allow.yaml
  - object: deny.yaml
    assertions:
    - violations: "yes"
  - object: referential.yaml
    inventory: [inventory.yaml]
`),
				},
			},
			want: []*Suite{{
				AbsolutePath: "test.yaml",
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Object: "allow.yaml",
					}, {
						Object: "deny.yaml",
						Assertions: []Assertion{{
							Violations: gator.IntStrFromStr("yes"),
						}},
					}, {
						Object:    "referential.yaml",
						Inventory: []string{"inventory.yaml"},
					}},
				}},
			}},
			wantErr: nil,
		},
		{
			name:      "suite with tests and no cases",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
tests:
- template: template.yaml
  constraint: constraint.yaml
`),
				},
			},
			want: []*Suite{{
				AbsolutePath: "test.yaml",
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			}},
			wantErr: nil,
		},
		{
			name:      "invalid suite",
			target:    "test.yaml",
			recursive: false,
			fileSystem: fstest.MapFS{
				"test.yaml": &fstest.MapFile{
					Data: []byte(`
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
tests: {}
`),
				},
			},
			wantErr: gator.ErrInvalidYAML,
		},
	}

	for _, tc := range testCases {
		// Required for parallel tests.
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				got    []*Suite
				gotErr error
				ignore cmp.Option
			)
			// for tests that don't mimic a relative path test mode
			if tc.originalPath == "" {
				got, gotErr = ReadSuites(tc.fileSystem, tc.target, tc.target, tc.recursive)
				ignore = cmpopts.IgnoreFields(Suite{}, "InputPath")
			} else {
				got, gotErr = ReadSuites(tc.fileSystem, tc.target, tc.originalPath, tc.recursive)
			}

			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("got error %v, want error %v",
					gotErr, tc.wantErr)
			}

			if diff := cmp.Diff(tc.want, got, cmpopts.EquateEmpty(), cmpopts.IgnoreUnexported(Assertion{}), ignore); diff != "" {
				t.Error(diff)
			}
		})
	}
}
