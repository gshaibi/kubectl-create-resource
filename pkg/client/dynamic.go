package client

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// K8sClient wraps the Kubernetes dynamic client and discovery client
type K8sClient struct {
	dynamicClient   dynamic.Interface
	discoveryClient discovery.DiscoveryInterface
	restConfig      *rest.Config
}

// ResourceInfo contains information about an API resource
type ResourceInfo struct {
	Name       string
	Group      string
	Version    string
	Kind       string
	Namespaced bool
	Verbs      []string
}

// NewK8sClient creates a new Kubernetes client
func NewK8sClient(kubeconfigPath string) (*K8sClient, error) {
	config, err := buildConfig(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}

	return &K8sClient{
		dynamicClient:   dynamicClient,
		discoveryClient: discoveryClient,
		restConfig:      config,
	}, nil
}

// buildConfig creates a Kubernetes rest.Config from kubeconfig
func buildConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		// Try in-cluster config first
		config, err := rest.InClusterConfig()
		if err == nil {
			return config, nil
		}

		// Fall back to default kubeconfig location
		if home := homedir.HomeDir(); home != "" {
			kubeconfigPath = filepath.Join(home, ".kube", "config")
		}
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}

// DiscoverResources returns all available API resources in the cluster
func (c *K8sClient) DiscoverResources() ([]ResourceInfo, error) {
	_, resourceLists, err := c.discoveryClient.ServerGroupsAndResources()
	if err != nil {
		// Some resources might fail discovery but we can still proceed
		if !discovery.IsGroupDiscoveryFailedError(err) {
			return nil, fmt.Errorf("failed to discover resources: %w", err)
		}
	}

	var resources []ResourceInfo
	seen := make(map[string]bool)

	for _, resourceList := range resourceLists {
		gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, r := range resourceList.APIResources {
			// Skip subresources (e.g., pods/status)
			if strings.Contains(r.Name, "/") {
				continue
			}

			// Skip resources that don't support create
			if !containsVerb(r.Verbs, "create") {
				continue
			}

			// Create a unique key to avoid duplicates
			key := fmt.Sprintf("%s.%s", r.Name, gv.Group)
			if seen[key] {
				continue
			}
			seen[key] = true

			resources = append(resources, ResourceInfo{
				Name:       r.Name,
				Group:      gv.Group,
				Version:    gv.Version,
				Kind:       r.Kind,
				Namespaced: r.Namespaced,
				Verbs:      r.Verbs,
			})
		}
	}

	return resources, nil
}

// ResolveResourceType resolves a resource type string to a GroupVersionResource
func (c *K8sClient) ResolveResourceType(resourceType string) (schema.GroupVersionResource, error) {
	resources, err := c.DiscoverResources()
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	// Parse the resource type (could be "deployment" or "deployment.apps" or "deployments.apps")
	parts := strings.SplitN(resourceType, ".", 2)
	name := strings.ToLower(parts[0])
	var group string
	if len(parts) > 1 {
		group = parts[1]
	}

	// Find matching resource
	var matches []ResourceInfo
	for _, r := range resources {
		resourceName := strings.ToLower(r.Name)
		resourceKind := strings.ToLower(r.Kind)

		// Match by name (plural) or kind (singular)
		if resourceName == name || resourceKind == name ||
			resourceName == name+"s" || resourceKind+"s" == name {
			if group == "" || strings.EqualFold(r.Group, group) {
				matches = append(matches, r)
			}
		}
	}

	if len(matches) == 0 {
		return schema.GroupVersionResource{}, fmt.Errorf("resource type %q not found", resourceType)
	}

	if len(matches) > 1 && group == "" {
		// Multiple matches - prefer core API group
		for _, m := range matches {
			if m.Group == "" {
				return schema.GroupVersionResource{
					Group:    m.Group,
					Version:  m.Version,
					Resource: m.Name,
				}, nil
			}
		}
		// Otherwise, list the options
		var options []string
		for _, m := range matches {
			if m.Group == "" {
				options = append(options, m.Name)
			} else {
				options = append(options, fmt.Sprintf("%s.%s", m.Name, m.Group))
			}
		}
		return schema.GroupVersionResource{}, fmt.Errorf("ambiguous resource type %q, specify group: %s",
			resourceType, strings.Join(options, ", "))
	}

	match := matches[0]
	return schema.GroupVersionResource{
		Group:    match.Group,
		Version:  match.Version,
		Resource: match.Name,
	}, nil
}

// GetResourceSchema returns the OpenAPI schema for a resource
func (c *K8sClient) GetResourceSchema(gvr schema.GroupVersionResource) (*ResourceSchema, error) {
	return GetSchema(c.discoveryClient, gvr)
}

// CreateResource creates a resource in the cluster
func (c *K8sClient) CreateResource(gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	ctx := context.Background()

	// Determine if resource is namespaced
	namespaced := c.isNamespaced(gvr)

	var resourceInterface dynamic.ResourceInterface
	if namespaced {
		resourceInterface = c.dynamicClient.Resource(gvr).Namespace(namespace)
	} else {
		resourceInterface = c.dynamicClient.Resource(gvr)
	}

	return resourceInterface.Create(ctx, obj, metav1.CreateOptions{})
}

// GetResource fetches an existing resource and returns it as unstructured
func (c *K8sClient) GetResource(gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	ctx := context.Background()

	// Determine if resource is namespaced
	namespaced := c.isNamespaced(gvr)

	var resourceInterface dynamic.ResourceInterface
	if namespaced {
		resourceInterface = c.dynamicClient.Resource(gvr).Namespace(namespace)
	} else {
		resourceInterface = c.dynamicClient.Resource(gvr)
	}

	// Get the resource
	return resourceInterface.Get(ctx, name, metav1.GetOptions{})
}

// GetResourceSpec fetches an existing resource and returns its spec as a flat map
func (c *K8sClient) GetResourceSpec(gvr schema.GroupVersionResource, namespace, name string) (map[string]interface{}, error) {
	obj, err := c.GetResource(gvr, namespace, name)
	if err != nil {
		return nil, err
	}

	// Extract and flatten the spec
	result := make(map[string]interface{})

	// Flatten spec fields
	if spec, ok := obj.Object["spec"].(map[string]interface{}); ok {
		flattenMap(spec, "spec", result)
	}

	return result, nil
}

// isNamespaced checks if a resource type is namespaced
func (c *K8sClient) isNamespaced(gvr schema.GroupVersionResource) bool {
	resources, err := c.DiscoverResources()
	if err != nil {
		return true // Default to namespaced
	}

	for _, r := range resources {
		if r.Name == gvr.Resource && r.Group == gvr.Group {
			return r.Namespaced
		}
	}
	return true
}

// flattenMap flattens a nested map into dot-notation paths
func flattenMap(m map[string]interface{}, prefix string, result map[string]interface{}) {
	for k, v := range m {
		path := prefix + "." + k
		switch val := v.(type) {
		case map[string]interface{}:
			// Recurse into nested maps
			flattenMap(val, path, result)
		case []interface{}:
			// Handle arrays - store the whole array and also individual items
			result[path] = val
			for i, item := range val {
				itemPath := fmt.Sprintf("%s[%d]", path, i)
				if itemMap, ok := item.(map[string]interface{}); ok {
					flattenMap(itemMap, itemPath, result)
				} else {
					result[itemPath] = item
				}
			}
		default:
			result[path] = val
		}
	}
}

// containsVerb checks if a verb is in the list
func containsVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}
