package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gatekeeper-test-alpha path [--run=name]",
	Short: "Gatekeeper Test Alpha is a unit test CLI for Gatekeeper Constraints",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
