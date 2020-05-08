package util

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// ReadCRD reads the CRDs from the specified file and Unmarshals them into structs
// Adapted from https://github.com/kubernetes-sigs/controller-runtime/blob/master/pkg/envtest/crd.go
func ReadCRD(filePath string) ([]runtime.Object, error) {
	var crds []runtime.Object

	// Unmarshal CRDs from file into structs
	docs, err := readDocuments(filePath)
	if err != nil {
		return nil, err
	}

	for _, doc := range docs {
		crd := &unstructured.Unstructured{}
		if err = yaml.Unmarshal(doc, crd); err != nil {
			return nil, err
		}

		// Check that it is actually a CRD
		crdKind, _, err := unstructured.NestedString(crd.Object, "spec", "names", "kind")
		if err != nil {
			return nil, err
		}
		crdGroup, _, err := unstructured.NestedString(crd.Object, "spec", "group")
		if err != nil {
			return nil, err
		}

		if crd.GetKind() != "CustomResourceDefinition" || crdKind == "" || crdGroup == "" {
			continue
		}
		crds = append(crds, crd)
	}

	return crds, nil
}

// readDocuments reads documents from file
// Adapted from https://github.com/kubernetes-sigs/controller-runtime/blob/master/pkg/envtest/crd.go
func readDocuments(fp string) ([][]byte, error) {
	b, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, err
	}

	docs := [][]byte{}
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(b)))
	for {
		// Read document
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		docs = append(docs, doc)
	}

	return docs, nil
}
