package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/labels"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ConstraintTemplateGVR is the GroupVersionResource for ConstraintTemplates.
var ConstraintTemplateGVR = schema.GroupVersionResource{
	Group:    "templates.gatekeeper.sh",
	Version:  "v1",
	Resource: "constrainttemplates",
}

// InstalledPolicy represents an installed policy in the cluster.
type InstalledPolicy struct {
	Name        string
	Version     string
	Bundle      string
	InstalledAt string
	ManagedBy   string
}

// Client provides operations for managing policies in a Kubernetes cluster.
type Client interface {
	// GatekeeperInstalled checks if Gatekeeper CRDs are installed.
	GatekeeperInstalled(ctx context.Context) (bool, error)

	// ListManagedTemplates lists all ConstraintTemplates managed by gator.
	ListManagedTemplates(ctx context.Context) ([]InstalledPolicy, error)

	// GetTemplate gets a ConstraintTemplate by name.
	GetTemplate(ctx context.Context, name string) (*unstructured.Unstructured, error)

	// InstallTemplate installs or updates a ConstraintTemplate.
	InstallTemplate(ctx context.Context, template *unstructured.Unstructured) error

	// InstallConstraint installs or updates a Constraint.
	InstallConstraint(ctx context.Context, constraint *unstructured.Unstructured) error

	// GetConstraint gets a Constraint by GVR and name.
	GetConstraint(ctx context.Context, gvr schema.GroupVersionResource, name string) (*unstructured.Unstructured, error)

	// DeleteTemplate deletes a ConstraintTemplate.
	DeleteTemplate(ctx context.Context, name string) error

	// DeleteConstraint deletes a Constraint.
	DeleteConstraint(ctx context.Context, gvr schema.GroupVersionResource, name string) error

	// WaitForTemplateReady waits for a ConstraintTemplate to have status.created = true.
	WaitForTemplateReady(ctx context.Context, templateName string, timeout time.Duration) error

	// WaitForConstraintCRD waits for the Constraint CRD to be available.
	WaitForConstraintCRD(ctx context.Context, kind string, timeout time.Duration) error
}

// K8sClient implements Client using the Kubernetes API.
type K8sClient struct {
	dynamicClient dynamic.Interface
}

// NewK8sClient creates a new K8sClient using the default kubeconfig.
func NewK8sClient() (*K8sClient, error) {
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("getting kubeconfig: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	return &K8sClient{dynamicClient: dynamicClient}, nil
}

// NewK8sClientWithConfig creates a new K8sClient with the given config.
func NewK8sClientWithConfig(config *rest.Config) (*K8sClient, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	return &K8sClient{dynamicClient: dynamicClient}, nil
}

func getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	return kubeConfig.ClientConfig()
}

// GatekeeperInstalled checks if Gatekeeper CRDs are installed.
func (c *K8sClient) GatekeeperInstalled(ctx context.Context) (bool, error) {
	_, err := c.dynamicClient.Resource(ConstraintTemplateGVR).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		if isCRDNotRegisteredError(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking Gatekeeper installation: %w", err)
	}
	return true, nil
}

// ListManagedTemplates lists all ConstraintTemplates managed by gator.
func (c *K8sClient) ListManagedTemplates(ctx context.Context) ([]InstalledPolicy, error) {
	labelSelector := fmt.Sprintf("%s=%s", labels.LabelManagedBy, labels.ManagedByValue)
	list, err := c.dynamicClient.Resource(ConstraintTemplateGVR).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("listing managed templates: %w", err)
	}

	policies := make([]InstalledPolicy, 0, len(list.Items))
	for _, item := range list.Items {
		policies = append(policies, InstalledPolicy{
			Name:        item.GetName(),
			Version:     labels.GetPolicyVersion(&item),
			Bundle:      labels.GetBundle(&item),
			InstalledAt: labels.GetInstalledAt(&item),
			ManagedBy:   labels.ManagedByValue,
		})
	}

	return policies, nil
}

// GetTemplate gets a ConstraintTemplate by name.
func (c *K8sClient) GetTemplate(ctx context.Context, name string) (*unstructured.Unstructured, error) {
	return c.dynamicClient.Resource(ConstraintTemplateGVR).Get(ctx, name, metav1.GetOptions{})
}

// InstallTemplate installs or updates a ConstraintTemplate.
func (c *K8sClient) InstallTemplate(ctx context.Context, template *unstructured.Unstructured) error {
	existing, err := c.GetTemplate(ctx, template.GetName())
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new
			_, err = c.dynamicClient.Resource(ConstraintTemplateGVR).Create(ctx, template, metav1.CreateOptions{})
			return err
		}
		return err
	}

	// Update existing
	template.SetResourceVersion(existing.GetResourceVersion())
	_, err = c.dynamicClient.Resource(ConstraintTemplateGVR).Update(ctx, template, metav1.UpdateOptions{})
	return err
}

// InstallConstraint installs or updates a Constraint.
func (c *K8sClient) InstallConstraint(ctx context.Context, constraint *unstructured.Unstructured) error {
	gvr := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: getConstraintResource(constraint.GetKind()),
	}

	existing, err := c.dynamicClient.Resource(gvr).Get(ctx, constraint.GetName(), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new
			_, err = c.dynamicClient.Resource(gvr).Create(ctx, constraint, metav1.CreateOptions{})
			return err
		}
		return err
	}

	// Update existing
	constraint.SetResourceVersion(existing.GetResourceVersion())
	_, err = c.dynamicClient.Resource(gvr).Update(ctx, constraint, metav1.UpdateOptions{})
	return err
}

// GetConstraint gets a Constraint by GVR and name.
func (c *K8sClient) GetConstraint(ctx context.Context, gvr schema.GroupVersionResource, name string) (*unstructured.Unstructured, error) {
	return c.dynamicClient.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
}

// DeleteTemplate deletes a ConstraintTemplate.
func (c *K8sClient) DeleteTemplate(ctx context.Context, name string) error {
	err := c.dynamicClient.Resource(ConstraintTemplateGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// DeleteConstraint deletes a Constraint.
func (c *K8sClient) DeleteConstraint(ctx context.Context, gvr schema.GroupVersionResource, name string) error {
	err := c.dynamicClient.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// WaitForTemplateReady waits for a ConstraintTemplate to have status.created = true.
func (c *K8sClient) WaitForTemplateReady(ctx context.Context, templateName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		template, err := c.dynamicClient.Resource(ConstraintTemplateGVR).Get(ctx, templateName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(500 * time.Millisecond):
					continue
				}
			}
			return fmt.Errorf("getting template status: %w", err)
		}

		// Check if status.created is true
		created, found, err := unstructured.NestedBool(template.Object, "status", "created")
		if err == nil && found && created {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Continue polling
		}
	}
	return fmt.Errorf("timeout waiting for template %s to be ready", templateName)
}

// WaitForConstraintCRD waits for the Constraint CRD to be available.
func (c *K8sClient) WaitForConstraintCRD(ctx context.Context, kind string, timeout time.Duration) error {
	gvr := schema.GroupVersionResource{
		Group:    "constraints.gatekeeper.sh",
		Version:  "v1beta1",
		Resource: getConstraintResource(kind),
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := c.dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1})
		if err == nil {
			return nil
		}
		if !isCRDNotRegisteredError(err) {
			return fmt.Errorf("checking constraint CRD availability: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Continue polling
		}
	}
	return fmt.Errorf("timeout waiting for constraint CRD %s to be available", kind)
}

func getConstraintResource(kind string) string {
	// Gatekeeper constraint CRDs use lowercase kind as the resource name
	// e.g., Kind=K8sPSPAppArmor -> resource=k8spspapparmor
	return strings.ToLower(kind)
}

// isCRDNotRegisteredError checks if the error indicates the CRD/resource type is not registered.
// This is different from IsNotFound which indicates the resource instance doesn't exist.
func isCRDNotRegisteredError(err error) bool {
	if err == nil {
		return false
	}
	// Check for "no matches" error string which occurs when CRD is not yet registered
	errStr := err.Error()
	return strings.Contains(errStr, "no matches for kind") ||
		strings.Contains(errStr, "the server could not find the requested resource")
}
