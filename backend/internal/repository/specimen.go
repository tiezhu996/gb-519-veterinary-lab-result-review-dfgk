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
	CountOpenByRelatedCodes(context.Context, []string) (map[string]int64, error)
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

// CountOpenByRelatedCodes groups specimens that are neither released nor disposed
// by related code, so 动物样本来源 can report and block on 未结样本 before closing.
func (r *specimenRepository) CountOpenByRelatedCodes(ctx context.Context, codes []string) (map[string]int64, error) {
	counts := make(map[string]int64, len(codes))
	if len(codes) == 0 {
		return counts, nil
	}
	rows, err := r.db.WithContext(ctx).Model(&model.Specimen{}).
		Select("related_code, COUNT(*) AS total").
		Where("related_code IN ?", codes).
		Where("status NOT IN ?", constants.SettledSpecimenStates).
		Group("related_code").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		var total int64
		if err := rows.Scan(&code, &total); err != nil {
			return nil, err
		}
		counts[code] = total
	}
	return counts, rows.Err()
}
