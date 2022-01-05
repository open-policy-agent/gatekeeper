/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

*/
package validate

import (
	"fmt"
	"io"
	"os"

	"github.com/open-policy-agent/gatekeeper/pkg/gator/validate"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Cmd is the gator validate subcommand.
// TODO(juliankatz): write the description and add an examples block
var Cmd = &cobra.Command{
	Use:   "validate",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: run,
}

func init() {
	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// validateCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// validateCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func run(cmd *cobra.Command, args []string) {
	var objs []*unstructured.Unstructured

	decoder := yaml.NewYAMLOrJSONDecoder(os.Stdin, 1000)
	for {
		u := &unstructured.Unstructured{
			Object: make(map[string]interface{}),
		}
		err := decoder.Decode(&u.Object)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("reading chunk: %v\n", err)
			os.Exit(1)
		}

		objs = append(objs, u)
	}

	responses, err := validate.Validate(objs)
	if err != nil {
		fmt.Printf("auditing objects: %v\n", err)
		os.Exit(1)
	}

	results := responses.Results()
	if len(results) > 0 {
		fmt.Printf("results: %v\n", results)
		os.Exit(1)
	}
}
