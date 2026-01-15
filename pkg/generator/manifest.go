package generator

import (
	"encoding/json"
	"fmt"

	"github.com/gshaibi/kubectl-create-resource/pkg/prompt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// GenerateManifest creates an unstructured Kubernetes manifest from collected values
func GenerateManifest(gvr schema.GroupVersionResource, namespace string, values *prompt.CollectedValues) (*unstructured.Unstructured, error) {
	// Build the nested structure from flat values
	nested := prompt.BuildNestedMap(values.Values)
	
	// Ensure metadata exists
	metadata, ok := nested["metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})
		nested["metadata"] = metadata
	}
	
	// Set the name
	metadata["name"] = values.Name
	
	// Set namespace if provided and resource is namespaced
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	
	// Build the object
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": gvrToAPIVersion(gvr),
			"kind":       gvrToKind(gvr),
		},
	}
	
	// Merge in the nested values
	for k, v := range nested {
		obj.Object[k] = v
	}
	
	return obj, nil
}

// PrintManifest prints a manifest in the specified format
func PrintManifest(obj *unstructured.Unstructured, format string) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(obj.Object, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal to JSON: %w", err)
		}
		fmt.Println(string(data))
	case "yaml", "":
		data, err := yaml.Marshal(obj.Object)
		if err != nil {
			return fmt.Errorf("failed to marshal to YAML: %w", err)
		}
		fmt.Print(string(data))
	default:
		return fmt.Errorf("unsupported output format: %s (use yaml or json)", format)
	}
	return nil
}

// gvrToAPIVersion converts a GVR to an API version string
func gvrToAPIVersion(gvr schema.GroupVersionResource) string {
	if gvr.Group == "" {
		return gvr.Version
	}
	return gvr.Group + "/" + gvr.Version
}

// gvrToKind converts a GVR to a Kind string
// This is a simple heuristic - the actual kind should be obtained from discovery
func gvrToKind(gvr schema.GroupVersionResource) string {
	resource := gvr.Resource
	
	// Handle common irregular plurals and compound words
	irregulars := map[string]string{
		// Core resources
		"configmaps":                      "ConfigMap",
		"endpoints":                       "Endpoints",
		"limitranges":                     "LimitRange",
		"namespaces":                      "Namespace",
		"persistentvolumeclaims":          "PersistentVolumeClaim",
		"persistentvolumes":               "PersistentVolume",
		"podtemplates":                    "PodTemplate",
		"replicationcontrollers":          "ReplicationController",
		"resourcequotas":                  "ResourceQuota",
		"serviceaccounts":                 "ServiceAccount",
		// Apps
		"controllerrevisions":             "ControllerRevision",
		"daemonsets":                      "DaemonSet",
		"deployments":                     "Deployment",
		"replicasets":                     "ReplicaSet",
		"statefulsets":                    "StatefulSet",
		// Batch
		"cronjobs":                        "CronJob",
		// Networking
		"ingresses":                       "Ingress",
		"ingressclasses":                  "IngressClass",
		"networkpolicies":                 "NetworkPolicy",
		// Policy
		"poddisruptionbudgets":            "PodDisruptionBudget",
		"podsecuritypolicies":             "PodSecurityPolicy",
		// RBAC
		"clusterrolebindings":             "ClusterRoleBinding",
		"clusterroles":                    "ClusterRole",
		"rolebindings":                    "RoleBinding",
		// Storage
		"csidrivers":                      "CSIDriver",
		"csinodes":                        "CSINode",
		"csistoragecapacities":            "CSIStorageCapacity",
		"storageclasses":                  "StorageClass",
		"volumeattachments":               "VolumeAttachment",
		// Scheduling
		"priorityclasses":                 "PriorityClass",
		// Node
		"runtimeclasses":                  "RuntimeClass",
		// Autoscaling
		"horizontalpodautoscalers":        "HorizontalPodAutoscaler",
		// API extensions
		"customresourcedefinitions":       "CustomResourceDefinition",
		// Admission
		"mutatingwebhookconfigurations":   "MutatingWebhookConfiguration",
		"validatingwebhookconfigurations": "ValidatingWebhookConfiguration",
	}
	
	if kind, ok := irregulars[resource]; ok {
		return kind
	}
	
	// Standard plural to singular conversion
	if len(resource) > 3 && resource[len(resource)-3:] == "ies" {
		resource = resource[:len(resource)-3] + "y"
	} else if len(resource) > 2 && resource[len(resource)-2:] == "es" {
		// Check for words ending in 's', 'x', 'z', 'ch', 'sh'
		base := resource[:len(resource)-2]
		if len(base) > 0 {
			lastChar := base[len(base)-1]
			if lastChar == 's' || lastChar == 'x' || lastChar == 'z' {
				resource = base
			} else if len(base) > 1 {
				last2 := base[len(base)-2:]
				if last2 == "ch" || last2 == "sh" {
					resource = base
				} else {
					resource = resource[:len(resource)-1]
				}
			} else {
				resource = resource[:len(resource)-1]
			}
		}
	} else if len(resource) > 1 && resource[len(resource)-1] == 's' {
		resource = resource[:len(resource)-1]
	}
	
	// Capitalize first letter of each word (for compound names)
	result := make([]byte, 0, len(resource))
	capitalizeNext := true
	for i := 0; i < len(resource); i++ {
		c := resource[i]
		if capitalizeNext && c >= 'a' && c <= 'z' {
			result = append(result, c-32)
			capitalizeNext = false
		} else {
			result = append(result, c)
		}
	}
	
	return string(result)
}
