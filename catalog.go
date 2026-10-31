package main

import (
	"encoding/json"
	"log"
	"sort"
	"strings"

	"github.com/nakaji22/moglog_Web/internal/data"
)

// Food は内蔵食品データベースの1件（値は100gあたり）。
type Food struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Kcal         float64  `json:"kcal"`
	Protein      float64  `json:"protein"`
	Fat          float64  `json:"fat"`
	Carbs        float64  `json:"carbs"`
	Fiber        float64  `json:"fiber"`
	Salt         float64  `json:"salt"`
	DefaultGrams float64  `json:"defaultGrams"`
	Aliases      []string `json:"aliases"`
}

// Per100 は100gあたりの栄養素。
func (f Food) Per100() Nutrients {
	return Nutrients{Kcal: f.Kcal, Protein: f.Protein, Fat: f.Fat, Carbs: f.Carbs, Fiber: f.Fiber, Salt: f.Salt}
}

// NutrientsFor は指定グラム数の栄養素を返す。
func (f Food) NutrientsFor(grams float64) Nutrients {
	return f.Per100().Scale(grams / 100)
}

// MealTemplate は定番メニューのテンプレート。
type MealTemplate struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	MealTypes []string `json:"mealTypes"`
	Keywords  []string `json:"keywords"`
	Items     []struct {
		FoodID string  `json:"foodId"`
		Grams  float64 `json:"grams"`
	} `json:"items"`
}

// TemplateItem はテンプレート適用後の表示用アイテム。
type TemplateItem struct {
	FoodID string
	Name   string
	Grams  float64
}

var (
	allFoods     []Food
	foodByID     map[string]Food
	allTemplates []MealTemplate
)

func loadCatalog() {
	if err := json.Unmarshal(data.FoodsJSON, &allFoods); err != nil {
		log.Fatalf("foods.json の読み込みに失敗: %v", err)
	}
	foodByID = make(map[string]Food, len(allFoods))
	for _, f := range allFoods {
		foodByID[f.ID] = f
	}
	if err := json.Unmarshal(data.TemplatesJSON, &allTemplates); err != nil {
		log.Fatalf("templates.json の読み込みに失敗: %v", err)
	}
	log.Printf("食品データ %d 件、テンプレート %d 件を読み込みました", len(allFoods), len(allTemplates))
}

func getFood(id string) (Food, bool) {
	f, ok := foodByID[id]
	return f, ok
}

func searchFoods(q string) []Food {
	q = strings.TrimSpace(q)
	if q == "" {
		return allFoods
	}
	var out []Food
	for _, f := range allFoods {
		if strings.Contains(f.Name, q) || strings.Contains(f.Category, q) {
			out = append(out, f)
			continue
		}
		for _, a := range f.Aliases {
			if strings.Contains(a, q) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// TemplateItems はテンプレートを食品名つきのアイテム列に展開する。
func (t MealTemplate) TemplateItems() []TemplateItem {
	var out []TemplateItem
	for _, it := range t.Items {
		if f, ok := getFood(it.FoodID); ok {
			out = append(out, TemplateItem{FoodID: f.ID, Name: f.Name, Grams: it.Grams})
		}
	}
	return out
}

// TemplateTotals はテンプレート全体の栄養素合計。
func (t MealTemplate) TemplateTotals() Nutrients {
	var total Nutrients
	for _, it := range t.Items {
		if f, ok := getFood(it.FoodID); ok {
			total = total.Add(f.NutrientsFor(it.Grams))
		}
	}
	return total
}

// FrequentFood はユーザーがよく記録する食品。
type FrequentFood struct {
	Food  Food
	Count int
}

// SuggestForPhoto は写真記録時の候補を返す。
// 写真からの自動判定は行わず、定番テンプレートと本人がよく記録する食品を候補として提示する。
func SuggestForPhoto(frequent []FrequentFood) (templates []MealTemplate, frequentFoods []FrequentFood) {
	sorted := make([]FrequentFood, len(frequent))
	copy(sorted, frequent)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })
	if len(sorted) > 8 {
		sorted = sorted[:8]
	}
	return allTemplates, sorted
}
