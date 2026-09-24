package auth

import "github.com/google/uuid"

// SetIDGenerator replaces the identifier generator, so tests can force its failure.
func (s *Service) SetIDGenerator(f func() (uuid.UUID, error)) { s.newID = f }
