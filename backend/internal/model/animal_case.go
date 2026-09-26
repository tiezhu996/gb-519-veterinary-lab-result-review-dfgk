package model

import "time"

// AnimalCase models 动物样本来源 as an independently versioned aggregate. The fields
// cover ownership, operational context, evidence and measured risk so later
// changes naturally span persistence, service and UI layers.
type AnimalCase struct {
	BaseModel
	Facility    string    `json:"facility" gorm:"size:120;index"`
	Owner       string    `json:"owner" gorm:"size:120;index"`
	Category    string    `json:"category" gorm:"size:80;index"`
	RiskLevel   string    `json:"riskLevel" gorm:"size:32;index"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit" gorm:"size:24"`
	EffectiveAt time.Time `json:"effectiveAt"`
	Evidence    string    `json:"evidence" gorm:"size:2000"`
	RelatedCode string    `json:"relatedCode" gorm:"size:64;index"`
}

func (item *AnimalCase) GetBase() *BaseModel { return &item.BaseModel }

// AnimalCaseSummary enriches a list row with the number of specimens under the
// case that are neither released nor disposed, so the workbench can show how
// many 未结样本 still block closing.
type AnimalCaseSummary struct {
	AnimalCase
	OpenSpecimens int64 `json:"openSpecimens"`
}

func (item AnimalCase) TableName() string { return "animal_cases" }

var AnimalCaseInitialStatus = "registered"
