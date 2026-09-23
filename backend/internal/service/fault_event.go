package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/solar-inverter-incident-control/backend/internal/constants"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/dto"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/model"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/repository"
	"gorm.io/gorm"
)

type FaultEventService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.FaultEvent], error)
	Get(context.Context, uint) (model.FaultEvent, error)
	Create(context.Context, dto.CreateFaultEvent, string, string) (model.FaultEvent, error)
	Update(context.Context, uint, dto.UpdateFaultEvent, string, string) (model.FaultEvent, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string) (model.FaultEvent, error)
	BatchClaim(context.Context, dto.BatchClaimRequest, string, string) (dto.BatchClaimResult, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type faultEventService struct {
	db         *gorm.DB
	repository repository.FaultEventRepository
	inverters  repository.InverterUnitRepository
	security   SecurityService
}

func NewFaultEventService(db *gorm.DB, repo repository.FaultEventRepository, inverters repository.InverterUnitRepository, security SecurityService) FaultEventService {
	return &faultEventService{db: db, repository: repo, inverters: inverters, security: security}
}

func (s *faultEventService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.FaultEvent], error) {
	return s.repository.List(ctx, query)
}

func (s *faultEventService) Get(ctx context.Context, id uint) (model.FaultEvent, error) {
	return s.repository.Get(ctx, id)
}

func (s *faultEventService) Create(ctx context.Context, input dto.CreateFaultEvent, actor, requestID string) (model.FaultEvent, error) {
	if err := validateFaultEventBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.FaultEvent{}, err
	}
	item := model.FaultEvent{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.FaultEventInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
	}
	if err := s.repository.Create(ctx, &item); err != nil {
		return model.FaultEvent{}, fmt.Errorf("create 故障事件: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "FaultEvent", item.ID, "", item.Status, "created 故障事件")
	return item, nil
}

func (s *faultEventService) Update(ctx context.Context, id uint, input dto.UpdateFaultEvent, actor, requestID string) (model.FaultEvent, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.FaultEvent{}, err
	}
	if err := validateFaultEventBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.FaultEvent{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
		return model.FaultEvent{}, fmt.Errorf("update 故障事件: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "FaultEvent", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *faultEventService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, requestID string) (model.FaultEvent, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.FaultEvent{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.FaultEventTransitions, current.Status, target) {
		return model.FaultEvent{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
		return model.FaultEvent{}, fmt.Errorf("transition 故障事件: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "FaultEvent", id, before, target, input.Reason); err != nil {
		return model.FaultEvent{}, fmt.Errorf("persist transition audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *faultEventService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "FaultEvent", id, current.Status, "deleted", "soft deleted 故障事件")
}

// batchBlockTag carries the reason for a single submitted entry when the
// all-or-nothing batch cannot proceed.
type batchBlockTag struct {
	code   string
	reason string
}

// BatchClaim acknowledges up to BatchClaimMaxItems open faults in a single
// transaction. Every submitted entry is validated first; any missing code,
// non-open status or stale version rejects the entire request with a per-code
// reason and zero writes. On success the tripped inverters of every affected
// facility are cascaded to warning, with one audit row per change.
func (s *faultEventService) BatchClaim(ctx context.Context, input dto.BatchClaimRequest, actor, requestID string) (dto.BatchClaimResult, error) {
	reason := strings.TrimSpace(input.Reason)
	if len(input.Items) == 0 || len(input.Items) > dto.BatchClaimMaxItems || reason == "" {
		return dto.BatchClaimResult{}, ErrInvalidInput
	}

	// Normalize codes once; duplicates are themselves blocking because the
	// operator must submit a unique set of identifiers.
	normalized := make([]dto.BatchClaimItem, 0, len(input.Items))
	seen := make(map[string]bool, len(input.Items))
	duplicates := make(map[string]bool)
	for _, item := range input.Items {
		code := strings.ToUpper(strings.TrimSpace(item.Code))
		normalized = append(normalized, dto.BatchClaimItem{Code: code, ExpectedVersion: item.ExpectedVersion})
		if seen[code] {
			duplicates[code] = true
		}
		seen[code] = true
	}

	// Pre-transaction validation gives the per-entry blocking details without
	// opening a transaction on requests that cannot succeed.
	tags, err := s.collectBatchBlocks(ctx, normalized, duplicates)
	if err != nil {
		return dto.BatchClaimResult{}, err
	}
	if len(tags) > 0 {
		return dto.BatchClaimResult{}, NewBatchClaimRejectedError(toBlocks(tags))
	}

	var result dto.BatchClaimResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := repository.WithTx(ctx, tx)

		faults, err := s.repository.ListByCodes(txCtx, codesOf(normalized))
		if err != nil {
			return fmt.Errorf("load batch faults: %w", err)
		}
		byCode := make(map[string]model.FaultEvent, len(faults))
		for _, fault := range faults {
			byCode[fault.Code] = fault
		}

		// Re-validate inside the transaction so a status or version change that
		// landed between the pre-check and the tx rejects the whole batch.
		tags := make([]batchBlockTag, 0)
		claimed := make([]model.FaultEvent, 0, len(normalized))
		for _, item := range normalized {
			fault, ok := byCode[item.Code]
			switch {
			case !ok:
				tags = append(tags, batchBlockTag{item.Code, "编号不存在"})
			case fault.Status != string(constants.FaultStateOpen):
				tags = append(tags, batchBlockTag{item.Code, fmt.Sprintf("异常状态：当前为 %s，仅待认领（open）可认领", fault.Status)})
			case fault.Version != item.ExpectedVersion:
				tags = append(tags, batchBlockTag{item.Code, fmt.Sprintf("旧版本：页面版本 %d，当前版本 %d，请刷新后重试", item.ExpectedVersion, fault.Version)})
			default:
				claimed = append(claimed, fault)
			}
		}
		if len(tags) > 0 {
			return NewBatchClaimRejectedError(toBlocks(tags))
		}

		facilities := make([]string, 0)
		facilitySeen := make(map[string]bool)
		result.ClaimedFaults = make([]dto.BatchClaimFault, 0, len(claimed))
		for _, fault := range claimed {
			if err := s.repository.AdvanceStatus(txCtx, fault.ID, fault.Version, string(constants.FaultStateOpen), string(constants.FaultStateAcknowledged)); err != nil {
				// Lost the race between the in-tx read and the update: the
				// conditional write failed, so reject the batch atomically.
				if errors.Is(err, repository.ErrVersionConflict) {
					return NewBatchClaimRejectedError([]dto.BatchClaimBlock{{Code: fault.Code, Reason: "旧版本：记录已被其他请求修改，请刷新后重试"}})
				}
				return fmt.Errorf("acknowledge fault %s: %w", fault.Code, err)
			}
			detail := fmt.Sprintf("批量认领 %d 项故障；原因：%s", len(claimed), truncateAuditDetail(reason))
			if err := s.security.Audit(txCtx, actor, requestID, "transition", "FaultEvent", fault.ID, string(constants.FaultStateOpen), string(constants.FaultStateAcknowledged), detail); err != nil {
				return fmt.Errorf("audit fault %s: %w", fault.Code, err)
			}
			result.ClaimedFaults = append(result.ClaimedFaults, dto.BatchClaimFault{ID: fault.ID, Code: fault.Code, Version: fault.Version + 1})
			if fault.Facility != "" && !facilitySeen[fault.Facility] {
				facilitySeen[fault.Facility] = true
				facilities = append(facilities, fault.Facility)
			}
		}
		result.FaultAuditCount = len(claimed)

		// Cascade: tripped inverters of the same facilities move to warning.
		tripped, err := s.inverters.ListByFacilityAndStatus(txCtx, facilities, string(constants.InverterStateTripped))
		if err != nil {
			return fmt.Errorf("load tripped inverters: %w", err)
		}
		result.InverterChanges = make([]dto.BatchClaimInverterChange, 0)
		for _, inverter := range tripped {
			if err := s.inverters.AdvanceStatusByID(txCtx, inverter.ID, string(constants.InverterStateTripped), string(constants.InverterStateWarning)); err != nil {
				if errors.Is(err, repository.ErrVersionConflict) {
					// Another concurrent request already moved it; nothing left
					// to cascade for this unit.
					continue
				}
				return fmt.Errorf("cascade inverter %s: %w", inverter.Code, err)
			}
			detail := fmt.Sprintf("故障批量认联锁伴联动：同一场站 %s 的跳闸逆变器转为告警；关联故障 %d 项", inverter.Facility, len(claimed))
			if err := s.security.Audit(txCtx, actor, requestID, "transition", "InverterUnit", inverter.ID, string(constants.InverterStateTripped), string(constants.InverterStateWarning), detail); err != nil {
				return fmt.Errorf("audit inverter %s: %w", inverter.Code, err)
			}
			result.InverterChanges = append(result.InverterChanges, dto.BatchClaimInverterChange{
				ID: inverter.ID, Code: inverter.Code, Facility: inverter.Facility,
				FromStatus: string(constants.InverterStateTripped), ToStatus: string(constants.InverterStateWarning),
				Version: inverter.Version + 1,
			})
		}
		result.InverterAuditCount = len(result.InverterChanges)
		result.AffectedFacilities = facilities
		return nil
	})
	if err != nil {
		var rejected *BatchClaimRejectedError
		if errors.As(err, &rejected) {
			return dto.BatchClaimResult{}, rejected
		}
		return dto.BatchClaimResult{}, err
	}
	return result, nil
}

// collectBatchBlocks runs the entry-wise pre-checks: duplicates, missing
// records, abnormal status and stale versions.
func (s *faultEventService) collectBatchBlocks(ctx context.Context, items []dto.BatchClaimItem, duplicates map[string]bool) ([]batchBlockTag, error) {
	codes := make([]string, 0, len(items))
	for _, item := range items {
		codes = append(codes, item.Code)
	}
	faults, err := s.repository.ListByCodes(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("validate batch claim: %w", err)
	}
	byCode := make(map[string]model.FaultEvent, len(faults))
	for _, fault := range faults {
		byCode[fault.Code] = fault
	}
	tags := make([]batchBlockTag, 0)
	for _, item := range items {
		switch {
		case duplicates[item.Code]:
			tags = append(tags, batchBlockTag{item.Code, "编号在提交列表中重复"})
		case item.ExpectedVersion == 0:
			tags = append(tags, batchBlockTag{item.Code, "缺少版本号"})
		default:
			fault, ok := byCode[item.Code]
			switch {
			case !ok:
				tags = append(tags, batchBlockTag{item.Code, "编号不存在"})
			case fault.Status != string(constants.FaultStateOpen):
				tags = append(tags, batchBlockTag{item.Code, fmt.Sprintf("异常状态：当前为 %s，仅待认领（open）可认领", fault.Status)})
			case fault.Version != item.ExpectedVersion:
				tags = append(tags, batchBlockTag{item.Code, fmt.Sprintf("旧版本：页面版本 %d，当前版本 %d，请刷新后重试", item.ExpectedVersion, fault.Version)})
			}
		}
	}
	return tags, nil
}

func codesOf(items []dto.BatchClaimItem) []string {
	codes := make([]string, 0, len(items))
	for _, item := range items {
		codes = append(codes, item.Code)
	}
	return codes
}

func toBlocks(tags []batchBlockTag) []dto.BatchClaimBlock {
	blocks := make([]dto.BatchClaimBlock, 0, len(tags))
	for _, tag := range tags {
		blocks = append(blocks, dto.BatchClaimBlock{Code: tag.code, Reason: tag.reason})
	}
	return blocks
}

func truncateAuditDetail(reason string) string {
	const limit = 400
	if len(reason) <= limit {
		return reason
	}
	return reason[:limit]
}

func (s *faultEventService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func validateFaultEventBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}
