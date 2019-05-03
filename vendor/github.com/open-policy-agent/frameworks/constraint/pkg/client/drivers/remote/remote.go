package remote

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	ctypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/pkg/errors"
)

type arg func(*inits)

type inits struct {
	url          string
	opaCAs       *x509.CertPool
	auth         string
	traceEnabled bool
}

func URL(url string) arg {
	return func(i *inits) {
		i.url = url
	}
}

func OpaCA(ca *x509.CertPool) arg {
	return func(i *inits) {
		i.opaCAs = ca
	}
}

func Auth(auth string) arg {
	return func(i *inits) {
		i.auth = auth
	}
}

func Tracing(enabled bool) arg {
	return func(i *inits) {
		i.traceEnabled = enabled
	}
}

func New(args ...arg) (drivers.Driver, error) {
	i := &inits{}
	for _, arg := range args {
		arg(i)
	}
	if i.url == "" {
		return nil, errors.New("OPA URL not set")
	}
	return &driver{opa: newHttpClient(i.url, i.opaCAs, i.auth), traceEnabled: i.traceEnabled}, nil
}

var _ drivers.Driver = &driver{}

type driver struct {
	opa          client
	traceEnabled bool
}

func (d *driver) Init(ctx context.Context) error {
	return nil
}

func (d *driver) addTrace(path string) string {
	return path + "?explain=full&pretty=true"
}

func (d *driver) PutModule(ctx context.Context, name string, src string) error {
	return d.opa.InsertPolicy(name, []byte(src))
}

// DeleteModule deletes a rule from OPA and returns true if a rule was found and deleted, false
// if a rule was not found, and any errors
func (d *driver) DeleteModule(ctx context.Context, name string) (bool, error) {
	err := d.opa.DeletePolicy(name)
	if err != nil {
		if e, ok := errors.Cause(err).(*Error); ok {
			if e.Status == 404 {
				return false, nil
			}
		}
	}
	return err == nil, err
}

func (d *driver) PutData(ctx context.Context, path string, data interface{}) error {
	return d.opa.PutData(path, data)
}

// DeleteData deletes data from OPA and returns true if data was found and deleted, false
// if data was not found, and any errors
func (d *driver) DeleteData(ctx context.Context, path string) (bool, error) {
	err := d.opa.DeleteData(path)
	if err != nil {
		if e, ok := errors.Cause(err).(*Error); ok {
			if e.Status == 404 {
				return false, nil
			}
		}
	}
	return err == nil, err
}

// makeURLPath takes a path of the form data.foo["bar.baz"].yes and converts it to an URI path
// such as /data/foo/bar.baz/yes
func makeURLPath(path string) (string, error) {
	var pieces []string
	quoted := false
	openBracket := false
	builder := &strings.Builder{}
	for _, chr := range path {
		ch := string(chr)
		if !quoted {
			if ch == "." {
				pieces = append(pieces, builder.String())
				builder.Reset()
				continue
			}
			if ch == "[" {
				if !openBracket {
					openBracket = true
					pieces = append(pieces, builder.String())
					builder.Reset()
					continue
				} else {
					return "", fmt.Errorf("Mismatched bracketing: %s", path)
				}
			}
			if ch == "]" {
				if openBracket {
					openBracket = false
					continue
				} else {
					return "", fmt.Errorf("Mismatched bracketing: %s", path)
				}
			}
		}
		if ch == `"` {
			quoted = !quoted
			continue
		}
		fmt.Fprint(builder, ch)
	}
	pieces = append(pieces, builder.String())

	return strings.Join(pieces, "/"), nil
}

func (d *driver) Query(ctx context.Context, path string, input interface{}, opts ...drivers.QueryOpt) (*ctypes.Response, error) {
	cfg := &drivers.QueryCfg{}
	for _, opt := range opts {
		opt(cfg)
	}
	path, err := makeURLPath(path)
	if err != nil {
		return nil, err
	}
	if d.traceEnabled || cfg.TracingEnabled {
		path = d.addTrace(path)
	}
	response, err := d.opa.Query(path, input)
	if err != nil {
		return nil, err
	}

	var results []*ctypes.Result
	if response.Result != nil {
		if err := json.Unmarshal(response.Result, &results); err != nil {
			rawJson := string(response.Result)
			return nil, errors.Wrapf(err, "DriverQuery: Unmarshal result: %s", rawJson)
		}
	}
	resp := &ctypes.Response{Results: results}

	if (d.traceEnabled || cfg.TracingEnabled) && response.Explanation != nil {
		var t interface{}
		if err := json.Unmarshal(response.Explanation, &t); err != nil {
			return nil, err
		}
		trace, err := json.MarshalIndent(t, "", "   ")
		if err != nil {
			return nil, err
		}
		tr := string(trace)
		resp.Trace = &tr
	}
	inp, err := json.MarshalIndent(input, "", "   ")
	i := string(inp)
	resp.Input = &i

	return resp, nil
}

func (d *driver) Dump(ctx context.Context) (string, error) {
	response, err := d.opa.Query("", nil)
	if err != nil {
		return "", err
	}
	resp := make(map[string]interface{})
	resp["data"] = response.Result

	polResponse, err := d.opa.ListPolicies()
	if err != nil {
		return "", err
	}
	pols := make([]map[string]interface{}, 0)
	err = json.Unmarshal(polResponse.Result, &pols)
	if err != nil {
		return "", err
	}
	policies := make(map[string]string)
	for _, v := range pols {
		id, ok := v["id"]
		raw, ok2 := v["raw"]
		ids, ok3 := id.(string)
		raws, ok4 := raw.(string)
		if ok && ok2 && ok3 && ok4 {
			p, err := url.PathUnescape(ids)
			if err != nil {
				return "", err
			}
			policies[p] = raws
		}
	}
	resp["modules"] = policies
	b, err := json.MarshalIndent(resp, "", "   ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
