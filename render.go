package main

import (
	"embed"
	"fmt"
	"html/template"
	"math"
	"net/http"
)

//go:embed web/templates
var templatesFS embed.FS

//go:embed web/static
var staticFS embed.FS

type viewData struct {
	Title string
	User  *User
	CSRF  string
	Flash string
	Error string
	V     any
}

var pageNames = []string{
	"landing", "login", "register", "home", "meal_form",
	"history", "day", "weight", "foods", "settings",
}

func loadTemplates() (map[string]*template.Template, error) {
	funcs := template.FuncMap{
		"kcal":      func(v float64) string { return fmt.Sprintf("%d", int(math.Round(v))) },
		"g1":        func(v float64) string { return fmt.Sprintf("%.1f", v) },
		"dateJP":    DateJP,
		"mealLabel": mealTypeLabel,
		"pct": func(part, goal float64) float64 {
			if goal <= 0 {
				return 0
			}
			p := part / goal * 100
			if p > 100 {
				return 100
			}
			return p
		},
		"pctRaw": func(part, goal float64) float64 {
			if goal <= 0 {
				return 0
			}
			return part / goal * 100
		},
	}
	out := map[string]*template.Template{}
	for _, name := range pageNames {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(
			templatesFS, "web/templates/layout.html", "web/templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("テンプレート %s の読み込みに失敗: %w", name, err)
		}
		out[name] = t
	}
	return out, nil
}

func (a *App) render(w http.ResponseWriter, page string, vd viewData) {
	t, ok := a.tmpls[page]
	if !ok {
		http.Error(w, "テンプレートが見つかりません", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout.html", vd); err != nil {
		// ヘッダ送信後の可能性があるため、ここではログのみ。
		fmt.Printf("テンプレート実行エラー (%s): %v\n", page, err)
	}
}
