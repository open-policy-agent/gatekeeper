package common

import (
	"strconv"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("pubsub-common")

func GetBool(input *bool, dft bool) bool {
	if input == nil {
		return dft
	}
	return *input
}

func GetString(input string, dft string) string {
	if input == "" {
		return dft
	}
	return input
}

func GetInt(input string, dft int) int {
	if input == "" {
		return dft
	}
	val, err := strconv.Atoi(input)
	if err != nil {
		log.Error(err, "Failed to parse input integer value, returning default")
		return dft
	}
	return val
}
