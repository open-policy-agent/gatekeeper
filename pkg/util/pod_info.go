package util

import "os"

func GetNamespace() string {
	ns, found := os.LookupEnv("POD_NAMESPACE")
	if !found {
		return "gatekeeper-system"
	}
	return ns
}
