package shared

import "strings"

// PathBase returns the last element of a file path, handling both Unix ("/")
// and Windows ("\") separators regardless of the OS the server runs on.
func PathBase(p string) string {
	p = strings.TrimRight(p, `/\`)
	if p == "" {
		return ""
	}
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
