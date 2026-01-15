package prompt

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSetValues parses --set flag values into a map
// Supports formats like:
//   - spec.replicas=3
//   - spec.template.spec.containers[0].image=nginx
//   - metadata.labels.app=myapp
func ParseSetValues(setValues []string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	
	for _, sv := range setValues {
		parts := strings.SplitN(sv, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --set format: %q (expected key=value)", sv)
		}
		
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		
		if key == "" {
			return nil, fmt.Errorf("empty key in --set: %q", sv)
		}
		
		// Parse the value to appropriate type
		parsedValue := parseValue(value)
		result[key] = parsedValue
	}
	
	return result, nil
}

// parseValue attempts to parse a string value to its appropriate type
func parseValue(value string) interface{} {
	// Try boolean
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}
	
	// Try integer
	if i, err := strconv.ParseInt(value, 10, 64); err == nil {
		return i
	}
	
	// Try float
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		// Only use float if it has a decimal point
		if strings.Contains(value, ".") {
			return f
		}
	}
	
	// Return as string
	return value
}

// BuildNestedMap builds a nested map from dot-notation paths
// e.g., {"spec.replicas": 3} -> {"spec": {"replicas": 3}}
func BuildNestedMap(flatMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	for path, value := range flatMap {
		setNestedValue(result, path, value)
	}
	
	return result
}

// setNestedValue sets a value at a nested path in a map
func setNestedValue(m map[string]interface{}, path string, value interface{}) {
	parts := parsePath(path)
	
	current := m
	for i, part := range parts[:len(parts)-1] {
		key, index := parsePathPart(part)
		
		if index >= 0 {
			// Array access
			arr, ok := current[key].([]interface{})
			if !ok {
				arr = make([]interface{}, index+1)
				current[key] = arr
			}
			// Extend array if needed
			for len(arr) <= index {
				arr = append(arr, nil)
				current[key] = arr
			}
			
			// Check if next element needs to be a map
			if arr[index] == nil {
				if i < len(parts)-2 {
					arr[index] = make(map[string]interface{})
				}
			}
			
			if nextMap, ok := arr[index].(map[string]interface{}); ok {
				current = nextMap
			} else {
				nextMap := make(map[string]interface{})
				arr[index] = nextMap
				current = nextMap
			}
		} else {
			// Object access
			if _, ok := current[key]; !ok {
				current[key] = make(map[string]interface{})
			}
			if nextMap, ok := current[key].(map[string]interface{}); ok {
				current = nextMap
			}
		}
	}
	
	// Set the final value
	lastPart := parts[len(parts)-1]
	key, index := parsePathPart(lastPart)
	
	if index >= 0 {
		arr, ok := current[key].([]interface{})
		if !ok {
			arr = make([]interface{}, index+1)
		}
		for len(arr) <= index {
			arr = append(arr, nil)
		}
		arr[index] = value
		current[key] = arr
	} else {
		current[key] = value
	}
}

// parsePath splits a path by dots, respecting array brackets
func parsePath(path string) []string {
	var parts []string
	var current strings.Builder
	
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c == '.' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		} else if c == '[' {
			// Include the bracket part with the current segment
			if current.Len() > 0 {
				for i < len(path) && path[i] != ']' {
					current.WriteByte(path[i])
					i++
				}
				if i < len(path) {
					current.WriteByte(']')
				}
			}
		} else {
			current.WriteByte(c)
		}
	}
	
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	
	return parts
}

// parsePathPart parses a path part that may include an array index
// Returns the key name and index (-1 if not an array access)
func parsePathPart(part string) (string, int) {
	bracketIdx := strings.Index(part, "[")
	if bracketIdx < 0 {
		return part, -1
	}
	
	key := part[:bracketIdx]
	indexStr := strings.TrimSuffix(strings.TrimPrefix(part[bracketIdx:], "["), "]")
	
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return part, -1
	}
	
	return key, index
}
