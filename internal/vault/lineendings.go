package vault

import (
	"bytes"
	"fmt"
	"runtime"
)

// LineEndingPolicy controls how a project's stored env bytes map to the bytes
// written to disk. It exists because a vault is shared across operating
// systems: capturing on Windows and applying on Linux otherwise carries CRLF
// into the Linux .env forever.
//
// The empty policy preserves bytes exactly, which is gitenv's original
// contract, so existing vaults keep behaving identically.
type LineEndingPolicy string

const (
	// LineEndingsPreserve stores and restores bytes verbatim.
	LineEndingsPreserve LineEndingPolicy = ""
	// LineEndingsLF stores LF and always writes LF.
	LineEndingsLF LineEndingPolicy = "lf"
	// LineEndingsCRLF stores LF and always writes CRLF.
	LineEndingsCRLF LineEndingPolicy = "crlf"
	// LineEndingsNative stores LF and writes the host's convention.
	LineEndingsNative LineEndingPolicy = "native"
)

// LineEndingPolicies lists every selectable policy in presentation order.
var LineEndingPolicies = []LineEndingPolicy{
	LineEndingsPreserve,
	LineEndingsNative,
	LineEndingsLF,
	LineEndingsCRLF,
}

func (p LineEndingPolicy) String() string {
	switch p {
	case LineEndingsPreserve:
		return "preserve"
	case LineEndingsLF:
		return "lf"
	case LineEndingsCRLF:
		return "crlf"
	case LineEndingsNative:
		return "native"
	default:
		return string(p)
	}
}

// ValidateLineEndingPolicy rejects unknown policies before they reach a vault.
func ValidateLineEndingPolicy(policy LineEndingPolicy) error {
	switch policy {
	case LineEndingsPreserve, LineEndingsLF, LineEndingsCRLF, LineEndingsNative:
		return nil
	default:
		return fmt.Errorf("unknown line ending policy %q", string(policy))
	}
}

// ParseLineEndingPolicy accepts the display form produced by String.
func ParseLineEndingPolicy(value string) (LineEndingPolicy, error) {
	switch value {
	case "", "preserve":
		return LineEndingsPreserve, nil
	case "lf":
		return LineEndingsLF, nil
	case "crlf":
		return LineEndingsCRLF, nil
	case "native":
		return LineEndingsNative, nil
	default:
		return LineEndingsPreserve, fmt.Errorf("unknown line ending policy %q", value)
	}
}

// NormalizeStored converts on-disk bytes to the vault's canonical form for a
// policy. Every policy except preserve stores LF, so a project captured on
// Windows and one captured on Linux produce identical ciphertext input.
//
// This is also the normalization used before checksum comparison, otherwise a
// converting policy would report the local file as permanently modified.
func NormalizeStored(data []byte, policy LineEndingPolicy) []byte {
	if policy == LineEndingsPreserve || !bytes.Contains(data, []byte("\r\n")) {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

// RenderForDisk converts canonical stored bytes to the bytes a policy wants on
// disk. Input is normalized first so a vault written before the policy existed
// still renders correctly.
func RenderForDisk(data []byte, policy LineEndingPolicy) []byte {
	switch policy {
	case LineEndingsPreserve:
		return data
	case LineEndingsLF:
		return NormalizeStored(data, LineEndingsLF)
	case LineEndingsCRLF:
		return toCRLF(data)
	case LineEndingsNative:
		if runtime.GOOS == "windows" {
			return toCRLF(data)
		}
		return NormalizeStored(data, LineEndingsLF)
	default:
		return data
	}
}

func toCRLF(data []byte) []byte {
	lf := NormalizeStored(data, LineEndingsLF)
	if !bytes.Contains(lf, []byte("\n")) {
		return lf
	}
	return bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n"))
}
