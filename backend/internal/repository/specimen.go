package repository

import (
	"context"

	"github.com/blueship581/veterinary-lab-result-review/backend/internal/constants"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/dto"
	"github.com/blueship581/veterinary-lab-result-review/backend/internal/model"
	"gorm.io/gorm"
)

// SpecimenRepository owns all persistence operations for 检验样本.
type SpecimenRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.Specimen], error)
	Get(context.Context, uint) (model.Specimen, error)
	Create(context.Context, *model.Specimen) error
	Update(context.Context, uint, uint, *model.Specimen) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
	CountUnsettledByCaseCode(context.Context, string) (int64, error)
	CountUnsettledByCaseCodes(context.Context, []string) (map[string]int64, error)
}

type specimenRepository struct {
	store *Store[model.Specimen]
	db    *gorm.DB
}

func NewSpecimenRepository(db *gorm.DB) SpecimenRepository {
	return &specimenRepository{store: NewStore[model.Specimen](db), db: db}
}

func (r *specimenRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.Specimen], error) {
	return r.store.List(ctx, q)
}
func (r *specimenRepository) Get(ctx context.Context, id uint) (model.Specimen, error) {
	return r.store.Get(ctx, id)
}
func (r *specimenRepository) Create(ctx context.Context, item *model.Specimen) error {
	return r.store.Create(ctx, item)
}
func (r *specimenRepository) Update(ctx context.Context, id, version uint, item *model.Specimen) error {
	return r.store.Update(ctx, id, version, item)
}
func (r *specimenRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *specimenRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

// CountUnsettledByCaseCode counts specimens of one 动物样本来源 that are neither
// released nor disposed, i.e. the close-blocking remainder for that case.
func (r *specimenRepository) CountUnsettledByCaseCode(ctx context.Context, caseCode string) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&model.Specimen{}).
		Where("case_code = ?", caseCode).
		Where("status NOT IN ?", constants.SettledSpecimenStates).
		Count(&total).Error
	return total, err
}

// CountUnsettledByCaseCodes is the batch form used by list views so every case
// row can display its unsettled specimen count without N+1 queries.
func (r *specimenRepository) CountUnsettledByCaseCodes(ctx context.Context, caseCodes []string) (map[string]int64, error) {
	counts := make(map[string]int64, len(caseCodes))
	if len(caseCodes) == 0 {
		return counts, nil
	}
	rows, err := r.db.WithContext(ctx).Model(&model.Specimen{}).
		Select("case_code, COUNT(*) AS total").
		Where("case_code IN ?", caseCodes).
		Where("status NOT IN ?", constants.SettledSpecimenStates).
		Group("case_code").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var caseCode string
		var total int64
		if err := rows.Scan(&caseCode, &total); err != nil {
			return nil, err
		}
		counts[caseCode] = total
	}
	return counts, rows.Err()
}
