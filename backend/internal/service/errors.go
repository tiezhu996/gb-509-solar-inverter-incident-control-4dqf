package service

import (
	"errors"

	"github.com/blueship581/solar-inverter-incident-control/backend/internal/dto"
)

var (
	ErrInvalidTransition = errors.New("requested status transition is not allowed")
	ErrInvalidInput      = errors.New("business input validation failed")
	ErrUnauthorized      = errors.New("invalid username or password")
	ErrInactiveUser      = errors.New("user account is inactive")
)

// BatchClaimRejectedError means an all-or-nothing batch claim was refused; it
// carries one blocking reason per submitted entry and guarantees no write.
type BatchClaimRejectedError struct {
	Blocks []dto.BatchClaimBlock
}

func (e *BatchClaimRejectedError) Error() string {
	return "batch claim rejected; no records were changed"
}

func NewBatchClaimRejectedError(blocks []dto.BatchClaimBlock) *BatchClaimRejectedError {
	return &BatchClaimRejectedError{Blocks: blocks}
}
