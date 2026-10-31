package main

import (
	"math"
	"testing"
)

func almostEq(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestBasalMetabolicRate(t *testing.T) {
	// 男性: 10*65 + 6.25*170 - 5*30 + 5 = 1567.5
	if got := BasalMetabolicRate("male", 65, 170, 30); !almostEq(got, 1567.5) {
		t.Errorf("male BMR = %v, want 1567.5", got)
	}
	// 女性: 10*55 + 6.25*158 - 5*28 - 161 = 1236.5
	if got := BasalMetabolicRate("female", 55, 158, 28); !almostEq(got, 1236.5) {
		t.Errorf("female BMR = %v, want 1236.5", got)
	}
}

func TestRecommendIntakeLose(t *testing.T) {
	p := Profile{Sex: "male", WeightKg: 80, HeightCm: 175, Age: 35, ActivityLevel: "moderate", Purpose: "lose"}
	rec := RecommendIntake(p)
	// BMR = 800 + 1093.75 - 175 + 5 = 1723.75 / TDEE = 2671.8 / -15% = 2271 > TDEE-500=2171.8 → 2271
	if rec.Kcal != 2271 {
		t.Errorf("lose kcal = %v, want 2271", rec.Kcal)
	}
	if rec.ClampedToSafetyFloor {
		t.Errorf("should not be clamped")
	}
}

func TestRecommendIntakeSafetyFloor(t *testing.T) {
	// 小柄な女性の減量では下限1200kcalに切り上がること
	p := Profile{Sex: "female", WeightKg: 45, HeightCm: 150, Age: 40, ActivityLevel: "sedentary", Purpose: "lose"}
	rec := RecommendIntake(p)
	if rec.Kcal != 1200 {
		t.Errorf("kcal = %v, want safety floor 1200", rec.Kcal)
	}
	if !rec.ClampedToSafetyFloor {
		t.Errorf("ClampedToSafetyFloor should be true")
	}
}

func TestPFCGoals(t *testing.T) {
	g := PFCGoals(2000, "maintain")
	if !almostEq(g.Protein, 100) || !almostEq(g.Fat, 55.56) || !almostEq(g.Carbs, 275) {
		t.Errorf("unexpected PFC goals: %+v", g)
	}
	g2 := PFCGoals(2000, "lose")
	if !almostEq(g2.Protein, 125) {
		t.Errorf("lose protein = %v, want 125", g2.Protein)
	}
}

func TestFoodNutrients(t *testing.T) {
	loadCatalog()
	rice, ok := getFood("rice")
	if !ok {
		t.Fatal("rice not found")
	}
	n := rice.NutrientsFor(150)
	if !almostEq(n.Kcal, 234) {
		t.Errorf("rice 150g kcal = %v, want 234", n.Kcal)
	}
	if len(allFoods) != 114 {
		t.Errorf("foods = %d, want 114", len(allFoods))
	}
	if len(allTemplates) != 16 {
		t.Errorf("templates = %d, want 16", len(allTemplates))
	}
	if got := len(searchFoods("鮭")); got == 0 {
		t.Errorf("search 鮭 returned 0")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	if !checkPassword(h, "password123") {
		t.Error("correct password rejected")
	}
	if checkPassword(h, "wrong-password") {
		t.Error("wrong password accepted")
	}
}

func TestValidEmailAndDate(t *testing.T) {
	if !validEmail("user@example.co.jp") {
		t.Error("valid email rejected")
	}
	for _, bad := range []string{"", "a@", "@b.jp", "a b@c.jp", "a@b"} {
		if validEmail(bad) {
			t.Errorf("invalid email accepted: %q", bad)
		}
	}
	if !validDate("2026-10-31") {
		t.Error("valid date rejected")
	}
	if validDate("2026-13-40") {
		t.Error("invalid date accepted")
	}
}
