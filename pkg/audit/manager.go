package audit

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/pkg/errors"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("controller").WithValues("metaKind", "audit")

const (
	crdName       = "constrainttemplates.templates.gatekeeper.sh"
	constraintsGV = "constraints.gatekeeper.sh/v1alpha1"
	msgSize       = 256
)

var (
	auditInterval = flag.Int("auditInterval", 60, "interval to run audit in seconds. defaulted to 60 secs if unspecified ")
)

// auditManager allows us to audit resources periodically
type auditManager struct {
	mgr     manager.Manager
	opa     opa.Client
	stopper chan struct{}
	stopped chan struct{}
	cfg     *rest.Config
}

type auditResult struct {
	cname       string
	cgroup      string
	ckind       string
	cversion    string
	capiversion string
	rkind       string
	rname       string
	rnamespace  string
	message     string
}

// New starts a new manager for audit
func New(ctx context.Context, cfg *rest.Config, opa opa.Client) error {
	am := &auditManager{
		opa:     opa,
		stopper: make(chan struct{}),
		stopped: make(chan struct{}),
		cfg:     cfg,
	}
	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		return err
	}
	am.mgr = mgr
	go startMgr(mgr, am.stopper, am.stopped)
	go am.auditManagerLoop(ctx)
	return nil
}

// audit audits resources periodically
func (am *auditManager) audit() error {
	c := am.mgr.GetClient()

	// don't audit anything until the constraintTemplate crd is in the cluster
	crd := &apiextensionsv1beta1.CustomResourceDefinition{}
	if err := c.Get(context.TODO(), types.NamespacedName{Name: crdName}, crd); err != nil {
		return err
	}
	resp, err := am.opa.Audit(context.TODO())
	if err != nil {
		return err
	}
	log.Info("Audit opa.Audit() audit results", "violations", len(resp.Results()))
	dynamic := dynamic.NewForConfigOrDie(am.cfg)

	if len(resp.Results()) > 0 {
		updateList := make(map[string][]auditResult)

		for _, r := range resp.Results() {
			name := r.Constraint.GetName()
			apiVersion := r.Constraint.GetAPIVersion()
			gvk := r.Constraint.GroupVersionKind()
			kind := gvk.Kind
			group := gvk.Group
			version := gvk.Version
			selfLink := r.Constraint.GetSelfLink()
			message := r.Msg
			if len(message) > msgSize {
				message = truncateString(message, msgSize)
			}
			resource, ok := r.Resource.(*unstructured.Unstructured)
			if !ok {
				return errors.Errorf("could not cast resource as reviewResource: %v", r.Resource)
			}
			rname := resource.GetName()
			rkind := resource.GetKind()
			rnamespace := resource.GetNamespace()

			updateList[selfLink] = append(updateList[selfLink], auditResult{
				cgroup:      group,
				cversion:    version,
				capiversion: apiVersion,
				ckind:       kind,
				cname:       name,
				rkind:       rkind,
				rname:       rname,
				rnamespace:  rnamespace,
				message:     message,
			})
		}
		// get all constraints kind
		clientset := kubernetes.NewForConfigOrDie(am.cfg)
		rs, err := clientset.Discovery().ServerResourcesForGroupVersion(constraintsGV)
		if err != nil {
			return err
		}
		// get constraints for each kind
		for _, r := range rs.APIResources {
			log.Info("constraint", "resource kind", r.Kind)
			resourceGvk := strings.Split(rs.GroupVersion, "/")
			group := resourceGvk[0]
			version := resourceGvk[1]
			resourceClient := dynamic.Resource(schema.GroupVersionResource{Group: group, Version: version, Resource: r.Name})
			l, err := resourceClient.List(metav1.ListOptions{})
			if err != nil {
				return err
			}
			// get and clear/reset each constraint violation if it's not currently empty
			for _, item := range l.Items {
				oname := item.GetName()
				oapiVersion := item.GetAPIVersion()
				ogvk := item.GroupVersionKind()
				okind := ogvk.Kind
				ogroup := ogvk.Group
				oversion := ogvk.Version
				violations, found, err := unstructured.NestedSlice(item.Object, "status", "violations")
				if err != nil {
					return err
				}
				if !found || len(violations) == 0 {
					fmt.Printf("constraint %s violations is either not found or empty, skip clear \n", oname)
				} else {
					fmt.Printf("constraint name %s, apiversion %s, kind %s, group %s, version %s, violations %v \n", oname, oapiVersion, okind, ogroup, oversion, violations)
					var emptyAuditResults []auditResult
					err = updateConstraintStatus(dynamic, ogroup, oversion, okind, oname, oapiVersion, emptyAuditResults)
					if err != nil {
						return err
					}
					fmt.Printf("constraint %s violations have been cleared \n", oname)
				}
			}
		}
		// update each violated constraint
		for selfLink := range updateList {
			auditResults := updateList[selfLink]
			aResult := auditResults[0]
			return updateConstraintStatus(dynamic, aResult.cgroup, aResult.cversion, aResult.ckind, aResult.cname, aResult.capiversion, auditResults)
		}
	}
	return nil
}

func (am *auditManager) auditManagerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Audit Manager close")
			close(am.stopper)
			return
		default:
			time.Sleep(time.Duration(*auditInterval) * time.Second)
			if err := am.audit(); err != nil {
				log.Error(err, "audit manager audit() failed")
			}
		}
	}
}

func startMgr(mgr manager.Manager, stopper chan struct{}, stopped chan<- struct{}) {
	if err := mgr.Start(stopper); err != nil {
		log.Error(err, "Error starting audit watch manager")
	}
	// mgr.Start() only returns after the manager has completely stopped
	close(stopped)
	log.Info("audit exiting")
}

func updateConstraintStatus(dynamic dynamic.Interface, cgroup string, cversion string, ckind string, cname string, capiversion string, auditResults []auditResult) error {
	cresource := fmt.Sprintf("%ss", strings.ToLower(ckind))
	cstrClient := dynamic.Resource(schema.GroupVersionResource{Group: cgroup, Version: cversion, Resource: cresource})
	o, err := cstrClient.Get(cname, metav1.GetOptions{TypeMeta: metav1.TypeMeta{Kind: ckind, APIVersion: capiversion}})
	if err != nil {
		return err
	}
	val, found, err := unstructured.NestedBool(o.Object, "status", "enforced")
	if err != nil {
		return err
	}
	if !found {
		return errors.Errorf("constraint %s status enforced not found", cname)
	}
	if !val {
		return errors.Errorf("constraint %s status not enforced", cname)
	}
	// create constraint status violations
	var statusViolations []interface{}
	for _, ar := range auditResults {
		statusViolations = append(statusViolations, map[string]string{
			"kind":      ar.rkind,
			"name":      ar.rname,
			"namespace": ar.rnamespace,
			"message":   ar.message,
		})
	}
	raw, err := json.Marshal(statusViolations)
	if err != nil {
		return err
	}
	// need to convert to []interface{}
	violations := make([]interface{}, 0)
	err = json.Unmarshal(raw, &violations)
	if err != nil {
		return err
	}
	// update constraint status
	if len(violations) == 0 {
		unstructured.RemoveNestedField(o.Object, "status", "violations")
		_, err = cstrClient.Update(o, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		log.Info("removed status violations")
	} else {
		unstructured.SetNestedSlice(o.Object, violations, "status", "violations")
		log.Info("update constraint", "object", o)
		_, err = cstrClient.Update(o, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		log.Info("updated constraint status violations", "count", len(violations))
	}
	return nil
}

func truncateString(str string, size int) string {
	shortenStr := str
	if len(str) > size {
		if size > 3 {
			size -= 3
		}
		shortenStr = str[0:size] + "..."
	}
	return shortenStr
}
