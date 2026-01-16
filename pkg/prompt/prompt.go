package prompt

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gshaibi/kubectl-create-resource/pkg/client"
	"github.com/manifoldco/promptui"
)

// CollectedValues holds the values collected from user input
type CollectedValues struct {
	Name   string
	Values map[string]interface{}
}

// CollectFieldValues collects field values through interactive prompts and/or flags
func CollectFieldValues(schema *client.ResourceSchema, name string, setValues []string) (*CollectedValues, error) {
	return CollectFieldValuesWithTemplate(schema, name, setValues, nil)
}

// CollectFieldValuesWithTemplate collects field values using an optional template for defaults
func CollectFieldValuesWithTemplate(schema *client.ResourceSchema, name string, setValues []string, templateValues map[string]interface{}) (*CollectedValues, error) {
	values := &CollectedValues{
		Name:   name,
		Values: make(map[string]interface{}),
	}

	// Parse --set values first (highest priority)
	flagValues, err := ParseSetValues(setValues)
	if err != nil {
		return nil, err
	}

	// Start with template values as base (if provided)
	if templateValues != nil {
		for k, v := range templateValues {
			values.Values[k] = v
		}
		fmt.Printf("Loaded %d fields from template\n", len(templateValues))
	}

	// Override with flag values (flags take precedence over template)
	for k, v := range flagValues {
		values.Values[k] = v
	}

	// If name not provided via flag, prompt for it
	if values.Name == "" {
		nameVal, ok := values.Values["metadata.name"]
		if ok {
			values.Name = fmt.Sprintf("%v", nameVal)
		} else {
			promptedName, err := promptForField(client.FieldSchema{
				Path:        "metadata.name",
				Name:        "name",
				Type:        "string",
				Description: "Name of the resource",
				Required:    true,
			}, nil)
			if err != nil {
				return nil, err
			}
			values.Name = promptedName.(string)
		}
	}
	values.Values["metadata.name"] = values.Name

	// If we have template values, prompt user to confirm/modify each spec field
	if templateValues != nil {
		err = promptForTemplateFields(values, flagValues)
		if err != nil {
			return nil, err
		}
	} else {
		// Prompt for fields from the schema (original behavior)
		err = promptForFields(schema.Fields, values, flagValues)
		if err != nil {
			return nil, err
		}
	}

	return values, nil
}

// promptForTemplateFields prompts user to confirm/modify fields from template
func promptForTemplateFields(values *CollectedValues, flagValues map[string]interface{}) error {
	// Get all spec fields from current values
	var specFields []string
	for path := range values.Values {
		if strings.HasPrefix(path, "spec.") && !strings.Contains(path, "[") {
			specFields = append(specFields, path)
		}
	}
	
	// Sort for consistent ordering
	sort.Strings(specFields)

	fmt.Println("\nTemplate fields (press Enter to keep, or type new value):")
	
	for _, path := range specFields {
		// Skip if already set via flag
		if _, ok := flagValues[path]; ok {
			continue
		}

		currentVal := values.Values[path]
		
		// Create a field schema from the template value
		field := client.FieldSchema{
			Path:     path,
			Name:     path,
			Type:     inferType(currentVal),
			Required: false,
		}

		newVal, err := promptForField(field, currentVal)
		if err != nil {
			if err == promptui.ErrInterrupt {
				return fmt.Errorf("interrupted")
			}
			continue
		}

		if newVal != nil && newVal != "" {
			values.Values[path] = newVal
		}
	}

	return nil
}

// inferType infers the field type from a value
func inferType(val interface{}) string {
	switch val.(type) {
	case bool:
		return "boolean"
	case int, int64, float64:
		return "integer"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "string"
	}
}

// promptForFields recursively prompts for fields
func promptForFields(fields []client.FieldSchema, values *CollectedValues, flagValues map[string]interface{}) error {
	for _, field := range fields {
		// Skip if already set via flags
		if _, ok := flagValues[field.Path]; ok {
			continue
		}

		// Skip metadata fields (already handled)
		if strings.HasPrefix(field.Path, "metadata.") {
			continue
		}

		// For required fields or spec fields, prompt the user
		if field.Required || strings.HasPrefix(field.Path, "spec.") {
			// Handle nested objects with properties
			if field.Type == "object" && len(field.Properties) > 0 {
				// Recursively prompt for nested required fields
				err := promptForFields(field.Properties, values, flagValues)
				if err != nil {
					return err
				}
				continue
			}

			// Skip complex types without a clear prompting strategy
			if field.Type == "object" && len(field.Properties) == 0 {
				// This might be a map type - show info but skip interactive prompt
				if field.Required {
					fmt.Printf("Note: %s is required but is a complex type. Use --set=%s.key=value\n", field.Path, field.Path)
				}
				continue
			}

			// Prompt for the field
			val, err := promptForField(field, nil)
			if err != nil {
				if err == promptui.ErrInterrupt {
					return fmt.Errorf("interrupted")
				}
				// Skip fields where user just pressed enter (empty optional fields)
				continue
			}

			if val != nil && val != "" {
				values.Values[field.Path] = val
			}
		}
	}

	return nil
}

// promptForField prompts the user for a field value
// templateDefault is used as the default value if provided (overrides schema default)
func promptForField(field client.FieldSchema, templateDefault interface{}) (interface{}, error) {
	// Build a clear label
	label := field.Path
	if field.Required {
		label += " *"
	}

	// Use template default if provided, otherwise use schema default
	defaultVal := field.Default
	if templateDefault != nil {
		defaultVal = templateDefault
		// Show current value in label
		label += fmt.Sprintf(" [current: %v]", templateDefault)
	}

	// Print description separately so prompt label stays clean
	if field.Description != "" {
		desc := field.Description
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		fmt.Printf("  %s\n", desc)
	}

	switch field.Type {
	case "boolean":
		return promptBoolean(label, defaultVal)
	case "integer":
		return promptInteger(label, defaultVal, field.Required)
	case "number":
		return promptNumber(label, defaultVal, field.Required)
	case "array":
		return promptArray(label, field.Items)
	default: // string and others
		return promptString(label, defaultVal, field.Required)
	}
}

// promptString prompts for a string value
func promptString(label string, defaultVal interface{}, required bool) (string, error) {
	defaultStr := ""
	if defaultVal != nil {
		defaultStr = fmt.Sprintf("%v", defaultVal)
	}

	validateFunc := func(input string) error {
		if required && input == "" && defaultStr == "" {
			return fmt.Errorf("required")
		}
		return nil
	}

	prompt := promptui.Prompt{
		Label:    label,
		Default:  defaultStr,
		Validate: validateFunc,
		Templates: &promptui.PromptTemplates{
			Prompt:  "{{ . }}: ",
			Valid:   "{{ . | green }}: ",
			Invalid: "{{ . | red }}: ",
			Success: "{{ . | bold }}: ",
		},
	}

	result, err := prompt.Run()
	if err != nil {
		return "", err
	}

	if result == "" && defaultStr != "" {
		return defaultStr, nil
	}
	return result, nil
}

// promptInteger prompts for an integer value
func promptInteger(label string, defaultVal interface{}, required bool) (int64, error) {
	defaultStr := ""
	if defaultVal != nil {
		defaultStr = fmt.Sprintf("%v", defaultVal)
	}

	prompt := promptui.Prompt{
		Label:   label,
		Default: defaultStr,
		Validate: func(input string) error {
			if input == "" {
				if required && defaultStr == "" {
					return fmt.Errorf("required")
				}
				return nil
			}
			_, err := strconv.ParseInt(input, 10, 64)
			if err != nil {
				return fmt.Errorf("must be integer")
			}
			return nil
		},
		Templates: &promptui.PromptTemplates{
			Prompt:  "{{ . }}: ",
			Valid:   "{{ . | green }}: ",
			Invalid: "{{ . | red }}: ",
			Success: "{{ . | bold }}: ",
		},
	}

	result, err := prompt.Run()
	if err != nil {
		return 0, err
	}

	if result == "" {
		if defaultStr != "" {
			result = defaultStr
		} else {
			return 0, nil
		}
	}

	return strconv.ParseInt(result, 10, 64)
}

// promptNumber prompts for a float value
func promptNumber(label string, defaultVal interface{}, required bool) (float64, error) {
	defaultStr := ""
	if defaultVal != nil {
		defaultStr = fmt.Sprintf("%v", defaultVal)
	}

	prompt := promptui.Prompt{
		Label:   label,
		Default: defaultStr,
		Validate: func(input string) error {
			if input == "" {
				if required && defaultStr == "" {
					return fmt.Errorf("this field is required")
				}
				return nil
			}
			_, err := strconv.ParseFloat(input, 64)
			if err != nil {
				return fmt.Errorf("must be a valid number")
			}
			return nil
		},
	}

	result, err := prompt.Run()
	if err != nil {
		return 0, err
	}

	if result == "" {
		if defaultStr != "" {
			result = defaultStr
		} else {
			return 0, nil
		}
	}

	return strconv.ParseFloat(result, 64)
}

// promptBoolean prompts for a boolean value
func promptBoolean(label string, defaultVal interface{}) (bool, error) {
	items := []string{"true", "false"}
	index := 1 // default to false
	if defaultVal == true {
		index = 0
	}

	prompt := promptui.Select{
		Label:     label,
		Items:     items,
		CursorPos: index,
	}

	_, result, err := prompt.Run()
	if err != nil {
		return false, err
	}

	return result == "true", nil
}

// promptArray prompts for array values
func promptArray(label string, items *client.FieldSchema) ([]interface{}, error) {
	fmt.Printf("%s (enter values one per line, empty line to finish):\n", label)

	var values []interface{}
	for {
		prompt := promptui.Prompt{
			Label: fmt.Sprintf("  [%d]", len(values)),
		}

		result, err := prompt.Run()
		if err != nil {
			if err == promptui.ErrInterrupt {
				return nil, err
			}
			break
		}

		if result == "" {
			break
		}

		// Convert based on item type
		if items != nil && items.Type == "integer" {
			val, err := strconv.ParseInt(result, 10, 64)
			if err != nil {
				fmt.Println("  Invalid integer, try again")
				continue
			}
			values = append(values, val)
		} else {
			values = append(values, result)
		}
	}

	return values, nil
}
