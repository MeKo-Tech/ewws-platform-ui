package registry

import "regexp"

func mustCompileSchema(p string) *regexp.Regexp {
	return regexp.MustCompile(p)
}
