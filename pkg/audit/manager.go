package audit

import (
	"context"
	"encoding/json"
	"flag"
	"strings"
	"time"

	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	constraintTypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/pkg/errors"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var log = logf.Log.WithName("controller").WithValues("metaKind", "audit")

const (
	crdName                          = "constrainttemplates.templates.gatekeeper.sh"
	constraintsGV                    = "constraints.gatekeeper.sh/v1beta1"
	msgSize                          = 256
	defaultAuditInterval             = 60
	defaultConstraintViolationsLimit = 20
)

var (
	auditInterval                       = flag.Int("audit-interval", defaultAuditInterval, "interval to run audit in seconds. defaulted to 60 secs if unspecified, 0 to disable ")
	constraintViolationsLimit           = flag.Int("constraint-violations-limit", defaultConstraintViolationsLimit, "limit of number of violations per constraint. defaulted to 20 violations if unspecified ")
	auditIntervalDeprecated             = flag.Int("auditInterval", defaultAuditInterval, "DEPRECATED - use --audit-interval")
	constraintViolationsLimitDeprecated = flag.Int("constraintViolationsLimit", defaultConstraintViolationsLimit, "DEPRECATED - use --constraint-violations-limit")
	emptyAuditResults                   []auditResult
)

// Manager allows us to audit resources periodically
type Manager struct {
	client   client.Client
	opa      *opa.Client
	stopper  chan struct{}
	stopped  chan struct{}
	mgr      manager.Manager
	ctx      context.Context
	ucloop   *updateConstraintLoop
	reporter *reporter
}

type auditResult struct {
	cname             string
	cnamespace        string
	cgvk              schema.GroupVersionKind
	capiversion       string
	rkind             string
	rname             string
	rnamespace        string
	message           string
	enforcementAction string
}

// StatusViolation represents each violation under status
type StatusViolation struct {
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Namespace         string `json:"namespace,omitempty"`
	Message           string `json:"message"`
	EnforcementAction string `json:"enforcementAction"`
}

// New creates a new manager for audit
func New(ctx context.Context, mgr manager.Manager, opa *opa.Client) (*Manager, error) {
	checkDeprecatedFlags()
	reporter, err := newStatsReporter()
	if err != nil {
		log.Error(err, "StatsReporter could not start")
		return nil, err
	}

	am := &Manager{
		opa:      opa,
		stopper:  make(chan struct{}),
		stopped:  make(chan struct{}),
		mgr:      mgr,
		ctx:      ctx,
		reporter: reporter,
	}
	return am, nil
}

// audit performs an audit then updates the status of all constraint resources with the results
func (am *Manager) audit(ctx context.Context) error {
	startTime := time.Now()
	// record audit latency
	defer func() {
		latency := time.Since(startTime)
		if err := am.reporter.reportLatency(latency); err != nil {
			log.Error(err, "failed to report latency")
		}
	}()

	if err := am.reporter.reportRunStart(startTime); err != nil {
		log.Error(err, "failed to report run start time")
	}

	timestamp := startTime.UTC().Format(time.RFC3339)
	// new client to get updated restmapper
	c, err := client.New(am.mgr.GetConfig(), client.Options{Scheme: am.mgr.GetScheme(), Mapper: nil})
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

	updateLists, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, err := getUpdateListsFromAuditResponses(resp)
	if err != nil {
		return err
	}
	for k, v := range totalViolationsPerEnforcementAction {
		if err := am.reporter.reportTotalViolations(k, v); err != nil {
			log.Error(err, "failed to report total violations")
		}
	}
	// get all constraint kinds
	rs, err := am.getAllConstraintKinds()
	if err != nil {
		// if no constraint is found with the constraint apiversion, then return
		log.Info("no constraint is found with apiversion", "constraint apiversion", constraintsGV)
		return nil
	}
	// update constraints for each kind
	return am.writeAuditResults(ctx, rs, updateLists, timestamp, totalViolationsPerConstraint)
}

func (am *Manager) auditManagerLoop(ctx context.Context) {
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
func (am *Manager) Start(stop <-chan struct{}) error {
	log.Info("Starting Audit Manager")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go am.auditManagerLoop(ctx)
	<-stop
	log.Info("Stopping audit manager workers")
	return nil
}

func (am *Manager) ensureCRDExists(ctx context.Context) error {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{}
	return am.client.Get(ctx, types.NamespacedName{Name: crdName}, crd)
}

func (am *Manager) getAllConstraintKinds() (*metav1.APIResourceList, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(am.mgr.GetConfig())
	if err != nil {
		return nil, err
	}
	return discoveryClient.ServerResourcesForGroupVersion(constraintsGV)
}

func getUpdateListsFromAuditResponses(resp *constraintTypes.Responses) (map[string][]auditResult, map[string]int64, map[util.EnforcementAction]int64, error) {
	updateLists := make(map[string][]auditResult)
	totalViolationsPerConstraint := make(map[string]int64)
	totalViolationsPerEnforcementAction := make(map[util.EnforcementAction]int64)
	// resetting total violations per enforcement action
	for _, action := range util.KnownEnforcementActions {
		totalViolationsPerEnforcementAction[action] = 0
	}

	for _, r := range resp.Results() {
		selfLink := r.Constraint.GetSelfLink()
		totalViolationsPerConstraint[selfLink]++
		// skip if this constraint has reached the constraintViolationsLimit
		if len(updateLists[selfLink]) < *constraintViolationsLimit {
			name := r.Constraint.GetName()
			namespace := r.Constraint.GetNamespace()
			apiVersion := r.Constraint.GetAPIVersion()
			gvk := r.Constraint.GroupVersionKind()
			enforcementAction := r.EnforcementAction
			message := r.Msg
			if len(message) > msgSize {
				message = truncateString(message, msgSize)
			}
			resource, ok := r.Resource.(*unstructured.Unstructured)
			if !ok {
				return nil, nil, nil, errors.Errorf("could not cast resource as reviewResource: %v", r.Resource)
			}
			rname := resource.GetName()
			rkind := resource.GetKind()
			rnamespace := resource.GetNamespace()
			updateLists[selfLink] = append(updateLists[selfLink], auditResult{
				cgvk:              gvk,
				capiversion:       apiVersion,
				cname:             name,
				cnamespace:        namespace,
				rkind:             rkind,
				rname:             rname,
				rnamespace:        rnamespace,
				message:           message,
				enforcementAction: enforcementAction,
			})
		}
		enforcementAction := util.EnforcementAction(r.EnforcementAction)
		totalViolationsPerEnforcementAction[enforcementAction]++
	}
	return updateLists, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, nil
}

func (am *Manager) writeAuditResults(ctx context.Context, resourceList *metav1.APIResourceList, updateLists map[string][]auditResult, timestamp string, totalViolations map[string]int64) error {
	resourceGV := strings.Split(resourceList.GroupVersion, "/")
	group := resourceGV[0]
	version := resourceGV[1]

	// get constraints for each Kind
	for _, r := range resourceList.APIResources {
		log.Info("constraint", "resource kind", r.Kind)
		constraintGvk := schema.GroupVersionKind{
			Group:   group,
			Version: version,
			Kind:    r.Kind + "List",
		}
		instanceList := &unstructured.UnstructuredList{}
		instanceList.SetGroupVersionKind(constraintGvk)
		err := am.client.List(ctx, instanceList)
		if err != nil {
			return err
		}
		log.Info("constraint", "count of constraints", len(instanceList.Items))

		updateConstraints := make(map[string]unstructured.Unstructured, len(instanceList.Items))
		// get each constraint
		for _, item := range instanceList.Items {
			updateConstraints[item.GetSelfLink()] = item
		}
		if len(updateConstraints) > 0 {
			if am.ucloop != nil {
				close(am.ucloop.stop)
				select {
				case <-am.ucloop.stopped:
				case <-time.After(time.Duration(*auditInterval) * time.Second):
				}
			}
			am.ucloop = &updateConstraintLoop{
				uc:      updateConstraints,
				client:  am.client,
				stop:    make(chan struct{}),
				stopped: make(chan struct{}),
				ul:      updateLists,
				ts:      timestamp,
				tv:      totalViolations,
			}
			log.Info("starting update constraints loop", "updateConstraints", updateConstraints)
			go am.ucloop.update()
		}
	}
	return nil
}

func (ucloop *updateConstraintLoop) updateConstraintStatus(ctx context.Context, instance *unstructured.Unstructured, auditResults []auditResult, timestamp string, totalViolations int64) error {
	constraintName := instance.GetName()
	log.Info("updating constraint status", "constraintName", constraintName)
	// create constraint status violations
	var statusViolations []interface{}
	for _, ar := range auditResults {
		statusViolations = append(statusViolations, StatusViolation{
			Kind:              ar.rkind,
			Name:              ar.rname,
			Namespace:         ar.rnamespace,
			Message:           ar.message,
			EnforcementAction: ar.enforcementAction,
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
	// update constraint status auditTimestamp
	if err = unstructured.SetNestedField(instance.Object, timestamp, "status", "auditTimestamp"); err != nil {
		return err
	}
	// update constraint status totalViolations
	if err = unstructured.SetNestedField(instance.Object, totalViolations, "status", "totalViolations"); err != nil {
		return err
	}
	// update constraint status violations
	if len(violations) == 0 {
		_, found, err := unstructured.NestedSlice(instance.Object, "status", "violations")
		if err != nil {
			return err
		}
		if found {
			unstructured.RemoveNestedField(instance.Object, "status", "violations")
			log.Info("removed status violations", "constraintName", constraintName)
		}
		err = ucloop.client.Update(ctx, instance)
		if err != nil {
			return err
		}
	} else {
		if err := unstructured.SetNestedSlice(instance.Object, violations, "status", "violations"); err != nil {
			return err
		}
		log.Info("update constraint", "object", instance)
		err = ucloop.client.Update(ctx, instance)
		if err != nil {
			return err
		}
		log.Info("updated constraint status violations", "constraintName", constraintName, "count", len(violations))
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

type updateConstraintLoop struct {
	uc      map[string]unstructured.Unstructured
	client  client.Client
	stop    chan struct{}
	stopped chan struct{}
	ul      map[string][]auditResult
	ts      string
	tv      map[string]int64
}

func (ucloop *updateConstraintLoop) update() {
	defer close(ucloop.stopped)
	updateLoop := func() (bool, error) {
		for _, item := range ucloop.uc {
			select {
			case <-ucloop.stop:
				return true, nil
			default:
				failure := false
				ctx := context.Background()
				var latestItem unstructured.Unstructured
				item.DeepCopyInto(&latestItem)
				name := latestItem.GetName()
				namespace := latestItem.GetNamespace()
				namespacedName := types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				}
				// get the latest constraint
				err := ucloop.client.Get(ctx, namespacedName, &latestItem)
				if err != nil {
					failure = true
					log.Error(err, "could not get latest constraint during update", "name", name, "namespace", namespace)
				}
				if constraintAuditResults, ok := ucloop.ul[latestItem.GetSelfLink()]; !ok {
					err := ucloop.updateConstraintStatus(ctx, &latestItem, emptyAuditResults, ucloop.ts, 0)
					if err != nil {
						failure = true
						log.Error(err, "could not update constraint status", "name", name, "namespace", namespace)
					}
				} else {
					totalViolations := ucloop.tv[latestItem.GetSelfLink()]
					// update the constraint
					err := ucloop.updateConstraintStatus(ctx, &latestItem, constraintAuditResults, ucloop.ts, totalViolations)
					if err != nil {
						failure = true
						log.Error(err, "could not update constraint status", "name", name, "namespace", namespace)
					}
				}
				if !failure {
					delete(ucloop.uc, latestItem.GetSelfLink())
				}
			}
		}
		if len(ucloop.uc) == 0 {
			return true, nil
		}
		return false, nil
	}

	if err := wait.ExponentialBackoff(wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2,
		Jitter:   1,
		Steps:    5,
	}, updateLoop); err != nil {
		log.Error(err, "could not update constraint reached max retries", "remaining update constraints", ucloop.uc)
	}
}

// Temporary fallback to check deprecated --auditInterval and --constraintViolationsLimit flags, which are now --audit-interval and --constraint-violations-limit
// @TODO to be removed in an upcoming release
func checkDeprecatedFlags() {
	foundAuditInterval := false
	foundConstraintViolationsLimit := false

	flag.Visit(func(f *flag.Flag) {
		if f.Name == "audit-interval" {
			foundAuditInterval = true
		} else if f.Name == "constraint-violations-limit" {
			foundConstraintViolationsLimit = true
		}
	})

	if !foundAuditInterval {
		auditInterval = auditIntervalDeprecated
	}
	if !foundConstraintViolationsLimit {
		constraintViolationsLimit = constraintViolationsLimitDeprecated
	}
}
