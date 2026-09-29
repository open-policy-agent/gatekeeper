package main

import "testing"

func TestRejoinFoldedActions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "line break before closing braces",
			in:   "        - --admission-events-involved-namespace={{ .Values.admissionEventsInvolvedNamespace\n          }}\n",
			want: "        - --admission-events-involved-namespace={{ .Values.admissionEventsInvolvedNamespace }}\n",
		},
		{
			name: "line break inside a pipeline",
			in:   "        - --log-level={{ (.Values.controllerManager.logLevel | empty | not) | ternary\n          .Values.controllerManager.logLevel .Values.logLevel }}\n",
			want: "        - --log-level={{ (.Values.controllerManager.logLevel | empty | not) | ternary .Values.controllerManager.logLevel .Values.logLevel }}\n",
		},
		{
			name: "single line action is untouched",
			in:   "        - --log-denies={{ .Values.logDenies }}\n",
			want: "        - --log-denies={{ .Values.logDenies }}\n",
		},
		{
			name: "separate actions on their own lines are untouched",
			in:   "{{- if .Values.rbac.create }}\nkind: Role\n{{- end }}\n",
			want: "{{- if .Values.rbac.create }}\nkind: Role\n{{- end }}\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rejoinFoldedActions(tc.in); got != tc.want {
				t.Errorf("rejoinFoldedActions() = %q, want %q", got, tc.want)
			}
		})
	}
}
