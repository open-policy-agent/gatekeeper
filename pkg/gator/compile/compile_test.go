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
}

func TestCompileWithExplicitWorkingDir(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("testdata", "policy-repo")
	templatePath := filepath.Join(repoRoot, "src", "general", "samplepolicy", "constraint.tmpl")

	output, err := Compile(Options{
		TemplatePath: templatePath,
		WorkingDir:   repoRoot,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if !strings.Contains(output, "package samplepolicy") {
		t.Fatalf("expected compiled rego in output, got:\n%s", output)
	}
}

func TestCompileInjectRegoIntoScaffold(t *testing.T) {
	t.Parallel()

	templatePath := filepath.Join("testdata", "scaffold", "template.yaml")
	regoPath := filepath.Join("testdata", "scaffold", "policy.rego")

	output, err := Compile(Options{
		TemplatePath: templatePath,
		RegoPaths:    []string{regoPath},
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if !strings.Contains(output, "package scaffoldpolicy") {
		t.Fatalf("expected injected rego in output, got:\n%s", output)
	}
	if !strings.Contains(output, "name: scaffoldpolicy") {
		t.Fatalf("expected metadata.name preserved in output, got:\n%s", output)
	}
	if !strings.Contains(output, "kind: ScaffoldPolicy") {
		t.Fatalf("expected spec.crd.spec.names.kind preserved in output, got:\n%s", output)
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

func TestRenderGomplateSnippetsMissingFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	content := `{{ file.Read "missing.rego" | strings.Indent 2 | strings.TrimSuffix "\n" }}`
	if err := os.WriteFile(filepath.Join(tmpDir, "constraint.tmpl"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Compile(Options{
		TemplatePath: filepath.Join(tmpDir, "constraint.tmpl"),
		WorkingDir:   tmpDir,
	})
	if err == nil {
		t.Fatal("expected error for missing referenced file")
	}
}
