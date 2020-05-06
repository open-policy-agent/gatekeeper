package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v2"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

var (
	outputDir  = flag.String("output-dir", "manifest_staging/charts/gatekeeper", "The root directory in which to write the Helm chart")
	useCRDsDir = flag.Bool("use-crds-dir", false, `Use the "crds" subdirectory, which requires Helm v3`)
)

var kindRegex = regexp.MustCompile(`(?m)^kind:[\s]+([\S]+)[\s]*$`)

// use exactly two spaces to be sure we are capturing metadata.name
var nameRegex = regexp.MustCompile(`(?m)^  name:[\s]+([\S]+)[\s]*$`)

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
	crd := &apiextensionsv1beta1.CustomResourceDefinition{}
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
			if *useCRDsDir {
				subPath = "crds"
				parentDir := path.Join(*outputDir, subPath)
				fmt.Printf("Making %s\n", parentDir)
				if err := os.Mkdir(parentDir, 0755); err != nil {
					return err
				}
			}
		}
		for _, obj := range objs {
			name, err := nameExtractor(obj)
			if err != nil {
				return err
			}
			fileName := fmt.Sprintf("%s-%s.yaml", strings.ToLower(name), strings.ToLower(kind))
			destFile := path.Join(*outputDir, subPath, fileName)
			fmt.Printf("Writing %s\n", destFile)
			if err := ioutil.WriteFile(destFile, []byte(obj), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func doReplacements(obj string) string {
	for old, new := range replacements {
		obj = strings.ReplaceAll(obj, old, new)
	}
	return obj
}

func copyStaticFiles(root string, subdirs ...string) error {
	p := path.Join(append([]string{root}, subdirs...)...)
	files, err := ioutil.ReadDir(p)
	if err != nil {
		return err
	}
	for _, f := range files {
		newSubDirs := append([]string{}, subdirs...)
		newSubDirs = append(newSubDirs, f.Name())
		destination := path.Join(append([]string{*outputDir}, newSubDirs...)...)
		if f.IsDir() {
			fmt.Printf("Making %s\n", destination)
			if err := os.Mkdir(destination, 0755); err != nil {
				return err
			}
			if err := copyStaticFiles(root, newSubDirs...); err != nil {
				return err
			}
		} else {
			contents, err := ioutil.ReadFile(path.Join(p, f.Name()))
			if err != nil {
				return err
			}
			fmt.Printf("Writing %s\n", destination)
			if err := ioutil.WriteFile(destination, contents, 0644); err != nil {
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
