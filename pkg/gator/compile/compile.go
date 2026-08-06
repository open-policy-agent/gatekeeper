package compile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	"k8s.io/apimachinery/pkg/runtime"
)

// Supported constraint.tmpl expressions (gatekeeper-library layout):
//
//	{{ file.Read "<path>" | strings.Indent <n> | strings.TrimSuffix "\n" }}
//
// <path> is resolved relative to the working directory (repo root). Paths that
// escape the working directory are rejected. Any other {{ ... }} expression is
// unsupported and causes compile to fail.
var fileReadPattern = regexp.MustCompile(`\{\{\s*file\.Read\s+"([^"]+)"\s*\|\s*strings\.Indent\s+(\d+)\s*\|\s*strings\.TrimSuffix\s+"\\n"\s*\}\}`)

var leftoverTemplatePattern = regexp.MustCompile(`\{\{`)

// Options configures ConstraintTemplate compilation.
type Options struct {
	// SourceDir is a policy source directory containing constraint.tmpl.
	SourceDir string
	// WorkingDir is the repository root used to resolve file.Read paths.
	// Inferred from a parent src/ directory when omitted.
	WorkingDir string
}

// Compile renders a ConstraintTemplate from a gatekeeper-library source dir.
func Compile(opts Options) (string, error) {
	templatePath, workingDir, err := resolvePaths(opts)
	if err != nil {
		return "", err
	}

	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("reading template %q: %w", templatePath, err)
	}

	content, err := renderGomplateSnippets(string(templateBytes), workingDir)
	if err != nil {
		return "", err
	}

	if leftoverTemplatePattern.MatchString(content) {
		return "", fmt.Errorf("unsupported or unexpanded template expression in %q; only {{ file.Read \"path\" | strings.Indent N | strings.TrimSuffix \"\\n\" }} is supported", templatePath)
	}

	if err := validateCompiledTemplate(content); err != nil {
		return "", err
	}

	return content, nil
}

func resolvePaths(opts Options) (templatePath, workingDir string, err error) {
	if opts.SourceDir == "" {
		return "", "", fmt.Errorf("--source-dir must be specified")
	}

	templatePath = filepath.Join(opts.SourceDir, "constraint.tmpl")
	workingDir = opts.WorkingDir
	if workingDir == "" {
		workingDir, err = inferRepoRoot(opts.SourceDir)
		if err != nil {
			return "", "", err
		}
	}

	if _, err := os.Stat(templatePath); err != nil {
		return "", "", fmt.Errorf("template not found at %q: %w", templatePath, err)
	}

	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", "", fmt.Errorf("resolving working directory: %w", err)
	}

	return templatePath, absWorkingDir, nil
}

// inferRepoRoot walks up from a src/<category>/<policy> directory to the repository root.
func inferRepoRoot(sourceDir string) (string, error) {
	absSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", fmt.Errorf("resolving source directory: %w", err)
	}

	dir := absSourceDir
	for {
		if filepath.Base(dir) == "src" {
			return filepath.Dir(dir), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not infer repository root from %q; set --working-dir explicitly", sourceDir)
}

func renderGomplateSnippets(content, workingDir string) (string, error) {
	var renderErr error
	rendered := fileReadPattern.ReplaceAllStringFunc(content, func(match string) string {
		if renderErr != nil {
			return match
		}

		submatches := fileReadPattern.FindStringSubmatch(match)
		if len(submatches) != 3 {
			renderErr = fmt.Errorf("invalid file.Read snippet: %s", match)
			return match
		}

		relPath := submatches[1]
		indentSpaces, err := parseIndent(submatches[2])
		if err != nil {
			renderErr = err
			return match
		}

		filePath, err := resolveUnderWorkingDir(workingDir, relPath)
		if err != nil {
			renderErr = err
			return match
		}

		fileBytes, err := os.ReadFile(filePath)
		if err != nil {
			renderErr = fmt.Errorf("reading %q: %w", filePath, err)
			return match
		}

		return indentLines(string(fileBytes), indentSpaces)
	})

	if renderErr != nil {
		return "", renderErr
	}

	return rendered, nil
}

func resolveUnderWorkingDir(workingDir, relPath string) (string, error) {
	fromSlash := filepath.FromSlash(relPath)
	if filepath.IsAbs(fromSlash) {
		return "", fmt.Errorf("file.Read path %q must be relative to the working directory", relPath)
	}

	cleaned := filepath.Clean(fromSlash)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file.Read path %q escapes the working directory", relPath)
	}

	full := filepath.Join(workingDir, cleaned)
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("resolving file.Read path %q: %w", relPath, err)
	}

	rel, err := filepath.Rel(workingDir, absFull)
	if err != nil {
		return "", fmt.Errorf("resolving file.Read path %q: %w", relPath, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file.Read path %q escapes the working directory", relPath)
	}

	return absFull, nil
}

func parseIndent(raw string) (int, error) {
	var indent int
	if _, err := fmt.Sscanf(raw, "%d", &indent); err != nil {
		return 0, fmt.Errorf("parsing indent value %q: %w", raw, err)
	}
	if indent < 0 {
		return 0, fmt.Errorf("indent must be non-negative, got %d", indent)
	}
	return indent, nil
}

func indentLines(content string, spaces int) string {
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return ""
	}

	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func validateCompiledTemplate(content string) error {
	scheme := runtime.NewScheme()
	if err := apis.AddToScheme(scheme); err != nil {
		return fmt.Errorf("building scheme: %w", err)
	}

	u, err := reader.ReadUnstructured([]byte(content))
	if err != nil {
		return fmt.Errorf("parsing compiled ConstraintTemplate: %w", err)
	}

	template, err := reader.ToTemplate(scheme, u)
	if err != nil {
		return fmt.Errorf("decoding compiled ConstraintTemplate: %w", err)
	}

	client, err := gator.NewOPAClient(false, gator.WithK8sCEL())
	if err != nil {
		return fmt.Errorf("creating gator client: %w", err)
	}

	if _, err := client.AddTemplate(context.Background(), template); err != nil {
		return fmt.Errorf("compiling ConstraintTemplate %q: %w", template.GetName(), err)
	}

	return nil
}
