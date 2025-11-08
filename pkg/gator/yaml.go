package gator

import (
	"encoding/json"

	"go.yaml.in/yaml/v3"
)

func ParseYaml(yamlBytes []byte, v interface{}) error {
	obj := make(map[string]interface{})

	err := yaml.Unmarshal(yamlBytes, obj)
	if err != nil {
		return err
	}

	return FixYAML(obj, v)
}

// Pass through JSON since k8s parsing logic doesn't fully handle objects
// parsed directly from YAML. Without passing through JSON, the OPA client
// panics when handed scalar types it doesn't recognize.
func FixYAML(obj map[string]interface{}, v interface{}) error {
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	return parseJSON(jsonBytes, v)
}

func parseJSON(jsonBytes []byte, v interface{}) error {
	return json.Unmarshal(jsonBytes, v)
}
