package vault

import (
	"fmt"
	"regexp"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func ValidateName(kind, value string) error {
	if !validName.MatchString(value) {
		return fmt.Errorf("invalid %s %q: use 1-64 letters, digits, dot, underscore or dash", kind, value)
	}
	return nil
}
