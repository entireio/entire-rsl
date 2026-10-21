// Package refname validates git ref names.
//
// Validate applies the rules of git check-ref-format with --allow-onelevel,
// so it accepts both full names (refs/heads/main) and short ones (main), and
// additionally refuses a leading '-' so a name can never be read as an option.
package refname

import (
	"errors"
	"fmt"
	"strings"
)

// maxLen caps a ref name.
const maxLen = 1024

// ErrInvalid is wrapped by every validation failure.
var ErrInvalid = errors.New("invalid ref name")

// Validate reports whether name is a valid ref name.
func Validate(name string) error {
	if err := validate(name); err != nil {
		return fmt.Errorf("%w %q: %s", ErrInvalid, truncate(name), err.Error())
	}
	return nil
}

func validate(name string) error {
	switch {
	case name == "":
		return errors.New("empty")
	case len(name) > maxLen:
		return fmt.Errorf("longer than %d bytes", maxLen)
	case strings.HasPrefix(name, "-"):
		return errors.New("must not begin with '-'")
	case name == "@":
		return errors.New("must not be '@'")
	case strings.HasPrefix(name, "/"), strings.HasSuffix(name, "/"):
		return errors.New("must not begin or end with '/'")
	case strings.HasSuffix(name, "."):
		return errors.New("must not end with '.'")
	case strings.Contains(name, "//"):
		return errors.New("must not contain '//'")
	case strings.Contains(name, ".."):
		return errors.New("must not contain '..'")
	case strings.Contains(name, "@{"):
		return errors.New("must not contain '@{'")
	}

	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("must not contain control characters")
		}
		if strings.ContainsRune(" ~^:?*[\\", r) {
			return fmt.Errorf("must not contain %q", r)
		}
	}

	for component := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(component, ".") {
			return errors.New("no component may begin with '.'")
		}
		if strings.HasSuffix(component, ".lock") {
			return errors.New("no component may end with '.lock'")
		}
	}

	return nil
}

// IsFull reports whether name is HEAD or a full name under refs/.
func IsFull(name string) bool {
	return name == "HEAD" || strings.HasPrefix(name, "refs/")
}

func truncate(s string) string {
	const n = 80
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
