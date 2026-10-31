package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS profiles (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  age INTEGER NOT NULL,
  sex TEXT NOT NULL,
  height_cm REAL NOT NULL,
  weight_kg REAL NOT NULL,
  target_weight_kg REAL NOT NULL DEFAULT 0,
  activity_level TEXT NOT NULL,
  purpose TEXT NOT NULL,
  kcal_goal_override REAL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS meals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  date TEXT NOT NULL,
  meal_type TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual',
  photo_name TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_meals_user_date ON meals(user_id, date);
CREATE TABLE IF NOT EXISTS meal_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  meal_id INTEGER NOT NULL REFERENCES meals(id) ON DELETE CASCADE,
  food_id TEXT,
  name TEXT NOT NULL,
  grams REAL NOT NULL DEFAULT 0,
  kcal REAL NOT NULL DEFAULT 0,
  protein REAL NOT NULL DEFAULT 0,
  fat REAL NOT NULL DEFAULT 0,
  carbs REAL NOT NULL DEFAULT 0,
  fiber REAL NOT NULL DEFAULT 0,
  salt REAL NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS weight_records (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  date TEXT NOT NULL,
  weight_kg REAL NOT NULL,
  PRIMARY KEY (user_id, date)
);
`

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "uploads"), 0o755); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		filepath.Join(dataDir, "moglog.db"))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// ---- ユーザーとセッション ----

type User struct {
	ID    int64
	Email string
}

var ErrNotFound = errors.New("not found")

func (s *Store) CreateUser(email, passwordHash string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO users(email, password_hash, created_at) VALUES(?, ?, ?)`,
		email, passwordHash, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UserByEmail(email string) (User, string, error) {
	var u User
	var hash string
	err := s.db.QueryRow(`SELECT id, email, password_hash FROM users WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	return u, hash, err
}

func (s *Store) CreateSession(tokenHash string, userID int64, csrf string, expires time.Time) error {
	_, err := s.db.Exec(`INSERT INTO sessions(token_hash, user_id, csrf, created_at, expires_at) VALUES(?, ?, ?, ?, ?)`,
		tokenHash, userID, csrf, time.Now().Unix(), expires.Unix())
	return err
}

type Session struct {
	UserID int64
	CSRF   string
}

func (s *Store) SessionByTokenHash(tokenHash string) (Session, User, error) {
	var sess Session
	var u User
	err := s.db.QueryRow(`
		SELECT s.user_id, s.csrf, u.id, u.email FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, time.Now().Unix()).
		Scan(&sess.UserID, &sess.CSRF, &u.ID, &u.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, User{}, ErrNotFound
	}
	return sess, u, err
}

func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// ---- プロフィール ----

func (s *Store) Profile(userID int64) (Profile, error) {
	var p Profile
	var override sql.NullFloat64
	err := s.db.QueryRow(`
		SELECT user_id, age, sex, height_cm, weight_kg, target_weight_kg, activity_level, purpose, kcal_goal_override
		FROM profiles WHERE user_id = ?`, userID).
		Scan(&p.UserID, &p.Age, &p.Sex, &p.HeightCm, &p.WeightKg, &p.TargetWeightKg, &p.ActivityLevel, &p.Purpose, &override)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, err
	}
	if override.Valid {
		v := override.Float64
		p.KcalGoalOverride = &v
	}
	return p, nil
}

func (s *Store) SaveProfile(p Profile) error {
	var override any
	if p.KcalGoalOverride != nil {
		override = *p.KcalGoalOverride
	}
	_, err := s.db.Exec(`
		INSERT INTO profiles(user_id, age, sex, height_cm, weight_kg, target_weight_kg, activity_level, purpose, kcal_goal_override, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET age=excluded.age, sex=excluded.sex, height_cm=excluded.height_cm,
		  weight_kg=excluded.weight_kg, target_weight_kg=excluded.target_weight_kg,
		  activity_level=excluded.activity_level, purpose=excluded.purpose,
		  kcal_goal_override=excluded.kcal_goal_override, updated_at=excluded.updated_at`,
		p.UserID, p.Age, p.Sex, p.HeightCm, p.WeightKg, p.TargetWeightKg, p.ActivityLevel, p.Purpose, override, time.Now().Unix())
	return err
}

// ---- 食事 ----

type MealItem struct {
	ID        int64
	FoodID    string
	Name      string
	Grams     float64
	Nutrients Nutrients
}

type Meal struct {
	ID        int64
	UserID    int64
	Date      string
	MealType  string
	Title     string
	Note      string
	Source    string
	PhotoName string
	Items     []MealItem
	CreatedAt int64
}

func (m Meal) Totals() Nutrients {
	var t Nutrients
	for _, it := range m.Items {
		t = t.Add(it.Nutrients)
	}
	return t
}

func (s *Store) CreateMeal(m Meal) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO meals(user_id, date, meal_type, title, note, source, photo_name, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		m.UserID, m.Date, m.MealType, m.Title, m.Note, m.Source, m.PhotoName, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, it := range m.Items {
		if _, err := tx.Exec(`INSERT INTO meal_items(meal_id, food_id, name, grams, kcal, protein, fat, carbs, fiber, salt)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, nullStr(it.FoodID), it.Name, it.Grams, it.Nutrients.Kcal, it.Nutrients.Protein,
			it.Nutrients.Fat, it.Nutrients.Carbs, it.Nutrients.Fiber, it.Nutrients.Salt); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) mealsWhere(userID int64, where string, args ...any) ([]Meal, error) {
	q := `SELECT id, user_id, date, meal_type, title, note, source, photo_name, created_at FROM meals WHERE user_id = ? AND ` + where + ` ORDER BY date, CASE meal_type WHEN 'breakfast' THEN 0 WHEN 'lunch' THEN 1 WHEN 'dinner' THEN 2 ELSE 3 END, id`
	fullArgs := append([]any{userID}, args...)
	rows, err := s.db.Query(q, fullArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var meals []Meal
	ids := map[int64]int{}
	for rows.Next() {
		var m Meal
		if err := rows.Scan(&m.ID, &m.UserID, &m.Date, &m.MealType, &m.Title, &m.Note, &m.Source, &m.PhotoName, &m.CreatedAt); err != nil {
			return nil, err
		}
		ids[m.ID] = len(meals)
		meals = append(meals, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(meals) == 0 {
		return meals, nil
	}
	// アイテムをまとめて取得
	placeholders := ""
	itemArgs := []any{}
	for i, m := range meals {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		itemArgs = append(itemArgs, m.ID)
	}
	irows, err := s.db.Query(`SELECT id, meal_id, IFNULL(food_id,''), name, grams, kcal, protein, fat, carbs, fiber, salt
		FROM meal_items WHERE meal_id IN (`+placeholders+`) ORDER BY id`, itemArgs...)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	for irows.Next() {
		var it MealItem
		var mealID int64
		if err := irows.Scan(&it.ID, &mealID, &it.FoodID, &it.Name, &it.Grams,
			&it.Nutrients.Kcal, &it.Nutrients.Protein, &it.Nutrients.Fat, &it.Nutrients.Carbs, &it.Nutrients.Fiber, &it.Nutrients.Salt); err != nil {
			return nil, err
		}
		if idx, ok := ids[mealID]; ok {
			meals[idx].Items = append(meals[idx].Items, it)
		}
	}
	return meals, irows.Err()
}

func (s *Store) MealsOn(userID int64, date string) ([]Meal, error) {
	return s.mealsWhere(userID, `date = ?`, date)
}

func (s *Store) MealsInMonth(userID int64, monthPrefix string) ([]Meal, error) {
	return s.mealsWhere(userID, `date LIKE ?`, monthPrefix+"%")
}

func (s *Store) MealByID(userID, id int64) (Meal, error) {
	meals, err := s.mealsWhere(userID, `id = ?`, id)
	if err != nil {
		return Meal{}, err
	}
	if len(meals) == 0 {
		return Meal{}, ErrNotFound
	}
	return meals[0], nil
}

func (s *Store) UpdateMeal(m Meal) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE meals SET date = ?, meal_type = ?, title = ?, note = ?, source = ?, photo_name = ? WHERE id = ? AND user_id = ?`,
		m.Date, m.MealType, m.Title, m.Note, m.Source, m.PhotoName, m.ID, m.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM meal_items WHERE meal_id = ?`, m.ID); err != nil {
		return err
	}
	for _, it := range m.Items {
		if _, err := tx.Exec(`INSERT INTO meal_items(meal_id, food_id, name, grams, kcal, protein, fat, carbs, fiber, salt)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ID, nullStr(it.FoodID), it.Name, it.Grams, it.Nutrients.Kcal, it.Nutrients.Protein,
			it.Nutrients.Fat, it.Nutrients.Carbs, it.Nutrients.Fiber, it.Nutrients.Salt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteMeal(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM meals WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// DayTotal は指定日の合計カロリー等。
func (s *Store) DayTotals(userID int64, from, to string) (map[string]Nutrients, error) {
	rows, err := s.db.Query(`
		SELECT m.date, SUM(i.kcal), SUM(i.protein), SUM(i.fat), SUM(i.carbs), SUM(i.fiber), SUM(i.salt)
		FROM meals m JOIN meal_items i ON i.meal_id = m.id
		WHERE m.user_id = ? AND m.date >= ? AND m.date <= ?
		GROUP BY m.date`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Nutrients{}
	for rows.Next() {
		var d string
		var n Nutrients
		if err := rows.Scan(&d, &n.Kcal, &n.Protein, &n.Fat, &n.Carbs, &n.Fiber, &n.Salt); err != nil {
			return nil, err
		}
		out[d] = n
	}
	return out, rows.Err()
}

// FrequentFoods はユーザーがよく記録する食品（food_id あり）を集計する。
func (s *Store) FrequentFoods(userID int64, limit int) ([]FrequentFood, error) {
	rows, err := s.db.Query(`
		SELECT i.food_id, COUNT(*) FROM meal_items i
		JOIN meals m ON m.id = i.meal_id
		WHERE m.user_id = ? AND i.food_id IS NOT NULL AND i.food_id <> ''
		GROUP BY i.food_id ORDER BY COUNT(*) DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FrequentFood
	for rows.Next() {
		var fid string
		var cnt int
		if err := rows.Scan(&fid, &cnt); err != nil {
			return nil, err
		}
		if f, ok := getFood(fid); ok {
			out = append(out, FrequentFood{Food: f, Count: cnt})
		}
	}
	return out, rows.Err()
}

// AllMealsForExport はCSV出力用に全食事を日付順に返す。
func (s *Store) AllMeals(userID int64) ([]Meal, error) {
	return s.mealsWhere(userID, `1 = 1`)
}

// ---- 体重 ----

type WeightRecord struct {
	Date     string
	WeightKg float64
}

func (s *Store) SaveWeight(userID int64, date string, kg float64) error {
	_, err := s.db.Exec(`INSERT INTO weight_records(user_id, date, weight_kg) VALUES(?, ?, ?)
		ON CONFLICT(user_id, date) DO UPDATE SET weight_kg = excluded.weight_kg`, userID, date, kg)
	return err
}

func (s *Store) Weights(userID int64, limit int) ([]WeightRecord, error) {
	rows, err := s.db.Query(`SELECT date, weight_kg FROM weight_records WHERE user_id = ? ORDER BY date DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WeightRecord
	for rows.Next() {
		var w WeightRecord
		if err := rows.Scan(&w.Date, &w.WeightKg); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) DeleteWeight(userID int64, date string) error {
	_, err := s.db.Exec(`DELETE FROM weight_records WHERE user_id = ? AND date = ?`, userID, date)
	return err
}
