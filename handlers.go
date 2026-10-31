package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- ランディング ----

func (a *App) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if _, ok := a.currentAuth(r); ok {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	a.render(w, "landing", viewData{Title: "もぐログ"})
}

// ---- ホーム（今日の記録） ----

type mealGroup struct {
	Type     string
	Label    string
	Meals    []Meal
	Subtotal Nutrients
}

type dayBar struct {
	Date    string
	Day     int
	Kcal    float64
	Pct     float64
	IsToday bool
}

type homeView struct {
	Date         string
	HasProfile   bool
	GoalKcal     float64
	PFC          Nutrients
	Totals       Nutrients
	Remaining    float64
	RemainingAbs float64
	Groups       []mealGroup
	Week         []dayBar
}

func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	userID := ac.User.ID
	today := todayJST()

	profile, perr := a.store.Profile(userID)
	hasProfile := perr == nil
	goal := 2000.0
	var pfc Nutrients
	if hasProfile {
		goal = profile.GoalKcal()
		pfc = PFCGoals(goal, profile.Purpose)
	} else {
		pfc = PFCGoals(goal, "maintain")
	}

	meals, err := a.store.MealsOn(userID, today)
	if err != nil {
		http.Error(w, "データの取得に失敗しました", http.StatusInternalServerError)
		return
	}
	byType := map[string][]Meal{}
	var totals Nutrients
	for _, m := range meals {
		byType[m.MealType] = append(byType[m.MealType], m)
		totals = totals.Add(m.Totals())
	}
	var groups []mealGroup
	for _, t := range mealTypes {
		g := mealGroup{Type: t, Label: mealTypeLabel(t), Meals: byType[t]}
		for _, m := range g.Meals {
			g.Subtotal = g.Subtotal.Add(m.Totals())
		}
		groups = append(groups, g)
	}

	// 直近7日
	todayT, _ := parseDate(today)
	from := todayT.AddDate(0, 0, -6)
	weekTotals, _ := a.store.DayTotals(userID, from.Format(dateLayout), today)
	var week []dayBar
	for i := 0; i < 7; i++ {
		d := from.AddDate(0, 0, i)
		ds := d.Format(dateLayout)
		n := weekTotals[ds]
		pct := 0.0
		if goal > 0 {
			pct = n.Kcal / goal * 100
			if pct > 100 {
				pct = 100
			}
		}
		week = append(week, dayBar{Date: ds, Day: d.Day(), Kcal: n.Kcal, Pct: pct, IsToday: ds == today})
	}

	remaining := goal - totals.Kcal
	remAbs := remaining
	if remAbs < 0 {
		remAbs = -remAbs
	}
	a.render(w, "home", a.view(r, "今日の記録", homeView{
		Date: today, HasProfile: hasProfile, GoalKcal: goal, PFC: pfc,
		Totals: totals, Remaining: remaining, RemainingAbs: remAbs, Groups: groups, Week: week,
	}))
}

// ---- 食事フォーム（新規・編集共通） ----

type jsFood struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Category     string    `json:"category"`
	DefaultGrams float64   `json:"defaultGrams"`
	Per100       Nutrients `json:"per100"`
	Aliases      []string  `json:"aliases"`
}

type jsItem struct {
	FoodID    string     `json:"foodId"`
	Name      string     `json:"name"`
	Grams     float64    `json:"grams"`
	Per100    *Nutrients `json:"per100"`
	Nutrients Nutrients  `json:"nutrients"`
}

type templateVM struct {
	ID    string
	Title string
	Kcal  float64
	Items []TemplateItem
}

type mealFormView struct {
	Action        string
	Date          string
	MealType      string
	Title_        string
	Note          string
	PhotoName     string
	Source        string
	IsPhoto       bool
	ItemsJSON     template.JS
	FoodsJSON     template.JS
	TemplatesJSON template.JS
	Templates     []templateVM
	Frequent      []FrequentFood
	FormError     string
}

func (a *App) foodsJSON() template.JS {
	list := make([]jsFood, 0, len(allFoods))
	for _, f := range allFoods {
		list = append(list, jsFood{ID: f.ID, Name: f.Name, Category: f.Category, DefaultGrams: f.DefaultGrams, Per100: f.Per100(), Aliases: f.Aliases})
	}
	b, _ := json.Marshal(list)
	return template.JS(b)
}

func itemsJSON(items []MealItem) template.JS {
	list := make([]jsItem, 0, len(items))
	for _, it := range items {
		ji := jsItem{FoodID: it.FoodID, Name: it.Name, Grams: it.Grams, Nutrients: it.Nutrients}
		if f, ok := getFood(it.FoodID); ok {
			p := f.Per100()
			ji.Per100 = &p
		}
		list = append(list, ji)
	}
	b, _ := json.Marshal(list)
	return template.JS(b)
}

func (a *App) mealFormVM(r *http.Request, action string, meal Meal, formErr string) mealFormView {
	ac, _ := authFrom(r)
	freq, _ := a.store.FrequentFoods(ac.User.ID, 8)
	var tpls []templateVM
	for _, t := range allTemplates {
		tpls = append(tpls, templateVM{ID: t.ID, Title: t.Title, Kcal: t.TemplateTotals().Kcal, Items: t.TemplateItems()})
	}
	tplJSON, _ := json.Marshal(allTemplates)
	return mealFormView{
		Action:        action,
		Date:          meal.Date,
		MealType:      meal.MealType,
		Title_:        meal.Title,
		Note:          meal.Note,
		PhotoName:     meal.PhotoName,
		Source:        meal.Source,
		IsPhoto:       meal.PhotoName != "",
		ItemsJSON:     itemsJSON(meal.Items),
		FoodsJSON:     a.foodsJSON(),
		TemplatesJSON: template.JS(tplJSON),
		Templates:     tpls,
		Frequent:      freq,
		FormError:     formErr,
	}
}

func (a *App) handleMealNew(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	date := q.Get("date")
	if !validDate(date) {
		date = todayJST()
	}
	mealType := q.Get("meal_type")
	if !validMealType(mealType) {
		mealType = "lunch"
	}
	meal := Meal{Date: date, MealType: mealType, Source: "manual"}
	if photo := q.Get("photo"); photo != "" && validPhotoName(photo) {
		meal.PhotoName = photo
		meal.Source = "photo"
	}
	vd := a.view(r, "食事を記録", a.mealFormVM(r, "/meals", meal, ""))
	a.render(w, "meal_form", vd)
}

var photoNameRe = regexp.MustCompile(`^[a-f0-9]{32}\.(jpg|png|webp|gif)$`)

func validPhotoName(s string) bool { return photoNameRe.MatchString(s) }

func parseMealForm(r *http.Request, userID int64) (Meal, string) {
	if err := r.ParseForm(); err != nil {
		return Meal{}, "リクエストが不正です。"
	}
	date := r.FormValue("date")
	if !validDate(date) {
		return Meal{}, "日付が正しくありません。"
	}
	mealType := r.FormValue("meal_type")
	if !validMealType(mealType) {
		return Meal{}, "食事の区分が正しくありません。"
	}
	meal := Meal{
		UserID:   userID,
		Date:     date,
		MealType: mealType,
		Title:    strings.TrimSpace(r.FormValue("title")),
		Note:     strings.TrimSpace(r.FormValue("note")),
		Source:   "manual",
	}
	if p := r.FormValue("photo_name"); validPhotoName(p) {
		meal.PhotoName = p
		meal.Source = "photo"
	}
	if r.FormValue("source") == "template" {
		meal.Source = "template"
	}

	foodIDs := r.Form["food_id"]
	names := r.Form["item_name"]
	gramsList := r.Form["item_grams"]
	get := func(arr []string, i int) string {
		if i < len(arr) {
			return arr[i]
		}
		return ""
	}
	parseF := func(arr []string, i int) float64 {
		v, _ := strconv.ParseFloat(strings.TrimSpace(get(arr, i)), 64)
		if v < 0 {
			return 0
		}
		return v
	}
	n := len(names)
	for i := 0; i < n; i++ {
		name := strings.TrimSpace(get(names, i))
		fid := get(foodIDs, i)
		grams := parseF(gramsList, i)
		if name == "" && fid == "" {
			continue
		}
		item := MealItem{FoodID: fid, Name: name, Grams: grams}
		if food, ok := getFood(fid); ok {
			if item.Name == "" {
				item.Name = food.Name
			}
			item.Nutrients = food.NutrientsFor(grams)
		} else {
			item.FoodID = ""
			item.Nutrients = Nutrients{
				Kcal:    parseF(r.Form["item_kcal"], i),
				Protein: parseF(r.Form["item_protein"], i),
				Fat:     parseF(r.Form["item_fat"], i),
				Carbs:   parseF(r.Form["item_carbs"], i),
				Fiber:   parseF(r.Form["item_fiber"], i),
				Salt:    parseF(r.Form["item_salt"], i),
			}
		}
		meal.Items = append(meal.Items, item)
	}
	if len(meal.Items) == 0 {
		return Meal{}, "品目を1つ以上入力してください。"
	}
	if meal.Title == "" {
		meal.Title = meal.Items[0].Name
	}
	return meal, ""
}

func (a *App) handleMealCreate(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	meal, msg := parseMealForm(r, ac.User.ID)
	if msg != "" {
		vd := a.view(r, "食事を記録", a.mealFormVM(r, "/meals", Meal{Date: r.FormValue("date"), MealType: r.FormValue("meal_type")}, msg))
		a.render(w, "meal_form", vd)
		return
	}
	id, err := a.store.CreateMeal(meal)
	if err != nil {
		http.Error(w, "保存に失敗しました", http.StatusInternalServerError)
		return
	}
	_ = id
	http.Redirect(w, r, "/history/"+meal.Date+"?saved=1", http.StatusSeeOther)
}

func (a *App) handleMealEdit(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	meal, err := a.store.MealByID(ac.User.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	vd := a.view(r, "食事を編集", a.mealFormVM(r, fmt.Sprintf("/meals/%d", id), meal, ""))
	a.render(w, "meal_form", vd)
}

func (a *App) handleMealUpdate(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	existing, err := a.store.MealByID(ac.User.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	meal, msg := parseMealForm(r, ac.User.ID)
	if msg != "" {
		existing.Items = nil
		vd := a.view(r, "食事を編集", a.mealFormVM(r, fmt.Sprintf("/meals/%d", id), existing, msg))
		a.render(w, "meal_form", vd)
		return
	}
	meal.ID = id
	if err := a.store.UpdateMeal(meal); err != nil {
		http.Error(w, "保存に失敗しました", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/history/"+meal.Date+"?saved=1", http.StatusSeeOther)
}

func (a *App) handleMealDelete(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	meal, err := a.store.MealByID(ac.User.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.store.DeleteMeal(ac.User.ID, id); err != nil {
		http.Error(w, "削除に失敗しました", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/history/"+meal.Date, http.StatusSeeOther)
}

// ---- 履歴 ----

type historyDay struct {
	Date  string
	Day   int
	Wday  string
	Kcal  float64
	Count int
	Has   bool
}

type historyView struct {
	Month      string
	MonthLabel string
	Prev       string
	Next       string
	Days       []historyDay
	RecordDays int
	AvgKcal    float64
}

func (a *App) handleHistory(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	userID := ac.User.ID
	month := r.URL.Query().Get("month")
	base, err := time.ParseInLocation("2006-01", month, jst)
	if err != nil {
		now := time.Now().In(jst)
		base = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, jst)
		month = base.Format("2006-01")
	}
	first := time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, jst)
	last := first.AddDate(0, 1, -1)
	totals, _ := a.store.DayTotals(userID, first.Format(dateLayout), last.Format(dateLayout))
	meals, _ := a.store.MealsInMonth(userID, month)
	counts := map[string]int{}
	for _, m := range meals {
		counts[m.Date]++
	}
	var days []historyDay
	recordDays := 0
	var sum float64
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		ds := d.Format(dateLayout)
		n, has := totals[ds]
		if has {
			recordDays++
			sum += n.Kcal
		}
		days = append(days, historyDay{Date: ds, Day: d.Day(), Wday: weekdayJP[int(d.Weekday())], Kcal: n.Kcal, Count: counts[ds], Has: has})
	}
	avg := 0.0
	if recordDays > 0 {
		avg = sum / float64(recordDays)
	}
	a.render(w, "history", a.view(r, "履歴", historyView{
		Month:      month,
		MonthLabel: fmt.Sprintf("%d年%d月", first.Year(), int(first.Month())),
		Prev:       first.AddDate(0, -1, 0).Format("2006-01"),
		Next:       first.AddDate(0, 1, 0).Format("2006-01"),
		Days:       days, RecordDays: recordDays, AvgKcal: avg,
	}))
}

type dayView struct {
	Date       string
	Meals      []Meal
	Totals     Nutrients
	HasProfile bool
	GoalKcal   float64
	HasWeight  bool
	WeightKg   float64
}

func (a *App) handleDay(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	userID := ac.User.ID
	date := r.PathValue("date")
	if !validDate(date) {
		http.NotFound(w, r)
		return
	}
	meals, err := a.store.MealsOn(userID, date)
	if err != nil {
		http.Error(w, "データの取得に失敗しました", http.StatusInternalServerError)
		return
	}
	var totals Nutrients
	for _, m := range meals {
		totals = totals.Add(m.Totals())
	}
	dv := dayView{Date: date, Meals: meals, Totals: totals}
	if p, err := a.store.Profile(userID); err == nil {
		dv.HasProfile = true
		dv.GoalKcal = p.GoalKcal()
	}
	weights, _ := a.store.Weights(userID, 365)
	for _, wrec := range weights {
		if wrec.Date == date {
			dv.HasWeight = true
			dv.WeightKg = wrec.WeightKg
			break
		}
	}
	a.render(w, "day", a.view(r, DateJP(date)+"の記録", dv))
}

// ---- 体重 ----

type weightView struct {
	Today       string
	Records     []WeightRecord
	Points      string
	MinLabel    string
	MaxLabel    string
	FirstDate   string
	LastDate    string
	Latest      *WeightRecord
	StartWeight float64
	HasDiff     bool
	Diff        float64
}

func (a *App) handleWeight(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	userID := ac.User.ID
	records, err := a.store.Weights(userID, 180)
	if err != nil {
		http.Error(w, "データの取得に失敗しました", http.StatusInternalServerError)
		return
	}
	wv := weightView{Today: todayJST(), Records: records}
	if len(records) > 0 {
		latest := records[0]
		wv.Latest = &latest
		// グラフ用に古い順へ
		asc := make([]WeightRecord, len(records))
		for i, rec := range records {
			asc[len(records)-1-i] = rec
		}
		min, max := asc[0].WeightKg, asc[0].WeightKg
		for _, rec := range asc {
			if rec.WeightKg < min {
				min = rec.WeightKg
			}
			if rec.WeightKg > max {
				max = rec.WeightKg
			}
		}
		if max-min < 2 {
			max = min + 2
		}
		const W, H, P = 720.0, 200.0, 28.0
		var pts []string
		for i, rec := range asc {
			x := P
			if len(asc) > 1 {
				x = P + (W-2*P)*float64(i)/float64(len(asc)-1)
			}
			y := H - P - (H-2*P)*(rec.WeightKg-min)/(max-min)
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, y))
		}
		wv.Points = strings.Join(pts, " ")
		wv.MinLabel = fmt.Sprintf("%.1f kg", min)
		wv.MaxLabel = fmt.Sprintf("%.1f kg", max)
		wv.FirstDate = asc[0].Date
		wv.LastDate = asc[len(asc)-1].Date
		wv.StartWeight = asc[0].WeightKg
		if len(asc) > 1 {
			wv.HasDiff = true
			wv.Diff = round1(latest.WeightKg - asc[0].WeightKg)
		}
	}
	a.render(w, "weight", a.view(r, "体重の記録", wv))
}

func (a *App) handleWeightSave(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	date := r.FormValue("date")
	kg, err := strconv.ParseFloat(r.FormValue("weight"), 64)
	if !validDate(date) || err != nil || kg < 20 || kg > 400 {
		http.Redirect(w, r, "/weight", http.StatusSeeOther)
		return
	}
	if err := a.store.SaveWeight(ac.User.ID, date, kg); err != nil {
		http.Error(w, "保存に失敗しました", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/weight?saved=1", http.StatusSeeOther)
}

func (a *App) handleWeightDelete(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	date := r.FormValue("date")
	if validDate(date) {
		_ = a.store.DeleteWeight(ac.User.ID, date)
	}
	http.Redirect(w, r, "/weight", http.StatusSeeOther)
}

// ---- 食品データベース ----

type foodsView struct {
	Query string
	Foods []Food
	Count int
}

func (a *App) handleFoods(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	foods := searchFoods(q)
	a.render(w, "foods", a.view(r, "食品データベース", foodsView{Query: q, Foods: foods, Count: len(foods)}))
}

func (a *App) handleAPIFoods(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	foods := searchFoods(q)
	if len(foods) > 30 {
		foods = foods[:30]
	}
	list := make([]jsFood, 0, len(foods))
	for _, f := range foods {
		list = append(list, jsFood{ID: f.ID, Name: f.Name, Category: f.Category, DefaultGrams: f.DefaultGrams, Per100: f.Per100(), Aliases: f.Aliases})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(list)
}

// ---- 設定 ----

type settingsView struct {
	Profile     *Profile
	Rec         *Recommendation
	Welcome     bool
	FormError   string
	OverrideVal string
}

func (a *App) settingsVM(userID int64) settingsView {
	sv := settingsView{}
	if p, err := a.store.Profile(userID); err == nil {
		rec := RecommendIntake(p)
		sv.Profile = &p
		sv.Rec = &rec
		if p.KcalGoalOverride != nil {
			sv.OverrideVal = strconv.FormatFloat(*p.KcalGoalOverride, 'f', 0, 64)
		}
	}
	return sv
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	sv := a.settingsVM(ac.User.ID)
	sv.Welcome = r.URL.Query().Get("welcome") == "1"
	a.render(w, "settings", a.view(r, "設定", sv))
}

func (a *App) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	fail := func(msg string) {
		sv := a.settingsVM(ac.User.ID)
		sv.FormError = msg
		a.render(w, "settings", a.view(r, "設定", sv))
	}
	age, err1 := strconv.Atoi(r.FormValue("age"))
	height, err2 := strconv.ParseFloat(r.FormValue("height_cm"), 64)
	weight, err3 := strconv.ParseFloat(r.FormValue("weight_kg"), 64)
	targetW, _ := strconv.ParseFloat(r.FormValue("target_weight_kg"), 64)
	sex := r.FormValue("sex")
	activity := r.FormValue("activity_level")
	purpose := r.FormValue("purpose")
	if err1 != nil || err2 != nil || err3 != nil || age < 10 || age > 120 ||
		height < 100 || height > 250 || weight < 20 || weight > 400 ||
		(sex != "male" && sex != "female") {
		fail("入力内容を確認してください（年齢・身長・体重は必須です）。")
		return
	}
	if _, ok := activityFactors[activity]; !ok {
		fail("活動レベルを選択してください。")
		return
	}
	if _, ok := purposeLabels[purpose]; !ok {
		fail("目的を選択してください。")
		return
	}
	p := Profile{
		UserID: ac.User.ID, Age: age, Sex: sex, HeightCm: height, WeightKg: weight,
		TargetWeightKg: targetW, ActivityLevel: activity, Purpose: purpose,
	}
	if ov := strings.TrimSpace(r.FormValue("kcal_goal_override")); ov != "" {
		v, err := strconv.ParseFloat(ov, 64)
		if err != nil || v < 500 || v > 10000 {
			fail("目標カロリーの上書きは500〜10000 kcalの範囲で入力してください。")
			return
		}
		p.KcalGoalOverride = &v
	}
	if err := a.store.SaveProfile(p); err != nil {
		http.Error(w, "保存に失敗しました", http.StatusInternalServerError)
		return
	}
	// 最新の体重として今日の体重記録も更新する
	_ = a.store.SaveWeight(ac.User.ID, todayJST(), weight)
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// ---- CSV出力 ----

func (a *App) handleExport(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	meals, err := a.store.AllMeals(ac.User.ID)
	if err != nil {
		http.Error(w, "出力に失敗しました", http.StatusInternalServerError)
		return
	}
	sort.Slice(meals, func(i, j int) bool {
		if meals[i].Date != meals[j].Date {
			return meals[i].Date < meals[j].Date
		}
		return meals[i].ID < meals[j].ID
	})
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="moglog-export.csv"`)
	_, _ = w.Write([]byte("\ufeff"))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"日付", "食事区分", "タイトル", "品目", "グラム", "kcal", "たんぱく質(g)", "脂質(g)", "炭水化物(g)", "食物繊維(g)", "食塩相当量(g)"})
	f1 := func(v float64) string { return strconv.FormatFloat(round1(v), 'f', 1, 64) }
	for _, m := range meals {
		for _, it := range m.Items {
			_ = cw.Write([]string{
				m.Date, mealTypeLabel(m.MealType), m.Title, it.Name,
				f1(it.Grams), f1(it.Nutrients.Kcal), f1(it.Nutrients.Protein), f1(it.Nutrients.Fat),
				f1(it.Nutrients.Carbs), f1(it.Nutrients.Fiber), f1(it.Nutrients.Salt),
			})
		}
	}
	cw.Flush()
}

// ---- 写真アップロード ----

func (a *App) handlePhotoUpload(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "ファイルが大きすぎます（10MBまで）", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("photo")
	if err != nil {
		http.Error(w, "写真が選択されていません", http.StatusBadRequest)
		return
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := io.ReadAtLeast(file, head, 512)
	ctype := http.DetectContentType(head[:n])
	var ext string
	switch ctype {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		http.Error(w, "対応していない画像形式です（JPEG / PNG / WebP / GIF）", http.StatusBadRequest)
		return
	}
	name := randomHex(16) + ext
	dir := filepath.Join(a.dataDir, "uploads", strconv.FormatInt(ac.User.ID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "保存に失敗しました", http.StatusInternalServerError)
		return
	}
	dst, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		http.Error(w, "保存に失敗しました", http.StatusInternalServerError)
		return
	}
	defer dst.Close()
	if _, err := dst.Write(head[:n]); err != nil {
		http.Error(w, "保存に失敗しました", http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "保存に失敗しました", http.StatusInternalServerError)
		return
	}
	target := "/meals/new?photo=" + name
	if d := r.FormValue("date"); validDate(d) {
		target += "&date=" + d
	}
	if t := r.FormValue("meal_type"); validMealType(t) {
		target += "&meal_type=" + t
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *App) handleUploadServe(w http.ResponseWriter, r *http.Request) {
	ac, _ := authFrom(r)
	name := r.PathValue("name")
	if !validPhotoName(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.dataDir, "uploads", strconv.FormatInt(ac.User.ID, 10), name)
	http.ServeFile(w, r, path)
}
