// もぐログ Web版
// Go + SQLite で動く食事管理Webアプリ。サーバーサイドレンダリング。
package main

import (
	"context"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type App struct {
	store        *Store
	dataDir      string
	cookieSecure bool
	tmpls        map[string]*template.Template
}

type ctxKey string

const authCtxKey ctxKey = "auth"

func authFrom(r *http.Request) (authContext, bool) {
	v, ok := r.Context().Value(authCtxKey).(authContext)
	return v, ok
}

// loadAuth はセッションがあればコンテキストに入れる（ブロックしない）。
func (a *App) loadAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ac, ok := a.currentAuth(r); ok {
			r = r.WithContext(context.WithValue(r.Context(), authCtxKey, ac))
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth は未ログインを /login へ誘導し、ログイン済みPOSTのCSRFを検証する。
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ac, ok := authFrom(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			// multipart の場合は ParseForm だけでは本文が読まれないため、先に ParseMultipartForm を呼ぶ
			var err error
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				err = r.ParseMultipartForm(32 << 20)
			} else {
				err = r.ParseForm()
			}
			if err != nil {
				http.Error(w, "リクエストが不正です", http.StatusBadRequest)
				return
			}
			if r.FormValue("_csrf") != ac.Session.CSRF {
				http.Error(w, "セッションの有効期限が切れた可能性があります。画面を再読み込みしてください。", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

// view はログイン中ユーザー情報を埋めた viewData を作る。
func (a *App) view(r *http.Request, title string, v any) viewData {
	vd := viewData{Title: title, V: v}
	if ac, ok := authFrom(r); ok {
		u := ac.User
		vd.User = &u
		vd.CSRF = ac.Session.CSRF
	}
	if r.URL.Query().Get("saved") == "1" {
		vd.Flash = "保存しました。"
	}
	return vd
}

func main() {
	loadCatalog()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	store, err := OpenStore(dataDir)
	if err != nil {
		log.Fatalf("データベースを開けません: %v", err)
	}
	tmpls, err := loadTemplates()
	if err != nil {
		log.Fatalf("%v", err)
	}
	app := &App{
		store:        store,
		dataDir:      dataDir,
		cookieSecure: os.Getenv("COOKIE_SECURE") == "1",
		tmpls:        tmpls,
	}

	mux := http.NewServeMux()

	// 静的ファイル
	staticSub, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		log.Fatalf("静的ファイルの読み込みに失敗: %v", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	// 公開ページ
	mux.HandleFunc("GET /", app.handleLanding)
	mux.HandleFunc("GET /register", app.handleRegisterPage)
	mux.HandleFunc("POST /register", app.handleRegister)
	mux.HandleFunc("GET /login", app.handleLoginPage)
	mux.HandleFunc("POST /login", app.handleLogin)
	mux.HandleFunc("POST /logout", app.requireAuth(app.handleLogout))

	// ログイン必須ページ
	mux.HandleFunc("GET /home", app.requireAuth(app.handleHome))
	mux.HandleFunc("GET /meals/new", app.requireAuth(app.handleMealNew))
	mux.HandleFunc("POST /meals", app.requireAuth(app.handleMealCreate))
	mux.HandleFunc("GET /meals/{id}/edit", app.requireAuth(app.handleMealEdit))
	mux.HandleFunc("POST /meals/{id}", app.requireAuth(app.handleMealUpdate))
	mux.HandleFunc("POST /meals/{id}/delete", app.requireAuth(app.handleMealDelete))
	mux.HandleFunc("GET /history", app.requireAuth(app.handleHistory))
	mux.HandleFunc("GET /history/{date}", app.requireAuth(app.handleDay))
	mux.HandleFunc("GET /weight", app.requireAuth(app.handleWeight))
	mux.HandleFunc("POST /weight", app.requireAuth(app.handleWeightSave))
	mux.HandleFunc("POST /weight/delete", app.requireAuth(app.handleWeightDelete))
	mux.HandleFunc("GET /foods", app.requireAuth(app.handleFoods))
	mux.HandleFunc("GET /settings", app.requireAuth(app.handleSettings))
	mux.HandleFunc("POST /settings", app.requireAuth(app.handleSettingsSave))
	mux.HandleFunc("GET /export.csv", app.requireAuth(app.handleExport))
	mux.HandleFunc("POST /photos", app.requireAuth(app.handlePhotoUpload))
	mux.HandleFunc("GET /uploads/{name}", app.requireAuth(app.handleUploadServe))
	mux.HandleFunc("GET /api/foods", app.requireAuth(app.handleAPIFoods))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           app.loadAuth(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("もぐログ Web版を起動しました: http://localhost:%s (DATA_DIR=%s)", port, dataDir)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("サーバーが停止しました: %v", err)
	}
}

// safeRedirect はアプリ内パスのみリダイレクト先として許可する。
func safeRedirect(path, fallback string) string {
	if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
		return path
	}
	return fallback
}
