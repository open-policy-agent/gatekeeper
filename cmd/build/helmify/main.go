package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var outputDir = flag.String("output-dir", "manifest_staging/charts/gatekeeper", "The root directory in which to write the Helm chart")

var kindRegex = regexp.MustCompile(`(?m)^kind:[\s]+([\S]+)[\s]*$`)

// use exactly two spaces to be sure we are capturing metadata.name.
var nameRegex = regexp.MustCompile(`(?m)^  name:[\s]+([\S]+)[\s]*$`)

const (
	DeploymentKind     = "Deployment"
	ServiceAccountKind = "ServiceAccount"
	end                = "{{- end }}"
)

func isRbacKind(str string) bool {
	rbacKinds := [4]string{"Role", "ClusterRole", "RoleBinding", "ClusterRoleBinding"}
	result := false
	for _, x := range rbacKinds {
		if x == str {
			result = true
			break
		}
	}
	return result
}

func extractKind(s string) (string, error) {
	matches := kindRegex.FindStringSubmatch(s)
	if len(matches) != 2 {
		return "", fmt.Errorf("%s does not have a kind", s)
	}
	return strings.Trim(matches[1], `"'`), nil
}

func extractName(s string) (string, error) {
	matches := nameRegex.FindStringSubmatch(s)
	if len(matches) != 2 {
		return "", fmt.Errorf("%s does not have a name", s)
	}
	return strings.Trim(matches[1], `"'`), nil
}

func extractCRDKind(obj string) (string, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal([]byte(obj), crd); err != nil {
		return "", err
	}
	return crd.Spec.Names.Kind, nil
}

type kindSet struct {
	byKind map[string][]string
}

func (ks *kindSet) Add(obj string) error {
	kind, err := extractKind(obj)
	if err != nil {
		return err
	}
	objs, ok := ks.byKind[kind]
	if !ok {
		objs = []string{obj}
	} else {
		objs = append(objs, obj)
	}
	ks.byKind[kind] = objs
	return nil
}

func (ks *kindSet) Write() error {
	for kind, objs := range ks.byKind {
		subPath := "templates"
		nameExtractor := extractName
		if kind == "CustomResourceDefinition" {
			nameExtractor = extractCRDKind
			subPath = "crds"
			parentDir := path.Join(*outputDir, subPath)
			fmt.Printf("Making %s\n", parentDir)
			if err := os.Mkdir(parentDir, 0o750); err != nil {
				return err
			}
		}

		if kind == "Namespace" {
			continue
		}

		for _, obj := range objs {
			name, err := nameExtractor(obj)
			if err != nil {
				return err
			}

			fileName := fmt.Sprintf("%s-%s.yaml", strings.ToLower(name), strings.ToLower(kind))

			if name == "validation.gatekeeper.sh" {
				matchConditions := "  matchConditions: {{ toYaml .Values.validatingWebhookMatchConditions | nindent 4 }}"
				replace := fmt.Sprintf("  {{- if .Values.validatingWebhookMatchConditions }}\n  {{- if ge (int .Capabilities.KubeVersion.Minor) 28 }}\n%s\n  {{- end }}\n  {{- end }}", matchConditions)
				obj = "{{- if not .Values.disableValidatingWebhook }}\n" + strings.Replace(obj, matchConditions, replace, 1) + end + "\n"
				fileName = fmt.Sprintf("gatekeeper-validating-webhook-configuration-%s.yaml", strings.ToLower(kind))
			}

			if name == "mutation.gatekeeper.sh" {
				matchConditions := "  matchConditions: {{ toYaml .Values.mutatingWebhookMatchConditions | nindent 4 }}"
				replace := fmt.Sprintf("  {{- if .Values.mutatingWebhookMatchConditions }}\n  {{- if ge (int .Capabilities.KubeVersion.Minor) 28 }}\n%s\n  {{- end }}\n  {{- end }}", matchConditions)
				obj = "{{- if not .Values.disableMutation }}\n" + strings.Replace(obj, matchConditions, replace, 1) + end + "\n"
				fileName = fmt.Sprintf("gatekeeper-mutating-webhook-configuration-%s.yaml", strings.ToLower(kind))
			}

			destFile := path.Join(*outputDir, subPath, fileName)

			if name == "gatekeeper-webhook-server-cert" && kind == "Secret" {
				obj = "{{- if not .Values.externalCertInjection.enabled }}\n" + obj + "{{- end }}\n"
			}

			if name == "gatekeeper-critical-pods" && kind == "ResourceQuota" {
				obj = "{{- if .Values.resourceQuota }}\n" + obj + end + "\n"
			}

			if name == "gatekeeper-controller-manager" && kind == DeploymentKind {
				obj = strings.Replace(obj, "      labels:", "      labels:\n        {{- include \"gatekeeper.podLabels\" . | nindent 8 }}\n        {{- include \"controllerManager.podLabels\" . | nindent 8 }}\n        {{- include \"gatekeeper.commonLabels\" . | nindent 8 }}", 1)
				obj = strings.Replace(obj, "      priorityClassName: system-cluster-critical", "      {{- if .Values.controllerManager.priorityClassName }}\n      priorityClassName:  {{ .Values.controllerManager.priorityClassName }}\n      {{- end }}", 1)
			}

			if name == "gatekeeper-audit" && kind == DeploymentKind {
				obj = "{{- if not .Values.disableAudit }}\n" + obj + "{{- end }}\n"
				obj = strings.Replace(obj, "      labels:", "      labels:\n        {{- include \"gatekeeper.podLabels\" . | nindent 8 }}\n        {{- include \"audit.podLabels\" . | nindent 8 }}\n        {{- include \"gatekeeper.commonLabels\" . | nindent 8 }}", 1)
				obj = strings.Replace(obj, "      priorityClassName: system-cluster-critical", "      {{- if .Values.audit.priorityClassName }}\n      priorityClassName:  {{ .Values.audit.priorityClassName }}\n      {{- end }}", 1)
				obj = strings.Replace(obj, "      - emptyDir: {}", "      {{- if .Values.audit.writeToRAMDisk }}\n      - emptyDir:\n          medium: Memory\n      {{ else }}\n      - emptyDir: {}\n      {{- end }}", 1)
			}

			if name == "gatekeeper-manager-role" && kind == "Role" {
				obj += "{{- with .Values.controllerManager.extraRules }}\n  {{- toYaml . | nindent 0 }}\n{{- end }}\n"
			}

			if isRbacKind(kind) {
				obj = "{{- if .Values.rbac.create }}\n" + obj + end + "\n"
			}

			if name == "gatekeeper-admin" && kind == ServiceAccountKind {
				obj = "{{- if .Values.serviceAccount.gatekeeperAdmin.create }}\n" + obj + end + "\n"
			}

			if name == "gatekeeper-controller-manager" && kind == "PodDisruptionBudget" {
				obj = strings.Replace(obj, "apiVersion: policy/v1", "{{- $v1 := .Capabilities.APIVersions.Has \"policy/v1/PodDisruptionBudget\" -}}\n{{- $v1beta1 := .Capabilities.APIVersions.Has \"policy/v1beta1/PodDisruptionBudget\" -}}\napiVersion: policy/v1{{- if and (not $v1) $v1beta1 -}}beta1{{- end }}", 1)
			}

			if name == "gatekeeper-manager-role" && kind == "ClusterRole" {
				obj = strings.Replace(obj, "- apiGroups:\n  - policy\n  resourceNames:\n  - gatekeeper-admin\n  resources:\n  - podsecuritypolicies\n  verbs:\n  - use\n", "{{- if and .Values.psp.enabled (.Capabilities.APIVersions.Has \"policy/v1beta1/PodSecurityPolicy\") }}\n- apiGroups:\n  - policy\n  resourceNames:\n  - gatekeeper-admin\n  resources:\n  - podsecuritypolicies\n  verbs:\n  - use\n{{- end }}\n", 1)
				obj = strings.Replace(obj, "- gatekeeper-validating-webhook-configuration\n", "- {{ .Values.validatingWebhookName }}\n", 1)
				obj = strings.Replace(obj, "- gatekeeper-mutating-webhook-configuration\n", "- {{ .Values.mutatingWebhookName }}\n", 1)
			}

			fmt.Printf("Writing %s\n", destFile)

			addSeparator := "---\n" + obj
			if err := os.WriteFile(destFile, []byte(addSeparator), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func doReplacements(obj string) string {
	for old, replacement := range replacements {
		obj = strings.ReplaceAll(obj, old, replacement)
	}
	return obj
}

func copyStaticFiles(root string, subdirs ...string) error {
	p := path.Join(append([]string{root}, subdirs...)...)
	files, err := os.ReadDir(p)
	if err != nil {
		return err
	}
	for _, f := range files {
		newSubDirs := append([]string{}, subdirs...)
		newSubDirs = append(newSubDirs, f.Name())
		destination := path.Join(append([]string{*outputDir}, newSubDirs...)...)
		if f.IsDir() {
			fmt.Printf("Making %s\n", destination)
			if err := os.Mkdir(destination, 0o750); err != nil {
				return err
			}
			if err := copyStaticFiles(root, newSubDirs...); err != nil {
				return err
			}
		} else {
			contents, err := os.ReadFile(path.Join(p, f.Name())) // #nosec G304
			if err != nil {
				return err
			}
			fmt.Printf("Writing %s\n", destination)
			if err := os.WriteFile(destination, contents, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	flag.Parse()
	scanner := bufio.NewScanner(os.Stdin)
	kinds := kindSet{byKind: make(map[string][]string)}
	b := strings.Builder{}
	notate := func() {
		obj := doReplacements(b.String())
		b.Reset()
		if err := kinds.Add(obj); err != nil {
			log.Fatalf("Error adding object: %s, %s", err, b.String())
		}
	}

	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "---") {
			if b.Len() > 0 {
				notate()
			}
		} else {
			b.WriteString(scanner.Text())
			b.WriteString("\n")
		}
	}
	if b.Len() > 0 {
		notate()
	}
	if err := copyStaticFiles("cmd/build/helmify/static"); err != nil {
		log.Fatal(err)
	}
	if err := kinds.Write(); err != nil {
		log.Fatal(err)
	}
}
