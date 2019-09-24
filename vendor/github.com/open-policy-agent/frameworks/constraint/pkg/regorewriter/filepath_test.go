package regorewriter

import "testing"

func TestFilePath(t *testing.T) {
	testcases := []struct {
		name      string
		path      string
		old       string
		new       string
		wantError bool
		wantPath  string
	}{
		{
			name:      "relative path",
			path:      "the/quick/brown/fox.yaml",
			old:       "the/quick",
			new:       "a/slow",
			wantError: false,
			wantPath:  "a/slow/brown/fox.yaml",
		},
		{
			name:      "abs path",
			path:      "/the/quick/brown/fox.yaml",
			old:       "/the/quick",
			new:       "/a/slow",
			wantError: false,
			wantPath:  "/a/slow/brown/fox.yaml",
		},
		{
			name:      "abspath to relpath",
			path:      "/the/quick/brown/fox.yaml",
			old:       "/the/quick",
			new:       "a/slow",
			wantError: true,
		},
		{
			name:      "relpath to abspath",
			path:      "/the/quick/brown/fox.yaml",
			old:       "/the/quick",
			new:       "a/slow",
			wantError: true,
		},
		{
			name:      "path not in old",
			path:      "the/quick/brown/fox.yaml",
			old:       "not/prefix",
			new:       "a/slow",
			wantError: true,
		},
		{
			name:      "abspath mismatch path",
			path:      "/the/quick/brown/fox.yaml",
			old:       "an/awful",
			new:       "a/slow",
			wantError: true,
		},
		{
			name:      "abspath mismatch old",
			path:      "the/quick/brown/fox.yaml",
			old:       "/an/awful",
			new:       "a/slow",
			wantError: true,
		},
		{
			name:      "abspath mismatch old/new",
			path:      "/the/quick/brown/fox.yaml",
			old:       "/an/awful",
			new:       "a/slow",
			wantError: true,
		},
		{
			name:      "old is parent of current relpath",
			path:      "the/quick/brown/fox.yaml",
			old:       "..",
			new:       "a/slow",
			wantError: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fp := FilePath{path: tc.path}
			err := fp.Reparent(tc.old, tc.new)

			switch {
			case tc.wantError && err == nil:
				t.Fatal("expected error, got nil")
			case tc.wantError && err != nil:
				return
			case !tc.wantError && err != nil:
				t.Fatalf("unexepected error %s", err)
			}

			if tc.wantPath != fp.Path() {
				t.Errorf("wanted path %s, got path %s", tc.wantPath, fp.Path())
			}
		})
	}
}
