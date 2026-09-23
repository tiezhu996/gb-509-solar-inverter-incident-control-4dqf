package dto

import "time"

// CreateFaultEvent is the public write contract for 故障事件. Status is deliberately
// omitted so callers cannot bypass the service state machine.
type CreateFaultEvent struct {
	Code        string    `json:"code" binding:"required,min=2,max=64"`
	Name        string    `json:"name" binding:"required,min=2,max=160"`
	Description string    `json:"description" binding:"max=1000"`
	Facility    string    `json:"facility" binding:"required,max=120"`
	Owner       string    `json:"owner" binding:"required,max=120"`
	Category    string    `json:"category" binding:"required,max=80"`
	RiskLevel   string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt time.Time `json:"effectiveAt" binding:"required"`
	Evidence    string    `json:"evidence" binding:"max=2000"`
	RelatedCode string    `json:"relatedCode" binding:"max=64"`
}

type UpdateFaultEvent struct {
	ExpectedVersion uint      `json:"expectedVersion" binding:"required"`
	Name            string    `json:"name" binding:"required,min=2,max=160"`
	Description     string    `json:"description" binding:"max=1000"`
	Facility        string    `json:"facility" binding:"required,max=120"`
	Owner           string    `json:"owner" binding:"required,max=120"`
	Category        string    `json:"category" binding:"required,max=80"`
	RiskLevel       string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue     float64   `json:"metricValue"`
	MetricUnit      string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt     time.Time `json:"effectiveAt" binding:"required"`
	Evidence        string    `json:"evidence" binding:"max=2000"`
	RelatedCode     string    `json:"relatedCode" binding:"max=64"`
}

// BatchClaimFaultItem carries the human-facing code of a 故障事件 together with
// the version the operator last saw. A stale version means another request moved
// the record in the meantime, which blocks the whole batch.
type BatchClaimFaultItem struct {
	Code            string `json:"code" binding:"required,min=2,max=64"`
	ExpectedVersion uint   `json:"expectedVersion" binding:"required,gt=0"`
}

// BatchClaimFaultRequest is the write contract for 批量认领. A single reason is
// attached to every claim audit entry.
type BatchClaimFaultRequest struct {
	Items  []BatchClaimFaultItem `json:"items" binding:"required,min=1,max=20,dive"`
	Reason string                `json:"reason" binding:"required,min=3,max=500"`
}

// BatchClaimFaultResult is returned when the whole batch committed.
type BatchClaimFaultResult struct {
	ClaimedFaults  []BatchClaimFaultResultItem    `json:"claimedFaults"`
	CascadeUpdates []BatchClaimInverterResultItem `json:"cascadeUpdates"`
	Reason         string                         `json:"reason"`
}

type BatchClaimFaultResultItem struct {
	ID            uint   `json:"id"`
	Code          string `json:"code"`
	Facility      string `json:"facility"`
	BeforeStatus  string `json:"beforeStatus"`
	AfterStatus   string `json:"afterStatus"`
	BeforeVersion uint   `json:"beforeVersion"`
	AfterVersion  uint   `json:"afterVersion"`
}

type BatchClaimInverterResultItem struct {
	ID            uint   `json:"id"`
	Code          string `json:"code"`
	Facility      string `json:"facility"`
	BeforeStatus  string `json:"beforeStatus"`
	AfterStatus   string `json:"afterStatus"`
	BeforeVersion uint   `json:"beforeVersion"`
	AfterVersion  uint   `json:"afterVersion"`
}

// BatchClaimItemFailure explains why one submitted code rejected the batch. The
// request is atomic, so failures are returned per item without applying anything.
type BatchClaimItemFailure struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// BatchClaimRejection is the meta payload of a 422/409 batch response.
type BatchClaimRejection struct {
	Reason string                  `json:"reason"`
	Detail string                  `json:"detail"`
	Items  []BatchClaimItemFailure `json:"items"`
}
