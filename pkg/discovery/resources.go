package discovery

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gshaibi/kubectl-create-resource/pkg/client"
)

// ResourceType represents a discoverable Kubernetes resource type
type ResourceType struct {
	Name       string
	Group      string
	Version    string
	Kind       string
	Namespaced bool
}

// DiscoverResourceTypes discovers all available resource types from the cluster
func DiscoverResourceTypes(kubeconfigPath string) ([]ResourceType, error) {
	k8sClient, err := client.NewK8sClient(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	resources, err := k8sClient.DiscoverResources()
	if err != nil {
		return nil, fmt.Errorf("failed to discover resources: %w", err)
	}

	var types []ResourceType
	for _, r := range resources {
		types = append(types, ResourceType{
			Name:       r.Name,
			Group:      r.Group,
			Version:    r.Version,
			Kind:       r.Kind,
			Namespaced: r.Namespaced,
		})
	}

	// Sort by group then name
	sort.Slice(types, func(i, j int) bool {
		if types[i].Group != types[j].Group {
			// Core API group (empty string) comes first
			if types[i].Group == "" {
				return true
			}
			if types[j].Group == "" {
				return false
			}
			return types[i].Group < types[j].Group
		}
		return types[i].Name < types[j].Name
	})

	return types, nil
}

// FormatResourceType formats a resource type for display
func FormatResourceType(rt ResourceType) string {
	if rt.Group == "" {
		return rt.Name
	}
	return fmt.Sprintf("%s.%s", rt.Name, rt.Group)
}

// FindResourceType finds a resource type by name, supporting various formats
func FindResourceType(types []ResourceType, name string) (*ResourceType, error) {
	name = strings.ToLower(name)
	parts := strings.SplitN(name, ".", 2)
	resourceName := parts[0]
	var groupFilter string
	if len(parts) > 1 {
		groupFilter = parts[1]
	}

	var matches []ResourceType
	for _, rt := range types {
		rtName := strings.ToLower(rt.Name)
		rtKind := strings.ToLower(rt.Kind)
		
		// Match by plural name or singular kind
		if rtName == resourceName || rtKind == resourceName ||
		   rtName == resourceName+"s" || rtKind+"s" == resourceName {
			if groupFilter == "" || strings.EqualFold(rt.Group, groupFilter) {
				matches = append(matches, rt)
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("resource type %q not found", name)
	}

	if len(matches) > 1 && groupFilter == "" {
		// Prefer core API group
		for _, m := range matches {
			if m.Group == "" {
				return &m, nil
			}
		}
		// Otherwise return ambiguous error
		var options []string
		for _, m := range matches {
			options = append(options, FormatResourceType(m))
		}
		return nil, fmt.Errorf("ambiguous resource type %q, specify one of: %s", name, strings.Join(options, ", "))
	}

	return &matches[0], nil
}
