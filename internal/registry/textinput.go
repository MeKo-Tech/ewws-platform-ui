package registry

import (
	"fmt"
	"strings"
)

// secretRefPrefix marks an env value as a reference to one key of a
// Secret: KEY=secret:<secret-name>/<key>.
const secretRefPrefix = "secret:"

// ParseEnvLines parses one KEY=value per line. A value of the form
// secret:<secret-name>/<key> becomes a secret reference. Blank lines and
// lines starting with # are ignored, whitespace around keys and values is
// trimmed, an empty value is allowed and a key given twice is an error.
// Names and references are checked by Validate, not here.
func ParseEnvLines(text string) ([]EnvVar, error) {
	var out []EnvVar

	seen := map[string]bool{}

	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)

		if !ok || name == "" {
			return nil, fmt.Errorf("line %d: %q is not KEY=value", i+1, line)
		}

		if seen[name] {
			return nil, fmt.Errorf("line %d: %s is set twice", i+1, name)
		}

		seen[name] = true

		if ref, isRef := strings.CutPrefix(value, secretRefPrefix); isRef {
			secret, key, ok := strings.Cut(ref, "/")
			secret = strings.TrimSpace(secret)
			key = strings.TrimSpace(key)

			if !ok || secret == "" || key == "" {
				return nil, fmt.Errorf(
					"line %d: %s: secret reference must be secret:<secret-name>/<key>", i+1, name,
				)
			}

			out = append(out, EnvVar{Name: name, Secret: secret, Key: key})

			continue
		}

		out = append(out, EnvVar{Name: name, Value: value})
	}

	return out, nil
}

// FormatEnvLines is the inverse of ParseEnvLines.
func FormatEnvLines(vars []EnvVar) string {
	var b strings.Builder

	for _, v := range vars {
		if v.Secret != "" || v.Key != "" {
			fmt.Fprintf(&b, "%s=%s%s/%s\n", v.Name, secretRefPrefix, v.Secret, v.Key)
		} else {
			fmt.Fprintf(&b, "%s=%s\n", v.Name, v.Value)
		}
	}

	return b.String()
}

// ParseNameList splits a comma- or whitespace-separated list, dropping
// empty entries and duplicates while keeping the first-seen order.
func ParseNameList(text string) []string {
	var out []string

	seen := map[string]bool{}

	for _, f := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}

	return out
}
