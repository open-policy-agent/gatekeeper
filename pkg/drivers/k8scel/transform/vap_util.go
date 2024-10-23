package transform

import (
	"errors"
	"sync"

	"github.com/go-logr/logr"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	rest "k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	groupVersion := admissionregistrationv1.SchemeGroupVersion
	resList, err := clientset.Discovery().ServerResourcesForGroupVersion(groupVersion.String())
	if err == nil {
		for i := 0; i < len(resList.APIResources); i++ {
			if resList.APIResources[i].Name == "validatingadmissionpolicies" {
				VapAPIEnabled = new(bool)
				*VapAPIEnabled = true
				GroupVersion = &groupVersion
				return true, GroupVersion
			}
		}
	}

	groupVersion = admissionregistrationv1beta1.SchemeGroupVersion
	resList, err = clientset.Discovery().ServerResourcesForGroupVersion(groupVersion.String())
	if err == nil {
		for i := 0; i < len(resList.APIResources); i++ {
			if resList.APIResources[i].Name == "validatingadmissionpolicies" {
				VapAPIEnabled = new(bool)
				*VapAPIEnabled = true
				GroupVersion = &groupVersion
				return true, GroupVersion
			}
		}
	}

	log.Error(err, "error checking VAP API availability", "IsVapAPIEnabled", "false")
	VapAPIEnabled = new(bool)
	*VapAPIEnabled = false
	return false, nil
}

func VapForVersion(gvk *schema.GroupVersion) (client.Object, error) {
	switch gvk.Version {
	case "v1":
		return &admissionregistrationv1.ValidatingAdmissionPolicy{}, nil
	case "v1beta1":
		return &admissionregistrationv1beta1.ValidatingAdmissionPolicy{}, nil
	default:
		return nil, errors.New("unrecognized version")
	}
}

func VapBindingForVersion(gvk schema.GroupVersion) (client.Object, error) {
	switch gvk.Version {
	case "v1":
		return &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}, nil
	case "v1beta1":
		return &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{}, nil
	default:
		return nil, errors.New("unrecognized version")
	}
}
