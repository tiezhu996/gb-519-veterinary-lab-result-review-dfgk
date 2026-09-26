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
	specimens := repository.NewSpecimenRepository(db)
	svc := NewAnimalCaseService(repository.NewAnimalCaseRepository(db), specimens, security)
	specimenSvc := NewSpecimenService(specimens, security)
	ctx := context.Background()

	created, err := svc.Create(ctx, caseInput("AC-CLOSE-01"), "operator", "case-create-1")
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	sampling, err := svc.Transition(ctx, created.ID, dto.TransitionRequest{
		Status: "sampling", ExpectedVersion: created.Version, Reason: "start sampling",
	}, "operator", model.RoleOperator, "case-sampling-2")
	if err != nil {
		t.Fatalf("advance to sampling: %v", err)
	}
	testingCase, err := svc.Transition(ctx, sampling.ID, dto.TransitionRequest{
		Status: "testing", ExpectedVersion: sampling.Version, Reason: "start testing",
	}, "operator", model.RoleOperator, "case-testing-3")
	if err != nil {
		t.Fatalf("advance to testing: %v", err)
	}

	pending, err := specimenSvc.Create(ctx, specimenInput("S-CLOSE-01", testingCase.Code), "operator", "specimen-create-1")
	if err != nil {
		t.Fatalf("create pending specimen: %v", err)
	}
	settled, err := specimenSvc.Create(ctx, specimenInput("S-CLOSE-02", testingCase.Code), "operator", "specimen-create-2")
	if err != nil {
		t.Fatalf("create settled specimen: %v", err)
	}
	settled = advanceSpecimen(t, specimenSvc, settled, "testing", "specimen-testing-3")
	advanceSpecimen(t, specimenSvc, settled, "released", "specimen-released-4")

	closeRequest := dto.TransitionRequest{Status: "closed", ExpectedVersion: testingCase.Version, Reason: "close the case"}
	if _, err := svc.Transition(ctx, testingCase.ID, closeRequest, "operator", model.RoleOperator, "case-close-operator"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("operator closing must be forbidden, got %v", err)
	}
	if _, err := svc.Transition(ctx, testingCase.ID, closeRequest, "reviewer", model.RoleReviewer, "case-close-blocked"); !errors.Is(err, ErrUnsettledSpecimens) {
		t.Fatalf("closing with unsettled specimens must be blocked, got %v", err)
	} else if !strings.Contains(err.Error(), "1") {
		t.Fatalf("blocked close must report the remaining specimen count, got %v", err)
	}

	page, err := svc.List(ctx, dto.PageQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list cases: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].UnsettledSpecimens != 1 {
		t.Fatalf("list must expose one unsettled specimen, got %#v", page.Items)
	}

	pending = advanceSpecimen(t, specimenSvc, pending, "hold", "specimen-hold-5")
	advanceSpecimen(t, specimenSvc, pending, "disposed", "specimen-disposed-6")

	closed, err := svc.Transition(ctx, testingCase.ID, closeRequest, "reviewer", model.RoleReviewer, "case-close-7")
	if err != nil {
		t.Fatalf("close after all specimens settled: %v", err)
	}
	if closed.Status != "closed" {
		t.Fatalf("expected closed status, got %q", closed.Status)
	}
}

func advanceSpecimen(t *testing.T, svc SpecimenService, item model.Specimen, target, requestID string) model.Specimen {
	t.Helper()
	updated, err := svc.Transition(context.Background(), item.ID, dto.TransitionRequest{
		Status: target, ExpectedVersion: item.Version, Reason: "advance specimen " + target,
	}, "operator", requestID)
	if err != nil {
		t.Fatalf("advance specimen %s -> %s: %v", item.Code, target, err)
	}
	return updated
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

func caseInput(code string) dto.CreateAnimalCase {
	return dto.CreateAnimalCase{
		Code: code, Name: "Close guard case", Description: "case close guard coverage",
		Facility: "Veterinary Lab 1", Owner: "Case desk", Category: "常规", RiskLevel: "medium",
		MetricValue: 10, MetricUnit: "unit", EffectiveAt: time.Now().UTC(),
		Evidence: "case intake evidence", RelatedCode: "REL-CLOSE-01",
	}
}

func specimenInput(code, caseCode string) dto.CreateSpecimen {
	return dto.CreateSpecimen{
		Code: code, Name: "Close guard specimen", Description: "specimen linked to the guarded case",
		CaseCode: caseCode, Facility: "Veterinary Lab 1", Owner: "Specimen desk", Category: "常规",
		RiskLevel: "low", MetricValue: 5, MetricUnit: "unit", EffectiveAt: time.Now().UTC(),
		Evidence: "specimen intake evidence", RelatedCode: "REL-CLOSE-01",
	}
}
