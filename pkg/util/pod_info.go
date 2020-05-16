package util

import "os"

// PodName returns the name of the Gatekeeper pod
func GetPodName() string {
	return os.Getenv("POD_NAME")
}

// GetID returns a unique name for the Gatekeeper pod
func GetID() string {
	return GetPodName()
}

func GetNamespace() string {
	ns, found := os.LookupEnv("POD_NAMESPACE")
	if !found {
		return "gatekeeper-system"
	}
	return ns
}
