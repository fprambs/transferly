package remotefs

import "fmt"

// NegotiationError means the SSH peers have no common algorithm. Reconnecting cannot resolve it.
type NegotiationError struct {
	Category         string
	ServerAlgorithms []string
}

// Error describes the incompatible category and bounded server advertisement.
func (e *NegotiationError) Error() string {
	return fmt.Sprintf("SSH algorithm incompatibility (%s); server offers %v", e.Category, e.ServerAlgorithms)
}
