package audit

import (
	"context"
	"encoding/json"
	"flag"
	"strings"
	"time"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	constraintTypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/pkg/errors"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	crd           = &apiextensionsv1beta1.CustomResourceDefinition{}
	emptyAuditResults []auditResult
)

// auditManager allows us to audit resources periodically
type AuditManager struct {
	client  client.Client
	opa     opa.Client
	stopper chan struct{}
	stopped chan struct{}
	cfg     *rest.Config
}

type auditResult struct {
	cname       string
	cnamespace  string
	cgvk        schema.GroupVersionKind
	capiversion string
	rkind       string
	rname       string
	rnamespace  string
	message     string
}

// New starts a new manager for audit
func New(ctx context.Context, cfg *rest.Config, opa opa.Client) (*AuditManager, error) {
	am := &AuditManager{
		opa:     opa,
		stopper: make(chan struct{}),
		stopped: make(chan struct{}),
		cfg:     cfg,
	}
	go am.auditManagerLoop(ctx)
	return am, nil
}

// audit audits resources periodically
func (am *AuditManager) audit(ctx context.Context) error {	
	// new client to get updated restmapper
	c, err := client.New(am.cfg, client.Options{Scheme: nil, Mapper: nil})
	if err != nil {
		return err
	}
	am.client = c
	// don't audit anything until the constraintTemplate crd is in the cluster
	if err := am.ensureCRDExists(ctx); err != nil {
		log.Info("Audit exits, required crd has not been deployed ", "CRD", crdName)
		return nil
	}
	resp, err := am.opa.Audit(ctx)
	if err != nil {
		return err
	}
	log.Info("Audit opa.Audit() audit results", "violations", len(resp.Results()))
	// get updatedLists
	updateLists := make(map[string][]auditResult)
	if len(resp.Results()) > 0 {
		updateLists, err = getUpdateListsFromAuditResponses(resp)
		if err != nil {
			return err
		}
	}
	// get all constraints kind
	log.Info("getting all constraints kind")
	rs, err := am.getAllConstraintsKinds()
	if err != nil {
		log.Info("no constraints found with apiversion", "constraint apiversion", constraintsGV)
		return nil
	}
	// update constraints for each kind
	return am.updateConstraintsForKinds(ctx, rs, updateLists)
}

func (am *AuditManager) auditManagerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Info("Audit Manager close")
			close(am.stopper)
			return
		default:
			time.Sleep(time.Duration(*auditInterval) * time.Second)
			if err := am.audit(ctx); err != nil {
				log.Error(err, "audit manager audit() failed")
			}
		}
	}
}

// Start implements controller.Controller
func (am *AuditManager) Start(stop <-chan struct{}) error {
	log.Info("Starting Audit Manager")

	<-stop
	log.Info("Stopping audit manager workers")
	return nil
}

func (am *AuditManager) ensureCRDExists(ctx context.Context) error {
	return am.client.Get(ctx, types.NamespacedName{Name: crdName}, crd)
}

func (am *AuditManager) getAllConstraintsKinds() (*metav1.APIResourceList, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(am.cfg)
	if err != nil {
		return nil, err
	}
	return discoveryClient.ServerResourcesForGroupVersion(constraintsGV)
}

func getUpdateListsFromAuditResponses(resp *constraintTypes.Responses) (map[string][]auditResult, error) {
	updateLists := make(map[string][]auditResult)

	for _, r := range resp.Results() {
		name := r.Constraint.GetName()
		namespace := r.Constraint.GetNamespace()
		apiVersion := r.Constraint.GetAPIVersion()
		gvk := r.Constraint.GroupVersionKind()
		selfLink := r.Constraint.GetSelfLink()
		message := r.Msg
		if len(message) > msgSize {
			message = truncateString(message, msgSize)
		}
		resource, ok := r.Resource.(*unstructured.Unstructured)
		if !ok {
			return nil, errors.Errorf("could not cast resource as reviewResource: %v", r.Resource)
		}
		rname := resource.GetName()
		rkind := resource.GetKind()
		rnamespace := resource.GetNamespace()

		updateLists[selfLink] = append(updateLists[selfLink], auditResult{
			cgvk:        gvk,
			capiversion: apiVersion,
			cname:       name,
			cnamespace:  namespace,
			rkind:       rkind,
			rname:       rname,
			rnamespace:  rnamespace,
			message:     message,
		})
	}
	return updateLists, nil
}

func (am *AuditManager) updateConstraintsForKinds (ctx context.Context, resourceList *metav1.APIResourceList, updateLists map[string][]auditResult) error {
	resourceGV := strings.Split(resourceList.GroupVersion, "/")
	group := resourceGV[0]
	version := resourceGV[1]

	// get constraints for each Kind
	for _, r := range resourceList.APIResources {
		log.Info("constraint", "resource kind", r.Kind)
		constraintGvk := schema.GroupVersionKind {
			Group: group,
			Version: version,
			Kind: r.Kind,
		}
		instanceList := &unstructured.UnstructuredList{}
		instanceList.SetGroupVersionKind(constraintGvk)
		log.Info("client before list")
		err := am.client.List(ctx, &client.ListOptions{}, instanceList)
		if err != nil {
			return err
		}
		log.Info("constraint", "count of constraints", len(instanceList.Items))
		// get each constraint
		for _, item := range instanceList.Items {
			oname := item.GetName()
			// if constraint is in updatedLists, then update its violations
			constraintAuditResults, ok := updateLists[item.GetSelfLink()]
			// else set to empty if not empty
			if !ok {
				violations, found, err := unstructured.NestedSlice(item.Object, "status", "violations")
				if err != nil {
					return err
				}
				if !found || len(violations) == 0 {
					log.Info("constraint violations is either not found or empty, skip clear", "constraint name", oname)
				} else {
					err = am.updateConstraintStatus(ctx, &item, emptyAuditResults)
					if err != nil {
						return err
					}
					log.Info("constraint violations have been cleared", "constraint name", oname)
				}
			} else {
				aResult := constraintAuditResults[0]
				instance := &unstructured.Unstructured{}
				instance.SetGroupVersionKind(aResult.cgvk)
				namespacedName := types.NamespacedName{
					Name: aResult.cname,
					Namespace: aResult.cnamespace,
				}
				// get the constraint
				err := am.client.Get(ctx, namespacedName, instance)
				if err != nil {
					log.Error(err, "get constraint error", err)
				}
				// update the constraint
				err = am.updateConstraintStatus(ctx, instance, constraintAuditResults)
				if err != nil {
					log.Error(err, "update constraint error", err)
				}
			}
		}
	}
	return nil
}

func (am *AuditManager) updateConstraintStatus(ctx context.Context, instance *unstructured.Unstructured, auditResults []auditResult) error {
	log.Info("updating constraint", "constraint name", instance.GetName())
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
		unstructured.RemoveNestedField(instance.Object, "status", "violations")
		err = am.client.Update(ctx, instance)
		if err != nil {
			return err
		}
		log.Info("removed status violations")
	} else {
		unstructured.SetNestedSlice(instance.Object, violations, "status", "violations")
		log.Info("update constraint", "object", instance)
		err = am.client.Update(ctx, instance)
		if err != nil {
			return err
		}
		log.Info("updated constraint status violations", "constraint", instance.GetName(), "count", len(violations))
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
