package uint64bool

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"gopkg.in/yaml.v3"
)

type holder struct {
	Value *Uint64OrBool `json:"value,omitempty" yaml:"value,omitempty"`
}

func TestUint64OrBool_Unmarshal(t *testing.T) {
	testCases := []struct {
		name    string
		yaml    string
		json    string
		want    *Uint64OrBool
		wantErr error
	}{
		{
			name: "empty string",
			yaml: "",
			json: "{}",
		},
		{
			name: "true value",
			yaml: "value: true",
			json: `{"value": true}`,
			want: FromBool(true),
		},
		{
			name: "false value",
			yaml: "value: false",
			json: `{"value": false}`,
			want: FromBool(false),
		},
		{
			name:    "string of boolean value",
			yaml:    `value: "true"`,
			json:    `{"value": "true"}`,
			wantErr: ErrInvalidUint64OrBool,
		},
		{
			name:    "other value",
			yaml:    "value: other",
			json:    `{"value": "other"}`,
			wantErr: ErrInvalidUint64OrBool,
		},
		{
			name: "zero value",
			yaml: "value: 0",
			json: `{"value": 0}`,
			want: FromUint64(0),
		},
		{
			name: "one value",
			yaml: "value: 1",
			json: `{"value": 1}`,
			want: FromUint64(1),
		},
		{
			name:    "negative value",
			yaml:    "value: -1",
			json:    `{"value": -1}`,
			wantErr: ErrInvalidUint64OrBool,
		},
		{
			name:    "float value",
			yaml:    "value: 1.1",
			json:    `{"value": 1.1}`,
			wantErr: ErrInvalidUint64OrBool,
		},
		{
			name:    "array value",
			yaml:    "value: [true]",
			json:    `{"value": [true]}`,
			wantErr: ErrInvalidUint64OrBool,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name+" YAML", func(t *testing.T) {
			v := holder{}

			err := yaml.Unmarshal([]byte(tc.yaml), &v)
			if diff := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("got error %v,\nwant %v", err, tc.wantErr)
				return
			} else if err != nil {
				return
			}

			if diff := cmp.Diff(tc.want, v.Value); diff != "" {
				t.Errorf(diff)
			}
		})

		t.Run(tc.name+" JSON", func(t *testing.T) {
			v := holder{}

			err := json.Unmarshal([]byte(tc.json), &v)
			if diff := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("got error %v,\nwant %v", err, tc.wantErr)
				return
			} else if err != nil {
				return
			}

			if diff := cmp.Diff(tc.want, v.Value); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}

func TestUint64OrBool_Marshal(t *testing.T) {
	testCases := []struct {
		name     string
		value    holder
		wantYAML string
		wantJSON string
		wantErr  error
	}{
		{
			name:     "nil",
			value:    holder{},
			wantYAML: "{}\n",
			wantJSON: "{}",
		},
		{
			name:     "bool",
			value:    holder{Value: FromBool(true)},
			wantYAML: "value: true\n",
			wantJSON: `{"value":true}`,
		},
		{
			name:     "int",
			value:    holder{Value: FromUint64(3)},
			wantYAML: "value: 3\n",
			wantJSON: `{"value":3}`,
		},
		{
			name:    "invalid type",
			value:   holder{Value: &Uint64OrBool{Type: 4, Uint64Val: 3}},
			wantErr: ErrInvalidUint64OrBool,
		},
		{
			name:    "uint too large",
			value:   holder{Value: &Uint64OrBool{Type: Uint64, Uint64Val: 1 << 63}},
			wantErr: ErrInvalidUint64OrBool,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name+" YAML", func(t *testing.T) {
			got, gotErr := yaml.Marshal(tc.value)

			if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Fatal(diff)
			}

			if diff := cmp.Diff(tc.wantYAML, string(got)); diff != "" {
				t.Error(diff)
			}
		})

		t.Run(tc.name+" JSON", func(t *testing.T) {
			got, gotErr := json.Marshal(tc.value)

			if diff := cmp.Diff(tc.wantErr, gotErr, cmpopts.EquateErrors()); diff != "" {
				t.Fatal(diff)
			}

			if diff := cmp.Diff(tc.wantJSON, string(got)); diff != "" {
				t.Error(diff)
			}
		})
	}
}
