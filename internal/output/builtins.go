package output

import _ "embed"

//go:embed templates/ocm.tmpl
var ocmTemplate string

//go:embed templates/bom.tmpl
var bomTemplate string

// BuiltinFormat returns the FormatConfig for a named built-in format.
// Valid names: "ocm", "bom". Returns false if the name is unknown.
// Built-in formats run through the exact same TemplateFormatter path as custom configs.
func BuiltinFormat(name string) (FormatConfig, bool) {
	switch name {
	case "ocm":
		return FormatConfig{
			Template: ocmTemplate,
			Args: map[string]string{
				"component": "",
				"version":   "",
				"provider":  "",
			},
		}, true
	case "bom":
		return FormatConfig{
			Template: bomTemplate,
			Args: map[string]string{
				"name":      "",
				"namespace": "",
				"license":   "",
			},
		}, true
	default:
		return FormatConfig{}, false
	}
}
