package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blueship581/solar-inverter-incident-control/backend/internal/config"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/dto"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/model"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newBatchClaimTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=foreign_keys(1)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AuditLog{}, &model.FaultEvent{}, &model.InverterUnit{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM audit_logs").Error
		_ = db.Exec("DELETE FROM fault_events").Error
		_ = db.Exec("DELETE FROM inverter_units").Error
	})
	return db
}

func seedFault(t *testing.T, db *gorm.DB, code, status, facility string, version uint) {
	t.Helper()
	item := model.FaultEvent{
		BaseModel: model.BaseModel{Code: code, Name: "故障 " + code, Status: status, Version: version},
		Facility:  facility, Owner: "运行一组", Category: "常规", RiskLevel: "high",
		EffectiveAt: time.Now().UTC(),
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed fault %s: %v", code, err)
	}
}

func seedInverter(t *testing.T, db *gorm.DB, code, status, facility string, version uint) {
	t.Helper()
	item := model.InverterUnit{
		BaseModel: model.BaseModel{Code: code, Name: "逆变器 " + code, Status: status, Version: version},
		Facility:  facility, Owner: "运行一组", Category: "常规", RiskLevel: "high",
		EffectiveAt: time.Now().UTC(),
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed inverter %s: %v", code, err)
	}
}

func newBatchClaimService(db *gorm.DB) FaultEventService {
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	return NewFaultEventService(repository.NewFaultEventRepository(db), repository.NewBatchClaimRepository(db), security)
}

func batchRequest(codes []string, reason string) dto.BatchClaimFaultRequest {
	items := make([]dto.BatchClaimFaultItem, 0, len(codes))
	for _, code := range codes {
		items = append(items, dto.BatchClaimFaultItem{Code: code, ExpectedVersion: 1})
	}
	return dto.BatchClaimFaultRequest{Items: items, Reason: reason}
}

func TestBatchClaimClaimsFaultsAndCascadesTrippedInverters(t *testing.T) {
	db := newBatchClaimTestDB(t)
	seedFault(t, db, "FE-B01", "open", "光伏场站测试区A", 1)
	seedFault(t, db, "FE-B02", "open", "光伏场站测试区A", 1)
	seedFault(t, db, "FE-B03", "open", "光伏场站测试区B", 1)
	seedInverter(t, db, "IU-B01", "tripped", "光伏场站测试区A", 1)
	seedInverter(t, db, "IU-B02", "tripped", "光伏场站测试区B", 1)
	seedInverter(t, db, "IU-B03", "warning", "光伏场站测试区A", 1)
	seedInverter(t, db, "IU-B04", "tripped", "无关场站", 1)

	svc := newBatchClaimService(db)
	result, err := svc.BatchClaim(context.Background(), batchRequest([]string{"FE-B01", "fe-b02", "FE-B03"}, "批量认领测试原因"), "operator", "req-1")
	if err != nil {
		t.Fatalf("expected batch claim success, got %v", err)
	}
	if len(result.ClaimedFaults) != 3 {
		t.Fatalf("expected 3 claimed faults, got %d", len(result.ClaimedFaults))
	}
	if len(result.CascadeUpdates) != 2 {
		t.Fatalf("expected 2 cascaded inverters (one per involved site), got %d", len(result.CascadeUpdates))
	}
	codes := map[string]bool{}
	for _, inverter := range result.CascadeUpdates {
		codes[inverter.Code] = true
		if inverter.BeforeStatus != "tripped" || inverter.AfterStatus != "warning" {
			t.Fatalf("unexpected cascade states: %s %s -> %s", inverter.Code, inverter.BeforeStatus, inverter.AfterStatus)
		}
	}
	if !codes["IU-B01"] || !codes["IU-B02"] {
		t.Fatalf("expected IU-B01 and IU-B02 cascaded, got %v", codes)
	}

	var acknowledged int64
	db.Model(&model.FaultEvent{}).Where("status = ?", "acknowledged").Count(&acknowledged)
	if acknowledged != 3 {
		t.Fatalf("expected 3 acknowledged faults in db, got %d", acknowledged)
	}
	var open int64
	db.Model(&model.FaultEvent{}).Where("status = ? AND code IN ?", "open", []string{"FE-B01", "FE-B02", "FE-B03"}).Count(&open)
	if open != 0 {
		t.Fatalf("expected no open claimed faults left, got %d", open)
	}
	var siteAInverter model.InverterUnit
	db.Where("code = ?", "IU-B01").First(&siteAInverter)
	if siteAInverter.Status != "warning" || siteAInverter.Version != 2 {
		t.Fatalf("expected IU-B01 warning v2, got %s v%d", siteAInverter.Status, siteAInverter.Version)
	}
	var unaffected model.InverterUnit
	db.Where("code = ?", "IU-B04").First(&unaffected)
	if unaffected.Status != "tripped" {
		t.Fatalf("expected IU-B04 of unrelated site to stay tripped, got %s", unaffected.Status)
	}

	var claimAudits, cascadeAudits int64
	db.Model(&model.AuditLog{}).Where("action = ?", repository.AuditActionBatchClaim).Count(&claimAudits)
	db.Model(&model.AuditLog{}).Where("action = ?", repository.AuditActionCascadeState).Count(&cascadeAudits)
	if claimAudits != 3 {
		t.Fatalf("expected 3 claim audit rows, got %d", claimAudits)
	}
	if cascadeAudits != 2 {
		t.Fatalf("expected 2 cascade audit rows, got %d", cascadeAudits)
	}
	var sharedRequestID int64
	db.Model(&model.AuditLog{}).Where("request_id = ?", "req-1").Count(&sharedRequestID)
	if sharedRequestID != 5 {
		t.Fatalf("expected all 5 audit rows to share the request id, got %d", sharedRequestID)
	}
}

func TestBatchClaimRejectsEntireBatchWithPerItemReasons(t *testing.T) {
	db := newBatchClaimTestDB(t)
	seedFault(t, db, "FE-R01", "open", "光伏场站拒绝区", 1)
	seedFault(t, db, "FE-R02", "acknowledged", "光伏场站拒绝区", 1)
	seedFault(t, db, "FE-R03", "open", "光伏场站拒绝区", 7)
	seedInverter(t, db, "IU-R01", "tripped", "光伏场站拒绝区", 1)

	svc := newBatchClaimService(db)
	_, err := svc.BatchClaim(context.Background(), batchRequest([]string{"FE-R01", "FE-R02", "FE-R03", "FE-MISSING"}, "整批拒绝原因测试"), "operator", "req-2")
	if !errors.Is(err, ErrBatchRejected) {
		t.Fatalf("expected ErrBatchRejected, got %v", err)
	}
	rejection, ok := AsBatchClaimRejection(err)
	if !ok {
		t.Fatalf("expected structured rejection payload")
	}
	if len(rejection.Items) != 3 {
		t.Fatalf("expected 3 item failures, got %d: %+v", len(rejection.Items), rejection.Items)
	}
	reasons := map[string]string{}
	for _, failure := range rejection.Items {
		reasons[failure.Code] = failure.Reason
	}
	if reasons["FE-MISSING"] != claimFailureNotFound {
		t.Fatalf("expected not_found for missing code, got %q", reasons["FE-MISSING"])
	}
	if reasons["FE-R02"] != claimFailureNotOpen {
		t.Fatalf("expected not_pending_claim for acknowledged code, got %q", reasons["FE-R02"])
	}
	if reasons["FE-R03"] != claimFailureStale {
		t.Fatalf("expected version_conflict for stale code, got %q", reasons["FE-R03"])
	}

	// Atomicity: the valid fault in the same request must remain untouched.
	var valid model.FaultEvent
	db.Where("code = ?", "FE-R01").First(&valid)
	if valid.Status != "open" || valid.Version != 1 {
		t.Fatalf("expected FE-R01 to remain open v1 after rejected batch, got %s v%d", valid.Status, valid.Version)
	}
	var inverter model.InverterUnit
	db.Where("code = ?", "IU-R01").First(&inverter)
	if inverter.Status != "tripped" {
		t.Fatalf("expected no cascade after rejected batch, inverter is %s", inverter.Status)
	}
	var audits int64
	db.Model(&model.AuditLog{}).Count(&audits)
	if audits != 0 {
		t.Fatalf("expected zero audit rows after rejected batch, got %d", audits)
	}
}

func TestBatchClaimRejectsDuplicateCodes(t *testing.T) {
	db := newBatchClaimTestDB(t)
	seedFault(t, db, "FE-D01", "open", "光伏场站重复区", 1)

	svc := newBatchClaimService(db)
	_, err := svc.BatchClaim(context.Background(), batchRequest([]string{"FE-D01", "FE-D01"}, "重复编号拒绝"), "operator", "req-3")
	if !errors.Is(err, ErrBatchRejected) {
		t.Fatalf("expected ErrBatchRejected for duplicates, got %v", err)
	}
	rejection, _ := AsBatchClaimRejection(err)
	if len(rejection.Items) != 2 || rejection.Items[0].Reason != claimFailureDuplicate {
		t.Fatalf("expected duplicate reason per occurrence, got %+v", rejection.Items)
	}

	var fault model.FaultEvent
	db.Where("code = ?", "FE-D01").First(&fault)
	if fault.Status != "open" {
		t.Fatalf("expected FE-D01 to remain open, got %s", fault.Status)
	}
}

func TestBatchClaimEnforcesSizeLimitAndReason(t *testing.T) {
	db := newBatchClaimTestDB(t)
	svc := newBatchClaimService(db)

	codes := make([]string, BatchClaimLimit+1)
	for i := range codes {
		codes[i] = "FE-X"
	}
	_, err := svc.BatchClaim(context.Background(), batchRequest(codes, "超出批量上限"), "operator", "req-4")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for %d items, got %v", len(codes), err)
	}

	_, err = svc.BatchClaim(context.Background(), batchRequest([]string{"FE-X"}, "短"), "operator", "req-5")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for short reason, got %v", err)
	}
}

func TestBatchClaimTransactionRollsBackOnConcurrentVersionDrift(t *testing.T) {
	db := newBatchClaimTestDB(t)
	seedFault(t, db, "FE-C01", "open", "光伏场站并发区", 1)
	seedFault(t, db, "FE-C02", "open", "光伏场站并发区", 1)

	// Simulate another request moving FE-C02 between validation and commit by
	// changing the stored row while the validated candidate keeps v1: the
	// repository's compare-and-set must reject and roll back the whole batch.
	repo := repository.NewBatchClaimRepository(db)
	rows, err := repo.FindFaultsByCodes(context.Background(), []string{"FE-C01", "FE-C02"})
	if err != nil {
		t.Fatalf("load candidates: %v", err)
	}
	if err := db.Model(&model.FaultEvent{}).Where("code = ?", "FE-C02").
		Updates(map[string]any{"status": "acknowledged", "version": 2}).Error; err != nil {
		t.Fatalf("simulate concurrent change: %v", err)
	}
	_, _, err = repo.ApplyClaims(context.Background(), rows, "operator", "req-6", "并发漂移回滚测试")
	if !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict on drift, got %v", err)
	}
	var first model.FaultEvent
	db.Where("code = ?", "FE-C01").First(&first)
	if first.Status != "open" || first.Version != 1 {
		t.Fatalf("expected FE-C01 rolled back to open v1, got %s v%d", first.Status, first.Version)
	}
	var audits int64
	db.Model(&model.AuditLog{}).Where("request_id = ?", "req-6").Count(&audits)
	if audits != 0 {
		t.Fatalf("expected rolled-back transaction to leave no audit rows, got %d", audits)
	}
}
