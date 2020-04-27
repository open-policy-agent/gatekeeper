package webhook

import (
	"context"
	"io/ioutil"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	templv1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/api/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/test/clients"
	"github.com/pkg/errors"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"
)

type nsGetter struct {
	clients.NoopClient
	scheme *runtime.Scheme
}

func (f *nsGetter) Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
	if ns, ok := obj.(*corev1.Namespace); ok {
		ns.ObjectMeta = metav1.ObjectMeta{
			Name: key.Name,
		}
		return nil
	}

	return errors.New("not found")
}

// readTemplates reads templates from the pathList
func readTemplates(pathList []string) (templates.ConstraintTemplateList, error) {
	var result templates.ConstraintTemplateList
	result.Items = make([]templates.ConstraintTemplate, len(pathList))
	for i, path := range pathList {
		t := buildTemplatePath(path)
		yamlString, err := ioutil.ReadFile(t)
		if err != nil {
			return templates.ConstraintTemplateList{}, err
		}
		cstr := &templv1beta1.ConstraintTemplate{}
		if err := yaml.Unmarshal(yamlString, cstr); err != nil {
			return templates.ConstraintTemplateList{}, err
		}
		unversioned := templates.ConstraintTemplate{}
		if err := runtimeScheme.Convert(cstr, &unversioned, nil); err != nil {
			return templates.ConstraintTemplateList{}, err
		}
		result.Items[i] = unversioned
	}
	return result, nil
}
func buildTemplatePath(path string) string {
	var b strings.Builder
	b.WriteString(path)
	b.WriteString("template.yaml")
	return b.String()
}
func buildConstraintPath(path string) string {
	var b strings.Builder
	b.WriteString(path)
	b.WriteString("constraint.yaml")
	return b.String()
}
func buildExamplePath(path string) string {
	var b strings.Builder
	b.WriteString(path)
	b.WriteString("example.yaml")
	return b.String()
}

// readConstraints reads constraints from the pathList
func readConstraints(pathList []string) ([]unstructured.Unstructured, error) {
	result := make([]unstructured.Unstructured, len(pathList))
	for i, path := range pathList {
		c := buildConstraintPath(path)
		yamlString, err := ioutil.ReadFile(c)
		if err != nil {
			return nil, err
		}
		cr := unstructured.Unstructured{}
		if err := yaml.Unmarshal(yamlString, &cr); err != nil {
			return nil, err
		}
		result[i] = cr
	}
	return result, nil
}

func addTemplates(opa *opa.Client, list templates.ConstraintTemplateList) error {
	for _, ct := range list.Items {
		_, err := opa.AddTemplate(context.Background(), &ct)
		if err != nil {
			return err
		}
	}
	return nil
}

func addConstraints(opa *opa.Client, list unstructured.UnstructuredList) error {
	for _, cr := range list.Items {
		_, err := opa.AddConstraint(context.TODO(), &cr)
		if err != nil {
			return err
		}
	}
	return nil
}

// generateConstraints generates M constraints based on representative constraint in crList
func generateConstraints(M int, crList []unstructured.Unstructured) unstructured.UnstructuredList {
	result := unstructured.UnstructuredList{}
	result.Items = make([]unstructured.Unstructured, M)
	// seed random generator
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < M; i++ {
		r := crList[i%len(crList)]
		result.Items[i] = *(r.DeepCopy())
		r.SetName(genRandString(10))
	}
	return result
}

func genRandString(n int) string {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		c := 'a' + rand.Intn(26)
		out[i] = byte(c)
	}
	return string(out)
}

// libraryPathList contains a random set of library paths with resources
func libraryPathList() []string {
	pathList := []string{"testdata/privileged-containers/",
		"testdata/volumes/", "testdata/host-filesystem/", "testdata/host-network-ports/",
		"testdata/host-namespaces/"}
	return pathList
}

func BenchmarkValidationHandler(b *testing.B) {
	// setup test
	opa, err := makeOpaClient()
	if err != nil {
		b.Fatalf("could not initialize OPA: %s", err)
	}

	c := &nsGetter{scheme: scheme.Scheme, NoopClient: clients.NoopClient{}}
	//c := &clients.NoopClient{}
	cfg := &v1alpha1.Config{
		Spec: v1alpha1.ConfigSpec{
			Validation: v1alpha1.Validation{
				Traces: []v1alpha1.Trace{},
			},
		},
	}
	h := validationHandler{opa: opa, client: c, injectedConfig: cfg}

	// provide a bunch of paths from the library
	pathList := libraryPathList()

	// read templates from library
	ctList, err := readTemplates(pathList)
	if err != nil {
		b.Fatalf("failed to read template files: %s", err)
	}

	// read constraints from library
	crList, err := readConstraints(pathList)
	if err != nil {
		b.Fatalf("failed to read constraint files: %s", err)
	}

	// M constraints among T templates
	// M is divided into T, 2T, 10T, 20T...100000T
	benchmarks := []int{
		5, 10, 50, 100, 200, 1000, 2000, 10000, 20000, 100000,
	}
	for _, bm := range benchmarks {
		name := strconv.Itoa(bm)
		// Remove all data from OPA
		err = opa.Reset(context.TODO())
		if err != nil {
			b.Errorf("test %s, failed to reset OPA: %s", name, err)
		}
		// create T templates
		err = addTemplates(opa, ctList)
		if err != nil {
			b.Errorf("test %s, failed to load templates into OPA: %s", name, err)
		}
		// Load constraints into OPA
		err = addConstraints(opa, generateConstraints(bm, crList))
		if err != nil {
			b.Errorf("test %s, failed to load constraints into OPA: %s", name, err)
		}
		podList, err := readPods(pathList)
		if err != nil {
			b.Errorf("test %s, failed to retrieve pods: %s", name, err)
		}
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				req := createAdmissionRequests(podList, i)
				b.StartTimer()
				resp := h.Handle(context.TODO(), req)
				if resp.Result.Code == http.StatusInternalServerError || resp.Result.Code == http.StatusUnprocessableEntity {
					b.Errorf("expected a decision, received server error %d on test %s", resp.Result.Code, name)
				}
			}
		})
		// Remove all data from OPA
		err = opa.Reset(context.TODO())
		if err != nil {
			b.Errorf("test %s, failed to reset OPA: %s", name, err)
		}
	}
}

func readPods(pathList []string) ([]corev1.Pod, error) {
	result := make([]corev1.Pod, len(pathList))
	for i, path := range pathList {
		c := buildExamplePath(path)
		yamlString, err := ioutil.ReadFile(c)
		if err != nil {
			return nil, err
		}
		cr := corev1.Pod{}
		if err := yaml.Unmarshal(yamlString, &cr); err != nil {
			return nil, err
		}
		result[i] = cr
	}
	return result, nil
}

func createAdmissionRequests(podList []corev1.Pod, n int) atypes.Request {
	dryRun := false
	pod := podList[n%len(podList)]
	name := "pod-name-" + strconv.Itoa(n)
	namespace := "pod-namespace-" + strconv.Itoa(n)
	pod.SetName(name)
	pod.SetNamespace(namespace)
	oldPod := pod.DeepCopy()
	pod.SetResourceVersion("2")
	oldPod.SetResourceVersion("1")
	return atypes.Request{
		AdmissionRequest: admissionv1beta1.AdmissionRequest{
			UID:                types.UID(uuid.NewUUID()),
			Kind:               metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			Resource:           metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			SubResource:        "",
			RequestKind:        &metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			RequestResource:    &metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
			RequestSubResource: "",
			Name:               name,
			Namespace:          namespace,
			Operation:          "UPDATE",
			UserInfo: authenticationv1.UserInfo{
				Username: "pod-creator",
				UID:      "uid",
				Groups:   []string{"pod-creator-group"},
				Extra:    map[string]authenticationv1.ExtraValue{"extraKey": {"value1", "value2"}}},
			Object:    runtime.RawExtension{Object: &podList[n%len(podList)]},
			OldObject: runtime.RawExtension{Object: oldPod},
			DryRun:    &dryRun,
			Options:   runtime.RawExtension{},
		},
	}
}
