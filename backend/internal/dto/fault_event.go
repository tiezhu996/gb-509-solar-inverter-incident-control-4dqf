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

// BatchClaimMaxItems caps how many 故障事件 a single batch acknowledgement may cover.
const BatchClaimMaxItems = 20

// BatchClaimItem carries the operator-side version snapshot for one fault.
type BatchClaimItem struct {
	Code            string `json:"code" binding:"required,min=2,max=64"`
	ExpectedVersion uint   `json:"expectedVersion" binding:"required,gt=0"`
}

// BatchClaimRequest is the write contract for 批量认领. One reason applies to
// every selected fault and the request size is capped at BatchClaimMaxItems.
type BatchClaimRequest struct {
	Items  []BatchClaimItem `json:"items" binding:"required,min=1,max=20,dive"`
	Reason string           `json:"reason" binding:"required,min=3,max=500"`
}

// BatchClaimBlock describes why a single submitted entry prevents the whole
// batch from being accepted.
type BatchClaimBlock struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// BatchClaimFault is one acknowledged fault in a successful batch result.
type BatchClaimFault struct {
	ID      uint   `json:"id"`
	Code    string `json:"code"`
	Version uint   `json:"version"`
}

// BatchClaimInverterChange records a tripped inverter of an affected facility
// that was moved to warning as part of the same transaction.
type BatchClaimInverterChange struct {
	ID         uint   `json:"id"`
	Code       string `json:"code"`
	Facility   string `json:"facility"`
	FromStatus string `json:"fromStatus"`
	ToStatus   string `json:"toStatus"`
	Version    uint   `json:"version"`
}

// BatchClaimResult is returned when every submitted fault passes validation.
type BatchClaimResult struct {
	ClaimedFaults      []BatchClaimFault          `json:"claimedFaults"`
	InverterChanges    []BatchClaimInverterChange `json:"inverterChanges"`
	AffectedFacilities []string                   `json:"affectedFacilities"`
	FaultAuditCount    int                        `json:"faultAuditCount"`
	InverterAuditCount int                        `json:"inverterAuditCount"`
}
