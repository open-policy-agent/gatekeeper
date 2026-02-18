package transform

import (
	"sync"

	"github.com/go-logr/logr"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var vapMux sync.RWMutex

var VapAPIEnabled *bool

var GroupVersion *schema.GroupVersion

// SetVapAPIEnabled sets the VapAPIEnabled flag in a thread-safe manner.
// Use this instead of directly assigning transform.VapAPIEnabled when the
// value may be read concurrently (e.g., by a running controller).
func SetVapAPIEnabled(enabled *bool) {
	vapMux.Lock()
	defer vapMux.Unlock()
	VapAPIEnabled = enabled
}

// SetGroupVersion sets the GroupVersion in a thread-safe manner.
// Use this instead of directly assigning transform.GroupVersion when the
// value may be read concurrently (e.g., by a running controller).
func SetGroupVersion(gv *schema.GroupVersion) {
	vapMux.Lock()
	defer vapMux.Unlock()
	GroupVersion = gv
}

func IsVapAPIEnabled(log *logr.Logger) (bool, *schema.GroupVersion) {
	vapMux.RLock()
	if VapAPIEnabled != nil {
		apiEnabled, gvk := *VapAPIEnabled, GroupVersion
		vapMux.RUnlock()
		return apiEnabled, gvk
	}

	vapMux.RUnlock()
	vapMux.Lock()
	defer vapMux.Unlock()

	if VapAPIEnabled != nil {
		return *VapAPIEnabled, GroupVersion
	}
	cfg, err := config.GetConfig()
	if err != nil {
		log.Info("IsVapAPIEnabled GetConfig", "error", err)
		VapAPIEnabled = new(bool)
		*VapAPIEnabled = false
		return false, nil
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Info("IsVapAPIEnabled NewForConfig", "error", err)
		*VapAPIEnabled = false
		return false, nil
	}

	checkGroupVersion := func(gv schema.GroupVersion) (bool, *schema.GroupVersion) {
		resList, err := clientset.Discovery().ServerResourcesForGroupVersion(gv.String())
		if err == nil {
			for i := 0; i < len(resList.APIResources); i++ {
				if resList.APIResources[i].Name == "validatingadmissionpolicies" {
					VapAPIEnabled = new(bool)
					*VapAPIEnabled = true
					GroupVersion = &gv
					return true, GroupVersion
				}
			}
		}
		return false, nil
	}

	if ok, gvk := checkGroupVersion(admissionregistrationv1.SchemeGroupVersion); ok {
		return true, gvk
	}

	if ok, gvk := checkGroupVersion(admissionregistrationv1beta1.SchemeGroupVersion); ok {
		return true, gvk
	}

	log.Error(err, "error checking VAP API availability", "IsVapAPIEnabled", "false")
	VapAPIEnabled = new(bool)
	*VapAPIEnabled = false
	return false, nil
}
