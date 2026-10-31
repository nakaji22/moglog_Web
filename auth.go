package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "moglog_session"
const sessionLifetime = 30 * 24 * time.Hour

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ---- ログイン試行のレート制限（メモリ内、同一メールアドレスあたり15分に10回まで） ----

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

var limiter = &loginLimiter{attempts: map[string][]time.Time{}}

func (l *loginLimiter) allow(email string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-15 * time.Minute)
	kept := l.attempts[email][:0]
	for _, t := range l.attempts[email] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.attempts[email] = kept
	return len(kept) < 10
}

func (l *loginLimiter) record(email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[email] = append(l.attempts[email], time.Now())
}

func (l *loginLimiter) reset(email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, email)
}

// ---- セッション ----

type authContext struct {
	User    User
	Session Session
}

func (a *App) startSession(w http.ResponseWriter, userID int64) error {
	token := randomHex(32)
	csrf := randomHex(16)
	if err := a.store.CreateSession(tokenHash(token), userID, csrf, time.Now().Add(sessionLifetime)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.cookieSecure,
		MaxAge:   int(sessionLifetime.Seconds()),
	})
	return nil
}

func (a *App) currentAuth(r *http.Request) (authContext, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return authContext{}, false
	}
	sess, user, err := a.store.SessionByTokenHash(tokenHash(c.Value))
	if err != nil {
		return authContext{}, false
	}
	return authContext{User: user, Session: sess}, true
}

func (a *App) endSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		_ = a.store.DeleteSession(tokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: a.cookieSecure, MaxAge: -1,
	})
}

// validEmail は簡易的なメールアドレス検証。
func validEmail(s string) bool {
	if len(s) < 3 || len(s) > 254 || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(s, "@")
	if at <= 0 || at == len(s)-1 {
		return false
	}
	return strings.Contains(s[at+1:], ".")
}

// ---- 認証ハンドラ ----

func (a *App) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentAuth(r); ok {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	a.render(w, "register", viewData{Title: "新規登録", V: map[string]any{"Email": ""}})
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "リクエストが不正です", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	password2 := r.FormValue("password_confirm")
	fail := func(msg string) {
		a.render(w, "register", viewData{Title: "新規登録", Error: msg, V: map[string]any{"Email": email}})
	}
	switch {
	case !validEmail(email):
		fail("メールアドレスの形式が正しくありません。")
		return
	case len(password) < 8:
		fail("パスワードは8文字以上で入力してください。")
		return
	case password != password2:
		fail("パスワード（確認）が一致しません。")
		return
	}
	if _, _, err := a.store.UserByEmail(email); err == nil {
		fail("このメールアドレスは既に登録されています。ログインしてください。")
		return
	}
	hash, err := hashPassword(password)
	if err != nil {
		http.Error(w, "サーバーエラー", http.StatusInternalServerError)
		return
	}
	id, err := a.store.CreateUser(email, hash)
	if err != nil {
		fail("このメールアドレスは既に登録されています。ログインしてください。")
		return
	}
	if err := a.startSession(w, id); err != nil {
		http.Error(w, "サーバーエラー", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings?welcome=1", http.StatusSeeOther)
}

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentAuth(r); ok {
		http.Redirect(w, r, "/home", http.StatusSeeOther)
		return
	}
	a.render(w, "login", viewData{Title: "ログイン", V: map[string]any{"Email": ""}})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "リクエストが不正です", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	fail := func(msg string) {
		a.render(w, "login", viewData{Title: "ログイン", Error: msg, V: map[string]any{"Email": email}})
	}
	if !limiter.allow(email) {
		fail("ログイン試行が多すぎます。15分ほど待ってから再度お試しください。")
		return
	}
	user, hash, err := a.store.UserByEmail(email)
	if err != nil || !checkPassword(hash, password) {
		limiter.record(email)
		fail("メールアドレスまたはパスワードが正しくありません。")
		return
	}
	limiter.reset(email)
	if err := a.startSession(w, user.ID); err != nil {
		http.Error(w, "サーバーエラー", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/home", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.endSession(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
