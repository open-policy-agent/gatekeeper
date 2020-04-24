/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package readiness_test

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Templates and constraints in testdata/
var testTemplates = []*templates.ConstraintTemplate{
	makeTemplate("k8sallowedrepos"),
	makeTemplate("k8srequiredlabels"),
}
var testConstraints = []*unstructured.Unstructured{
	makeConstraint("ns-must-have-gk", "K8sRequiredLabels"),
	makeConstraint("prod-repo-is-openpolicyagent", "K8sAllowedRepos"),
}

// Templates and constraint in testdata/post/
var postTemplates = []*templates.ConstraintTemplate{
	makeTemplate("k8shttpsonly"),
}
var postConstraints = []*unstructured.Unstructured{
	makeConstraint("ingress-https-only", "K8sHttpsOnly"),
}

func makeTemplate(name string) *templates.ConstraintTemplate {
	return &templates.ConstraintTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func makeConstraint(name string, kind string) *unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("constraints.gatekeeper.sh/v1beta1")
	u.SetKind(kind)
	u.SetName(name)
	return &u
}
