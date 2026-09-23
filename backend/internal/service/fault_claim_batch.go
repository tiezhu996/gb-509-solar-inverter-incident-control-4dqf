package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blueship581/solar-inverter-incident-control/backend/internal/constants"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/dto"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/repository"
)

// BatchClaimLimit caps how many 故障事件 one request can claim.
const BatchClaimLimit = 20

// Item-level rejection reasons. They are stable contract values rendered by the
// 故障事件 list page as 阻断明细.
const (
	claimFailureDuplicate = "duplicate_code"
	claimFailureNotFound  = "code_not_found"
	claimFailureNotOpen   = "not_pending_claim"
	claimFailureStale     = "version_conflict"
)

type batchClaimRejectedError struct {
	rejection dto.BatchClaimRejection
}

func (e *batchClaimRejectedError) Error() string {
	return fmt.Sprintf("%s: %s", ErrBatchRejected.Error(), e.rejection.Detail)
}
func (e *batchClaimRejectedError) Unwrap() error { return ErrBatchRejected }

// AsBatchClaimRejection extracts the per-item rejection payload of a failed
// batch claim so handlers can return it in the response meta.
func AsBatchClaimRejection(err error) (dto.BatchClaimRejection, bool) {
	var rejected *batchClaimRejectedError
	if errors.As(err, &rejected) {
		return rejected.rejection, true
	}
	return dto.BatchClaimRejection{}, false
}

// BatchClaim atomically claims up to BatchClaimLimit open 故障事件. When any
// submitted code is missing, no longer 待认领 (open), duplicated or carries an
// outdated version, the entire request is rejected with per-item reasons and
// no row changes. On success the same transaction also moves every tripped
// 逆变器 in the involved 场站 to warning; each change gets its own audit row.
func (s *faultEventService) BatchClaim(ctx context.Context, input dto.BatchClaimFaultRequest, actor, requestID string) (dto.BatchClaimFaultResult, error) {
	reason := strings.TrimSpace(input.Reason)
	if reason == "" || len([]rune(reason)) < 3 || len([]rune(reason)) > 500 {
		return dto.BatchClaimFaultResult{}, fmt.Errorf("%w: reason must be 3-500 characters", ErrInvalidInput)
	}
	if len(input.Items) == 0 {
		return dto.BatchClaimFaultResult{}, fmt.Errorf("%w: at least one fault is required", ErrInvalidInput)
	}
	if len(input.Items) > BatchClaimLimit {
		return dto.BatchClaimFaultResult{}, fmt.Errorf("%w: at most %d faults per batch", ErrInvalidInput, BatchClaimLimit)
	}

	codes := make([]string, 0, len(input.Items))
	normalized := make([]dto.BatchClaimFaultItem, 0, len(input.Items))
	seen := make(map[string]int, len(input.Items))
	failures := make([]dto.BatchClaimItemFailure, 0)
	for _, item := range input.Items {
		code := strings.ToUpper(strings.TrimSpace(item.Code))
		if code == "" {
			failures = append(failures, dto.BatchClaimItemFailure{Code: item.Code, Reason: claimFailureNotFound, Detail: "故障编号不能为空"})
			continue
		}
		normalized = append(normalized, dto.BatchClaimFaultItem{Code: code, ExpectedVersion: item.ExpectedVersion})
		codes = append(codes, code)
		seen[code]++
	}

	// Duplicate codes inside one request are ambiguous for an atomic claim.
	for _, item := range normalized {
		if seen[item.Code] > 1 {
			failures = append(failures, dto.BatchClaimItemFailure{
				Code: item.Code, Reason: claimFailureDuplicate,
				Detail: fmt.Sprintf("编号在本次请求中重复出现 %d 次", seen[item.Code]),
			})
		}
	}

	if len(failures) > 0 {
		return dto.BatchClaimFaultResult{}, &batchClaimRejectedError{rejection: dto.BatchClaimRejection{
			Reason: "invalid_batch", Detail: "存在重复编号，整批已拒绝", Items: failures,
		}}
	}

	rows, err := s.batch.FindFaultsByCodes(ctx, codes)
	if err != nil {
		return dto.BatchClaimFaultResult{}, fmt.Errorf("load 故障事件 for batch claim: %w", err)
	}
	byCode := make(map[string]repository.FaultClaimCandidate, len(rows))
	for _, row := range rows {
		byCode[row.Code] = row
	}

	candidates := make([]repository.FaultClaimCandidate, 0, len(normalized))
	for _, item := range normalized {
		row, exists := byCode[item.Code]
		switch {
		case !exists:
			failures = append(failures, dto.BatchClaimItemFailure{Code: item.Code, Reason: claimFailureNotFound, Detail: "编号不存在或已被删除"})
		case row.Status != string(constants.FaultStateOpen):
			failures = append(failures, dto.BatchClaimItemFailure{
				Code: item.Code, Reason: claimFailureNotOpen,
				Detail: fmt.Sprintf("当前状态为 %s，仅待认领(open)故障可被认领", row.Status),
			})
		case row.Version != item.ExpectedVersion:
			failures = append(failures, dto.BatchClaimItemFailure{
				Code: item.Code, Reason: claimFailureStale,
				Detail: fmt.Sprintf("页面版本 v%d 已过期，当前版本 v%d，请刷新后重试", item.ExpectedVersion, row.Version),
			})
		default:
			candidates = append(candidates, row)
		}
	}

	if len(failures) > 0 {
		return dto.BatchClaimFaultResult{}, &batchClaimRejectedError{rejection: dto.BatchClaimRejection{
			Reason: "blocked", Detail: fmt.Sprintf("%d 条编号未通过认领前校验，整批未执行任何更新", len(failures)), Items: failures,
		}}
	}

	applied, cascaded, err := s.batch.ApplyClaims(ctx, candidates, actor, requestID, reason)
	if err != nil {
		// A conflict between validation and update means another request won the
		// race; the transaction rolled back, so surface the 409 for the client.
		if errors.Is(err, repository.ErrVersionConflict) {
			return dto.BatchClaimFaultResult{}, err
		}
		return dto.BatchClaimFaultResult{}, fmt.Errorf("apply batch claim transaction: %w", err)
	}

	claimed := make([]dto.BatchClaimFaultResultItem, 0, len(applied))
	for _, item := range applied {
		claimed = append(claimed, dto.BatchClaimFaultResultItem{
			ID: item.ID, Code: item.Code, Facility: item.Facility,
			BeforeStatus: item.BeforeStatus, AfterStatus: item.AfterStatus,
			BeforeVersion: item.BeforeVersion, AfterVersion: item.AfterVersion,
		})
	}
	inverterResults := make([]dto.BatchClaimInverterResultItem, 0, len(cascaded))
	for _, item := range cascaded {
		inverterResults = append(inverterResults, dto.BatchClaimInverterResultItem{
			ID: item.ID, Code: item.Code, Facility: item.Facility,
			BeforeStatus: "tripped", AfterStatus: "warning",
			BeforeVersion: item.BeforeVersion, AfterVersion: item.AfterVersion,
		})
	}
	return dto.BatchClaimFaultResult{ClaimedFaults: claimed, CascadeUpdates: inverterResults, Reason: reason}, nil
}
