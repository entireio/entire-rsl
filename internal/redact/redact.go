// Package redact removes credentials from strings before they are printed or
// logged.
package redact

import "regexp"

// userinfo matches the userinfo of a URL with an authority, such as
// "user:token@" in "https://user:token@example.com/repo". It is matched
// anywhere in the string, so URLs inside error messages are covered too.
var userinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)[^/@\s]*@`)

// Placeholder replaces redacted userinfo.
const Placeholder = "redacted@"

// URLs replaces the userinfo of every URL in s.
func URLs(s string) string {
	return userinfo.ReplaceAllString(s, "${1}"+Placeholder)
}

// Values returns a copy of m with URLs redacted in every value.
func Values(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = URLs(v)
	}
	return out
}
