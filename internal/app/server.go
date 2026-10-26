package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Weit145/opi-suai/internal/domain"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	DatabaseURL       string `env:"DATABASE_URL" env-required:"true"`
	JWTSecret         string `env:"JWT_SECRET" env-required:"true"`
	IntegrationSecret string `env:"INTEGRATION_SECRET" env-required:"true"`
	ProviderURL       string `env:"PROVIDER_URL" env-default:"http://127.0.0.1:8091"`
	PublicOrigin      string `env:"PUBLIC_ORIGIN" env-default:"http://localhost:8090"`
	Addr              string `env:"ADDR" env-default:"127.0.0.1:8090"`
	SecureCookies     bool   `env:"SECURE_COOKIES" env-default:"true"`
	Demo              bool   `env:"DEMO_MODE" env-default:"false"`
}
type Server struct {
	DB     *pgxpool.Pool
	C      Config
	V      *validator.Validate
	Client *http.Client
}
type problem struct {
	Status  int
	Message string
}

func (e problem) Error() string { return e.Message }
func bad(s string) error        { return problem{400, s} }
func conflict(s string) error   { return problem{409, s} }
func forbidden() error          { return problem{403, "Недостаточно прав"} }

type actorKey struct{}

func actor(r *http.Request) domain.User {
	u, _ := r.Context().Value(actorKey{}).(domain.User)
	return u
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func body(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad("Некорректный JSON запроса")
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return bad("Ожидается один JSON-объект")
	}
	return nil
}

type endpoint func(http.ResponseWriter, *http.Request) error

func (s *Server) handle(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if e := fn(w, r); e != nil {
			status, msg := 500, "Внутренняя ошибка сервера"
			var p problem
			var pg *pgconn.PgError
			switch {
			case errors.As(e, &p):
				status, msg = p.Status, p.Message
			case errors.Is(e, pgx.ErrNoRows):
				status, msg = 404, "Объект не найден"
			case errors.As(e, &pg) && (pg.Code == "23505" || pg.Code == "23503" || pg.Code == "23514" || pg.Code == "22P02"):
				status, msg = 409, "Данные конфликтуют с существующей записью или ограничением"
			default:
				slog.Error("request failed", "request_id", middleware.GetReqID(r.Context()), "error", e.Error())
			}
			write(w, status, map[string]any{"error": msg, "request_id": middleware.GetReqID(r.Context())})
		}
	}
}
func New(db *pgxpool.Pool, c Config) *Server {
	return &Server{db, c, validator.New(), &http.Client{Timeout: 10 * time.Second}}
}
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, middleware.Timeout(30*time.Second))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
			if origin := r.Header.Get("Origin"); origin != "" && origin != s.C.PublicOrigin {
				write(w, 403, map[string]string{"error": "Источник запроса не разрешён"})
				return
			}
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			slog.Info("http", "request_id", middleware.GetReqID(r.Context()), "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "duration_ms", time.Since(start).Milliseconds())
		})
	})
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", s.handle(func(w http.ResponseWriter, r *http.Request) error {
		if e := s.DB.Ping(r.Context()); e != nil {
			return problem{503, "База данных недоступна"}
		}
		write(w, 200, map[string]string{"status": "ready"})
		return nil
	}))
	r.Route("/api", func(r chi.Router) {
		r.Get("/info", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]bool{"demo": s.C.Demo}) })
		r.Post("/auth/register", s.handle(s.register))
		r.Post("/auth/login", s.handle(s.login))
		r.Post("/auth/refresh", s.handle(s.refresh))
		r.Post("/auth/logout", s.handle(s.logout))
		r.Get("/products", s.handle(s.products))
		r.Get("/products/{id}", s.handle(s.product))
		r.Get("/categories", s.handle(s.categories))
		r.Post("/compatibility", s.handle(s.compatibility))
		r.Post("/webhooks/payment", s.handle(s.paymentWebhook))
		r.Group(func(r chi.Router) {
			r.Use(s.auth)
			r.Get("/me", func(w http.ResponseWriter, r *http.Request) { write(w, 200, actor(r)) })
			r.Get("/cart", s.handle(s.cart))
			r.Put("/cart", s.handle(s.saveCart))
			r.Get("/configuration", s.handle(s.configuration))
			r.Put("/configuration", s.handle(s.saveConfiguration))
			r.Get("/orders", s.handle(s.orders))
			r.Post("/orders", s.handle(s.checkout))
			r.Get("/orders/{id}", s.handle(s.orderDetail))
			r.Post("/orders/{id}/action", s.handle(s.orderAction))
			r.Post("/orders/{id}/payment", s.handle(s.createPayment))
			r.Post("/orders/{id}/cancel", s.handle(s.cancel))
			r.Post("/orders/{id}/shipment", s.handle(s.createShipment))
			r.Post("/orders/{id}/shipment/sync", s.handle(s.syncShipment))
			r.Post("/orders/{id}/demo-payment", s.handle(s.demoPayment))
			r.Post("/orders/{id}/demo-delivery", s.handle(s.demoDelivery))
			r.Get("/notifications", s.handle(s.notifications))
			r.Get("/inventory", s.handle(s.inventory))
			r.Post("/inventory/movement", s.handle(s.movement))
			r.Post("/orders/{id}/stock-return", s.handle(s.stockReturn))
			r.Get("/staff/products", s.handle(s.staffProducts))
			r.Post("/staff/products", s.handle(s.saveProduct))
			r.Post("/staff/categories", s.handle(s.saveCategory))
			r.Get("/users", s.handle(s.users))
			r.Post("/users", s.handle(s.createUser))
			r.Patch("/users/{id}", s.handle(s.updateUser))
		})
	})
	return r
}
func allowed(r *http.Request, roles ...string) bool {
	a := actor(r)
	if a.Role == "admin" {
		return true
	}
	for _, v := range roles {
		if a.Role == v {
			return true
		}
	}
	return false
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func jsonQuery(ctx context.Context, q queryer, sql string, args ...any) (json.RawMessage, error) {
	var v json.RawMessage
	e := q.QueryRow(ctx, sql, args...).Scan(&v)
	return v, e
}
func (s *Server) json(w http.ResponseWriter, r *http.Request, sql string, args ...any) error {
	v, e := jsonQuery(r.Context(), s.DB, sql, args...)
	if e != nil {
		return e
	}
	write(w, 200, v)
	return nil
}
func (s *Server) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func requireID(id string) error {
	if len(id) != 36 {
		return bad("Некорректный идентификатор")
	}
	return nil
}
func trim(v string) string { return strings.TrimSpace(v) }
func validConfig(c Config) error {
	if len(c.JWTSecret) < 32 || len(c.IntegrationSecret) < 32 {
		return fmt.Errorf("secrets must contain at least 32 characters")
	}
	return nil
}
