package compile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// fileReadPattern matches gatekeeper-library gomplate snippets used in constraint.tmpl files.
var fileReadPattern = regexp.MustCompile(`\{\{\s*file\.Read\s+"([^"]+)"\s*\|\s*strings\.Indent\s+(\d+)\s*\|\s*strings\.TrimSuffix\s+"\\n"\s*\}\}`)

// Options configures ConstraintTemplate compilation.
type Options struct {
	// TemplatePath is the path to a constraint.tmpl or template scaffold YAML file.
	TemplatePath string
	// SourceDir is a policy source directory containing constraint.tmpl (gatekeeper-library layout).
	SourceDir string
	// WorkingDir is the repository root used to resolve file.Read paths in constraint.tmpl.
	WorkingDir string
	// RegoPaths inject Rego source into a template scaffold when no gomplate snippets are present.
	RegoPaths []string
	// CelPath injects CEL source into a template scaffold for the K8sNativeValidation engine.
	CelPath string
}

// Compile renders a ConstraintTemplate manifest from source files.
func Compile(opts Options) (string, error) {
	templatePath, workingDir, err := resolvePaths(opts)
	if err != nil {
		return "", err
	}

	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("reading template %q: %w", templatePath, err)
	}

	content := string(templateBytes)
	if fileReadPattern.MatchString(content) {
		content, err = renderGomplateSnippets(content, workingDir)
		if err != nil {
			return "", err
		}
	} else if len(opts.RegoPaths) > 0 || opts.CelPath != "" {
		content, err = injectSources(content, opts.RegoPaths, opts.CelPath)
		if err != nil {
			return "", err
		}
	}

	if err := validateConstraintTemplateYAML(content); err != nil {
		return "", err
	}

	return content, nil
}

func resolvePaths(opts Options) (templatePath, workingDir string, err error) {
	switch {
	case opts.SourceDir != "":
		templatePath = filepath.Join(opts.SourceDir, "constraint.tmpl")
		workingDir = opts.WorkingDir
		if workingDir == "" {
			workingDir, err = inferRepoRoot(opts.SourceDir)
			if err != nil {
				return "", "", err
			}
		}
	case opts.TemplatePath != "":
		templatePath = opts.TemplatePath
		workingDir = opts.WorkingDir
		if workingDir == "" {
			workingDir = inferWorkingDirFromTemplate(templatePath)
		}
	default:
		return "", "", fmt.Errorf("either --source-dir or --filename must be specified")
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

func inferWorkingDirFromTemplate(templatePath string) string {
	absTemplatePath, err := filepath.Abs(templatePath)
	if err != nil {
		return filepath.Dir(templatePath)
	}

	dir := filepath.Dir(absTemplatePath)
	for {
		if filepath.Base(dir) == "src" {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(absTemplatePath)
		}
		dir = parent
	}
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

		filePath := filepath.Join(workingDir, filepath.FromSlash(relPath))
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

type constraintTemplate struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Spec       struct {
		Targets []target `yaml:"targets"`
	} `yaml:"spec"`
}

type target struct {
	Target string `yaml:"target"`
	Code   []codeBlock `yaml:"code"`
}

type codeBlock struct {
	Engine string `yaml:"engine"`
	Source map[string]interface{} `yaml:"source"`
}

func injectSources(content string, regoPaths []string, celPath string) (string, error) {
	var doc constraintTemplate
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return "", fmt.Errorf("parsing template yaml: %w", err)
	}
	if doc.Kind != "ConstraintTemplate" {
		return "", fmt.Errorf("expected ConstraintTemplate, got %q", doc.Kind)
	}

	regoBody, err := readCombinedSources(regoPaths)
	if err != nil {
		return "", err
	}

	var celBody string
	if celPath != "" {
		celBytes, err := os.ReadFile(celPath)
		if err != nil {
			return "", fmt.Errorf("reading cel file %q: %w", celPath, err)
		}
		celBody = strings.TrimSuffix(string(celBytes), "\n")
	}

	regoInjected := false
	celInjected := false
	for i := range doc.Spec.Targets {
		for j := range doc.Spec.Targets[i].Code {
			block := &doc.Spec.Targets[i].Code[j]
			switch block.Engine {
			case "Rego":
				if regoBody == "" {
					continue
				}
				if block.Source == nil {
					block.Source = map[string]interface{}{}
				}
				block.Source["rego"] = regoBody
				regoInjected = true
			case "K8sNativeValidation":
				if celBody == "" {
					continue
				}
				if block.Source == nil {
					block.Source = map[string]interface{}{}
				}
				if err := yaml.Unmarshal([]byte(celBody), &block.Source); err != nil {
					return "", fmt.Errorf("parsing cel yaml: %w", err)
				}
				celInjected = true
			}
		}
	}

	if regoBody != "" && !regoInjected {
		return "", fmt.Errorf("template does not contain a Rego engine target to inject --rego into")
	}
	if celBody != "" && !celInjected {
		return "", fmt.Errorf("template does not contain a K8sNativeValidation engine target to inject --cel into")
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&doc); err != nil {
		return "", fmt.Errorf("marshaling compiled template: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("closing yaml encoder: %w", err)
	}

	return buf.String(), nil
}

func readCombinedSources(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}

	var b strings.Builder
	for i, path := range paths {
		fileBytes, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading rego file %q: %w", path, err)
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSuffix(string(fileBytes), "\n"))
	}
	return b.String(), nil
}

func validateConstraintTemplateYAML(content string) error {
	var doc constraintTemplate
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return fmt.Errorf("compiled output is not valid yaml: %w", err)
	}
	if doc.APIVersion == "" || doc.Kind != "ConstraintTemplate" {
		return fmt.Errorf("compiled output is not a ConstraintTemplate manifest")
	}
	if len(doc.Spec.Targets) == 0 {
		return fmt.Errorf("compiled ConstraintTemplate has no targets")
	}
	return nil
}
