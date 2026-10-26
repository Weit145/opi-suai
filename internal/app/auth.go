package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Weit145/opi-suai/internal/domain"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type credentials struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=10,max=72"`
	Name     string `json:"name" validate:"required,max=120"`
	Role     string `json:"role"`
}

func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }
func (s *Server) user(ctx context.Context, id string) (domain.User, error) {
	var u domain.User
	e := s.DB.QueryRow(ctx, "SELECT id,email,name,role,active FROM users WHERE id=$1", id).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active)
	return u, e
}
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			write(w, 401, map[string]string{"error": "Необходим вход"})
			return
		}
		t, e := jwt.ParseWithClaims(strings.TrimPrefix(h, "Bearer "), &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) { return []byte(s.C.JWTSecret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("opi-pc-shop"), jwt.WithExpirationRequired())
		if e != nil || !t.Valid {
			write(w, 401, map[string]string{"error": "Сессия истекла"})
			return
		}
		id, _ := t.Claims.GetSubject()
		u, e := s.user(r.Context(), id)
		if e != nil || !u.Active {
			write(w, 401, map[string]string{"error": "Учётная запись недоступна"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, u)))
	})
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	var c credentials
	if e := body(r, &c); e != nil {
		return e
	}
	c.Role = "customer"
	u, e := s.addUser(r.Context(), c)
	if e != nil {
		return e
	}
	return s.session(w, r, u)
}
func (s *Server) addUser(ctx context.Context, c credentials) (domain.User, error) {
	c.Email = strings.ToLower(trim(c.Email))
	c.Name = trim(c.Name)
	if s.V.Struct(c) != nil || !domain.Roles[c.Role] {
		return domain.User{}, bad("Укажите имя, корректный email и пароль длиной от 10 до 72 символов")
	}
	h, e := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if e != nil {
		return domain.User{}, e
	}
	u := domain.User{ID: uuid.NewString(), Name: c.Name, Email: c.Email, Role: c.Role, Active: true}
	_, e = s.DB.Exec(ctx, "INSERT INTO users(id,email,name,password_hash,role) VALUES($1,$2,$3,$4,$5)", u.ID, u.Email, u.Name, string(h), u.Role)
	return u, e
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	var id, h string
	var active bool
	e := s.DB.QueryRow(r.Context(), "SELECT id,password_hash,active FROM users WHERE email=$1", strings.ToLower(trim(c.Email))).Scan(&id, &h, &active)
	if e != nil || !active || bcrypt.CompareHashAndPassword([]byte(h), []byte(c.Password)) != nil {
		return problem{401, "Неверный email или пароль"}
	}
	u, e := s.user(r.Context(), id)
	if e != nil {
		return e
	}
	return s.session(w, r, u)
}
func (s *Server) session(w http.ResponseWriter, r *http.Request, u domain.User) error {
	now := time.Now()
	claims := jwt.RegisteredClaims{Issuer: "opi-pc-shop", Subject: u.ID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute))}
	access, e := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.C.JWTSecret))
	if e != nil {
		return e
	}
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		return e
	}
	token := hex.EncodeToString(b)
	expires := now.Add(7 * 24 * time.Hour)
	_, e = s.DB.Exec(r.Context(), "INSERT INTO refresh_tokens(token_hash,user_id,expires_at) VALUES($1,$2,$3)", hashToken(token), u.ID, expires)
	if e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: "opi_refresh", Value: token, Path: "/api/auth", HttpOnly: true, Secure: s.C.SecureCookies, SameSite: http.SameSiteStrictMode, Expires: expires})
	write(w, 200, map[string]any{"access_token": access, "user": u})
	return nil
}
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) error {
	c, e := r.Cookie("opi_refresh")
	if e != nil {
		return problem{401, "Нет активной сессии"}
	}
	var id string
	e = s.DB.QueryRow(r.Context(), "DELETE FROM refresh_tokens WHERE token_hash=$1 AND expires_at>now() RETURNING user_id", hashToken(c.Value)).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return problem{401, "Сессия истекла"}
	}
	if e != nil {
		return e
	}
	u, e := s.user(r.Context(), id)
	if e != nil || !u.Active {
		return problem{401, "Учётная запись недоступна"}
	}
	return s.session(w, r, u)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	if c, e := r.Cookie("opi_refresh"); e == nil {
		if _, e = s.DB.Exec(r.Context(), "DELETE FROM refresh_tokens WHERE token_hash=$1", hashToken(c.Value)); e != nil {
			return e
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "opi_refresh", Value: "", Path: "/api/auth", HttpOnly: true, Secure: s.C.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	write(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) users(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r) {
		return forbidden()
	}
	return s.json(w, r, "SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT id,name,email,role,active FROM users ORDER BY created_at) t")
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r) {
		return forbidden()
	}
	var c credentials
	if e := body(r, &c); e != nil {
		return e
	}
	u, e := s.addUser(r.Context(), c)
	if e != nil {
		return e
	}
	write(w, 201, u)
	return nil
}
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r) {
		return forbidden()
	}
	id := chi.URLParam(r, "id")
	var c struct {
		Role   string `json:"role"`
		Active bool   `json:"active"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	if !domain.Roles[c.Role] {
		return bad("Неизвестная роль")
	}
	if id == actor(r).ID && (c.Role != "admin" || !c.Active) {
		return bad("Нельзя отключить собственную административную запись")
	}
	tag, e := s.DB.Exec(r.Context(), "UPDATE users SET role=$2,active=$3 WHERE id=$1", id, c.Role, c.Active)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	write(w, 200, map[string]bool{"ok": true})
	return nil
}
