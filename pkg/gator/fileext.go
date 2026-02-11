package gator

// File extension constants for supported file formats.
const (
	// ExtYAML is the standard YAML file extension.
	ExtYAML = ".yaml"
	// ExtYML is the alternative YAML file extension.
	ExtYML = ".yml"
	// ExtJSON is the JSON file extension.
	ExtJSON = ".json"
)

// IsYAMLExtension returns true if the extension is a valid YAML extension.
func IsYAMLExtension(ext string) bool {
	return ext == ExtYAML || ext == ExtYML
}

// IsSupportedExtension returns true if the extension is supported (YAML or JSON).
func IsSupportedExtension(ext string) bool {
	return ext == ExtYAML || ext == ExtYML || ext == ExtJSON
}
