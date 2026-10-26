package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Weit145/opi-suai/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	s        *Server
	http     *httptest.Server
	tokens   map[string]string
	products []domain.Product
	t        *testing.T
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL acceptance tests")
	}
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("acceptance tests require an explicitly isolated *_test database")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	name := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+name); e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	c := Config{JWTSecret: strings.Repeat("j", 48), IntegrationSecret: strings.Repeat("i", 48), Demo: true}
	s := New(db, c)
	if e = s.Initialize(ctx, true, "acceptance-password-123"); e != nil {
		t.Fatal(e)
	}
	h := httptest.NewServer(s.Routes())
	f := &fixture{s: s, http: h, tokens: map[string]string{}, t: t}
	t.Cleanup(func() { h.Close(); db.Close(); admin.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); admin.Close() })
	for _, role := range []string{"customer", "manager", "warehouse", "assembler", "admin"} {
		code, b := f.req("POST", "/api/auth/login", "", map[string]string{"email": role + "@lab.invalid", "password": "acceptance-password-123"})
		if code != 200 {
			t.Fatalf("login %d %s", code, b)
		}
		var out struct {
			Token string `json:"access_token"`
		}
		json.Unmarshal(b, &out)
		f.tokens[role] = out.Token
	}
	code, b := f.req("GET", "/api/products", "", nil)
	if code != 200 {
		t.Fatalf("catalog %d %s", code, b)
	}
	json.Unmarshal(b, &f.products)
	return f
}
func (f *fixture) req(method, path, role string, input any) (int, []byte) {
	f.t.Helper()
	b, _ := json.Marshal(input)
	r, _ := http.NewRequest(method, f.http.URL+path, bytes.NewReader(b))
	if role != "" {
		r.Header.Set("Authorization", "Bearer "+f.tokens[role])
	}
	r.Header.Set("Content-Type", "application/json")
	resp, e := f.http.Client().Do(r)
	if e != nil {
		f.t.Fatal(e)
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(resp.Body)
	if e != nil {
		f.t.Fatal(e)
	}
	return resp.StatusCode, data
}
func (f *fixture) checkout(p []domain.Product, assembly bool) (int, []byte) {
	items := []domain.Item{}
	for _, x := range p {
		items = append(items, domain.Item{ProductID: x.ID, Quantity: 1, ExpectedPrice: x.Price})
	}
	return f.req("POST", "/api/orders", "customer", map[string]any{"items": items, "assembly": assembly, "receiving": "pickup", "contact_name": "Приёмочный тест", "phone": "+70000000000", "checkout_key": uuid.NewString()})
}
func (f *fixture) newOrder(p []domain.Product, assembly bool) string {
	f.t.Helper()
	status, b := f.checkout(p, assembly)
	if status != 201 {
		f.t.Fatalf("checkout %d %s", status, b)
	}
	var m map[string]string
	json.Unmarshal(b, &m)
	return m["id"]
}
func (f *fixture) pay(id, kind, eventID string) (int, []byte) {
	var amount int64
	f.s.DB.QueryRow(context.Background(), "SELECT total FROM orders WHERE id=$1", id).Scan(&amount)
	pid := "pay-" + id
	_, e := f.s.DB.Exec(context.Background(), "INSERT INTO payments(order_id,provider_id,amount) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", id, pid, amount)
	if e != nil {
		f.t.Fatal(e)
	}
	b, _ := json.Marshal(PaymentEvent{EventID: eventID, PaymentID: pid, OrderID: id, Amount: amount, Currency: "RUB", Kind: kind})
	r, _ := http.NewRequest("POST", f.http.URL+"/api/webhooks/payment", bytes.NewReader(b))
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	r.Header.Set("X-Timestamp", stamp)
	r.Header.Set("X-Signature", Sign(f.s.C.IntegrationSecret, stamp, b))
	resp, e := f.http.Client().Do(r)
	if e != nil {
		f.t.Fatal(e)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}
func (f *fixture) expectAction(id, role, action string, want int) {
	f.t.Helper()
	code, b := f.req("POST", "/api/orders/"+id+"/action", role, map[string]string{"action": action, "note": "Проверка"})
	if code != want {
		f.t.Fatalf("%s/%s: expected %d got %d %s", role, action, want, code, b)
	}
}

func TestConcurrentLastItemReservation(t *testing.T) {
	f := setup(t)
	p := f.products[0]
	f.s.DB.Exec(context.Background(), "UPDATE stock SET available=1 WHERE product_id=$1", p.ID)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); code, _ := f.checkout([]domain.Product{p}, false); codes <- code }()
	}
	wg.Wait()
	close(codes)
	got := []int{}
	for c := range codes {
		got = append(got, c)
	}
	sort.Ints(got)
	if fmt.Sprint(got) != "[201 409]" {
		t.Fatalf("expected exactly one reservation: %v", got)
	}
	var available, reserved int
	f.s.DB.QueryRow(context.Background(), "SELECT available,reserved FROM stock WHERE product_id=$1", p.ID).Scan(&available, &reserved)
	if available != 0 || reserved != 1 {
		t.Fatalf("incorrect stock: %d %d", available, reserved)
	}
}
func TestPriceSnapshotsAndAtomicRollback(t *testing.T) {
	f := setup(t)
	a, b := f.products[0], f.products[1]
	f.s.DB.Exec(context.Background(), "UPDATE stock SET available=0 WHERE product_id=$1", b.ID)
	code, _ := f.checkout([]domain.Product{a, b}, false)
	if code != 409 {
		t.Fatalf("expected shortage, got %d", code)
	}
	var reserved int
	f.s.DB.QueryRow(context.Background(), "SELECT reserved FROM stock WHERE product_id=$1", a.ID).Scan(&reserved)
	if reserved != 0 {
		t.Fatal("partial reservation survived rollback")
	}
	id := f.newOrder([]domain.Product{a}, false)
	f.s.DB.Exec(context.Background(), "UPDATE products SET price=price+10000 WHERE id=$1", a.ID)
	var price int64
	f.s.DB.QueryRow(context.Background(), "SELECT price FROM order_items WHERE order_id=$1", id).Scan(&price)
	if price != a.Price {
		t.Fatal("historical price changed")
	}
	code, _ = f.checkout([]domain.Product{a}, false)
	if code != 409 {
		t.Fatalf("unconfirmed changed price accepted: %d", code)
	}
}
func TestPaymentReplayAndAuthorization(t *testing.T) {
	f := setup(t)
	id := f.newOrder([]domain.Product{f.products[0]}, false)
	for i := 0; i < 2; i++ {
		code, b := f.pay(id, "paid", "unique-paid-event")
		if code != 200 {
			t.Fatalf("payment %d %s", code, b)
		}
	}
	var count int
	f.s.DB.QueryRow(context.Background(), "SELECT count(*) FROM order_events WHERE order_id=$1 AND status='paid'", id).Scan(&count)
	if count != 1 {
		t.Fatalf("duplicate payment effect %d", count)
	}
	f.expectAction(id, "customer", "pick", 409)
	f.expectAction(id, "warehouse", "pick", 200)
	f.expectAction(id, "warehouse", "picked", 200)
	f.expectAction(id, "manager", "pickup", 200)
	for _, path := range []string{"/api/users", "/api/inventory", "/api/staff/products"} {
		code, _ := f.req("GET", path, "customer", nil)
		if code != 403 {
			t.Fatalf("customer may access %s: %d", path, code)
		}
	}
	code, _ := f.req("POST", "/api/webhooks/payment", "", PaymentEvent{})
	if code != 401 {
		t.Fatal("unsigned webhook accepted")
	}
	code, _ = f.req("POST", "/api/auth/register", "", map[string]string{"email": "other@lab.invalid", "name": "Другой покупатель", "password": "other-password-123"})
	if code != 200 {
		t.Fatal(code)
	}
	code, b := f.req("POST", "/api/auth/login", "", map[string]string{"email": "other@lab.invalid", "password": "other-password-123"})
	var token struct {
		Token string `json:"access_token"`
	}
	json.Unmarshal(b, &token)
	f.tokens["other"] = token.Token
	code, _ = f.req("GET", "/api/orders/"+id, "other", nil)
	if code != 403 {
		t.Fatal("another customer's order is exposed")
	}
}
func TestAssemblyFailureRefundAndInspectedReturn(t *testing.T) {
	f := setup(t)
	products := []domain.Product{}
	for _, p := range f.products {
		if strings.Contains(p.Name, "A6") || strings.Contains(p.Name, "A620") || p.Category == "ram" && p.Specs["memory_type"] == "DDR5" || p.Category == "storage" || p.Category == "psu" && p.Specs["power_w"] == "650" || p.Category == "case" && strings.Contains(p.Name, "Compact") {
			products = append(products, p)
		}
	}
	id := f.newOrder(products, true)
	code, b := f.pay(id, "paid", "assembly-paid")
	if code != 200 {
		t.Fatalf("pay %d %s", code, b)
	}
	f.pay(id, "paid", "assembly-paid")
	var count int
	f.s.DB.QueryRow(context.Background(), "SELECT count(*) FROM assembly_tasks WHERE order_id=$1", id).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate assembly task")
	}
	f.expectAction(id, "warehouse", "pick", 200)
	f.expectAction(id, "warehouse", "picked", 200)
	f.expectAction(id, "assembler", "assemble", 200)
	f.expectAction(id, "assembler", "pass", 409)
	f.expectAction(id, "assembler", "test", 200)
	f.expectAction(id, "assembler", "fail", 200)
	code, _ = f.req("POST", "/api/orders/"+id+"/cancel", "customer", nil)
	if code != 403 {
		t.Fatal("customer cancelled after assembly start")
	}
	code, _ = f.req("POST", "/api/orders/"+id+"/cancel", "manager", nil)
	if code != 200 {
		t.Fatal("manager cancellation failed")
	}
	var available int
	f.s.DB.QueryRow(context.Background(), "SELECT available FROM stock WHERE product_id=$1", products[0].ID).Scan(&available)
	if available != 99 {
		t.Fatal("consumed stock was returned without inspection")
	}
	code, _ = f.pay(id, "refunded", "refund-event")
	if code != 200 {
		t.Fatal("refund not applied")
	}
	code, _ = f.req("POST", "/api/orders/"+id+"/stock-return", "warehouse", map[string]string{"note": "Склад проверил комплектность и исправность"})
	if code != 200 {
		t.Fatal("inspected return failed")
	}
	f.s.DB.QueryRow(context.Background(), "SELECT available FROM stock WHERE product_id=$1", products[0].ID).Scan(&available)
	if available != 100 {
		t.Fatal("stock not restored")
	}
	code, _ = f.req("POST", "/api/orders/"+id+"/stock-return", "warehouse", map[string]string{"note": "Повтор"})
	if code != 409 {
		t.Fatal("duplicate stock return accepted")
	}
}
func TestCompatibilityRejectsConflict(t *testing.T) {
	f := setup(t)
	ids := []string{}
	for _, p := range f.products {
		if strings.Contains(p.Name, "I6") || strings.Contains(p.Name, "A620") {
			ids = append(ids, p.ID)
		}
	}
	code, b := f.req("POST", "/api/compatibility", "", map[string]any{"ids": ids, "complete": false})
	if code != 200 {
		t.Fatal(code)
	}
	var out struct {
		Compatible bool     `json:"compatible"`
		Issues     []string `json:"issues"`
	}
	json.Unmarshal(b, &out)
	if out.Compatible || len(out.Issues) == 0 {
		t.Fatal("socket mismatch accepted")
	}
}
