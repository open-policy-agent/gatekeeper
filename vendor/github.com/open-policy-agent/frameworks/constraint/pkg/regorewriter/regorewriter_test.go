package regorewriter

import (
	"fmt"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
)

const regoSrcTemplateText = `
package {{.Package}}

{{range .Imports}}
import {{.}}
{{end}}

{{if .DenyBody}}
deny[{
    "msg": message,
    "details": metadata,
}] {
    {{.DenyBody}}
}
{{end}}

{{.Body}}
`

var regoSrcTemplate *template.Template

func init() {
	var err error
	regoSrcTemplate, err = template.New("template").Parse(regoSrcTemplateText)
	if err != nil {
		panic(err)
	}
}

type regoSrc struct {
	Package  string
	Imports  []string
	DenyBody string
	Body     string
}

type RegoOption func(src *regoSrc)

func Body(b string) RegoOption {
	return func(src *regoSrc) {
		src.Body = b
	}
}

func DenyBody(db string) RegoOption {
	return func(src *regoSrc) {
		src.DenyBody = db
	}
}

func FuncBody(b string) RegoOption {
	return func(src *regoSrc) {
		src.Body = fmt.Sprintf(`
myfunc() {
  %s
}
`, b)
	}
}

func Import(i ...string) RegoOption {
	return func(src *regoSrc) {
		src.Imports = i
	}
}

func RegoSrc(pkg string, opts ...RegoOption) string {
	rs := regoSrc{
		Package: pkg,
	}
	for _, opt := range opts {
		opt(&rs)
	}
	str := &strings.Builder{}
	if err := regoSrcTemplate.Execute(str, rs); err != nil {
		panic(err)
	}
	return str.String()
}

const constraintTemplateTemplate = `
package {{.Package}}

{{range .Import}}
import {{.}}
{{end}}

{{if .DenyBody}}
deny[{
    "msg": message,
    "details": metadata,
}] {
    {{.DenyBody}}
}
{{end}}

{{range .Aux}}
{{.}}
{{end}}
`

type CT struct {
	Package  string
	Imports  []string
	DenyBody string
	Aux      []string
}

func (c CT) String() string {
	tmpl, err := template.New("template").Parse(constraintTemplateTemplate)
	if err != nil {
		panic(err)
	}
	str := &strings.Builder{}
	if err := tmpl.Execute(str, c); err != nil {
		panic(err)
	}
	return str.String()
}

const libTemplate = `
package {{.Package}}

{{range .Import}}
import {{.}}
{{end}}

{{.Body}}
`

type Lib struct {
	Package string
	Imports []string
	Body    string
}

func (l Lib) String() string {
	tmpl, err := template.New("template").Parse(libTemplate)
	if err != nil {
		panic(err)
	}
	str := &strings.Builder{}
	if err := tmpl.Execute(str, l); err != nil {
		panic(err)
	}
	return str.String()
}

// RegoRewriterTestcase is a testcase for rewriting rego.
type RegoRewriterTestcase struct {
	name       string            // testcase name
	baseSrcs   map[string]string // entrypoint files
	libSrcs    map[string]string // lib files
	wantError  bool              // true if RegoRewriter should reject input
	wantResult map[string]string // expected output files from RegoRewriter
}

func (tc *RegoRewriterTestcase) Run(t *testing.T) {
	pp := NewPackagePrefixer("foo.bar")
	libs := []string{"data.lib"}
	externs := []string{"data.inventory"}
	rr, err := New(pp, libs, externs)
	if err != nil {
		t.Fatalf("Failed to create %s", err)
	}

	// TODO: factor out code for filesystem testing
	for path, content := range tc.baseSrcs {
		if err := rr.AddEntryPoint(path, content); err != nil {
			// TODO: add testcase for failed parse
			t.Fatalf("failed to add base %s %v", path, err)
		}
	}
	for path, content := range tc.libSrcs {
		if err := rr.AddLib(path, content); err != nil {
			// TODO: add testcase for failed parse
			t.Fatalf("failed to add lib %s %v", path, err)
		}
	}
	// end TODO: factor out code for filesystem testing

	sources, err := rr.Rewrite()
	if tc.wantError {
		if err == nil {
			t.Errorf("wanted error, got nil")
		}
		return
	}

	if err != nil {
		t.Fatalf("unexpected error during rewrite: %s", err)
	}

	result, err := sources.AsMap()
	if err != nil {
		t.Fatalf("unexpected error during Sources.AsMap: %s", err)
	}
	if diff := cmp.Diff(result, tc.wantResult); diff != "" {
		t.Errorf("result differs from desired:\n%s", diff)
	}
}

func TestRegoRewriter(t *testing.T) {
	testcases := []RegoRewriterTestcase{
		{
			name: "entry point imports lib and lib imports other lib",
			baseSrcs: map[string]string{
				"my_template.rego": RegoSrc("templates.stuff.MyTemplateV1",
					DenyBody(`
  alpha.check[input.name]
	data.lib.alpha.check[input.name]
`,
					),
					Import("data.lib.alpha"),
				),
			},
			libSrcs: map[string]string{
				"lib/alpha.rego": RegoSrc("lib.alpha",
					Import("data.lib.beta"),
					Body(`
check(objects) = object {
  object := objects[_]
  beta.check(object)
	data.lib.beta.check(object)
}
`,
					),
				),
				"lib/beta.rego": RegoSrc("lib.beta",
					Body(`
check(name) {
	name == "beta"
}
`,
					),
				),
			},
			wantResult: map[string]string{
				"my_template.rego": `package templates.stuff.MyTemplateV1

import data.foo.bar.lib.alpha

deny[{
	"msg": message,
	"details": metadata,
}] {
	alpha.check[input.name]
	data.foo.bar.lib.alpha.check[input.name]
}
`,
				"lib/alpha.rego": `package foo.bar.lib.alpha

import data.foo.bar.lib.beta

check(objects) = object {
	object := objects[_]
	beta.check(object)
	data.foo.bar.lib.beta.check(object)
}
`,
				"lib/beta.rego": `package foo.bar.lib.beta

check(name) {
	name == "beta"
}
`,
			},
		},
		{
			name: "entry point binds data.lib to var",
			baseSrcs: map[string]string{
				"my_template.rego": RegoSrc("templates.stuff.MyTemplateV1",
					Import("data.lib.alpha"),
					DenyBody(`
	x := data.lib
  y := x[_]
`,
					),
				),
			},
			libSrcs: map[string]string{
				"my_lib.rego": RegoSrc("lib.mylib",
					Body(`
myfunc() {
	x := data.lib
  y := x[_]
}
`,
					),
				),
			},
			wantResult: map[string]string{
				"my_lib.rego": `package foo.bar.lib.mylib

myfunc {
	x := data.foo.bar.lib
	y := x[_]
}
`,
				"my_template.rego": `package templates.stuff.MyTemplateV1

import data.foo.bar.lib.alpha

deny[{
	"msg": message,
	"details": metadata,
}] {
	x := data.foo.bar.lib
	y := x[_]
}
`,
			},
		},
		{
			name: "entry point uses data.lib[_]",
			baseSrcs: map[string]string{
				"my_template.rego": RegoSrc("templates.stuff.MyTemplateV1",
					Import("data.lib.alpha"),
					DenyBody(`
	x := data.lib[_]
`,
					),
				),
			},
			wantResult: map[string]string{
				"my_template.rego": `package templates.stuff.MyTemplateV1

import data.foo.bar.lib.alpha

deny[{
	"msg": message,
	"details": metadata,
}] {
	x := data.foo.bar.lib[_]
}
`,
			},
		},
		{
			name: "lib uses data.lib[_]",
			libSrcs: map[string]string{
				"lib/alpha.rego": RegoSrc("lib.alpha",
					Body(`
check(object) {
  x := data.lib[_]
  object == "foo"
}
`,
					),
				),
			},
			wantResult: map[string]string{
				"lib/alpha.rego": `package foo.bar.lib.alpha

check(object) {
	x := data.foo.bar.lib[_]
	object == "foo"
}
`,
			},
		},
		{
			name: "entry point references input",
			baseSrcs: map[string]string{
				"my_template.rego": RegoSrc("templates.stuff.MyTemplateV1",
					DenyBody("bucket := input.asset.bucket"),
				),
			},
			wantResult: map[string]string{
				"my_template.rego": `package templates.stuff.MyTemplateV1

deny[{
	"msg": message,
	"details": metadata,
}] {
	bucket := input.asset.bucket
}
`,
			},
		},
		{
			name: "lib references input",
			libSrcs: map[string]string{
				"lib/my_lib.rego": RegoSrc("lib.myLib",
					Body(`
is_foo(name) {
  input.foo[name]
}
`)),
			},
			wantResult: map[string]string{
				"lib/my_lib.rego": `package foo.bar.lib.myLib

is_foo(name) {
	input.foo[name]
}
`,
			},
		},
		{
			name: "walk data.lib",
			libSrcs: map[string]string{
				"lib/my_lib.rego": RegoSrc("lib.myLib",
					Body(`
is_foo(name) {
  walk(data.lib, [p, v])
}
`)),
			},
			wantResult: map[string]string{
				"lib/my_lib.rego": `package foo.bar.lib.myLib

is_foo(name) {
	walk(data.foo.bar.lib, [p, v])
}
`,
			},
		},

		// Special error cases
		{
			name: "lib cannot have package name data.lib",
			libSrcs: map[string]string{
				"lib/my_lib.rego": RegoSrc("lib"),
			},
			wantError: true,
		},
		{
			name: "lib has invalid package prefix",
			libSrcs: map[string]string{
				"lib/my_lib.rego": RegoSrc("mystuff.myLib"),
			},
			wantError: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, tc.Run)
	}
}

func TestRegoRewriterErrorCases(t *testing.T) {
	testcases := []struct {
		name         string
		imports      string
		snippet      string
		excludeEntry bool
		excludeLib   bool
	}{
		{
			name:    "invalid data object reference",
			snippet: "data.badextern.fungibles[name]",
		},
		{
			name:    "import invalid lib",
			imports: "data.stuff.foolib",
			snippet: "foolib.check(input)",
		},
		{
			name:    "import invalid lib path",
			imports: "data.stuff.foolib",
			snippet: "foolib.check(input)",
		},
		{
			name: "invalid binding of data to var",
			snippet: `
  x := data
  x.stuff.more.stuff
`,
		},
		{
			name: "invalid reference of data object with key var",
			snippet: `
	x := input.name
	y := data[x]
`,
		},
		{
			name: "invalid reference of data object with key literal",
			snippet: `
	y := data["foo"]
`,
		},
		{
			name:    "invalid import of input",
			imports: "input.metadata",
			snippet: `
	metadata.x == "abc"
`,
		},
		{
			name:    "invalid assignment to data using with from var",
			imports: "data.lib.util",
			snippet: `
  util with data.checks as data.lib.mychecks
`,
		},
		{
			name:    "invalid assignment to data using with from literal",
			imports: "data.lib.util",
			snippet: `
  util with data.bobs as {"dev": ["bob"]}
`,
		},
	}

	for _, tc := range testcases {
		for _, srcTypeMeta := range []struct {
			name           string
			pkg            string
			run            bool
			snippetBuilder func(string) RegoOption
			srcSetter      func(*RegoRewriterTestcase, string)
		}{
			{
				name:           "entrypoint",
				pkg:            "template.stuff.MyTemplateV1",
				run:            !tc.excludeEntry,
				snippetBuilder: DenyBody,
				srcSetter: func(tc *RegoRewriterTestcase, src string) {
					tc.baseSrcs["my_template.rego"] = src
				},
			},
			{
				name:           "lib",
				pkg:            "lib.fail",
				run:            !tc.excludeLib,
				snippetBuilder: FuncBody,
				srcSetter: func(tc *RegoRewriterTestcase, src string) {
					tc.baseSrcs["my_lib.rego"] = src
				},
			},
		} {
			if !srcTypeMeta.run {
				continue
			}

			var opts []RegoOption
			if tc.imports != "" {
				opts = append(opts, Import(tc.imports))
			}
			opts = append(opts, srcTypeMeta.snippetBuilder(tc.snippet))

			subTc := RegoRewriterTestcase{
				name:      tc.name,
				wantError: true,
				baseSrcs:  map[string]string{},
				libSrcs:   map[string]string{},
			}
			srcTypeMeta.srcSetter(&subTc, RegoSrc(srcTypeMeta.pkg, opts...))
			t.Run(fmt.Sprintf("%s-%s", srcTypeMeta.name, tc.name), subTc.Run)
		}
	}
}
