package transform

import (
	"sync"

	"github.com/go-logr/logr"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var vapMux sync.RWMutex

var VapAPIEnabled *bool

var GroupVersion *schema.GroupVersion

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
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Info("IsVapAPIEnabled InClusterConfig", "error", err)
		VapAPIEnabled = new(bool)
		*VapAPIEnabled = false
		return false, nil
	}
	clientset, err := kubernetes.NewForConfig(config)
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
