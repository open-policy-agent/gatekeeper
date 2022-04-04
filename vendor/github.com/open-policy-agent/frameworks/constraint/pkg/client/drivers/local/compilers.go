package local

import (
	"fmt"
	"sync"

	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/regorewriter"
	"github.com/open-policy-agent/opa/ast"
)

// Compilers is a threadsafe store of Compilers for ConstraintTemplates.
type Compilers struct {
	mtx sync.RWMutex

	// compilers is a map from target name to a map from Constraint kind to the
	// compiler for the corresponding ConstraintTemplate.
	compilers map[string]map[string]*ast.Compiler

	// externs are the subpaths of "data" which ConstraintTemplates are allowed to
	// reference without being defined. For example, "inventory" for "data.inventory".
	externs []string

	capabilities *ast.Capabilities
}

func (d *Compilers) addTemplate(templ *templates.ConstraintTemplate, printEnabled bool) error {
	compilers := make(map[string]*ast.Compiler)

	modules, err := parseConstraintTemplate(templ, d.externs)
	if err != nil {
		return err
	}

	for target, targetModules := range modules {
		compiler, err := compileTemplateTarget(targetModules, d.capabilities, printEnabled)
		if err != nil {
			return err
		}

		compilers[target] = compiler
	}

	// Don't lock the mutex until after compilation is done. Compilation is
	// expensive, so this allows templates to be compiled in parallel through
	// separate calls but added serially.
	d.mtx.Lock()
	defer d.mtx.Unlock()

	kind := templ.Spec.CRD.Spec.Names.Kind
	for target, targetCompilers := range d.compilers {
		delete(targetCompilers, kind)
		d.compilers[target] = targetCompilers
	}

	if d.compilers == nil {
		d.compilers = make(map[string]map[string]*ast.Compiler)
	}

	for target, compiler := range compilers {
		targetCompilers := d.compilers[target]
		if targetCompilers == nil {
			targetCompilers = make(map[string]*ast.Compiler)
		}
		targetCompilers[kind] = compiler
		d.compilers[target] = targetCompilers
	}

	return nil
}

func (d *Compilers) getCompiler(target, kind string) *ast.Compiler {
	d.mtx.RLock()
	defer d.mtx.RUnlock()

	if len(d.compilers) == 0 {
		return nil
	}

	targetCompilers := d.compilers[target]
	if len(targetCompilers) == 0 {
		return nil
	}

	return targetCompilers[kind]
}

func (d *Compilers) removeTemplate(kind string) {
	d.mtx.Lock()
	defer d.mtx.Unlock()

	for target, templateCompilers := range d.compilers {
		delete(templateCompilers, kind)
		d.compilers[target] = templateCompilers
	}
}

// list returns a shallow copy of the map of Compilers.
// The map is safe to modify; the Compilers are not.
func (d *Compilers) list() map[string]map[string]*ast.Compiler {
	result := make(map[string]map[string]*ast.Compiler)

	d.mtx.RLock()
	defer d.mtx.RUnlock()
	for targetName, targetCompilers := range d.compilers {
		resultTargetCompilers := make(map[string]*ast.Compiler)

		for kind, compiler := range targetCompilers {
			resultTargetCompilers[kind] = compiler
		}

		result[targetName] = resultTargetCompilers
	}

	return result
}

// parseConstraintTemplate validates the rego in template target by parsing
// rego modules.
func parseConstraintTemplate(templ *templates.ConstraintTemplate, externs []string) (map[string][]*ast.Module, error) {
	rr, err := regorewriter.New(regorewriter.NewPackagePrefixer(templateLibPrefix), []string{libRoot}, externs)
	if err != nil {
		return nil, fmt.Errorf("creating rego rewriter: %w", err)
	}

	mods := make(map[string][]*ast.Module)
	for _, target := range templ.Spec.Targets {
		targetMods, err := parseConstraintTemplateTarget(rr, target)
		if err != nil {
			return nil, err
		}

		mods[target.Target] = targetMods
	}

	return mods, nil
}

func parseConstraintTemplateTarget(rr *regorewriter.RegoRewriter, targetSpec templates.Target) ([]*ast.Module, error) {
	entryPoint, err := parseModule(templatePath, targetSpec.Rego)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", clienterrors.ErrInvalidConstraintTemplate, err)
	}

	if entryPoint == nil {
		return nil, fmt.Errorf("%w: failed to parse module for unknown reason",
			clienterrors.ErrInvalidConstraintTemplate)
	}

	if err := rewriteModulePackage(entryPoint); err != nil {
		return nil, err
	}

	req := map[string]struct{}{violation: {}}

	if err := requireModuleRules(entryPoint, req); err != nil {
		return nil, fmt.Errorf("%w: invalid rego: %v",
			clienterrors.ErrInvalidConstraintTemplate, err)
	}

	rr.AddEntryPointModule(templatePath, entryPoint)
	for idx, libSrc := range targetSpec.Libs {
		libPath := fmt.Sprintf(`%s["lib_%d"]`, templateLibPrefix, idx)

		m, err := parseModule(libPath, libSrc)
		if err != nil {
			return nil, fmt.Errorf("%w: %v",
				clienterrors.ErrInvalidConstraintTemplate, err)
		}

		if err = rr.AddLib(libPath, m); err != nil {
			return nil, fmt.Errorf("%w: %v",
				clienterrors.ErrInvalidConstraintTemplate, err)
		}
	}

	sources, err := rr.Rewrite()
	if err != nil {
		return nil, fmt.Errorf("%w: %v",
			clienterrors.ErrInvalidConstraintTemplate, err)
	}

	var mods []*ast.Module
	for _, m := range sources.EntryPoints {
		mods = append(mods, m.Module)
	}
	for _, m := range sources.Libs {
		mods = append(mods, m.Module)
	}

	return mods, nil
}

func compileTemplateTarget(module []*ast.Module, capabilities *ast.Capabilities, printEnabled bool) (*ast.Compiler, error) {
	compiler := ast.NewCompiler().
		WithCapabilities(capabilities).
		WithEnablePrintStatements(printEnabled)

	modules := make(map[string]*ast.Module, len(module)+1)
	modules[hookModulePath] = hookModule

	for i, lib := range module {
		libPath := fmt.Sprintf("%s%d", templatePath, i)
		modules[libPath] = lib
	}

	compiler.Compile(modules)
	if compiler.Failed() {
		return nil, fmt.Errorf("%w: %v", clienterrors.ErrCompile, compiler.Errors)
	}

	return compiler, nil
}

// parseModule parses the module and also fails empty modules.
func parseModule(path, rego string) (*ast.Module, error) {
	module, err := ast.ParseModule(path, rego)
	if err != nil {
		return nil, err
	}

	if module == nil {
		return nil, fmt.Errorf("%w: module %q is empty",
			clienterrors.ErrInvalidModule, templatePath)
	}

	return module, nil
}
