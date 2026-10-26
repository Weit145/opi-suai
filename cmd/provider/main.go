// This local simulator never performs real payments, sends mail, or contacts delivery companies.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Weit145/opi-suai/internal/app"
	"github.com/jackc/pgx/v5/pgxpool"
)

type provider struct {
	db               *pgxpool.Pool
	secret, callback string
	client           *http.Client
}

func main() {
	if e := run(); e != nil {
		slog.Error("provider stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	if os.Getenv("DEMO_MODE") != "true" {
		return fmt.Errorf("provider simulator requires DEMO_MODE=true")
	}
	secret := os.Getenv("INTEGRATION_SECRET")
	if len(secret) < 32 {
		return fmt.Errorf("integration secret too short")
	}
	db, e := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if e != nil {
		return e
	}
	defer db.Close()
	callback := os.Getenv("APP_URL")
	if callback == "" {
		callback = "http://127.0.0.1:8090"
	}
	p := &provider{db, secret, callback, &http.Client{Timeout: 10 * time.Second}}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8091"
	}
	server := &http.Server{Addr: addr, Handler: http.HandlerFunc(p.serve), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 25 * time.Second}
	return server.ListenAndServe()
}
func (p *provider) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.Write([]byte("ok"))
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil || !app.Verify(p.secret, r, b) {
		http.Error(w, "invalid signature", 401)
		return
	}
	var data map[string]any
	if json.Unmarshal(b, &data) != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	out, e := p.dispatch(r.Context(), r.URL.Path, data)
	if e != nil {
		slog.Warn("simulator operation failed", "path", r.URL.Path, "error", e)
		http.Error(w, "simulator rejected operation", 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func (p *provider) record(ctx context.Context, kind, key string, data map[string]any) (map[string]any, error) {
	if key == "" {
		return nil, fmt.Errorf("missing key")
	}
	b, _ := json.Marshal(data)
	_, e := p.db.Exec(ctx, "INSERT INTO provider_records(kind,request_key,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", kind, key, b)
	if e != nil {
		return nil, e
	}
	return p.get(ctx, kind, key)
}
func (p *provider) get(ctx context.Context, kind, key string) (map[string]any, error) {
	var b []byte
	e := p.db.QueryRow(ctx, "SELECT payload FROM provider_records WHERE kind=$1 AND request_key=$2", kind, key).Scan(&b)
	if e != nil {
		return nil, e
	}
	var m map[string]any
	e = json.Unmarshal(b, &m)
	return m, e
}
func (p *provider) dispatch(ctx context.Context, path string, d map[string]any) (any, error) {
	switch path {
	case "/payments":
		amount, ok := d["amount"].(float64)
		if !ok || amount <= 0 || str(d, "currency") != "RUB" {
			return nil, fmt.Errorf("invalid amount")
		}
		key := str(d, "order_id")
		d["id"] = "pay-" + key
		d["status"] = "created"
		out, e := p.record(ctx, "payment", key, d)
		if e == nil && (out["amount"] != d["amount"] || out["currency"] != d["currency"]) {
			return nil, fmt.Errorf("idempotency mismatch")
		}
		return out, e
	case "/payments/confirm", "/refunds":
		key := strings.TrimPrefix(str(d, "id"), "pay-")
		out, e := p.get(ctx, "payment", key)
		if e != nil {
			return nil, e
		}
		kind := "paid"
		if path == "/refunds" {
			kind = "refunded"
			if out["status"] != "paid" && out["status"] != "refunded" {
				return nil, fmt.Errorf("payment not captured")
			}
		}
		event := app.PaymentEvent{EventID: str(out, "id") + "-" + kind, PaymentID: str(out, "id"), OrderID: key, Amount: int64(out["amount"].(float64)), Currency: "RUB", Kind: kind}
		if e = p.callbackEvent(ctx, event); e != nil {
			return nil, e
		}
		if out["status"] != "refunded" {
			out["status"] = kind
			b, _ := json.Marshal(out)
			_, e = p.db.Exec(ctx, "UPDATE provider_records SET payload=$3 WHERE kind=$1 AND request_key=$2", "payment", key, b)
		}
		return out, e
	case "/shipments":
		key := str(d, "order_id")
		if str(d, "address") == "" {
			return nil, fmt.Errorf("missing address")
		}
		d["id"] = "shp-" + key
		d["tracking"] = "LAB-" + strings.ToUpper(strings.ReplaceAll(key, "-", ""))[:12]
		d["status"] = "shipped"
		return p.record(ctx, "shipment", key, d)
	case "/shipments/status", "/shipments/deliver":
		key := strings.TrimPrefix(str(d, "id"), "shp-")
		out, e := p.get(ctx, "shipment", key)
		if e != nil {
			return nil, e
		}
		if path == "/shipments/deliver" {
			out["status"] = "delivered"
			b, _ := json.Marshal(out)
			_, e = p.db.Exec(ctx, "UPDATE provider_records SET payload=$3 WHERE kind=$1 AND request_key=$2", "shipment", key, b)
		}
		return out, e
	case "/notifications":
		return p.record(ctx, "notification", str(d, "id"), d)
	default:
		return nil, fmt.Errorf("unknown operation")
	}
}
func (p *provider) callbackEvent(ctx context.Context, event app.PaymentEvent) error {
	b, _ := json.Marshal(event)
	req, e := http.NewRequestWithContext(ctx, "POST", p.callback+"/api/webhooks/payment", bytes.NewReader(b))
	if e != nil {
		return e
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", stamp)
	req.Header.Set("X-Signature", app.Sign(p.secret, stamp, b))
	resp, e := p.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("callback status %d", resp.StatusCode)
	}
	return nil
}
