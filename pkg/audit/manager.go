package audit

import (
	"context"
	"encoding/json"
	"flag"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	opa "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	constraintTypes "github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/pkg/controller/config/process"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/target"
	"github.com/open-policy-agent/gatekeeper/pkg/util"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var log = logf.Log.WithName("controller").WithValues(logging.Process, "audit")

const (
	crdName                          = "constrainttemplates.templates.gatekeeper.sh"
	constraintsGV                    = "constraints.gatekeeper.sh/v1beta1"
	msgSize                          = 256
	defaultAuditInterval             = 60
	defaultConstraintViolationsLimit = 20
	defaultListLimit                 = 0
)

var (
	auditInterval             = flag.Uint("audit-interval", defaultAuditInterval, "interval to run audit in seconds. defaulted to 60 secs if unspecified, 0 to disable")
	constraintViolationsLimit = flag.Uint("constraint-violations-limit", defaultConstraintViolationsLimit, "limit of number of violations per constraint. defaulted to 20 violations if unspecified")
	auditChunkSize            = flag.Uint64("audit-chunk-size", defaultListLimit, "(alpha) Kubernetes API chunking List results when retrieving cluster resources using discovery client. defaulted to 0 if unspecified")
	auditFromCache            = flag.Bool("audit-from-cache", false, "pull resources from OPA cache when auditing")
	emitAuditEvents           = flag.Bool("emit-audit-events", false, "(alpha) emit Kubernetes events in gatekeeper namespace with detailed info for each violation from an audit")
	emptyAuditResults         []auditResult
)

// Manager allows us to audit resources periodically
type Manager struct {
	client          client.Client
	opa             *opa.Client
	stopper         chan struct{}
	stopped         chan struct{}
	mgr             manager.Manager
	ctx             context.Context
	ucloop          *updateConstraintLoop
	reporter        *reporter
	log             logr.Logger
	processExcluder *process.Excluder
	eventRecorder   record.EventRecorder
	gkNamespace     string
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
	constraint        *unstructured.Unstructured
}

// StatusViolation represents each violation under status
type StatusViolation struct {
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Namespace         string `json:"namespace,omitempty"`
	Message           string `json:"message"`
	EnforcementAction string `json:"enforcementAction"`
}

// nsCache is used for caching namespaces and their labels
type nsCache struct {
	cache map[string]corev1.Namespace
}

func newNSCache() *nsCache {
	return &nsCache{
		cache: make(map[string]corev1.Namespace),
	}
}

func (c *nsCache) Get(ctx context.Context, client client.Client, namespace string) (corev1.Namespace, error) {
	if ns, ok := c.cache[namespace]; !ok {
		if err := client.Get(ctx, types.NamespacedName{Name: namespace}, &ns); err != nil {
			return corev1.Namespace{}, err
		}
		c.cache[namespace] = ns
	}

	return c.cache[namespace], nil
}

// New creates a new manager for audit
func New(ctx context.Context, mgr manager.Manager, opa *opa.Client, processExcluder *process.Excluder) (*Manager, error) {
	reporter, err := newStatsReporter()
	if err != nil {
		log.Error(err, "StatsReporter could not start")
		return nil, err
	}
	eventBroadcaster := record.NewBroadcaster()
	kubeClient := kubernetes.NewForConfigOrDie(mgr.GetConfig())
	eventBroadcaster.StartRecordingToSink(&clientcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(
		scheme.Scheme,
		corev1.EventSource{Component: "gatekeeper-audit"})

	am := &Manager{
		opa:             opa,
		stopper:         make(chan struct{}),
		stopped:         make(chan struct{}),
		mgr:             mgr,
		ctx:             ctx,
		reporter:        reporter,
		processExcluder: processExcluder,
		eventRecorder:   recorder,
		gkNamespace:     util.GetNamespace(),
	}
	return am, nil
}

// audit performs an audit then updates the status of all constraint resources with the results
func (am *Manager) audit(ctx context.Context) error {
	startTime := time.Now()
	timestamp := startTime.UTC().Format(time.RFC3339)
	am.log = log.WithValues(logging.AuditID, timestamp)
	logStart(am.log)
	// record audit latency
	defer func() {
		logFinish(am.log)
		latency := time.Since(startTime)
		if err := am.reporter.reportLatency(latency); err != nil {
			am.log.Error(err, "failed to report latency")
		}
	}()

	if err := am.reporter.reportRunStart(startTime); err != nil {
		am.log.Error(err, "failed to report run start time")
	}

	// new client to get updated restmapper
	c, err := client.New(am.mgr.GetConfig(), client.Options{Scheme: am.mgr.GetScheme(), Mapper: nil})
	if err != nil {
		return err
	}
	am.client = c
	// don't audit anything until the constraintTemplate crd is in the cluster
	if err := am.ensureCRDExists(ctx); err != nil {
		am.log.Info("Audit exits, required crd has not been deployed ", "CRD", crdName)
		return nil
	}

	// get all constraint kinds
	resourceList, err := am.getAllConstraintKinds()
	if err != nil {
		// if no constraint is found with the constraint apiversion, then return
		am.log.Info("no constraint is found with apiversion", "constraint apiversion", constraintsGV)
		return nil
	}

	var resp *constraintTypes.Responses
	var res []*constraintTypes.Result

	updateLists := make(map[string][]auditResult)
	totalViolationsPerConstraint := make(map[string]int64)
	totalViolationsPerEnforcementAction := make(map[util.EnforcementAction]int64)
	// resetting total violations per enforcement action
	for _, action := range util.KnownEnforcementActions {
		totalViolationsPerEnforcementAction[action] = 0
	}

	if *auditFromCache {
		am.log.Info("Auditing from cache")
		resp, err = am.opa.Audit(ctx)
		if err != nil {
			return err
		}
		res = resp.Results()
		am.log.Info("Audit opa.Audit() results", "violations", len(res))

		err := am.addAuditResponsesToUpdateLists(updateLists, res, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, timestamp)
		if err != nil {
			return err
		}
	} else {
		am.log.Info("Auditing via discovery client")
		err := am.auditResources(ctx, resourceList, updateLists, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, timestamp)
		if err != nil {
			return err
		}
	}

	// log constraints with violations
	for link := range updateLists {
		ar := updateLists[link][0]
		logConstraint(am.log, ar.constraint, ar.enforcementAction, totalViolationsPerConstraint[link])
	}

	for k, v := range totalViolationsPerEnforcementAction {
		if err := am.reporter.reportTotalViolations(k, v); err != nil {
			am.log.Error(err, "failed to report total violations")
		}
	}

	// update constraints for each kind
	return am.writeAuditResults(ctx, resourceList, updateLists, timestamp, totalViolationsPerConstraint)
}

// Audits server resources via the discovery client, as an alternative to opa.Client.Audit()
func (am *Manager) auditResources(
	ctx context.Context,
	resourceList []schema.GroupVersionKind,
	updateLists map[string][]auditResult,
	totalViolationsPerConstraint map[string]int64,
	totalViolationsPerEnforcementAction map[util.EnforcementAction]int64,
	timestamp string) error {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(am.mgr.GetConfig())
	if err != nil {
		return err
	}

	serverResourceLists, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		return err
	}

	clusterAPIResources := make(map[metav1.GroupVersion]map[string]bool)
	for _, rl := range serverResourceLists {
		gvParsed, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			am.log.Error(err, "Error parsing groupversion", "groupversion", rl.GroupVersion)
			continue
		}

		gv := metav1.GroupVersion{
			Group:   gvParsed.Group,
			Version: gvParsed.Version,
		}
		if _, ok := clusterAPIResources[gv]; !ok {
			clusterAPIResources[gv] = make(map[string]bool)
		}
		for _, resource := range rl.APIResources {
			for _, verb := range resource.Verbs {
				if verb == "list" {
					clusterAPIResources[gv][resource.Kind] = true
					break
				}
			}
		}
	}

	var errs opa.Errors
	nsCache := newNSCache()

	for gv, gvKinds := range clusterAPIResources {
		for kind := range gvKinds {
			objList := &unstructured.UnstructuredList{}
			opts := &client.ListOptions{
				Limit: int64(*auditChunkSize),
			}
			resourceVersion := ""

			for {
				objList.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   gv.Group,
					Version: gv.Version,
					Kind:    kind + "List",
				})
				objList.SetResourceVersion(resourceVersion)

				err := am.client.List(ctx, objList, opts)
				if err != nil {
					am.log.Error(err, "Unable to list objects for gvk", "group", gv.Group, "version", gv.Version, "kind", kind)
					continue
				}

				for _, obj := range objList.Items {
					objNamespace := obj.GetNamespace()
					if am.skipExcludedNamespace(objNamespace) {
						continue
					}

					ns := corev1.Namespace{}
					if objNamespace != "" {
						ns, err = nsCache.Get(ctx, am.client, objNamespace)
						if err != nil {
							am.log.Error(err, "Unable to look up object namespace", "group", gv.Group, "version", gv.Version, "kind", kind)
							continue
						}
					}

					augmentedObj := target.AugmentedUnstructured{
						Object:    obj,
						Namespace: &ns,
					}
					resp, err := am.opa.Review(ctx, augmentedObj)
					if err != nil {
						errs = append(errs, err)
					} else if len(resp.Results()) > 0 {
						err = am.addAuditResponsesToUpdateLists(updateLists, resp.Results(), totalViolationsPerConstraint, totalViolationsPerEnforcementAction, timestamp)
						if err != nil {
							return err
						}
					}
				}

				resourceVersion = objList.GetResourceVersion()
				opts.Continue = objList.GetContinue()
				if opts.Continue == "" {
					break
				}
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
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

func (am *Manager) getAllConstraintKinds() ([]schema.GroupVersionKind, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(am.mgr.GetConfig())
	if err != nil {
		return nil, err
	}
	l, err := discoveryClient.ServerResourcesForGroupVersion(constraintsGV)
	if err != nil {
		return nil, err
	}
	resourceGV := strings.Split(constraintsGV, "/")
	group := resourceGV[0]
	version := resourceGV[1]
	// We have seen duplicate GVK entries on shifting to status client, remove them
	unique := make(map[schema.GroupVersionKind]bool)
	for _, i := range l.APIResources {
		unique[schema.GroupVersionKind{Group: group, Version: version, Kind: i.Kind}] = true
	}
	var ret []schema.GroupVersionKind
	for gvk := range unique {
		ret = append(ret, gvk)
	}
	return ret, nil
}

func (am *Manager) addAuditResponsesToUpdateLists(
	updateLists map[string][]auditResult,
	res []*constraintTypes.Result,
	totalViolationsPerConstraint map[string]int64,
	totalViolationsPerEnforcementAction map[util.EnforcementAction]int64,
	timestamp string) error {
	for _, r := range res {
		selfLink := r.Constraint.GetSelfLink()
		totalViolationsPerConstraint[selfLink]++
		name := r.Constraint.GetName()
		namespace := r.Constraint.GetNamespace()
		apiVersion := r.Constraint.GetAPIVersion()
		gvk := r.Constraint.GroupVersionKind()
		enforcementAction := r.EnforcementAction
		message := r.Msg
		resource, ok := r.Resource.(*unstructured.Unstructured)
		if !ok {
			return errors.Errorf("could not cast resource as reviewResource: %v", r.Resource)
		}
		rname := resource.GetName()
		rkind := resource.GetKind()
		rnamespace := resource.GetNamespace()
		// append audit results only if it is below violations limit
		if uint(len(updateLists[selfLink])) < *constraintViolationsLimit {
			result := auditResult{
				cgvk:              gvk,
				capiversion:       apiVersion,
				cname:             name,
				cnamespace:        namespace,
				rkind:             rkind,
				rname:             rname,
				rnamespace:        rnamespace,
				message:           message,
				enforcementAction: enforcementAction,
				constraint:        r.Constraint,
			}
			updateLists[selfLink] = append(updateLists[selfLink], result)
		}
		ea := util.EnforcementAction(enforcementAction)
		totalViolationsPerEnforcementAction[ea]++
		logViolation(am.log, r.Constraint, r.EnforcementAction, rkind, rnamespace, rname, message)
		if *emitAuditEvents {
			emitEvent(r.Constraint, timestamp, enforcementAction, rkind, rnamespace, rname, message, am.gkNamespace, am.eventRecorder)
		}
	}
	return nil
}

func (am *Manager) writeAuditResults(ctx context.Context, resourceList []schema.GroupVersionKind, updateLists map[string][]auditResult, timestamp string, totalViolations map[string]int64) error {
	// get constraints for each Kind
	for _, constraintGvk := range resourceList {
		am.log.Info("constraint", "resource kind", constraintGvk.Kind)
		instanceList := &unstructured.UnstructuredList{}
		instanceList.SetGroupVersionKind(constraintGvk)
		err := am.client.List(ctx, instanceList)
		if err != nil {
			return err
		}
		am.log.Info("constraint", "count of constraints", len(instanceList.Items))

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
			am.log.Info("starting update constraints loop", "updateConstraints", updateConstraints)
			go am.ucloop.update()
		}
	}
	return nil
}

func (am *Manager) skipExcludedNamespace(namespace string) bool {
	return am.processExcluder.IsNamespaceExcluded(process.Audit, namespace)
}

func (ucloop *updateConstraintLoop) updateConstraintStatus(ctx context.Context, instance *unstructured.Unstructured, auditResults []auditResult, timestamp string, totalViolations int64) error {
	constraintName := instance.GetName()
	log.Info("updating constraint status", "constraintName", constraintName)
	// create constraint status violations
	var statusViolations []interface{}
	for _, ar := range auditResults {
		// append statusViolations for this constraint until constraintViolationsLimit has reached
		if uint(len(statusViolations)) < *constraintViolationsLimit {
			msg := ar.message
			if len(msg) > msgSize {
				msg = truncateString(msg, msgSize)
			}
			statusViolations = append(statusViolations, StatusViolation{
				Kind:              ar.rkind,
				Name:              ar.rname,
				Namespace:         ar.rnamespace,
				Message:           msg,
				EnforcementAction: ar.enforcementAction,
			})
		}
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
		err = ucloop.client.Status().Update(ctx, instance)
		if err != nil {
			return err
		}
	} else {
		if err := unstructured.SetNestedSlice(instance.Object, violations, "status", "violations"); err != nil {
			return err
		}
		log.Info("update constraint", "object", instance)
		err = ucloop.client.Status().Update(ctx, instance)
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
				totalViolations := ucloop.tv[latestItem.GetSelfLink()]
				if constraintAuditResults, ok := ucloop.ul[latestItem.GetSelfLink()]; !ok {
					err := ucloop.updateConstraintStatus(ctx, &latestItem, emptyAuditResults, ucloop.ts, totalViolations)
					if err != nil {
						failure = true
						log.Error(err, "could not update constraint status", "name", name, "namespace", namespace)
					}
				} else {
					// update the constraint
					err := ucloop.updateConstraintStatus(ctx, &latestItem, constraintAuditResults, ucloop.ts, totalViolations)
					if err != nil {
						failure = true
						log.Error(err, "could not update constraint status", "name", name, "namespace", namespace)
					}
				}
				if !failure {
					delete(ucloop.uc, item.GetSelfLink())
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

func logStart(l logr.Logger) {
	l.Info(
		"auditing constraints and violations",
		logging.EventType, "audit_started",
	)
}

func logFinish(l logr.Logger) {
	l.Info(
		"auditing is complete",
		logging.EventType, "audit_finished",
	)
}

func logConstraint(l logr.Logger, constraint *unstructured.Unstructured, enforcementAction string, totalViolations int64) {
	l.Info(
		"audit results for constraint",
		logging.EventType, "constraint_audited",
		logging.ConstraintKind, constraint.GetKind(),
		logging.ConstraintName, constraint.GetName(),
		logging.ConstraintNamespace, constraint.GetNamespace(),
		logging.ConstraintAction, enforcementAction,
		logging.ConstraintStatus, "enforced",
		logging.ConstraintViolations, strconv.FormatInt(totalViolations, 10),
	)
}

func logViolation(l logr.Logger,
	constraint *unstructured.Unstructured,
	enforcementAction, rkind, rnamespace, rname, message string) {
	l.Info(
		message,
		logging.EventType, "violation_audited",
		logging.ConstraintKind, constraint.GetKind(),
		logging.ConstraintName, constraint.GetName(),
		logging.ConstraintNamespace, constraint.GetNamespace(),
		logging.ConstraintAction, enforcementAction,
		logging.ResourceKind, rkind,
		logging.ResourceNamespace, rnamespace,
		logging.ResourceName, rname,
	)
}

func emitEvent(constraint *unstructured.Unstructured,
	timestamp, enforcementAction, rkind, rnamespace, rname, message, gkNamespace string,
	eventRecorder record.EventRecorder) {
	annotations := map[string]string{
		"process":                   "audit",
		"auditTimestamp":            timestamp,
		logging.EventType:           "violation_audited",
		logging.ConstraintKind:      constraint.GetKind(),
		logging.ConstraintName:      constraint.GetName(),
		logging.ConstraintNamespace: constraint.GetNamespace(),
		logging.ConstraintAction:    enforcementAction,
		logging.ResourceKind:        rkind,
		logging.ResourceNamespace:   rnamespace,
		logging.ResourceName:        rname,
	}
	reason := "AuditViolation"
	ref := getViolationRef(gkNamespace, rkind, rname, rnamespace, constraint.GetKind(), constraint.GetName(), constraint.GetNamespace())

	eventRecorder.AnnotatedEventf(ref, annotations, corev1.EventTypeWarning, reason, "Timestamp: %s, Resource Namespace: %s, Constraint: %s, Message: %s", timestamp, rnamespace, constraint.GetName(), message)
}

func getViolationRef(gkNamespace, rkind, rname, rnamespace, ckind, cname, cnamespace string) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		Kind:      rkind,
		Name:      rname,
		UID:       types.UID(rkind + "/" + rnamespace + "/" + rname + "/" + ckind + "/" + cnamespace + "/" + cname),
		Namespace: gkNamespace,
	}
}
