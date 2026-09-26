package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/config"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestAnimalCaseCloseRequiresReviewerAndSettledSpecimens(t *testing.T) {
	db := newAnimalCaseTestDB(t)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	svc := NewAnimalCaseService(repository.NewAnimalCaseRepository(db), repository.NewSpecimenRepository(db), security)
	ctx := context.Background()

	animalCase := seedAnimalCaseForClose(t, db, "AC-CLOSE-1", "testing", "REL-CLOSE-1")
	seedSpecimenForCase(t, db, "S-OPEN-1", "testing", "REL-CLOSE-1")
	seedSpecimenForCase(t, db, "S-SETTLED-1", "released", "REL-CLOSE-1")
	seedSpecimenForCase(t, db, "S-SETTLED-2", "disposed", "REL-CLOSE-1")

	closeRequest := dto.TransitionRequest{Status: "closed", ExpectedVersion: animalCase.Version, Reason: "全部检测完成，申请结单"}

	if _, err := svc.Transition(ctx, animalCase.ID, closeRequest, "operator", model.RoleOperator, "close-operator-denied"); !errors.Is(err, ErrCaseCloseRole) {
		t.Fatalf("operator closing must require reviewer or admin, got %v", err)
	}

	_, err := svc.Transition(ctx, animalCase.ID, closeRequest, "reviewer", model.RoleReviewer, "close-blocked-open")
	if !errors.Is(err, ErrCaseOpenSpecimens) {
		t.Fatalf("closing with unsettled specimens must be blocked, got %v", err)
	}
	if !strings.Contains(err.Error(), "还剩 1 条") {
		t.Fatalf("block message must report the remaining specimen count, got %q", err.Error())
	}

	// 被拦截的尝试既不能改状态也不能写审计，版本号保持可重试。
	fresh, err := svc.Get(ctx, animalCase.ID)
	if err != nil {
		t.Fatalf("reload case: %v", err)
	}
	if fresh.Status != "testing" || fresh.Version != animalCase.Version {
		t.Fatalf("blocked close must not mutate the case, got %#v", fresh)
	}

	if err := db.Model(&model.Specimen{}).Where("code = ?", "S-OPEN-1").Update("status", "released").Error; err != nil {
		t.Fatalf("settle specimen: %v", err)
	}
	closed, err := svc.Transition(ctx, animalCase.ID, closeRequest, "reviewer", model.RoleReviewer, "close-allowed")
	if err != nil {
		t.Fatalf("close with all specimens settled: %v", err)
	}
	if closed.Status != "closed" || closed.Version != animalCase.Version+1 {
		t.Fatalf("unexpected closed case: %#v", closed)
	}

	var audits int64
	if err := db.Model(&model.AuditLog{}).Where("entity_type = ? AND entity_id = ? AND action = ?", "AnimalCase", animalCase.ID, "transition").Count(&audits).Error; err != nil {
		t.Fatalf("count audits: %v", err)
	}
	if audits != 1 {
		t.Fatalf("only the successful close may write an audit, got %d", audits)
	}
}

func TestAnimalCaseCloseAllowsAdminAndKeepsOperatorOnOtherTransitions(t *testing.T) {
	db := newAnimalCaseTestDB(t)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	svc := NewAnimalCaseService(repository.NewAnimalCaseRepository(db), repository.NewSpecimenRepository(db), security)
	ctx := context.Background()

	animalCase := seedAnimalCaseForClose(t, db, "AC-CLOSE-2", "testing", "")

	// operator 仍可推进非结单状态。
	advanced, err := svc.Transition(ctx, animalCase.ID, dto.TransitionRequest{
		Status: "sampling", ExpectedVersion: animalCase.Version, Reason: "补充采样",
	}, "operator", model.RoleOperator, "case-back-to-sampling")
	if err != nil {
		t.Fatalf("operator non-close transition: %v", err)
	}
	if advanced.Status != "sampling" {
		t.Fatalf("unexpected status after operator transition: %#v", advanced)
	}

	// 名下没有未结样本时 admin 可以直接结单。
	closed, err := svc.Transition(ctx, animalCase.ID, dto.TransitionRequest{
		Status: "closed", ExpectedVersion: advanced.Version, Reason: "复核通过，关闭来源单",
	}, "admin", model.RoleAdmin, "case-admin-close")
	if err != nil {
		t.Fatalf("admin close without open specimens: %v", err)
	}
	if closed.Status != "closed" {
		t.Fatalf("unexpected admin closed case: %#v", closed)
	}
}

func TestAnimalCaseListReportsOpenSpecimenCount(t *testing.T) {
	db := newAnimalCaseTestDB(t)
	svc := NewAnimalCaseService(repository.NewAnimalCaseRepository(db), repository.NewSpecimenRepository(db), nil)
	ctx := context.Background()

	seedAnimalCaseForClose(t, db, "AC-LIST-1", "testing", "REL-LIST-1")
	seedSpecimenForCase(t, db, "S-LIST-1", "received", "REL-LIST-1")
	seedSpecimenForCase(t, db, "S-LIST-2", "hold", "REL-LIST-1")
	seedSpecimenForCase(t, db, "S-LIST-3", "released", "REL-LIST-1")

	seedAnimalCaseForClose(t, db, "AC-LIST-2", "sampling", "REL-LIST-2")
	seedSpecimenForCase(t, db, "S-LIST-4", "disposed", "REL-LIST-2")
	// 样本也可以直接以来源单编码关联。
	seedSpecimenForCase(t, db, "S-LIST-5", "testing", "AC-LIST-2")

	// 空关联码的来源单不能意外吞并其他无关联样本。
	seedAnimalCaseForClose(t, db, "AC-LIST-3", "registered", "")
	seedSpecimenForCase(t, db, "S-LIST-6", "testing", "")

	page, err := svc.List(ctx, dto.PageQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list cases: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("expected 3 cases, got %d", len(page.Items))
	}
	counts := make(map[string]int64, len(page.Items))
	for _, item := range page.Items {
		counts[item.Code] = item.OpenSpecimens
	}
	if counts["AC-LIST-1"] != 2 {
		t.Fatalf("AC-LIST-1 must report 2 open specimens, got %d", counts["AC-LIST-1"])
	}
	if counts["AC-LIST-2"] != 1 {
		t.Fatalf("AC-LIST-2 must report 1 open specimen linked by case code, got %d", counts["AC-LIST-2"])
	}
	if counts["AC-LIST-3"] != 0 {
		t.Fatalf("AC-LIST-3 must not absorb unlinked specimens, got %d", counts["AC-LIST-3"])
	}
}

func newAnimalCaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AuditLog{}, &model.AnimalCase{}, &model.Specimen{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	return db
}

func seedAnimalCaseForClose(t *testing.T, db *gorm.DB, code, status, relatedCode string) model.AnimalCase {
	t.Helper()
	item := model.AnimalCase{
		BaseModel: model.BaseModel{Code: code, Name: "结单校验来源单 " + code, Status: status, Version: 1},
		Facility:  "兽医检验区", Owner: "运行组", Category: "常规", RiskLevel: "medium",
		EffectiveAt: time.Now().UTC(), RelatedCode: relatedCode,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed animal case: %v", err)
	}
	return item
}

func seedSpecimenForCase(t *testing.T, db *gorm.DB, code, status, relatedCode string) model.Specimen {
	t.Helper()
	item := model.Specimen{
		BaseModel: model.BaseModel{Code: code, Name: "结单校验样本 " + code, Status: status, Version: 1},
		Facility:  "兽医检验区", Owner: "运行组", Category: "常规", RiskLevel: "medium",
		EffectiveAt: time.Now().UTC(), RelatedCode: relatedCode,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed specimen: %v", err)
	}
	return item
}
