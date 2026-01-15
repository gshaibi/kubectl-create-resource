package prompt

import (
	"fmt"
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
	values := &CollectedValues{
		Name:   name,
		Values: make(map[string]interface{}),
	}

	// Parse --set values first
	flagValues, err := ParseSetValues(setValues)
	if err != nil {
		return nil, err
	}

	// Merge flag values into collected values
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
			})
			if err != nil {
				return nil, err
			}
			values.Name = promptedName.(string)
		}
	}
	values.Values["metadata.name"] = values.Name

	// Prompt for required fields that haven't been set
	for _, field := range schema.Fields {
		// Skip if already set via flags
		if _, ok := values.Values[field.Path]; ok {
			continue
		}

		// Only prompt for required fields or fields in spec
		if !field.Required && !strings.HasPrefix(field.Path, "spec.") {
			continue
		}

		// Skip complex nested objects for now - they can be set via --set
		if field.Type == "object" && len(field.Properties) > 0 {
			continue
		}

		// Prompt for the field
		val, err := promptForField(field)
		if err != nil {
			if err == promptui.ErrInterrupt {
				return nil, fmt.Errorf("interrupted")
			}
			// Skip fields where user just pressed enter (empty optional fields)
			continue
		}
		
		if val != nil && val != "" {
			values.Values[field.Path] = val
		}
	}

	return values, nil
}

// promptForField prompts the user for a field value
func promptForField(field client.FieldSchema) (interface{}, error) {
	label := field.Name
	if field.Required {
		label += " (required)"
	}
	if field.Description != "" {
		// Truncate long descriptions
		desc := field.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		label += fmt.Sprintf(" [%s]", desc)
	}

	switch field.Type {
	case "boolean":
		return promptBoolean(label, field.Default)
	case "integer":
		return promptInteger(label, field.Default, field.Required)
	case "array":
		return promptArray(label, field.Items)
	default: // string and others
		return promptString(label, field.Default, field.Required)
	}
}

// promptString prompts for a string value
func promptString(label string, defaultVal interface{}, required bool) (string, error) {
	defaultStr := ""
	if defaultVal != nil {
		defaultStr = fmt.Sprintf("%v", defaultVal)
	}

	prompt := promptui.Prompt{
		Label:   label,
		Default: defaultStr,
		Validate: func(input string) error {
			if required && input == "" && defaultStr == "" {
				return fmt.Errorf("this field is required")
			}
			return nil
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
					return fmt.Errorf("this field is required")
				}
				return nil
			}
			_, err := strconv.ParseInt(input, 10, 64)
			if err != nil {
				return fmt.Errorf("must be a valid integer")
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

	return strconv.ParseInt(result, 10, 64)
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
