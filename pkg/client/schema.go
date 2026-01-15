package client

import (
	"encoding/json"
	"fmt"
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
	Path        string      // JSON path (e.g., "spec.replicas")
	Name        string      // Field name (e.g., "replicas")
	Type        string      // Field type (string, integer, boolean, array, object)
	Description string      // Field description
	Required    bool        // Whether the field is required
	Default     interface{} // Default value if any
	Items       *FieldSchema // For arrays, the schema of items
	Properties  []FieldSchema // For objects, nested properties
}

// GetSchema retrieves the OpenAPI schema for a resource
func GetSchema(discoveryClient discovery.DiscoveryInterface, gvr schema.GroupVersionResource) (*ResourceSchema, error) {
	// Get the OpenAPI v3 client
	openAPIClient := discoveryClient.OpenAPIV3()
	if openAPIClient == nil {
		return nil, fmt.Errorf("OpenAPI v3 not available")
	}

	// Get the paths
	paths, err := openAPIClient.Paths()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenAPI paths: %w", err)
	}

	// Find the schema for this resource
	gvk := schema.GroupVersionKind{
		Group:   gvr.Group,
		Version: gvr.Version,
		Kind:    gvrToKind(gvr),
	}

	// Try to find the schema in the OpenAPI spec
	schemaRef := gvkToSchemaRef(gvk)
	
	for pathKey, pathValue := range paths {
		// Look for the API group path
		if !matchesAPIPath(pathKey, gvr) {
			continue
		}

		schema, err := pathValue.Schema("application/json")
		if err != nil {
			continue
		}

		// Parse the schema
		resourceSchema, err := parseOpenAPISchema(schema, gvk, schemaRef)
		if err != nil {
			continue
		}

		return resourceSchema, nil
	}

	// If we couldn't find the schema, return a basic schema with common fields
	return createBasicSchema(gvk), nil
}

// parseOpenAPISchema parses the OpenAPI schema bytes into a ResourceSchema
func parseOpenAPISchema(schemaBytes []byte, gvk schema.GroupVersionKind, schemaRef string) (*ResourceSchema, error) {
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

	// Find the schema for our resource
	var resourceDef map[string]interface{}
	for name, def := range schemas {
		if strings.Contains(name, schemaRef) || matchesGVK(name, gvk) {
			if d, ok := def.(map[string]interface{}); ok {
				resourceDef = d
				break
			}
		}
	}

	if resourceDef == nil {
		return nil, fmt.Errorf("schema not found for %s", schemaRef)
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

	// Sort property names for consistent ordering
	var propNames []string
	for name := range properties {
		propNames = append(propNames, name)
	}
	sort.Strings(propNames)

	for _, name := range propNames {
		prop := properties[name]
		propDef, ok := prop.(map[string]interface{})
		if !ok {
			continue
		}

		// Skip metadata, apiVersion, kind, and status as they're handled specially
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

		// Handle nested objects
		if field.Type == "object" && len(field.Properties) == 0 {
			field.Properties = extractFields(propDef, path, allSchemas)
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

// gvrToKind converts a GVR to a Kind name
func gvrToKind(gvr schema.GroupVersionResource) string {
	// Simple heuristic: remove trailing 's' and capitalize
	resource := gvr.Resource
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

// gvkToSchemaRef converts a GVK to an OpenAPI schema reference name
func gvkToSchemaRef(gvk schema.GroupVersionKind) string {
	if gvk.Group == "" {
		return fmt.Sprintf("io.k8s.api.core.%s.%s", gvk.Version, gvk.Kind)
	}
	// Convert group to schema format (e.g., apps -> io.k8s.api.apps.v1.Deployment)
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

// matchesGVK checks if a schema name matches a GVK
func matchesGVK(schemaName string, gvk schema.GroupVersionKind) bool {
	return strings.HasSuffix(schemaName, "."+gvk.Kind)
}
