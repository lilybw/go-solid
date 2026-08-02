package registry

import "strings"

// Whether or not to skip a dir for registry or cache purposes. Skips node_modules and any directory starting with a dot (.) except the root.
func SkipDir(base string, path, root string) bool {
	if base == "node_modules" {
		return true
	}
	if strings.HasPrefix(base, ".") && path != root {
		return true
	}
	return false
}
