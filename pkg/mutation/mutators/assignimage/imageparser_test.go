package assignimage

import (
	"errors"
	"strings"
	"testing"
)

func TestNewImage(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		want     image
	}{
		{
			name:     "full image with tag",
			imageRef: "gcr.io/some-image/hello:latest",
			want: image{
				domain: "gcr.io",
				path:   "some-image/hello",
				tag:    ":latest",
			},
		},
		{
			name:     "all empty components",
			imageRef: "",
			want: image{
				domain: "",
				path:   "",
				tag:    "",
			},
		},
		{
			name:     "full image with hash",
			imageRef: "some-image/hello@sha256:abcde",
			want: image{
				domain: "",
				path:   "some-image/hello",
				tag:    "@sha256:abcde",
			},
		},
		{
			name:     "slash in location with tag",
			imageRef: "some-image/hello:latest",
			want: image{
				domain: "",
				path:   "some-image/hello",
				tag:    ":latest",
			},
		},
		{
			name:     "only location",
			imageRef: "some-image/hello",
			want: image{
				domain: "",
				path:   "some-image/hello",
				tag:    "",
			},
		},
		{
			name:     "no slash in location",
			imageRef: "some-image:tag123",
			want: image{
				domain: "",
				path:   "some-image",
				tag:    ":tag123",
			},
		},
		{
			name:     "just location",
			imageRef: "alpine",
			want: image{
				domain: "",
				path:   "alpine",
				tag:    "",
			},
		},
		{
			name:     "leading underscore",
			imageRef: "_/alpine",
			want: image{
				domain: "",
				path:   "_/alpine",
				tag:    "",
			},
		},
		{
			name:     "leading underscore with tag",
			imageRef: "_/alpine:latest",
			want: image{
				domain: "",
				path:   "_/alpine",
				tag:    ":latest",
			},
		},
		{
			name:     "no domain, location has /",
			imageRef: "library/busybox:v9",
			want: image{
				domain: "",
				path:   "library/busybox",
				tag:    ":v9",
			},
		},
		{
			name:     "dots in domain",
			imageRef: "this.that.com/repo/alpine:1.23",
			want: image{
				domain: "this.that.com",
				path:   "repo/alpine",
				tag:    ":1.23",
			},
		},
		{
			name:     "port and dots in domain",
			imageRef: "this.that.com:5000/repo/alpine:latest",
			want: image{
				domain: "this.that.com:5000",
				path:   "repo/alpine",
				tag:    ":latest",
			},
		},
		{
			name:     "localhost with port",
			imageRef: "localhost:5000/repo/alpine:latest",
			want: image{
				domain: "localhost:5000",
				path:   "repo/alpine",
				tag:    ":latest",
			},
		},
		{
			name:     "dots in location",
			imageRef: "x.y.z/gcr.io/repo:latest",
			want: image{
				domain: "x.y.z",
				path:   "gcr.io/repo",
				tag:    ":latest",
			},
		},
		{
			name:     "dot in domain",
			imageRef: "gcr.io/repo:latest",
			want: image{
				domain: "gcr.io",
				path:   "repo",
				tag:    ":latest",
			},
		},
		{
			name:     "invalid ref still parses",
			imageRef: "x.io/get/ready4.//this/not.good:404@yikes/bad/string",
			want: image{
				domain: "x.io",
				path:   "get/ready4.//this/not.good",
				tag:    ":404@yikes/bad/string",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := newImage(tc.imageRef)
			if got != tc.want {
				t.Errorf("got: %v, want %v", got, tc.want)
			}
		})
	}
}

func isDomainError(domain string) func(error) bool {
	return func(err error) bool {
		return errors.As(err, &invalidDomainError{}) && strings.Contains(err.Error(), domain)
	}
}

func isPathError(path string) func(error) bool {
	return func(err error) bool {
		return errors.As(err, &invalidPathError{}) && strings.Contains(err.Error(), path)
	}
}

func isTagError(tag string) func(error) bool {
	return func(err error) bool {
		return errors.As(err, &invalidTagError{}) && strings.Contains(err.Error(), tag)
	}
}

func isEmptyArgsError() func(error) bool {
	return func(err error) bool {
		return errors.As(err, &missingComponentsError{})
	}
}

func isPathlikeDomainError() func(error) bool {
	return func(err error) bool {
		return errors.As(err, &domainLikePathError{})
	}
}

func TestValidateImageParts(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		path   string
		tag    string
		errFn  func(error) bool
	}{
		{
			name:   "all valid components",
			domain: "my.register.io:5000",
			path:   "lib/stuff/app",
			tag:    ":latest",
		},
		{
			name:  "no fields set returns err",
			errFn: isEmptyArgsError(),
		},
		{
			name:   "valid domain with port",
			domain: "localhost:5000",
		},
		{
			name:   "valid domain no port",
			domain: "localhost",
		},
		{
			name:   "valid domain with .",
			domain: "a.b.c",
		},
		{
			name:   "valid domain with - . and port",
			domain: "a-b-c.com:5000",
		},
		{
			name:   "valid domain with . and port",
			domain: "a.b.c:123",
		},
		{
			name:   "invalid domain no .",
			domain: "foobar",
			errFn:  isDomainError("foobar"),
		},
		{
			name:   "invalid domain leading .",
			domain: ".foobar",
			errFn:  isDomainError(".foobar"),
		},
		{
			name:   "invalid domain trailing .",
			domain: "foobar.",
			errFn:  isDomainError("foobar."),
		},
		{
			name:   "invalid domain . before port",
			domain: "foobar.:5000",
			errFn:  isDomainError("foobar.:5000"),
		},
		{
			name:   "invalid domain / before port",
			domain: "foobar/:5000",
			errFn:  isDomainError("foobar/:5000"),
		},
		{
			name:   "invalid domain leading and trailing .",
			domain: ".foobar.",
			errFn:  isDomainError(".foobar."),
		},
		{
			name:   "invalid domain with _ and port but no .",
			domain: "a_b_c:123",
			errFn:  isDomainError("a_b_c:123"),
		},
		{
			name:   "invalid domain with leading /",
			domain: "/not.ok.io:2000",
			errFn:  isDomainError("/not.ok.io:2000"),
		},
		{
			name:   "invalid domain with trailing /",
			domain: "not.ok.io:2000/",
			errFn:  isDomainError("not.ok.io:2000/"),
		},
		{
			name:   "invalid domain with middle /",
			domain: "not/ok/io",
			errFn:  isDomainError("not/ok/io"),
		},
		{
			name:   "invalid domain port start with alpha",
			domain: "my.reg.io:abc2000",
			errFn:  isDomainError("my.reg.io:abc2000"),
		},
		{
			name:   "invalid domain with multiple :",
			domain: "my.reg.io:2000:",
			errFn:  isDomainError("my.reg.io:2000:"),
		},
		{
			name:   "invalid domain with repeat :",
			domain: "my.reg.io::2000",
			errFn:  isDomainError("my.reg.io::2000"),
		},
		{
			name:   "invalid domain with tag",
			domain: "my.reg.io:latest",
			errFn:  isDomainError("my.reg.io:latest"),
		},
		{
			name:   "invalid domain with digest",
			domain: "my.reg.io@sha256:abcde123456789",
			errFn:  isDomainError("my.reg.io@sha256:abcde123456789"),
		},
		{
			name:   "invalid domain with bad character",
			domain: ";!234.com",
			errFn:  isDomainError(";!234.com"),
		},
		{
			name: "valid path",
			path: "lib/stuff",
		},
		{
			name:   "domain-like path with domain",
			domain: "my.reg.io:5000",
			path:   "a.b.c/stuff",
		},
		{
			name:  "domain-like path without domain returns err",
			path:  "a.b.c/stuff",
			tag:   ":latest",
			errFn: isPathlikeDomainError(),
		},
		{
			name: "valid path . and -",
			path: "lib/stuff-app__thing/a/b--c/e",
		},
		{
			name:  "invalid path ending / returns err",
			path:  "lib/stuff/app/",
			errFn: isPathError("lib/stuff/app/"),
		},
		{
			name:  "invalid path with leading / returns err",
			path:  "/lib/stuff/app",
			errFn: isPathError("/lib/stuff/app"),
		},
		{
			name:   "invalid path with leading : returns err",
			domain: "my.register.io:5000",
			path:   ":lib/stuff/app",
			errFn:  isPathError(":lib/stuff/app"),
		},
		{
			name:   "invalid path with leading @ returns err",
			domain: "my.register.io:5000",
			path:   "@lib/stuff/app",
			errFn:  isPathError("@lib/stuff/app"),
		},
		{
			name:   "invalid path with trailing : returns err",
			domain: "my.register.io:5000",
			path:   "lib/stuff/app:",
			errFn:  isPathError("lib/stuff/app:"),
		},
		{
			name:   "invalid path with trailing @ returns err",
			domain: "my.register.io:5000",
			path:   "lib/stuff/app@",
			errFn:  isPathError("lib/stuff/app@"),
		},
		{
			name:   "invalid path : in middle returns err",
			domain: "my.register.io:5000",
			path:   "lib/stuff:things/app",
			errFn:  isPathError("lib/stuff:things/app"),
		},
		{
			name: "test valid tag",
			tag:  ":latest",
		},
		{
			name: "test valid digest",
			tag:  "@sha256:12345678901234567890123456789012",
		},
		{
			name:  "invalid tag no leading :",
			tag:   "latest",
			errFn: isTagError("latest"),
		},
		{
			name:  "invalid digest no leading @",
			tag:   "sha256:12345678901234567890123456789012",
			errFn: isTagError("sha256:12345678901234567890123456789012"),
		},
		{
			name:  "invalid digest hash too short",
			tag:   "@sha256:123456",
			errFn: isTagError("@sha256:123456"),
		},
		{
			name:  "invalid digest not base 16",
			tag:   "@sha256:1XYZ5678901234567890123456789012",
			errFn: isTagError("@sha256:1XYZ5678901234567890123456789012"),
		},
		{
			name:  "invalid tag leading /",
			tag:   "/:latest",
			errFn: isTagError("/:latest"),
		},
		{
			name:  "invalid tag trailing /",
			tag:   ":latest/",
			errFn: isTagError(":latest/"),
		},
		{
			name:  "invalid tag trailing :",
			tag:   ":latest:",
			errFn: isTagError(":latest:"),
		},
		{
			name:  "invalid tag trailing @",
			tag:   ":latest@",
			errFn: isTagError(":latest@"),
		},
		{
			name:  "invalid tag : inside",
			tag:   ":lat:est",
			errFn: isTagError(":lat:est"),
		},
		{
			name:  "invalid tag @ inside",
			tag:   "@sha256:12345678901234567890123456789012@sha256:12345678901234567890123456789012",
			errFn: isTagError("@sha256:12345678901234567890123456789012@sha256:12345678901234567890123456789012"),
		},
		{
			name:  "invalid tag double :",
			tag:   "::latest",
			errFn: isTagError("::latest"),
		},
		{
			name:  "invalid digest double @",
			tag:   "@@sha256:1XYZ5678901234567890123456789012",
			errFn: isTagError("@@sha256:1XYZ5678901234567890123456789012"),
		},
		{
			name:  "invalid tag @ and :",
			tag:   "@:latest",
			errFn: isTagError("@:latest"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImageParts(tc.domain, tc.path, tc.tag)
			if tc.errFn == nil && err != nil {
				t.Errorf("(domain=%s, path=%s, tag=%s) did not want error but got: %v", tc.domain, tc.path, tc.tag, err)
			}
			if tc.errFn != nil {
				if err == nil {
					t.Errorf("(domain=%s, path=%s, tag=%s) wanted error but got nil", tc.domain, tc.path, tc.tag)
				} else if !tc.errFn(err) {
					t.Errorf("got error of unexpected type: %s", err)
				}
			}
		})
	}
}
