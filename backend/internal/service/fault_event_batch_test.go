package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blueship581/solar-inverter-incident-control/backend/internal/config"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/dto"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/model"
	"github.com/blueship581/solar-inverter-incident-control/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type batchFixture struct {
	db        *gorm.DB
	faults    repository.FaultEventRepository
	inverters repository.InverterUnitRepository
	security  SecurityService
	service   FaultEventService
}

func newBatchFixture(t *testing.T) batchFixture {
	t.Helper()
	dsn := "file:" + strings.NewReplacer("/", "_", " ", "_").Replace(strings.ToLower(t.Name())) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.FaultEvent{}, &model.InverterUnit{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	faultRepo := repository.NewFaultEventRepository(db)
	inverterRepo := repository.NewInverterUnitRepository(db)
	securityRepo := repository.NewSecurityRepository(db)
	security := NewSecurityService(securityRepo, config.Config{})
	svc := NewFaultEventService(db, faultRepo, inverterRepo, security)
	return batchFixture{db: db, faults: faultRepo, inverters: inverterRepo, security: security, service: svc}
}

func (f batchFixture) createFault(t *testing.T, code, status string, version uint, facility string) model.FaultEvent {
	t.Helper()
	item := model.FaultEvent{
		BaseModel: model.BaseModel{
			Code: code, Name: "故障 " + code, Status: status, Version: version,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		Facility: facility, Owner: "运行一组", Category: "常规", RiskLevel: "medium",
		EffectiveAt: time.Now().UTC(),
	}
	if err := f.faults.Create(context.Background(), &item); err != nil {
		t.Fatalf("create fault %s: %v", code, err)
	}
	return item
}

func (f batchFixture) createInverter(t *testing.T, code, status, facility string) model.InverterUnit {
	t.Helper()
	item := model.InverterUnit{
		BaseModel: model.BaseModel{
			Code: code, Name: "逆变器 " + code, Status: status, Version: 1,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		},
		Facility: facility, Owner: "运行一组", Category: "常规", RiskLevel: "high",
		EffectiveAt: time.Now().UTC(),
	}
	if err := f.inverters.Create(context.Background(), &item); err != nil {
		t.Fatalf("create inverter %s: %v", code, err)
	}
	return item
}

func (f batchFixture) claim(codes []dto.BatchClaimItem) (dto.BatchClaimResult, error) {
	return f.service.BatchClaim(context.Background(),
		dto.BatchClaimRequest{Items: codes, Reason: "批量认领测试原因"},
		"operator", "request-test")
}

func openItem(code string, version uint) dto.BatchClaimItem {
	return dto.BatchClaimItem{Code: code, ExpectedVersion: version}
}

func TestBatchClaimSuccessAdvancesFaultsAndTrippedInverters(t *testing.T) {
	f := newBatchFixture(t)
	f1 := f.createFault(t, "FE-T1", "open", 1, "甲站")
	f2 := f.createFault(t, "FE-T2", "open", 3, "甲站")
	f3 := f.createFault(t, "FE-T3", "open", 1, "乙站")
	tripped := f.createInverter(t, "IU-T1", "tripped", "甲站")
	trippedOther := f.createInverter(t, "IU-T2", "tripped", "丙站")
	notTripped := f.createInverter(t, "IU-T3", "online", "甲站")

	result, err := f.claim([]dto.BatchClaimItem{openItem("FE-T1", 1), openItem("FE-T2", 3), openItem("FE-T3", 1)})
	if err != nil {
		t.Fatalf("batch claim: %v", err)
	}
	if len(result.ClaimedFaults) != 3 {
		t.Fatalf("expected 3 claimed faults, got %d", len(result.ClaimedFaults))
	}
	if len(result.InverterChanges) != 1 || result.InverterChanges[0].Code != "IU-T1" {
		t.Fatalf("expected only IU-T1 cascade, got %+v", result.InverterChanges)
	}

	got1, _ := f.faults.Get(context.Background(), f1.ID)
	got2, _ := f.faults.Get(context.Background(), f2.ID)
	if got1.Status != "acknowledged" || got1.Version != 2 {
		t.Fatalf("FE-T1 = %s v%d, want acknowledged v2", got1.Status, got1.Version)
	}
	if got2.Status != "acknowledged" || got2.Version != 4 {
		t.Fatalf("FE-T2 = %s v%d, want acknowledged v4", got2.Status, got2.Version)
	}
	got3, _ := f.faults.Get(context.Background(), f3.ID)
	if got3.Status != "acknowledged" || got3.Version != 2 {
		t.Fatalf("FE-T3 = %s v%d, want acknowledged v2", got3.Status, got3.Version)
	}
	gotTripped, _ := f.inverters.Get(context.Background(), tripped.ID)
	if gotTripped.Status != "warning" || gotTripped.Version != 2 {
		t.Fatalf("IU-T1 = %s v%d, want warning v2", gotTripped.Status, gotTripped.Version)
	}
	gotOther, _ := f.inverters.Get(context.Background(), trippedOther.ID)
	if gotOther.Status != "tripped" {
		t.Fatalf("IU-T2 in unrelated facility should stay tripped, got %s", gotOther.Status)
	}
	gotOnline, _ := f.inverters.Get(context.Background(), notTripped.ID)
	if gotOnline.Status != "online" {
		t.Fatalf("IU-T3 should stay online, got %s", gotOnline.Status)
	}

	logs, _, err := f.security.ListAudits(context.Background(), 1, 100, "")
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	var faultAudits, inverterAudits int
	for _, log := range logs {
		if log.RequestID != "request-test" {
			continue
		}
		switch log.EntityType {
		case "FaultEvent":
			faultAudits++
			if log.BeforeState != "open" || log.AfterState != "acknowledged" {
				t.Fatalf("fault audit states = %s->%s", log.BeforeState, log.AfterState)
			}
		case "InverterUnit":
			inverterAudits++
			if log.BeforeState != "tripped" || log.AfterState != "warning" {
				t.Fatalf("inverter audit states = %s->%s", log.BeforeState, log.AfterState)
			}
		}
	}
	if faultAudits != 3 || inverterAudits != 1 {
		t.Fatalf("audits = %d fault, %d inverter; want 3 and 1", faultAudits, inverterAudits)
	}
}

func TestBatchClaimRejectsAndChangesNothing(t *testing.T) {
	cases := []struct {
		name     string
		seed     func(f batchFixture) ([]dto.BatchClaimItem, map[string]string)
		wantCode string
	}{
		{
			name: "异常状态",
			seed: func(f batchFixture) ([]dto.BatchClaimItem, map[string]string) {
				f.createFault(t, "FE-B1", "open", 1, "甲站")
				f.createFault(t, "FE-B2", "acknowledged", 1, "甲站")
				return []dto.BatchClaimItem{openItem("FE-B1", 1), openItem("FE-B2", 1)},
					map[string]string{"FE-B1": "open", "FE-B2": "acknowledged"}
			},
			wantCode: "FE-B2",
		},
		{
			name: "编号不存在",
			seed: func(f batchFixture) ([]dto.BatchClaimItem, map[string]string) {
				f.createFault(t, "FE-B3", "open", 1, "甲站")
				return []dto.BatchClaimItem{openItem("FE-B3", 1), openItem("FE-MISSING", 1)},
					map[string]string{"FE-B3": "open"}
			},
			wantCode: "FE-MISSING",
		},
		{
			name: "旧版本",
			seed: func(f batchFixture) ([]dto.BatchClaimItem, map[string]string) {
				f.createFault(t, "FE-B4", "open", 5, "甲站")
				return []dto.BatchClaimItem{openItem("FE-B4", 4)},
					map[string]string{"FE-B4": "open"}
			},
			wantCode: "FE-B4",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBatchFixture(t)
			items, before := tc.seed(f)
			tripped := f.createInverter(t, "IU-B1", "tripped", "甲站")

			_, err := f.claim(items)
			var rejected *BatchClaimRejectedError
			if !errors.As(err, &rejected) {
				t.Fatalf("expected BatchClaimRejectedError, got %v", err)
			}
			found := false
			for _, block := range rejected.Blocks {
				if block.Code == tc.wantCode && block.Reason != "" {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected a block for %s, got %+v", tc.wantCode, rejected.Blocks)
			}

			// Nothing changed: every persisted fault keeps its seeded status/version.
			page, getErr := f.faults.List(context.Background(), dto.PageQuery{Page: 1, PageSize: 100})
			if getErr != nil {
				t.Fatalf("reload: %v", getErr)
			}
			for _, got := range page.Items {
				if want, ok := before[got.Code]; ok && got.Status != want {
					t.Fatalf("%s changed from %s to %s despite rejected batch", got.Code, want, got.Status)
				}
			}
			gotInverter, _ := f.inverters.Get(context.Background(), tripped.ID)
			if gotInverter.Status != "tripped" {
				t.Fatalf("tripped inverter changed to %s despite rejected batch", gotInverter.Status)
			}
			logs, _, _ := f.security.ListAudits(context.Background(), 1, 100, "")
			for _, log := range logs {
				if log.RequestID == "request-test" {
					t.Fatalf("no audit rows expected on rejected batch, found %+v", log)
				}
			}
		})
	}
}

func TestBatchClaimRejectsDuplicateCodes(t *testing.T) {
	f := newBatchFixture(t)
	f.createFault(t, "FE-D1", "open", 1, "甲站")
	_, err := f.claim([]dto.BatchClaimItem{openItem("FE-D1", 1), openItem("FE-D1", 1)})
	var rejected *BatchClaimRejectedError
	if !errors.As(err, &rejected) || len(rejected.Blocks) != 2 {
		t.Fatalf("expected 2 duplicate blocks, got %v", err)
	}
}

func TestBatchClaimCapsAtTwenty(t *testing.T) {
	f := newBatchFixture(t)
	items := make([]dto.BatchClaimItem, 0, 21)
	for i := 0; i < 21; i++ {
		items = append(items, openItem("FE-X", 1))
	}
	if _, err := f.claim(items); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for 21 items, got %v", err)
	}
}
