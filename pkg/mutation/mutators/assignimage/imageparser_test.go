package assignimage

import (
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

func TestValidateImageParts(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		path    string
		tag     string
		wantErr bool
	}{
		{
			name:   "all valid components",
			domain: "my.register.io:5000",
			path:   "lib/stuff/app",
			tag:    ":latest",
		},
		{
			name:    "no fields set returns err",
			wantErr: true,
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
			name:   "valid domain with _",
			domain: "a_b_c:123",
		},
		{
			name:    "invalid domain with leading /",
			domain:  "/not.ok.io:2000",
			wantErr: true,
		},
		{
			name:    "invalid domain with trailing /",
			domain:  "not.ok.io:2000/",
			wantErr: true,
		},
		{
			name:    "invalid domain with middle /",
			domain:  "not/ok/io",
			wantErr: true,
		},
		{
			name:    "invalid domain port start with alpha",
			domain:  "my.reg.io:abc2000",
			wantErr: true,
		},
		{
			name:    "invalid domain with multiple :",
			domain:  "my.reg.io:2000:",
			wantErr: true,
		},
		{
			name:    "invalid domain with repeat :",
			domain:  "my.reg.io::2000",
			wantErr: true,
		},
		{
			name:    "invalid domain with tag",
			domain:  "my.reg.io:latest",
			wantErr: true,
		},
		{
			name:    "invalid domain with digest",
			domain:  "my.reg.io@sha256:abcde123456789",
			wantErr: true,
		},
		{
			name:    "invalid domain with bad character",
			domain:  ";!234.com",
			wantErr: true,
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
			name:    "domain-like path without domain returns err",
			path:    "a.b.c/stuff",
			tag:     ":latest",
			wantErr: true,
		},
		{
			name: "valid path . and -",
			path: "lib/stuff-app__thing/a/b--c/e",
		},
		{
			name:    "invalid path ending / returns err",
			path:    "lib/stuff/app/",
			wantErr: true,
		},
		{
			name:    "invalid path with leading / returns err",
			path:    "/lib/stuff/app",
			wantErr: true,
		},
		{
			name:    "invalid path with leading : returns err",
			domain:  "my.register.io:5000",
			path:    ":lib/stuff/app",
			wantErr: true,
		},
		{
			name:    "invalid path with leading @ returns err",
			domain:  "my.register.io:5000",
			path:    "@lib/stuff/app",
			wantErr: true,
		},
		{
			name:    "invalid path with trailing : returns err",
			domain:  "my.register.io:5000",
			path:    "lib/stuff/app:",
			wantErr: true,
		},
		{
			name:    "invalid path with trailing @ returns err",
			domain:  "my.register.io:5000",
			path:    "lib/stuff/app@",
			wantErr: true,
		},
		{
			name:    "invalid path : in middle returns err",
			domain:  "my.register.io:5000",
			path:    "lib/stuff:things/app",
			wantErr: true,
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
			name:    "invalid tag no leading :",
			tag:     "latest",
			wantErr: true,
		},
		{
			name:    "invalid digest no leading @",
			tag:     "sha256:12345678901234567890123456789012",
			wantErr: true,
		},
		{
			name:    "invalid digest hash too short",
			tag:     "@sha256:123456",
			wantErr: true,
		},
		{
			name:    "invalid digest not base 16",
			tag:     "@sha256:1XYZ5678901234567890123456789012",
			wantErr: true,
		},
		{
			name:    "invalid tag leading /",
			tag:     "/:latest",
			wantErr: true,
		},
		{
			name:    "invalid tag trailing /",
			tag:     ":latest/",
			wantErr: true,
		},
		{
			name:    "invalid tag double :",
			tag:     "::latest",
			wantErr: true,
		},
		{
			name:    "invalid digest double @",
			tag:     "@@sha256:1XYZ5678901234567890123456789012",
			wantErr: true,
		},
		{
			name:    "invalid tag @ and :",
			tag:     "@:latest",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImageParts(tc.domain, tc.path, tc.tag)
			if !tc.wantErr && err != nil {
				t.Errorf("did not want error but got: %v", err)
			}
			if tc.wantErr && err == nil {
				t.Error("wanted error but got nil")
			}
		})
	}
}
