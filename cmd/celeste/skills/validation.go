package skills

import (
	"fmt"
	"regexp"
	"strings"
)

var skillNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

var allowedJSONTypes = map[string]struct{}{
	"string":  {},
	"number":  {},
	"integer": {},
	"boolean": {},
	"object":  {},
	"array":   {},
}

// ValidateSkillDefinition validates a skill structure before exposing it as a tool.
func ValidateSkillDefinition(skill Skill) error {
	name := strings.TrimSpace(skill.Name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if !skillNamePattern.MatchString(name) {
		return fmt.Errorf("skill name must match %s", skillNamePattern.String())
	}

	if strings.TrimSpace(skill.Description) == "" {
		return fmt.Errorf("skill description is required")
	}

	if skill.Parameters == nil {
		return fmt.Errorf("skill parameters are required")
	}

	paramType, ok := skill.Parameters["type"].(string)
	if !ok || paramType != "object" {
		return fmt.Errorf("skill parameters.type must be 'object'")
	}

	propertiesRaw, ok := skill.Parameters["properties"]
	if !ok {
		return fmt.Errorf("skill parameters.properties is required")
	}

	properties, ok := propertiesRaw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("skill parameters.properties must be an object")
	}

	for propName, propRaw := range properties {
		propMap, ok := propRaw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("property '%s' must be an object", propName)
		}

		propType, ok := propMap["type"].(string)
		if !ok || propType == "" {
			return fmt.Errorf("property '%s' must define a string type", propName)
		}

		if _, ok := allowedJSONTypes[propType]; !ok {
			return fmt.Errorf("property '%s' has unsupported type '%s'", propName, propType)
		}
	}

	requiredFields, err := parseRequiredFields(skill.Parameters["required"])
	if err != nil {
		return err
	}

	for _, required := range requiredFields {
		if _, exists := properties[required]; !exists {
			return fmt.Errorf("required field '%s' is not declared in properties", required)
		}
	}

	return nil
}

func parseRequiredFields(requiredRaw interface{}) ([]string, error) {
	if requiredRaw == nil {
		return nil, nil
	}

	switch required := requiredRaw.(type) {
	case []string:
		return required, nil
	case []interface{}:
		result := make([]string, 0, len(required))
		for i, field := range required {
			str, ok := field.(string)
			if !ok {
				return nil, fmt.Errorf("required[%d] must be a string", i)
			}
			result = append(result, str)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("skill parameters.required must be an array of strings")
	}
}
