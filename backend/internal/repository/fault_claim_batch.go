package repository

import (
	"context"
	"sort"
	"time"

	"github.com/blueship581/solar-inverter-incident-control/backend/internal/model"
	"gorm.io/gorm"
)

// Audit actions emitted by the 批量认领 flow. They are kept distinct from
// "transition" so the audit trail shows which fault claims cascaded onto
// 逆变器 records of the same 场站.
const (
	AuditActionBatchClaim   = "claim"
	AuditActionCascadeState = "cascade"
)

// FaultClaimCandidate is the live row the service re-validates before commit.
type FaultClaimCandidate struct {
	ID       uint
	Code     string
	Facility string
	Status   string
	Version  uint
}

// InverterCascadeResult describes one tripped 逆变器 moved to warning inside
// the batch transaction.
type InverterCascadeResult struct {
	ID            uint
	Code          string
	Facility      string
	BeforeVersion uint
	AfterVersion  uint
}

// FaultClaimApplied is the persisted outcome for one claimed 故障事件.
type FaultClaimApplied struct {
	ID            uint
	Code          string
	Facility      string
	BeforeStatus  string
	AfterStatus   string
	BeforeVersion uint
	AfterVersion  uint
}

// BatchClaimRepository keeps the atomic 批量认领 persistence in its own file so
// the generic CRUD boundary stays untouched. Every write happens in a single
// database transaction; a single version drift rolls the whole request back.
type BatchClaimRepository interface {
	FindFaultsByCodes(context.Context, []string) ([]FaultClaimCandidate, error)
	// ApplyClaims moves every fault open -> acknowledged and, in the same
	// transaction, flips each still-tripped 逆变器 of the involved 场站 to
	// warning. Audit rows for every change are written before commit.
	ApplyClaims(context.Context, []FaultClaimCandidate, string, string, string) ([]FaultClaimApplied, []InverterCascadeResult, error)
}

type batchClaimRepository struct{ db *gorm.DB }

func NewBatchClaimRepository(db *gorm.DB) BatchClaimRepository {
	return &batchClaimRepository{db: db}
}

func (r *batchClaimRepository) FindFaultsByCodes(ctx context.Context, codes []string) ([]FaultClaimCandidate, error) {
	rows := make([]FaultClaimCandidate, 0, len(codes))
	err := r.db.WithContext(ctx).Model(&model.FaultEvent{}).
		Select("id", "code", "facility", "status", "version").
		Where("code IN ?", codes).Find(&rows).Error
	return rows, err
}

func (r *batchClaimRepository) ApplyClaims(ctx context.Context, faults []FaultClaimCandidate, actor, requestID, reason string) ([]FaultClaimApplied, []InverterCascadeResult, error) {
	applied := make([]FaultClaimApplied, 0, len(faults))
	cascaded := make([]InverterCascadeResult, 0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		facilities := make(map[string]struct{}, len(faults))
		for _, fault := range faults {
			facilities[fault.Facility] = struct{}{}
		}

		// Read the tripped 逆变器 set inside the transaction so it reflects the
		// state at commit. The conditional UPDATE below is a compare-and-set:
		// on PostgreSQL/MySQL it row-locks, and any concurrent change makes
		// RowsAffected 0, which rejects (and rolls back) the entire batch.
		inverters := make([]model.InverterUnit, 0)
		if len(facilities) > 0 {
			facilityNames := make([]string, 0, len(facilities))
			for name := range facilities {
				facilityNames = append(facilityNames, name)
			}
			if err := tx.Model(&model.InverterUnit{}).
				Where("facility IN ? AND status = ?", facilityNames, "tripped").
				Find(&inverters).Error; err != nil {
				return err
			}
		}

		for _, fault := range faults {
			nextVersion := fault.Version + 1
			result := tx.Model(&model.FaultEvent{}).
				Where("id = ? AND version = ? AND status = ?", fault.ID, fault.Version, fault.Status).
				Updates(map[string]any{"status": "acknowledged", "version": nextVersion, "updated_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrVersionConflict
			}

			if err := tx.Create(&model.AuditLog{
				RequestID: requestID, Actor: actor, Action: AuditActionBatchClaim,
				EntityType: "FaultEvent", EntityID: fault.ID,
				BeforeState: fault.Status, AfterState: "acknowledged", Detail: reason, CreatedAt: now,
			}).Error; err != nil {
				return err
			}

			applied = append(applied, FaultClaimApplied{
				ID: fault.ID, Code: fault.Code, Facility: fault.Facility,
				BeforeStatus: fault.Status, AfterStatus: "acknowledged",
				BeforeVersion: fault.Version, AfterVersion: nextVersion,
			})
		}

		for _, inverter := range inverters {
			nextVersion := inverter.Version + 1
			result := tx.Model(&model.InverterUnit{}).
				Where("id = ? AND version = ? AND status = ?", inverter.ID, inverter.Version, "tripped").
				Updates(map[string]any{"status": "warning", "version": nextVersion, "updated_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrVersionConflict
			}

			if err := tx.Create(&model.AuditLog{
				RequestID: requestID, Actor: actor, Action: AuditActionCascadeState,
				EntityType: "InverterUnit", EntityID: inverter.ID,
				BeforeState: "tripped", AfterState: "warning",
				Detail: "批量认领同一 场站 故障后联动: " + reason, CreatedAt: now,
			}).Error; err != nil {
				return err
			}

			cascaded = append(cascaded, InverterCascadeResult{
				ID: inverter.ID, Code: inverter.Code, Facility: inverter.Facility,
				BeforeVersion: inverter.Version, AfterVersion: nextVersion,
			})
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(cascaded, func(i, j int) bool { return cascaded[i].ID < cascaded[j].ID })
	return applied, cascaded, nil
}
