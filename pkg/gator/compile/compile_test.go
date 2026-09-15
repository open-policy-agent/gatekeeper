package compile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileFromConstraintTmpl(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "policy-repo")
	sourceDir := filepath.Join(repoRoot, "src", "general", "samplepolicy")

	output, err := Compile(Options{
		SourceDir: sourceDir,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if !strings.Contains(output, "package samplepolicy") {
		t.Fatalf("expected compiled rego in output, got:\n%s", output)
	}
	if !strings.Contains(output, "kind: ConstraintTemplate") {
		t.Fatalf("expected ConstraintTemplate kind in output")
	}
	if strings.Contains(output, "file.Read") {
		t.Fatalf("gomplate snippets were not rendered:\n%s", output)
	}
	if strings.Contains(output, "{{") {
		t.Fatalf("unexpanded template expression left in output:\n%s", output)
	}
}

func TestCompileWithExplicitWorkingDir(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "policy-repo")
	sourceDir := filepath.Join(repoRoot, "src", "general", "samplepolicy")

	output, err := Compile(Options{
		SourceDir:  sourceDir,
		WorkingDir: repoRoot,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if !strings.Contains(output, "package samplepolicy") {
		t.Fatalf("expected compiled rego in output, got:\n%s", output)
	}
}

func TestCompileMissingTemplate(t *testing.T) {
	t.Parallel()

	_, err := Compile(Options{
		SourceDir: filepath.Join("testdata", "does-not-exist"),
	})
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}

func TestCompileRequiresSourceDir(t *testing.T) {
	t.Parallel()

	_, err := Compile(Options{})
	if err == nil {
		t.Fatal("expected error when --source-dir is omitted")
	}
}

func TestInferRepoRoot(t *testing.T) {
	t.Parallel()

	root, err := inferRepoRoot(filepath.Join("testdata", "policy-repo", "src", "general", "samplepolicy"))
	if err != nil {
		t.Fatalf("inferRepoRoot() error = %v", err)
	}

	want := filepath.Join("testdata", "policy-repo")
	if root != want {
		absWant, _ := filepath.Abs(want)
		absRoot, _ := filepath.Abs(root)
		if absWant != absRoot {
			t.Fatalf("inferRepoRoot() = %q, want %q", root, want)
		}
	}
}

func TestIndentLines(t *testing.T) {
	t.Parallel()

	got := indentLines("line1\nline2\n", 4)
	want := "    line1\n    line2"
	if got != want {
		t.Fatalf("indentLines() = %q, want %q", got, want)
	}
}

func TestCompileMissingReferencedFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourceDir := filepath.Join(repoRoot, "src", "general", "broken")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	content := `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: broken
spec:
  crd:
    spec:
      names:
        kind: Broken
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: Rego
          source:
            rego: |
{{ file.Read "src/general/broken/missing.rego" | strings.Indent 14 | strings.TrimSuffix "\n" }}
`
	if err := os.WriteFile(filepath.Join(sourceDir, "constraint.tmpl"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Compile(Options{SourceDir: sourceDir})
	if err == nil {
		t.Fatal("expected error for missing referenced file")
	}
}

func TestCompileRejectsPathEscape(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourceDir := filepath.Join(repoRoot, "src", "general", "escape")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	content := `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: escape
spec:
  crd:
    spec:
      names:
        kind: Escape
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: Rego
          source:
            rego: |
{{ file.Read "../outside.rego" | strings.Indent 14 | strings.TrimSuffix "\n" }}
`
	if err := os.WriteFile(filepath.Join(sourceDir, "constraint.tmpl"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Compile(Options{SourceDir: sourceDir})
	if err == nil {
		t.Fatal("expected error for path escape")
	}
	if !strings.Contains(err.Error(), "escapes the working directory") {
		t.Fatalf("expected path escape error, got: %v", err)
	}
}

func TestCompileRejectsUnsupportedExpression(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourceDir := filepath.Join(repoRoot, "src", "general", "unsupported")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	content := `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: unsupported
spec:
  crd:
    spec:
      names:
        kind: Unsupported
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: Rego
          source:
            rego: |
{{ env.Getenv "HOME" }}
`
	if err := os.WriteFile(filepath.Join(sourceDir, "constraint.tmpl"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Compile(Options{SourceDir: sourceDir})
	if err == nil {
		t.Fatal("expected error for unsupported template expression")
	}
	if !strings.Contains(err.Error(), "unsupported or unexpanded") {
		t.Fatalf("expected unsupported expression error, got: %v", err)
	}
}

func TestCompileInvalidRegoFails(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	sourceDir := filepath.Join(repoRoot, "src", "general", "badrego")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	tmpl := `apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: badrego
spec:
  crd:
    spec:
      names:
        kind: BadRego
  targets:
    - target: admission.k8s.gatekeeper.sh
      code:
        - engine: Rego
          source:
            rego: |
{{ file.Read "src/general/badrego/src.rego" | strings.Indent 14 | strings.TrimSuffix "\n" }}
`
	if err := os.WriteFile(filepath.Join(sourceDir, "constraint.tmpl"), []byte(tmpl), 0o600); err != nil {
		t.Fatalf("WriteFile() constraint.tmpl: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "src.rego"), []byte("package badrego\n\nthis is not valid rego !!!"), 0o600); err != nil {
		t.Fatalf("WriteFile() src.rego: %v", err)
	}

	_, err := Compile(Options{SourceDir: sourceDir})
	if err == nil {
		t.Fatal("expected error for invalid rego")
	}
	if !strings.Contains(err.Error(), "compiling ConstraintTemplate") {
		t.Fatalf("expected compile/validation error, got: %v", err)
	}
}
