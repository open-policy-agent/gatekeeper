package audit

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/controller/config/process"
	pubsubController "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/pubsub"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/expansion"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/logging"
	mutationtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
	defaultListLimit                 = 500
	defaultAPICacheDir               = "/tmp/audit"
	defaultConnection                = "audit-connection"
	defaultChannel                   = "audit-channel"
)

var (
	auditInterval                = flag.Uint("audit-interval", defaultAuditInterval, "interval to run audit in seconds. defaulted to 60 secs if unspecified, 0 to disable")
	constraintViolationsLimit    = flag.Uint("constraint-violations-limit", defaultConstraintViolationsLimit, "limit of number of violations per constraint. defaulted to 20 violations if unspecified")
	auditChunkSize               = flag.Uint64("audit-chunk-size", defaultListLimit, "(alpha) Kubernetes API chunking List results when retrieving cluster resources using discovery client. defaulted to 500 if unspecified")
	auditFromCache               = flag.Bool("audit-from-cache", false, "audit synced resources from internal cache, bypassing direct queries to Kubernetes API server")
	emitAuditEvents              = flag.Bool("emit-audit-events", false, "(alpha) emit Kubernetes events with detailed info for each violation from an audit")
	auditEventsInvolvedNamespace = flag.Bool("audit-events-involved-namespace", false, "emit audit events for each violation in the involved objects namespace, the default (false) generates events in the namespace Gatekeeper is installed in. Audit events from cluster-scoped resources will still follow the default behavior")
	auditMatchKindOnly           = flag.Bool("audit-match-kind-only", false, "only use kinds specified in all constraints for auditing cluster resources. if kind is not specified in any of the constraints, it will audit all resources (same as setting this flag to false)")
	apiCacheDir                  = flag.String("api-cache-dir", defaultAPICacheDir, "The directory where audit from api server cache are stored, defaults to /tmp/audit")
	auditConnection              = flag.String("audit-connection", defaultConnection, "Connection name for publishing audit violation messages. Defaults to audit-connection")
	auditChannel                 = flag.String("audit-channel", defaultChannel, "Channel name for publishing audit violation messages. Defaults to audit-channel")
	emptyAuditResults            []updateListEntry
	logStatsAudit                = flag.Bool("log-stats-audit", false, "(alpha) log stats metrics for the audit run")
)

// Manager allows us to audit resources periodically.
type Manager struct {
	client          client.Client
	opa             *constraintclient.Client
	stopper         chan struct{}
	stopped         chan struct{}
	mgr             manager.Manager
	ucloop          *updateConstraintLoop
	reporter        *reporter
	log             logr.Logger
	processExcluder *process.Excluder
	eventRecorder   record.EventRecorder
	gkNamespace     string

	// auditCache lists objects from the audit's cache if auditFromCache is enabled.
	auditCache *CacheLister

	expansionSystem *expansion.System
	pubsubSystem    *pubsub.System
}

// StatusViolation represents each violation under status.
type StatusViolation struct {
	Group             string `json:"group"`
	Version           string `json:"version"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Namespace         string `json:"namespace,omitempty"`
	Message           string `json:"message"`
	EnforcementAction string `json:"enforcementAction"`
}

// ConstraintMsg represents publish message for each constraint.
type PubsubMsg struct {
	ID                    string            `json:"id,omitempty"`
	Details               interface{}       `json:"details,omitempty"`
	EventType             string            `json:"eventType,omitempty"`
	Group                 string            `json:"group,omitempty"`
	Version               string            `json:"version,omitempty"`
	Kind                  string            `json:"kind,omitempty"`
	Name                  string            `json:"name,omitempty"`
	Namespace             string            `json:"namespace,omitempty"`
	Message               string            `json:"message,omitempty"`
	EnforcementAction     string            `json:"enforcementAction,omitempty"`
	ConstraintAnnotations map[string]string `json:"constraintAnnotations,omitempty"`
	ResourceGroup         string            `json:"resourceGroup,omitempty"`
	ResourceAPIVersion    string            `json:"resourceAPIVersion,omitempty"`
	ResourceKind          string            `json:"resourceKind,omitempty"`
	ResourceNamespace     string            `json:"resourceNamespace,omitempty"`
	ResourceName          string            `json:"resourceName,omitempty"`
	ResourceLabels        map[string]string `json:"resourceLabels,omitempty"`
}

// updateListEntry holds the information necessary to update the
// audit results in the `status` field of the constraint template.
// Adding data to this struct has a large impact on memory usage.
type updateListEntry struct {
	group             string
	version           string
	kind              string
	namespace         string
	name              string
	msg               string
	enforcementAction util.EnforcementAction
}

// nsCache is used for caching namespaces and their labels.
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

// New creates a new manager for audit.
func New(mgr manager.Manager, deps *Dependencies) (*Manager, error) {
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
		opa:             deps.Client,
		stopper:         make(chan struct{}),
		stopped:         make(chan struct{}),
		mgr:             mgr,
		reporter:        reporter,
		processExcluder: deps.ProcessExcluder,
		eventRecorder:   recorder,
		gkNamespace:     util.GetNamespace(),
		auditCache:      deps.CacheLister,
		expansionSystem: deps.ExpansionSystem,
		pubsubSystem:    deps.PubSubSystem,
	}
	return am, nil
}

// audit performs an audit then updates the status of all constraint resources with the results.
func (am *Manager) audit(ctx context.Context) error {
	startTime := time.Now()
	timestamp := startTime.UTC().Format(time.RFC3339)
	am.log = log.WithValues(logging.AuditID, timestamp)
	logStart(am.log)
	// record audit latency
	defer func() {
		logFinish(am.log)
		endTime := time.Now()
		latency := endTime.Sub(startTime)
		if err := am.reporter.reportLatency(latency); err != nil {
			am.log.Error(err, "failed to report latency")
		}
		if err := am.reporter.reportRunEnd(endTime); err != nil {
			am.log.Error(err, "failed to report run end time")
		}
	}()

	if err := am.reporter.reportRunStart(startTime); err != nil {
		am.log.Error(err, "failed to report run start time")
	}

	// Create a new client to get an updated RESTMapper.
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
	constraintsGVKs, err := am.getAllConstraintKinds()
	if err != nil {
		// if no constraint is found with the constraint apiversion, then return
		am.log.Info("no constraint is found with apiversion", "constraint apiversion", constraintsGV)
		return nil
	}

	updateLists := make(map[util.KindVersionName][]updateListEntry)
	totalViolationsPerConstraint := make(map[util.KindVersionName]int64)
	totalViolationsPerEnforcementAction := make(map[util.EnforcementAction]int64)
	// resetting total violations per enforcement action
	for _, action := range util.KnownEnforcementActions {
		totalViolationsPerEnforcementAction[action] = 0
	}

	if *auditFromCache {
		var res []Result
		am.log.Info("Auditing from cache")
		res, errs := am.auditFromCache(ctx)
		am.log.Info("Audit from cache results", "violations", len(res))
		for _, err := range errs {
			am.log.Error(err, "Auditing")
		}

		am.addAuditResponsesToUpdateLists(updateLists, res, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, timestamp)
	} else {
		am.log.Info("Auditing via discovery client")
		err := am.auditResources(ctx, constraintsGVKs, updateLists, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, timestamp)
		if err != nil {
			return err
		}
	}

	// log constraints with violations
	for gvknn := range updateLists {
		ar := updateLists[gvknn][0]
		logConstraint(am.log, &gvknn, ar.enforcementAction, totalViolationsPerConstraint[gvknn])
	}

	for k, v := range totalViolationsPerEnforcementAction {
		if err := am.reporter.reportTotalViolations(k, v); err != nil {
			am.log.Error(err, "failed to report total violations")
		}
	}

	// update constraints for each kind
	am.writeAuditResults(ctx, constraintsGVKs, updateLists, timestamp, totalViolationsPerConstraint)

	return nil
}

// Audits server resources via the discovery client.
func (am *Manager) auditResources(
	ctx context.Context,
	constraintsGVK []schema.GroupVersionKind,
	updateLists map[util.KindVersionName][]updateListEntry,
	totalViolationsPerConstraint map[util.KindVersionName]int64,
	totalViolationsPerEnforcementAction map[util.EnforcementAction]int64,
	timestamp string,
) error {
	// delete all from cache dir before starting audit
	err := am.removeAllFromDir(*apiCacheDir, int(*auditChunkSize))
	if err != nil {
		am.log.Error(err, "unable to remove existing content from cache directory in auditResources", "apiCacheDir", *apiCacheDir)
		return err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(am.mgr.GetConfig())
	if err != nil {
		return err
	}

	serverResourceLists, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			am.log.Error(err, "Kubernetes has an orphaned APIService. Delete orphaned APIService using kubectl delete apiservice <name>")
		} else {
			return err
		}
	}

	clusterAPIResources := make(map[metav1.GroupVersion]map[string]bool)
	for _, rl := range serverResourceLists {
		gvParsed, err := schema.ParseGroupVersion(rl.GroupVersion)
		if err != nil {
			am.log.Error(err, "Error parsing GroupVersion", "GroupVersion", rl.GroupVersion)
			continue
		}

		gv := metav1.GroupVersion{
			Group:   gvParsed.Group,
			Version: gvParsed.Version,
		}
		if _, ok := clusterAPIResources[gv]; !ok {
			clusterAPIResources[gv] = make(map[string]bool)
		}
		for i := range rl.APIResources {
			for _, verb := range rl.APIResources[i].Verbs {
				if verb == "list" {
					clusterAPIResources[gv][rl.APIResources[i].Kind] = true
					break
				}
			}
		}
	}

	var errs []error
	namespaceCache := newNSCache()

	matchedKinds := make(map[string]bool)
	if *auditMatchKindOnly {
		constraintList := &unstructured.UnstructuredList{}
	constraintsLoop:
		for _, c := range constraintsGVK {
			constraintList.SetGroupVersionKind(c)
			if err = am.client.List(ctx, constraintList); err != nil {
				am.log.Error(err, "Unable to list objects for gvk", "group", c.Group, "version", c.Version, "kind", c.Kind)
				continue
			}
			for _, constraint := range constraintList.Items {
				kinds, found, err := unstructured.NestedSlice(constraint.Object, "spec", "match", "kinds")
				if err != nil {
					am.log.Error(err, "Unable to return spec.match.kinds field", "group", c.Group, "version", c.Version, "kind", c.Kind)
					// looking at all kinds if there is an error
					matchedKinds["*"] = true
					break constraintsLoop
				}
				if found {
					for _, k := range kinds {
						kind, ok := k.(map[string]interface{})
						if !ok {
							am.log.Error(errors.New("could not cast kind as map[string]"), "kind", k)
							continue
						}
						kindsKind, _, err := unstructured.NestedSlice(kind, "kinds")
						if err != nil {
							am.log.Error(err, "Unable to return kinds.kinds field", "group", c.Group, "version", c.Version, "kind", c.Kind)
							continue
						}
						for _, kk := range kindsKind {
							kks, ok := kk.(string)
							if !ok {
								err := fmt.Errorf("invalid kinds.kinds value type %#v, want string", kk)
								am.log.Error(err, "group", c.Group, "version", c.Version, "kind", c.Kind)
								continue constraintsLoop
							}

							if kks == "" || kks == "*" {
								// no need to continue, all kinds are included
								matchedKinds["*"] = true
								break constraintsLoop
							}
							// adding constraint match kind to matchedKinds list
							matchedKinds[kks] = true
						}
					}
				} else {
					// if constraint doesn't have match kinds defined, we will look at all kinds
					matchedKinds["*"] = true
					break constraintsLoop
				}
			}
		}
	} else {
		matchedKinds["*"] = true
	}

	for gv, gvKinds := range clusterAPIResources {
	kindsLoop:
		for kind := range gvKinds {
			am.log.V(logging.DebugLevel).Info("Listing objects for GVK", "group", gv.Group, "version", gv.Version, "kind", kind)
			// delete all existing folders from cache dir before starting next kind
			err := am.removeAllFromDir(*apiCacheDir, int(*auditChunkSize))
			if err != nil {
				am.log.Error(err, "unable to remove existing content from cache directory in kindsLoop", "apiCacheDir", *apiCacheDir)
				return err
			}
			// tracking number of folders created for this kind
			folderCount := 0
			_, matchAll := matchedKinds["*"]
			if _, found := matchedKinds[kind]; !found && !matchAll {
				continue
			}

			objList := &unstructured.UnstructuredList{}
			opts := &client.ListOptions{
				Limit: int64(*auditChunkSize),
			}
			resourceVersion := ""
			subPath := ""
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
					continue kindsLoop
				}
				// for each batch, create a parent folder
				// prefix kind to avoid delays in removeall
				subPath = fmt.Sprintf("%s_%d", kind, folderCount)
				parentDir := path.Join(*apiCacheDir, subPath)
				if err := os.Mkdir(parentDir, 0o750); err != nil {
					am.log.Error(err, "Unable to create parentDir", "parentDir", parentDir)
					continue kindsLoop
				}
				folderCount++
				for index := range objList.Items {
					isExcludedNamespace, err := am.skipExcludedNamespace(&objList.Items[index])
					if err != nil {
						log.Error(err, "error while excluding namespaces")
					}

					if isExcludedNamespace {
						continue
					}

					fileName := fmt.Sprintf("%d", index)
					destFile := path.Join(*apiCacheDir, subPath, fileName)
					item := objList.Items[index]
					jsonBytes, err := item.MarshalJSON()
					if err != nil {
						log.Error(err, "error while marshaling unstructured object to JSON")
						continue
					}
					if err := os.WriteFile(destFile, jsonBytes, 0o600); err != nil {
						log.Error(err, "error writing data to file")
						continue
					}
				}

				resourceVersion = objList.GetResourceVersion()
				opts.Continue = objList.GetContinue()
				if opts.Continue == "" {
					am.log.V(logging.DebugLevel).Info("Finished listing objects for GVK", "group", gv.Group, "version", gv.Version, "kind", kind)
					break
				}
				am.log.V(logging.DebugLevel).Info("Requesting next chunk of objects for GVK", "group", gv.Group, "version", gv.Version, "kind", kind)
			}
			// Loop through all subDirs to review all files for this kind.
			am.log.V(logging.DebugLevel).Info("Reviewing objects for GVK", gv.Group, "version", gv.Version, "kind", kind)
			err = am.reviewObjects(ctx, kind, folderCount, namespaceCache, updateLists, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, timestamp)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			am.log.V(logging.DebugLevel).Info("Review complete for GVK", gv.Group, "version", gv.Version, "kind", kind)
		}
	}

	if len(errs) > 0 {
		return mergeErrors(errs)
	}
	return nil
}

func (am *Manager) auditFromCache(ctx context.Context) ([]Result, []error) {
	objs, err := am.auditCache.ListObjects(ctx)
	if err != nil {
		return nil, []error{fmt.Errorf("unable to list objects from audit cache: %w", err)}
	}
	nsMap, err := nsMapFromObjs(objs)
	if err != nil {
		return nil, []error{fmt.Errorf("unable to build namespaces from cache: %w", err)}
	}

	var results []Result

	var errs []error
	for i := range objs {
		// Prevent referencing loop variables directly.
		obj := objs[i]
		ns, exists := nsMap[obj.GetNamespace()]
		if !exists {
			ns = nil
		}

		excluded, err := am.skipExcludedNamespace(&obj)
		if err != nil {
			am.log.Error(err, "Unable to exclude object namespace for audit from cache %v %s/%s", obj.GroupVersionKind().String(), obj.GetNamespace(), obj.GetName())
			continue
		}

		if excluded {
			am.log.V(logging.DebugLevel).Info("excluding object from audit from cache %v %s/%s", obj.GroupVersionKind().String(), obj.GetNamespace(), obj.GetName())
			continue
		}

		au := &target.AugmentedUnstructured{
			Object:    obj,
			Namespace: ns,
		}
		resp, err := am.opa.Review(ctx, au, drivers.Stats(*logStatsAudit))
		if err != nil {
			am.log.Error(err, "Unable to review object from audit cache %v %s/%s", obj.GroupVersionKind().String(), obj.GetNamespace(), obj.GetName())
			continue
		}

		if *logStatsAudit {
			logging.LogStatsEntries(
				am.opa,
				am.log.WithValues(logging.EventType, "audit_cache_stats"),
				resp.StatsEntries,
				"audit from cache review request stats",
			)
		}

		for _, r := range resp.Results() {
			results = append(results, Result{
				Result: r,
				obj:    &obj,
			})
		}
	}

	return results, errs
}

// nsMapFromObjs creates a mapping of namespaceName -> corev1.Namespace for
// every Namespace in input `objs`.
func nsMapFromObjs(objs []unstructured.Unstructured) (map[string]*corev1.Namespace, error) {
	nsMap := make(map[string]*corev1.Namespace)
	for _, obj := range objs {
		if obj.GetKind() != "Namespace" {
			continue
		}

		var ns corev1.Namespace
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &ns)
		if err != nil {
			return nil, fmt.Errorf("error converting cached namespace %s from unstructured: %w", obj.GetName(), err)
		}
		nsMap[obj.GetName()] = &ns
	}

	return nsMap, nil
}

func (am *Manager) reviewObjects(ctx context.Context, kind string, folderCount int, nsCache *nsCache,
	updateLists map[util.KindVersionName][]updateListEntry,
	totalViolationsPerConstraint map[util.KindVersionName]int64,
	totalViolationsPerEnforcementAction map[util.EnforcementAction]int64,
	timestamp string,
) error {
	for i := 0; i < folderCount; i++ {
		// cache directory structure:
		// apiCacheDir/kind_folderIndex/fileIndex
		subDir := fmt.Sprintf("%s_%d", kind, i)
		pDir := path.Join(*apiCacheDir, subDir)

		files, err := am.getFilesFromDir(pDir, int(*auditChunkSize))
		if err != nil {
			am.log.Error(err, "Unable to get files from directory")
			continue
		}
		for _, fileName := range files {
			contents, err := os.ReadFile(path.Join(pDir, fileName)) // #nosec G304
			if err != nil {
				am.log.Error(err, "Unable to get content from file", "fileName", fileName)
				continue
			}
			objFile, err := am.readUnstructured(contents)
			if err != nil {
				am.log.Error(err, "Unable to get unstructured data from content in file", "fileName", fileName)
				continue
			}
			objNs := objFile.GetNamespace()
			var ns *corev1.Namespace
			if objNs != "" {
				nsRef, err := nsCache.Get(ctx, am.client, objNs)
				if err != nil {
					am.log.Error(err, "Unable to look up object namespace", "objNs", objNs)
					continue
				}
				ns = &nsRef
			}
			augmentedObj := target.AugmentedUnstructured{
				Object:    *objFile,
				Namespace: ns,
				Source:    mutationtypes.SourceTypeOriginal,
			}

			resp, err := am.opa.Review(ctx, augmentedObj, drivers.Stats(*logStatsAudit))
			if err != nil {
				am.log.Error(err, "Unable to review object from file", "fileName", fileName, "objNs", objNs)
				continue
			}

			// Expand object and review any resultant resources
			base := &mutationtypes.Mutable{
				Object:    objFile,
				Namespace: ns,
				Username:  "",
				Source:    mutationtypes.SourceTypeOriginal,
			}
			resultants, err := am.expansionSystem.Expand(base)
			if err != nil {
				am.log.Error(err, "unable to expand object", "objName", objFile.GetName())
				continue
			}
			for _, resultant := range resultants {
				au := target.AugmentedUnstructured{
					Object:    *resultant.Obj,
					Namespace: ns,
					Source:    mutationtypes.SourceTypeGenerated,
				}
				resultantResp, err := am.opa.Review(ctx, au, drivers.Stats(*logStatsAudit))
				if err != nil {
					am.log.Error(err, "Unable to review expanded object", "objName", (*resultant.Obj).GetName(), "objNs", ns)
					continue
				}
				expansion.OverrideEnforcementAction(resultant.EnforcementAction, resultantResp)
				expansion.AggregateResponses(resultant.TemplateName, resp, resultantResp)
				expansion.AggregateStats(resultant.TemplateName, resp, resultantResp)
			}

			if *logStatsAudit {
				logging.LogStatsEntries(
					am.opa,
					am.log.WithValues(logging.EventType, "audit_stats"),
					resp.StatsEntries,
					"audit review request stats",
				)
			}

			if len(resp.Results()) > 0 {
				results := ToResults(&augmentedObj.Object, resp)
				am.addAuditResponsesToUpdateLists(updateLists, results, totalViolationsPerConstraint, totalViolationsPerEnforcementAction, timestamp)
			}
		}
	}
	return nil
}

func (am *Manager) getFilesFromDir(directory string, batchSize int) (files []string, err error) {
	files = []string{}
	dir, err := os.Open(directory)
	if err != nil {
		return files, err
	}
	defer dir.Close()
	for {
		names, err := dir.Readdirnames(batchSize)
		if errors.Is(err, io.EOF) || len(names) == 0 {
			break
		}
		if err != nil {
			return files, err
		}
		files = append(files, names...)
	}
	return files, nil
}

func (am *Manager) removeAllFromDir(directory string, batchSize int) error {
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		names, err := dir.Readdirnames(batchSize)
		if errors.Is(err, io.EOF) || len(names) == 0 {
			break
		}
		for _, n := range names {
			err = os.RemoveAll(path.Join(directory, n))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (am *Manager) readUnstructured(jsonBytes []byte) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	err := json.Unmarshal(jsonBytes, u)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (am *Manager) auditManagerLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(*auditInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("Audit Manager close")
			close(am.stopper)
			return
		case <-ticker.C:
			if err := am.audit(ctx); err != nil {
				log.Error(err, "audit manager audit() failed")
			}
		}
	}
}

// Start implements controller.Controller.
func (am *Manager) Start(ctx context.Context) error {
	log.Info("Starting Audit Manager")
	go am.auditManagerLoop(ctx)
	<-ctx.Done()
	log.Info("Stopping audit manager workers")
	return nil
}

func (am *Manager) ensureCRDExists(ctx context.Context) error {
	crd := &apiextensionsv1.CustomResourceDefinition{}
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
	for i := range l.APIResources {
		unique[schema.GroupVersionKind{Group: group, Version: version, Kind: l.APIResources[i].Kind}] = true
	}
	var ret []schema.GroupVersionKind
	for gvk := range unique {
		ret = append(ret, gvk)
	}
	return ret, nil
}

func (am *Manager) addAuditResponsesToUpdateLists(
	updateLists map[util.KindVersionName][]updateListEntry,
	res []Result,
	totalViolationsPerConstraint map[util.KindVersionName]int64,
	totalViolationsPerEnforcementAction map[util.EnforcementAction]int64,
	timestamp string,
) {
	for _, r := range res {
		key := util.GetUniqueKey(*r.Constraint)
		totalViolationsPerConstraint[key]++
		details := r.Metadata["details"]

		gvk := r.obj.GroupVersionKind()
		namespace := r.obj.GetNamespace()
		name := r.obj.GetName()
		uid := r.obj.GetUID()
		rv := r.obj.GetResourceVersion()
		ea := util.EnforcementAction(r.EnforcementAction)

		// append audit results only if it is below violations limit
		if uint(len(updateLists[key])) < *constraintViolationsLimit {
			msg := r.Msg
			if len(msg) > msgSize {
				msg = truncateString(msg, msgSize)
			}
			entry := updateListEntry{
				group:             gvk.Group,
				version:           gvk.Version,
				kind:              gvk.Kind,
				namespace:         namespace,
				name:              name,
				msg:               msg,
				enforcementAction: ea,
			}
			updateLists[key] = append(updateLists[key], entry)
		}

		totalViolationsPerEnforcementAction[ea]++
		logViolation(am.log, r.Constraint, ea, gvk, namespace, name, r.Msg, details, r.obj.GetLabels())
		if *pubsubController.PubsubEnabled {
			err := am.pubsubSystem.Publish(context.Background(), *auditConnection, *auditChannel, violationMsg(r.Constraint, ea, gvk, namespace, name, r.Msg, details, r.obj.GetLabels(), timestamp))
			if err != nil {
				am.log.Error(err, "pubsub audit Publishing")
			}
		}
		if *emitAuditEvents {
			emitEvent(r.Constraint, timestamp, ea, gvk, namespace, name, rv, r.Msg, am.gkNamespace, uid, am.eventRecorder)
		}
	}
}

func (am *Manager) writeAuditResults(ctx context.Context, constraintsGVKs []schema.GroupVersionKind, updateLists map[util.KindVersionName][]updateListEntry, timestamp string, totalViolations map[util.KindVersionName]int64) {
	// if there is a previous reporting thread, close it before starting a new one
	if am.ucloop != nil {
		// this is closing the previous audit reporting thread
		am.log.Info("closing the previous audit reporting thread")
		close(am.ucloop.stop)
		select {
		case <-am.ucloop.stopped:
		case <-time.After(time.Duration(*auditInterval) * time.Second):
			// avoid deadlocking in cases where ucloop never stops
			// this creates potential leak of threads but avoids potential of deadlocking
			am.log.Info("timeout waiting for previous audit reporting thread to finish")
		}
	}

	am.ucloop = &updateConstraintLoop{
		client:  am.client,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
		ul:      updateLists,
		ts:      timestamp,
		tv:      totalViolations,
		log:     am.log,
	}

	go am.ucloop.update(ctx, constraintsGVKs)
}

func (am *Manager) skipExcludedNamespace(obj *unstructured.Unstructured) (bool, error) {
	isNamespaceExcluded, err := am.processExcluder.IsNamespaceExcluded(process.Audit, obj)
	if err != nil {
		return false, err
	}

	return isNamespaceExcluded, err
}

func (ucloop *updateConstraintLoop) updateConstraintStatus(ctx context.Context, instance *unstructured.Unstructured, auditResults []updateListEntry, timestamp string, totalViolations int64) error {
	constraintName := instance.GetName()
	ucloop.log.Info("updating constraint status", "constraintName", constraintName)
	// create constraint status violations
	var statusViolations []interface{}
	for i := range auditResults {
		ar := auditResults[i] // avoid large shallow copy in range loop
		// append statusViolations for this constraint until constraintViolationsLimit has reached
		if uint(len(statusViolations)) < *constraintViolationsLimit {
			statusViolations = append(statusViolations, StatusViolation{
				Group:             ar.group,
				Version:           ar.version,
				Kind:              ar.kind,
				Name:              ar.name,
				Namespace:         ar.namespace,
				Message:           ar.msg,
				EnforcementAction: string(ar.enforcementAction),
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
			ucloop.log.Info("removed status violations", "constraintName", constraintName)
		}
		err = ucloop.client.Status().Update(ctx, instance)
		if err != nil {
			return err
		}
	} else {
		if err := unstructured.SetNestedSlice(instance.Object, violations, "status", "violations"); err != nil {
			return err
		}
		ucloop.log.Info("constraint status update", "object", instance)
		err = ucloop.client.Status().Update(ctx, instance)
		if err != nil {
			return err
		}
		ucloop.log.Info("updated constraint status violations", "constraintName", constraintName, "count", len(violations))
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
	uc      map[util.KindVersionName]struct{}
	client  client.Client
	stop    chan struct{}
	stopped chan struct{}
	ul      map[util.KindVersionName][]updateListEntry
	ts      string
	tv      map[util.KindVersionName]int64
	log     logr.Logger
}

func (ucloop *updateConstraintLoop) update(ctx context.Context, constraintsGVKs []schema.GroupVersionKind) {
	defer close(ucloop.stopped)

	ucloop.uc = make(map[util.KindVersionName]struct{})

	// get constraints for each Kind
	for _, constraintGvk := range constraintsGVKs {
		select {
		case <-ucloop.stop:
			return
		default:
		}

		ucloop.log.Info("constraint", "resource kind", constraintGvk.Kind)
		instanceList := &unstructured.UnstructuredList{}
		instanceList.SetGroupVersionKind(constraintGvk)
		err := ucloop.client.List(ctx, instanceList)
		if err != nil {
			ucloop.log.Error(err, "error while listing constraints", "kind", constraintGvk.Kind)
			continue
		}
		ucloop.log.Info("constraint", "count of constraints", len(instanceList.Items))

		// get each constraint
		for _, item := range instanceList.Items {
			key := util.GetUniqueKey(item)
			ucloop.uc[key] = struct{}{}
		}
	}

	if len(ucloop.uc) == 0 {
		return
	}

	ucloop.log.Info("starting update constraints loop", "constraints to update", fmt.Sprintf("%v", ucloop.uc))

	updateLoop := func() (bool, error) {
		for key := range ucloop.uc {
			select {
			case <-ucloop.stop:
				return true, nil
			default:
				constraint := &unstructured.Unstructured{}
				constraint.SetKind(key.Kind)
				constraint.SetGroupVersionKind(schema.GroupVersionKind{Group: key.Group, Version: key.Version, Kind: key.Kind})
				namespacedName := types.NamespacedName{
					Name:      key.Name,
					Namespace: key.Namespace,
				}
				// get the latest constraint
				err := ucloop.client.Get(ctx, namespacedName, constraint)
				if err != nil {
					if apierrors.IsNotFound(err) {
						ucloop.log.Info("could not find constraint", "name", key.Name, "namespace", key.Namespace)
						delete(ucloop.uc, key)
					} else {
						ucloop.log.Error(err, "could not get latest constraint during update", "name", key.Name, "namespace", key.Namespace)
						continue
					}
				}
				totalViolations := ucloop.tv[key]
				if constraintAuditResults, ok := ucloop.ul[key]; !ok {
					err := ucloop.updateConstraintStatus(ctx, constraint, emptyAuditResults, ucloop.ts, totalViolations)
					if err != nil {
						ucloop.log.Error(err, "could not update constraint status", "name", key.Name, "namespace", key.Namespace)
						continue
					}
				} else {
					// update the constraint
					err := ucloop.updateConstraintStatus(ctx, constraint, constraintAuditResults, ucloop.ts, totalViolations)
					if err != nil {
						ucloop.log.Error(err, "could not update constraint status", "name", key.Name, "namespace", key.Namespace)
						continue
					}
				}
				delete(ucloop.uc, key)
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
		ucloop.log.Error(err, "could not update constraint reached max retries", "remaining update constraints", fmt.Sprintf("%v", ucloop.uc))
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

func logConstraint(l logr.Logger, gvknn *util.KindVersionName, enforcementAction util.EnforcementAction, totalViolations int64) {
	l.Info(
		"audit results for constraint",
		logging.EventType, "constraint_audited",
		logging.ConstraintGroup, gvknn.Group,
		logging.ConstraintAPIVersion, gvknn.Version,
		logging.ConstraintKind, gvknn.Kind,
		logging.ConstraintName, gvknn.Name,
		logging.ConstraintNamespace, gvknn.Namespace,
		logging.ConstraintAction, enforcementAction,
		logging.ConstraintStatus, "enforced",
		logging.ConstraintViolations, strconv.FormatInt(totalViolations, 10),
	)
}

func violationMsg(constraint *unstructured.Unstructured, enforcementAction util.EnforcementAction, resourceGroupVersionKind schema.GroupVersionKind, rnamespace, rname, message string, details interface{}, rlabels map[string]string, timestamp string) interface{} {
	userConstraintAnnotations := constraint.GetAnnotations()
	delete(userConstraintAnnotations, "kubectl.kubernetes.io/last-applied-configuration")

	return PubsubMsg{
		Message:               message,
		Details:               details,
		ID:                    timestamp,
		EventType:             "violation_audited",
		Group:                 constraint.GroupVersionKind().Group,
		Version:               constraint.GroupVersionKind().Version,
		Kind:                  constraint.GetKind(),
		Name:                  constraint.GetName(),
		Namespace:             constraint.GetNamespace(),
		EnforcementAction:     string(enforcementAction),
		ConstraintAnnotations: userConstraintAnnotations,
		ResourceGroup:         resourceGroupVersionKind.Group,
		ResourceAPIVersion:    resourceGroupVersionKind.Version,
		ResourceKind:          resourceGroupVersionKind.Kind,
		ResourceNamespace:     rnamespace,
		ResourceName:          rname,
		ResourceLabels:        rlabels,
	}
}

func logViolation(l logr.Logger,
	constraint *unstructured.Unstructured,
	enforcementAction util.EnforcementAction, resourceGroupVersionKind schema.GroupVersionKind, rnamespace, rname, message string, details interface{}, rlabels map[string]string,
) {
	userConstraintAnnotations := constraint.GetAnnotations()
	delete(userConstraintAnnotations, "kubectl.kubernetes.io/last-applied-configuration")

	l.Info(
		message,
		logging.Details, details,
		logging.EventType, "violation_audited",
		logging.ConstraintGroup, constraint.GroupVersionKind().Group,
		logging.ConstraintAPIVersion, constraint.GroupVersionKind().Version,
		logging.ConstraintKind, constraint.GetKind(),
		logging.ConstraintName, constraint.GetName(),
		logging.ConstraintNamespace, constraint.GetNamespace(),
		logging.ConstraintAction, enforcementAction,
		logging.ConstraintAnnotations, userConstraintAnnotations,
		logging.ResourceGroup, resourceGroupVersionKind.Group,
		logging.ResourceAPIVersion, resourceGroupVersionKind.Version,
		logging.ResourceKind, resourceGroupVersionKind.Kind,
		logging.ResourceNamespace, rnamespace,
		logging.ResourceName, rname,
		logging.ResourceLabels, rlabels,
	)
}

func emitEvent(constraint *unstructured.Unstructured,
	timestamp string, enforcementAction util.EnforcementAction, resourceGroupVersionKind schema.GroupVersionKind, rnamespace, rname, rrv, message, gkNamespace string, ruid types.UID,
	eventRecorder record.EventRecorder,
) {
	annotations := map[string]string{
		"process":                    "audit",
		"auditTimestamp":             timestamp,
		logging.EventType:            "violation_audited",
		logging.ConstraintGroup:      constraint.GroupVersionKind().Group,
		logging.ConstraintAPIVersion: constraint.GroupVersionKind().Version,
		logging.ConstraintKind:       constraint.GetKind(),
		logging.ConstraintName:       constraint.GetName(),
		logging.ConstraintNamespace:  constraint.GetNamespace(),
		logging.ConstraintAction:     string(enforcementAction),
		logging.ResourceGroup:        resourceGroupVersionKind.Group,
		logging.ResourceAPIVersion:   resourceGroupVersionKind.Version,
		logging.ResourceKind:         resourceGroupVersionKind.Kind,
		logging.ResourceNamespace:    rnamespace,
		logging.ResourceName:         rname,
	}

	reason := "AuditViolation"
	ref := getViolationRef(gkNamespace, resourceGroupVersionKind.Kind, rname, rnamespace, rrv, ruid, constraint.GetKind(), constraint.GetName(), constraint.GetNamespace(), *auditEventsInvolvedNamespace)

	if *auditEventsInvolvedNamespace {
		eventRecorder.AnnotatedEventf(ref, annotations, corev1.EventTypeWarning, reason, "Constraint: %s, Message: %s", constraint.GetName(), message)
	} else {
		eventRecorder.AnnotatedEventf(ref, annotations, corev1.EventTypeWarning, reason, "Resource Namespace: %s, Constraint: %s, Message: %s", rnamespace, constraint.GetName(), message)
	}
}

func getViolationRef(gkNamespace, rkind, rname, rnamespace, rrv string, ruid types.UID, ckind, cname, cnamespace string, emitInvolvedNamespace bool) *corev1.ObjectReference {
	enamespace := gkNamespace
	if emitInvolvedNamespace && len(rnamespace) > 0 {
		enamespace = rnamespace
	}
	ref := &corev1.ObjectReference{
		Kind:      rkind,
		Name:      rname,
		Namespace: enamespace,
	}
	if emitInvolvedNamespace && len(ruid) > 0 && len(rrv) > 0 {
		ref.UID = ruid
		ref.ResourceVersion = rrv
	} else if !emitInvolvedNamespace {
		ref.UID = types.UID(rkind + "/" + rnamespace + "/" + rname + "/" + ckind + "/" + cnamespace + "/" + cname)
	}
	return ref
}

// mergeErrors concatenates errs into a single error. None of the original errors
// may be extracted from the result.
func mergeErrors(errs []error) error {
	sb := strings.Builder{}
	for i, err := range errs {
		if i != 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(err.Error())
	}
	return errors.New(sb.String())
}
