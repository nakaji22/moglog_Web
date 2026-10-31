package main

import (
	"fmt"
	"math"
	"time"
)

// Nutrients は栄養素の合計値。
type Nutrients struct {
	Kcal    float64
	Protein float64
	Fat     float64
	Carbs   float64
	Fiber   float64
	Salt    float64
}

func (n Nutrients) Add(o Nutrients) Nutrients {
	return Nutrients{
		Kcal: n.Kcal + o.Kcal, Protein: n.Protein + o.Protein, Fat: n.Fat + o.Fat,
		Carbs: n.Carbs + o.Carbs, Fiber: n.Fiber + o.Fiber, Salt: n.Salt + o.Salt,
	}
}

// Scale は係数倍した栄養素を返す。
func (n Nutrients) Scale(f float64) Nutrients {
	return Nutrients{
		Kcal: n.Kcal * f, Protein: n.Protein * f, Fat: n.Fat * f,
		Carbs: n.Carbs * f, Fiber: n.Fiber * f, Salt: n.Salt * f,
	}
}

var mealTypeLabels = map[string]string{
	"breakfast": "朝食",
	"lunch":     "昼食",
	"dinner":    "夕食",
	"snack":     "間食",
}

var mealTypes = []string{"breakfast", "lunch", "dinner", "snack"}

func mealTypeLabel(t string) string {
	if l, ok := mealTypeLabels[t]; ok {
		return l
	}
	return t
}

func validMealType(t string) bool {
	_, ok := mealTypeLabels[t]
	return ok
}

// Profile はユーザーの身体情報と目標設定。
type Profile struct {
	UserID           int64
	Age              int
	Sex              string // male / female
	HeightCm         float64
	WeightKg         float64
	TargetWeightKg   float64
	ActivityLevel    string
	Purpose          string // lose / maintain / gain
	KcalGoalOverride *float64
}

var activityFactors = map[string]float64{
	"sedentary":   1.2,
	"light":       1.375,
	"moderate":    1.55,
	"active":      1.725,
	"very_active": 1.9,
}

var activityLabels = map[string]string{
	"sedentary":   "ほぼ運動しない（デスクワーク中心）",
	"light":       "軽い運動（週1〜3回）",
	"moderate":    "中程度の運動（週3〜5回）",
	"active":      "激しい運動（週6〜7回）",
	"very_active": "肉体労働・アスリートレベル",
}

var purposeLabels = map[string]string{
	"lose":     "減量",
	"maintain": "維持",
	"gain":     "増量",
}

// BasalMetabolicRate は Mifflin-St Jeor 式による基礎代謝量 (kcal/日)。
func BasalMetabolicRate(sex string, weightKg, heightCm float64, age int) float64 {
	base := 10*weightKg + 6.25*heightCm - 5*float64(age)
	if sex == "male" {
		return base + 5
	}
	return base - 161
}

func safetyFloorKcal(sex string) float64 {
	if sex == "male" {
		return 1500
	}
	return 1200
}

// Recommendation は推奨摂取カロリーの算出結果。
type Recommendation struct {
	Kcal                 float64
	BMR                  float64
	TDEE                 float64
	ClampedToSafetyFloor bool
	WeeklyChangeKg       float64
}

// RecommendIntake は推奨摂取カロリーを算出する。
// 減量: TDEEの-15%（ただし最大でも-500kcalまで）。増量: TDEEの+10%。
// 安全のため性別ごとの下限（男性1500/女性1200kcal）を下回る場合は下限に切り上げる。
func RecommendIntake(p Profile) Recommendation {
	bmr := BasalMetabolicRate(p.Sex, p.WeightKg, p.HeightCm, p.Age)
	factor, ok := activityFactors[p.ActivityLevel]
	if !ok {
		factor = 1.2
	}
	tdee := bmr * factor
	target := tdee
	switch p.Purpose {
	case "lose":
		target = math.Max(tdee*0.85, tdee-500)
	case "gain":
		target = tdee * 1.1
	}
	floor := safetyFloorKcal(p.Sex)
	clamped := false
	if target < floor {
		target = floor
		clamped = true
	}
	weekly := math.Round(((target-tdee)*7/7700)*100) / 100
	return Recommendation{
		Kcal:                 math.Round(target),
		BMR:                  math.Round(bmr),
		TDEE:                 math.Round(tdee),
		ClampedToSafetyFloor: clamped,
		WeeklyChangeKg:       weekly,
	}
}

// GoalKcal は実際に使う目標カロリー（上書きがあればそちら）。
func (p Profile) GoalKcal() float64 {
	if p.KcalGoalOverride != nil && *p.KcalGoalOverride > 0 {
		return *p.KcalGoalOverride
	}
	return RecommendIntake(p).Kcal
}

// PFCGoals は目標カロリーからPFC目標(g)を算出する。配分は P:20%（減量時25%） F:25% C:残り。
func PFCGoals(kcalGoal float64, purpose string) Nutrients {
	pRatio := 0.20
	if purpose == "lose" {
		pRatio = 0.25
	}
	fRatio := 0.25
	cRatio := 1 - pRatio - fRatio
	return Nutrients{
		Kcal:    kcalGoal,
		Protein: kcalGoal * pRatio / 4,
		Fat:     kcalGoal * fRatio / 9,
		Carbs:   kcalGoal * cRatio / 4,
		Fiber:   20,
	}
}

// ---- 日付ユーティリティ ----

const dateLayout = "2006-01-02"

func todayJST() string {
	return time.Now().In(jst).Format(dateLayout)
}

var jst = time.FixedZone("Asia/Tokyo", 9*60*60)

func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation(dateLayout, s, jst)
}

func validDate(s string) bool {
	_, err := parseDate(s)
	return err == nil
}

var weekdayJP = []string{"日", "月", "火", "水", "木", "金", "土"}

// DateJP は "2026年10月31日（土）" のような表示を返す。
func DateJP(s string) string {
	t, err := parseDate(s)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d年%d月%d日（%s）", t.Year(), int(t.Month()), t.Day(), weekdayJP[int(t.Weekday())])
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
