package client

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// ResourceSchema represents the schema for a Kubernetes resource
type ResourceSchema struct {
	GVK         schema.GroupVersionKind
	Description string
	Fields      []FieldSchema
}

// FieldSchema represents a field in a resource schema
type FieldSchema struct {
	Path        string        // JSON path (e.g., "spec.replicas")
	Name        string        // Field name (e.g., "replicas")
	Type        string        // Field type (string, integer, boolean, array, object)
	Description string        // Field description
	Required    bool          // Whether the field is required
	Default     interface{}   // Default value if any
	Items       *FieldSchema  // For arrays, the schema of items
	Properties  []FieldSchema // For objects, nested properties
}

// GetSchema retrieves the OpenAPI schema for a resource
func GetSchema(discoveryClient discovery.DiscoveryInterface, gvr schema.GroupVersionResource) (*ResourceSchema, error) {
	// Get the OpenAPI v3 client
	openAPIClient := discoveryClient.OpenAPIV3()
	if openAPIClient == nil {
		fmt.Fprintf(os.Stderr, "Note: OpenAPI v3 not available, using basic schema\n")
		return createBasicSchema(gvrToGVK(gvr)), nil
	}

	// Get the paths
	paths, err := openAPIClient.Paths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Note: Failed to get OpenAPI paths: %v\n", err)
		return createBasicSchema(gvrToGVK(gvr)), nil
	}

	gvk := gvrToGVK(gvr)

	// Build target path patterns to search for
	targetPatterns := buildTargetPaths(gvr)

	// Try to find the schema in the OpenAPI spec
	var matchedPath string
	for pathKey, pathValue := range paths {
		// Check if this path matches any of our target patterns
		matched := false
		for _, pattern := range targetPatterns {
			if strings.Contains(pathKey, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		matchedPath = pathKey

		schemaBytes, err := pathValue.Schema("application/json")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Note: Failed to get schema from %s: %v\n", pathKey, err)
			continue
		}

		// Parse the schema
		resourceSchema, err := parseOpenAPISchema(schemaBytes, gvk, gvr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Note: Failed to parse schema from %s: %v\n", pathKey, err)
			continue
		}

		fmt.Fprintf(os.Stderr, "Found schema with %d fields from %s\n", len(resourceSchema.Fields), pathKey)
		return resourceSchema, nil
	}

	// If we couldn't find the schema, list available paths for debugging
	if matchedPath == "" {
		fmt.Fprintf(os.Stderr, "Note: No matching API path found for %s.%s/%s\n", gvr.Resource, gvr.Group, gvr.Version)
		fmt.Fprintf(os.Stderr, "Looking for patterns: %v\n", targetPatterns)
		// List paths that might be related
		for pathKey := range paths {
			if strings.Contains(strings.ToLower(pathKey), strings.ToLower(gvr.Group)) ||
				strings.Contains(strings.ToLower(pathKey), gvr.Resource) {
				fmt.Fprintf(os.Stderr, "  Available: %s\n", pathKey)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "Using basic schema (name, namespace, labels, annotations)\n")
	return createBasicSchema(gvk), nil
}

// buildTargetPaths builds possible API path patterns for a GVR
func buildTargetPaths(gvr schema.GroupVersionResource) []string {
	var patterns []string

	if gvr.Group == "" {
		// Core API
		patterns = append(patterns, fmt.Sprintf("api/%s", gvr.Version))
	} else {
		// Named API groups - try multiple patterns
		patterns = append(patterns, fmt.Sprintf("apis/%s/%s", gvr.Group, gvr.Version))
		patterns = append(patterns, fmt.Sprintf("apis/%s", gvr.Group))
		// Some OpenAPI specs use the group without version in the path
		patterns = append(patterns, gvr.Group)
	}

	return patterns
}

// gvrToGVK converts a GVR to a GVK
func gvrToGVK(gvr schema.GroupVersionResource) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   gvr.Group,
		Version: gvr.Version,
		Kind:    resourceToKind(gvr.Resource),
	}
}

// parseOpenAPISchema parses the OpenAPI schema bytes into a ResourceSchema
func parseOpenAPISchema(schemaBytes []byte, gvk schema.GroupVersionKind, gvr schema.GroupVersionResource) (*ResourceSchema, error) {
	var openAPISpec map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &openAPISpec); err != nil {
		return nil, err
	}

	// Get components/schemas
	components, ok := openAPISpec["components"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no components in schema")
	}

	schemas, ok := components["schemas"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no schemas in components")
	}

	// Find the schema for our resource using multiple strategies
	resourceDef := findResourceSchema(schemas, gvk, gvr)
	if resourceDef == nil {
		return nil, fmt.Errorf("schema not found for %s.%s/%s", gvr.Resource, gvr.Group, gvr.Version)
	}

	// Extract fields from the schema
	fields := extractFields(resourceDef, "", schemas)

	// Get description
	description, _ := resourceDef["description"].(string)

	return &ResourceSchema{
		GVK:         gvk,
		Description: description,
		Fields:      fields,
	}, nil
}

// findResourceSchema searches for the resource schema using multiple strategies
func findResourceSchema(schemas map[string]interface{}, gvk schema.GroupVersionKind, gvr schema.GroupVersionResource) map[string]interface{} {
	// Strategy 1: Match by Kind suffix (works for most CRDs and built-in resources)
	for name, def := range schemas {
		if strings.HasSuffix(name, "."+gvk.Kind) {
			if d, ok := def.(map[string]interface{}); ok {
				// Verify it has properties (is a real resource schema)
				if _, hasProps := d["properties"]; hasProps {
					return d
				}
			}
		}
	}

	// Strategy 2: Match by group components in schema name
	// CRDs often use reversed domain notation: com.example.v1.MyResource
	groupParts := strings.Split(gvr.Group, ".")
	for name, def := range schemas {
		nameLower := strings.ToLower(name)
		// Check if schema name contains group parts and kind
		matchesGroup := true
		for _, part := range groupParts {
			if !strings.Contains(nameLower, strings.ToLower(part)) {
				matchesGroup = false
				break
			}
		}
		if matchesGroup && strings.Contains(nameLower, strings.ToLower(gvk.Kind)) {
			if d, ok := def.(map[string]interface{}); ok {
				if _, hasProps := d["properties"]; hasProps {
					return d
				}
			}
		}
	}

	// Strategy 3: Built-in Kubernetes resources
	builtInRef := gvkToSchemaRef(gvk)
	for name, def := range schemas {
		if strings.Contains(name, builtInRef) {
			if d, ok := def.(map[string]interface{}); ok {
				return d
			}
		}
	}

	return nil
}

// extractFields recursively extracts field schemas from an OpenAPI definition
func extractFields(def map[string]interface{}, prefix string, allSchemas map[string]interface{}) []FieldSchema {
	var fields []FieldSchema

	properties, ok := def["properties"].(map[string]interface{})
	if !ok {
		return fields
	}

	// Get required fields
	requiredFields := make(map[string]bool)
	if required, ok := def["required"].([]interface{}); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				requiredFields[s] = true
			}
		}
	}

	// Sort property names for consistent ordering, but put required fields first
	var requiredNames, optionalNames []string
	for name := range properties {
		if requiredFields[name] {
			requiredNames = append(requiredNames, name)
		} else {
			optionalNames = append(optionalNames, name)
		}
	}
	sort.Strings(requiredNames)
	sort.Strings(optionalNames)
	propNames := append(requiredNames, optionalNames...)

	for _, name := range propNames {
		prop := properties[name]
		propDef, ok := prop.(map[string]interface{})
		if !ok {
			continue
		}

		// Skip apiVersion, kind, and status as they're handled specially
		if prefix == "" && (name == "apiVersion" || name == "kind" || name == "status") {
			continue
		}

		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		field := FieldSchema{
			Path:     path,
			Name:     name,
			Required: requiredFields[name],
		}

		// Get type
		if t, ok := propDef["type"].(string); ok {
			field.Type = t
		}

		// Get description
		if d, ok := propDef["description"].(string); ok {
			field.Description = d
		}

		// Get default
		if d, ok := propDef["default"]; ok {
			field.Default = d
		}

		// Handle $ref
		if ref, ok := propDef["$ref"].(string); ok {
			refName := strings.TrimPrefix(ref, "#/components/schemas/")
			if refDef, ok := allSchemas[refName].(map[string]interface{}); ok {
				field.Type = "object"
				field.Properties = extractFields(refDef, path, allSchemas)
			}
		}

		// Handle nested objects with additionalProperties (maps)
		if field.Type == "object" {
			if addProps, ok := propDef["additionalProperties"].(map[string]interface{}); ok {
				// This is a map type - mark it specially
				if addType, ok := addProps["type"].(string); ok {
					field.Description = fmt.Sprintf("Map of string to %s. %s", addType, field.Description)
				}
			} else if len(field.Properties) == 0 {
				// Regular nested object
				field.Properties = extractFields(propDef, path, allSchemas)
			}
		}

		// Handle arrays
		if field.Type == "array" {
			if items, ok := propDef["items"].(map[string]interface{}); ok {
				itemField := FieldSchema{}
				if t, ok := items["type"].(string); ok {
					itemField.Type = t
				}
				if ref, ok := items["$ref"].(string); ok {
					refName := strings.TrimPrefix(ref, "#/components/schemas/")
					if refDef, ok := allSchemas[refName].(map[string]interface{}); ok {
						itemField.Type = "object"
						itemField.Properties = extractFields(refDef, path+"[*]", allSchemas)
					}
				}
				field.Items = &itemField
			}
		}

		fields = append(fields, field)
	}

	return fields
}

// createBasicSchema creates a basic schema with common Kubernetes resource fields
func createBasicSchema(gvk schema.GroupVersionKind) *ResourceSchema {
	return &ResourceSchema{
		GVK:         gvk,
		Description: fmt.Sprintf("A %s resource", gvk.Kind),
		Fields: []FieldSchema{
			{
				Path:        "metadata.name",
				Name:        "name",
				Type:        "string",
				Description: "Name of the resource",
				Required:    true,
			},
			{
				Path:        "metadata.namespace",
				Name:        "namespace",
				Type:        "string",
				Description: "Namespace of the resource",
				Required:    false,
			},
			{
				Path:        "metadata.labels",
				Name:        "labels",
				Type:        "object",
				Description: "Labels for the resource",
				Required:    false,
			},
			{
				Path:        "metadata.annotations",
				Name:        "annotations",
				Type:        "object",
				Description: "Annotations for the resource",
				Required:    false,
			},
		},
	}
}

// resourceToKind converts a resource name to Kind
func resourceToKind(resource string) string {
	// Handle common irregulars
	irregulars := map[string]string{
		"endpoints": "Endpoints",
		"queues":    "Queue",
	}
	if kind, ok := irregulars[resource]; ok {
		return kind
	}

	// Standard conversion: remove plural suffix and capitalize
	if strings.HasSuffix(resource, "ies") {
		resource = strings.TrimSuffix(resource, "ies") + "y"
	} else if strings.HasSuffix(resource, "ses") {
		resource = strings.TrimSuffix(resource, "es")
	} else if strings.HasSuffix(resource, "s") {
		resource = strings.TrimSuffix(resource, "s")
	}

	// Capitalize first letter
	if len(resource) > 0 {
		resource = strings.ToUpper(resource[:1]) + resource[1:]
	}

	return resource
}

// gvkToSchemaRef converts a GVK to an OpenAPI schema reference name for built-in resources
func gvkToSchemaRef(gvk schema.GroupVersionKind) string {
	if gvk.Group == "" {
		return fmt.Sprintf("io.k8s.api.core.%s.%s", gvk.Version, gvk.Kind)
	}
	groupParts := strings.Split(gvk.Group, ".")
	return fmt.Sprintf("io.k8s.api.%s.%s.%s", groupParts[0], gvk.Version, gvk.Kind)
}

// matchesAPIPath checks if an API path matches a GVR
func matchesAPIPath(path string, gvr schema.GroupVersionResource) bool {
	if gvr.Group == "" {
		return strings.Contains(path, fmt.Sprintf("/api/%s", gvr.Version))
	}
	return strings.Contains(path, fmt.Sprintf("/apis/%s/%s", gvr.Group, gvr.Version))
}
