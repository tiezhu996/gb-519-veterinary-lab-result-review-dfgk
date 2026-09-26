package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/constants"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/repository"
)

type AnimalCaseService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.AnimalCaseSummary], error)
	Get(context.Context, uint) (model.AnimalCase, error)
	Create(context.Context, dto.CreateAnimalCase, string, string) (model.AnimalCase, error)
	Update(context.Context, uint, dto.UpdateAnimalCase, string, string) (model.AnimalCase, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string, string) (model.AnimalCase, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type animalCaseService struct {
	repository repository.AnimalCaseRepository
	specimens  repository.SpecimenRepository
	security   SecurityService
}

func NewAnimalCaseService(repo repository.AnimalCaseRepository, specimens repository.SpecimenRepository, security SecurityService) AnimalCaseService {
	return &animalCaseService{repository: repo, specimens: specimens, security: security}
}

func (s *animalCaseService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.AnimalCaseSummary], error) {
	page, err := s.repository.List(ctx, query)
	if err != nil {
		return repository.Page[model.AnimalCaseSummary]{}, err
	}
	codes := make([]string, 0, len(page.Items)*2)
	for _, item := range page.Items {
		codes = append(codes, caseLinkCodes(item)...)
	}
	counts, err := s.specimens.CountOpenByRelatedCodes(ctx, codes)
	if err != nil {
		return repository.Page[model.AnimalCaseSummary]{}, fmt.Errorf("count open specimens: %w", err)
	}
	items := make([]model.AnimalCaseSummary, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, model.AnimalCaseSummary{AnimalCase: item, OpenSpecimens: openSpecimenCount(counts, item)})
	}
	return repository.Page[model.AnimalCaseSummary]{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize}, nil
}

func (s *animalCaseService) Get(ctx context.Context, id uint) (model.AnimalCase, error) {
	return s.repository.Get(ctx, id)
}

func (s *animalCaseService) Create(ctx context.Context, input dto.CreateAnimalCase, actor, requestID string) (model.AnimalCase, error) {
	if err := validateAnimalCaseBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.AnimalCase{}, err
	}
	item := model.AnimalCase{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.AnimalCaseInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
	}
	if err := s.repository.Create(ctx, &item); err != nil {
		return model.AnimalCase{}, fmt.Errorf("create 动物样本来源: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "AnimalCase", item.ID, "", item.Status, "created 动物样本来源")
	return item, nil
}

func (s *animalCaseService) Update(ctx context.Context, id uint, input dto.UpdateAnimalCase, actor, requestID string) (model.AnimalCase, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.AnimalCase{}, err
	}
	if err := validateAnimalCaseBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.AnimalCase{}, err
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
		return model.AnimalCase{}, fmt.Errorf("update 动物样本来源: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "AnimalCase", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *animalCaseService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, role, requestID string) (model.AnimalCase, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.AnimalCase{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.AnimalCaseTransitions, current.Status, target) {
		return model.AnimalCase{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	if target == "closed" {
		if role != model.RoleReviewer && role != model.RoleAdmin {
			return model.AnimalCase{}, ErrCaseCloseRole
		}
		counts, err := s.specimens.CountOpenByRelatedCodes(ctx, caseLinkCodes(current))
		if err != nil {
			return model.AnimalCase{}, fmt.Errorf("count open specimens: %w", err)
		}
		if open := openSpecimenCount(counts, current); open > 0 {
			return model.AnimalCase{}, fmt.Errorf("%w：还剩 %d 条检验样本未放行也未处置", ErrCaseOpenSpecimens, open)
		}
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
		return model.AnimalCase{}, fmt.Errorf("transition 动物样本来源: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "AnimalCase", id, before, target, input.Reason); err != nil {
		return model.AnimalCase{}, fmt.Errorf("persist transition audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *animalCaseService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "AnimalCase", id, current.Status, "deleted", "soft deleted 动物样本来源")
}

func (s *animalCaseService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func validateAnimalCaseBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}

// caseLinkCodes returns the non-empty codes a specimen can reference to belong
// to this case: the case code itself or the shared related batch code.
func caseLinkCodes(item model.AnimalCase) []string {
	seen := make(map[string]bool, 2)
	codes := make([]string, 0, 2)
	for _, code := range []string{item.Code, item.RelatedCode} {
		code = strings.TrimSpace(code)
		if code != "" && !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	return codes
}

func openSpecimenCount(counts map[string]int64, item model.AnimalCase) int64 {
	var open int64
	for _, code := range caseLinkCodes(item) {
		open += counts[code]
	}
	return open
}
