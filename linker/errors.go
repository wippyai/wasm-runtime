package linker

import (
	"fmt"
	"strings"
)

// InstantiationError provides context when component instantiation fails.
type InstantiationError struct {
	Cause         error
	Phase         string
	ImportPath    string
	Reason        string
	InstanceIndex int
}

func (e *InstantiationError) Error() string {
	var b strings.Builder
	b.WriteString("instantiation failed")

	if e.Phase != "" {
		b.WriteString(" at ")
		b.WriteString(e.Phase)
	}

	if e.InstanceIndex >= 0 {
		fmt.Fprintf(&b, " (instance %d)", e.InstanceIndex)
	}

	if e.ImportPath != "" {
		b.WriteString(": ")
		b.WriteString(e.ImportPath)
	}

	if e.Reason != "" {
		b.WriteString(": ")
		b.WriteString(e.Reason)
	}

	if e.Cause != nil {
		b.WriteString(": ")
		b.WriteString(e.Cause.Error())
	}

	return b.String()
}

func (e *InstantiationError) Unwrap() error {
	return e.Cause
}

// instError creates an InstantiationError with the given parameters
func instError(phase string, instanceIdx int, importPath, reason string, cause error) *InstantiationError {
	return &InstantiationError{
		Phase:         phase,
		InstanceIndex: instanceIdx,
		ImportPath:    importPath,
		Reason:        reason,
		Cause:         cause,
	}
}
