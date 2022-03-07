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

type TargetModule struct {
	Rego string
	Libs []string
}

// parseConstraintTemplate validates the rego in template target by parsing
// rego modules.
func parseConstraintTemplate(templ *templates.ConstraintTemplate, externs []string) (map[string]TargetModule, error) {
	kind := templ.Spec.CRD.Spec.Names.Kind
	pkgPrefix := templateLibPrefix(kind)

	rr, err := regorewriter.New(regorewriter.NewPackagePrefixer(pkgPrefix), []string{libRoot}, externs)
	if err != nil {
		return nil, fmt.Errorf("creating rego rewriter: %w", err)
	}

	mods := make(map[string]TargetModule)
	for _, target := range templ.Spec.Targets {
		targetMods, err := parseConstraintTemplateTarget(rr, pkgPrefix, target)
		if err != nil {
			return nil, err
		}

		mods[target.Target] = TargetModule{
			Rego: target.Rego,
			Libs: targetMods,
		}
	}

	return mods, nil
}

func parseConstraintTemplateTarget(rr *regorewriter.RegoRewriter, pkgPrefix string, targetSpec templates.Target) ([]string, error) {
	entryPoint, err := parseModule(targetSpec.Rego)
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
		libPath := fmt.Sprintf(`%s["lib_%d"]`, pkgPrefix, idx)
		if err = rr.AddLib(libPath, libSrc); err != nil {
			return nil, fmt.Errorf("%w: %v",
				clienterrors.ErrInvalidConstraintTemplate, err)
		}
	}

	sources, err := rr.Rewrite()
	if err != nil {
		return nil, fmt.Errorf("%w: %v",
			clienterrors.ErrInvalidConstraintTemplate, err)
	}

	var mods []string
	err = sources.ForEachModule(func(m *regorewriter.Module) error {
		content, err2 := m.Content()
		if err2 != nil {
			return err2
		}
		mods = append(mods, string(content))
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("%w: %v",
			clienterrors.ErrInvalidConstraintTemplate, err)
	}

	return mods, nil
}

func compileTemplateTarget(module TargetModule, capabilities *ast.Capabilities, printEnabled bool) (*ast.Compiler, error) {
	compiler := ast.NewCompiler().
		WithCapabilities(capabilities).
		WithEnablePrintStatements(printEnabled)

	modules := make(map[string]*ast.Module)

	builtinModule, err := ast.ParseModule(hookModulePath, hookModule)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", clienterrors.ErrParse, err)
	}
	modules[hookModulePath] = builtinModule

	regoModule, err := ast.ParseModule(templatePath, module.Rego)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", clienterrors.ErrParse, err)
	}
	modules[templatePath] = regoModule

	for i, lib := range module.Libs {
		libPath := fmt.Sprintf("%s%d", templatePath, i)
		libModule, err := ast.ParseModule(libPath, lib)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", clienterrors.ErrParse, err)
		}
		modules[libPath] = libModule
	}

	compiler.Compile(modules)
	if compiler.Failed() {
		return nil, fmt.Errorf("%w: %v", clienterrors.ErrCompile, compiler.Errors)
	}

	return compiler, nil
}
