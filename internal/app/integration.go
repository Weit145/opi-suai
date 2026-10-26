package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func Sign(secret, timestamp string, b []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(timestamp + "."))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func Verify(secret string, r *http.Request, b []byte) bool {
	stamp := r.Header.Get("X-Timestamp")
	ts, e := strconv.ParseInt(stamp, 10, 64)
	if e != nil || time.Since(time.Unix(ts, 0)) > 5*time.Minute || time.Until(time.Unix(ts, 0)) > time.Minute {
		return false
	}
	got, e := hex.DecodeString(r.Header.Get("X-Signature"))
	expected, _ := hex.DecodeString(Sign(secret, stamp, b))
	return e == nil && hmac.Equal(got, expected)
}
func (s *Server) provider(ctx context.Context, path string, input, output any) error {
	b, e := json.Marshal(input)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, s.C.ProviderURL+path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", stamp)
	req.Header.Set("X-Signature", Sign(s.C.IntegrationSecret, stamp, b))
	resp, e := s.Client.Do(req)
	if e != nil {
		return problem{502, "Учебный провайдер недоступен; повторите запрос"}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return problem{502, "Провайдер отклонил операцию"}
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(output)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}
func (s *Server) createPayment(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	o, e := readOrder(r.Context(), s.DB, id, false)
	if e != nil {
		return e
	}
	if o.UserID != actor(r).ID && !allowed(r, "manager") {
		return forbidden()
	}
	if o.Status != "awaiting_payment" {
		return conflict("Заказ не ожидает оплаты")
	}
	_, e = s.DB.Exec(r.Context(), "INSERT INTO payments(order_id,amount) VALUES($1,$2) ON CONFLICT DO NOTHING", id, o.Total)
	if e != nil {
		return e
	}
	var result struct {
		ID string `json:"id"`
	}
	if e = s.provider(r.Context(), "/payments", map[string]any{"order_id": id, "amount": o.Total, "currency": "RUB"}, &result); e != nil {
		return e
	}
	if result.ID == "" {
		return problem{502, "Провайдер не вернул идентификатор"}
	}
	_, e = s.DB.Exec(r.Context(), "UPDATE payments SET provider_id=$2,updated_at=now() WHERE order_id=$1 AND (provider_id IS NULL OR provider_id=$2)", id, result.ID)
	if e != nil {
		return e
	}
	write(w, 200, map[string]any{"provider_id": result.ID, "demo": s.C.Demo})
	return nil
}

type PaymentEvent struct {
	EventID   string `json:"event_id"`
	PaymentID string `json:"payment_id"`
	OrderID   string `json:"order_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Kind      string `json:"kind"`
}

func (s *Server) paymentWebhook(w http.ResponseWriter, r *http.Request) error {
	b, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil {
		return bad("Не удалось прочитать событие")
	}
	if !Verify(s.C.IntegrationSecret, r, b) {
		return problem{401, "Неверная подпись события"}
	}
	var c PaymentEvent
	if json.Unmarshal(b, &c) != nil || c.EventID == "" || c.PaymentID == "" || c.Currency != "RUB" || (c.Kind != "paid" && c.Kind != "refunded") {
		return bad("Некорректное платёжное событие")
	}
	e = s.tx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		o, e := readOrder(ctx, tx, c.OrderID, true)
		if e != nil {
			return e
		}
		var provider, status string
		var amount int64
		e = tx.QueryRow(ctx, "SELECT coalesce(provider_id,''),status,amount FROM payments WHERE order_id=$1 FOR UPDATE", o.ID).Scan(&provider, &status, &amount)
		if e != nil {
			return e
		}
		if provider != c.PaymentID || amount != c.Amount || amount != o.Total {
			return conflict("Платёж не соответствует заказу")
		}
		tag, e := tx.Exec(ctx, "INSERT INTO payment_events(event_id,order_id,kind) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", c.EventID, o.ID, c.Kind)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if c.Kind == "paid" {
			if status == "paid" || status == "refunded" {
				return nil
			}
			next := "paid"
			if o.Status == "cancelled" {
				next = "refund_pending"
			} else if o.Status != "awaiting_payment" {
				return conflict("Заказ не может принять оплату")
			}
			if _, e = tx.Exec(ctx, "UPDATE payments SET status='paid',updated_at=now() WHERE order_id=$1", o.ID); e != nil {
				return e
			}
			if o.Assembly && next == "paid" {
				if _, e = tx.Exec(ctx, "INSERT INTO assembly_tasks(order_id,status) VALUES($1,'planned') ON CONFLICT DO NOTHING", o.ID); e != nil {
					return e
				}
			}
			return event(ctx, tx, o, next, "Провайдер подтвердил оплату", "")
		}
		if status == "refunded" {
			return nil
		}
		if o.Status != "refund_pending" || status != "paid" {
			return conflict("Возврат не запрошен")
		}
		if _, e = tx.Exec(ctx, "UPDATE payments SET status='refunded',updated_at=now() WHERE order_id=$1", o.ID); e != nil {
			return e
		}
		return event(ctx, tx, o, "cancelled", "Провайдер подтвердил возврат средств", "")
	})
	if e != nil {
		return e
	}
	write(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) demoPayment(w http.ResponseWriter, r *http.Request) error {
	if !s.C.Demo {
		return problem{404, "Учебный режим выключен"}
	}
	o, e := readOrder(r.Context(), s.DB, chi.URLParam(r, "id"), false)
	if e != nil {
		return e
	}
	if o.UserID != actor(r).ID && !allowed(r, "manager") {
		return forbidden()
	}
	var provider string
	if e = s.DB.QueryRow(r.Context(), "SELECT provider_id FROM payments WHERE order_id=$1", o.ID).Scan(&provider); e != nil {
		return e
	}
	if e = s.provider(r.Context(), "/payments/confirm", map[string]string{"id": provider}, nil); e != nil {
		return e
	}
	return s.orderDetail(w, r)
}
func (s *Server) createShipment(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r, "warehouse", "manager") {
		return forbidden()
	}
	id := chi.URLParam(r, "id")
	var o order
	// Serialize shipment creation against cancellation with a temporary durable state.
	e := s.tx(r.Context(), func(tx pgx.Tx) error {
		var e error
		o, e = readOrder(r.Context(), tx, id, true)
		if e != nil {
			return e
		}
		if o.Receiving != "delivery" || (o.Status != "ready" && o.Status != "shipping_pending") {
			return conflict("Заказ не готов к доставке")
		}
		if _, e = tx.Exec(r.Context(), "INSERT INTO shipments(order_id) VALUES($1) ON CONFLICT DO NOTHING", id); e != nil {
			return e
		}
		_, e = tx.Exec(r.Context(), "UPDATE orders SET status='shipping_pending' WHERE id=$1", id)
		return e
	})
	if e != nil {
		return e
	}
	var result struct {
		ID       string `json:"id"`
		Tracking string `json:"tracking"`
	}
	var contact, address, phone string
	if e = s.DB.QueryRow(r.Context(), "SELECT contact_name,address,phone FROM orders WHERE id=$1", id).Scan(&contact, &address, &phone); e != nil {
		return e
	}
	if e = s.provider(r.Context(), "/shipments", map[string]any{"order_id": id, "address": address, "contact_name": contact, "phone": phone}, &result); e != nil {
		return e
	}
	if result.ID == "" || result.Tracking == "" {
		return problem{502, "Некорректный ответ доставки"}
	}
	e = s.tx(r.Context(), func(tx pgx.Tx) error {
		o, e := readOrder(r.Context(), tx, id, true)
		if e != nil {
			return e
		}
		if o.Status == "shipped" {
			return nil
		}
		if o.Status != "shipping_pending" {
			return conflict("Состояние заказа изменилось")
		}
		if _, e = tx.Exec(r.Context(), "UPDATE shipments SET provider_id=$2,tracking=$3,status='shipped',updated_at=now() WHERE order_id=$1", id, result.ID, result.Tracking); e != nil {
			return e
		}
		return event(r.Context(), tx, o, "shipped", "Передано в учебную службу доставки: "+result.Tracking, actor(r).ID)
	})
	if e != nil {
		return e
	}
	return s.orderDetail(w, r)
}
func (s *Server) syncShipment(w http.ResponseWriter, r *http.Request) error {
	o, e := readOrder(r.Context(), s.DB, chi.URLParam(r, "id"), false)
	if e != nil {
		return e
	}
	if !canView(r, o) {
		return forbidden()
	}
	var provider string
	if e = s.DB.QueryRow(r.Context(), "SELECT provider_id FROM shipments WHERE order_id=$1", o.ID).Scan(&provider); e != nil {
		return e
	}
	var result struct {
		Status string `json:"status"`
	}
	if e = s.provider(r.Context(), "/shipments/status", map[string]string{"id": provider}, &result); e != nil {
		return e
	}
	if result.Status == "delivered" {
		e = s.tx(r.Context(), func(tx pgx.Tx) error {
			o, e := readOrder(r.Context(), tx, o.ID, true)
			if e != nil {
				return e
			}
			if o.Status == "completed" {
				return nil
			}
			if o.Status != "shipped" {
				return conflict("Заказ не передан в доставку")
			}
			if _, e = tx.Exec(r.Context(), "UPDATE shipments SET status='delivered',updated_at=now() WHERE order_id=$1", o.ID); e != nil {
				return e
			}
			return event(r.Context(), tx, o, "completed", "Доставка подтверждена провайдером", "")
		})
		if e != nil {
			return e
		}
	}
	return s.orderDetail(w, r)
}
func (s *Server) demoDelivery(w http.ResponseWriter, r *http.Request) error {
	if !s.C.Demo {
		return problem{404, "Учебный режим выключен"}
	}
	if !allowed(r, "warehouse", "manager") {
		return forbidden()
	}
	var provider string
	if e := s.DB.QueryRow(r.Context(), "SELECT provider_id FROM shipments WHERE order_id=$1", chi.URLParam(r, "id")).Scan(&provider); e != nil {
		return e
	}
	if e := s.provider(r.Context(), "/shipments/deliver", map[string]string{"id": provider}, nil); e != nil {
		return e
	}
	return s.syncShipment(w, r)
}
func (s *Server) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.work(ctx)
		}
	}
}
func (s *Server) work(ctx context.Context) {
	rows, e := s.DB.Query(ctx, "SELECT p.provider_id,o.id FROM orders o JOIN payments p ON p.order_id=o.id WHERE o.status='refund_pending' AND p.status='paid' LIMIT 20")
	if e != nil {
		return
	}
	type refund struct{ ID, Order string }
	refunds := []refund{}
	for rows.Next() {
		var x refund
		if rows.Scan(&x.ID, &x.Order) == nil {
			refunds = append(refunds, x)
		}
	}
	rows.Close()
	for _, x := range refunds {
		if e = s.provider(ctx, "/refunds", map[string]string{"id": x.ID}, nil); e != nil {
			slog.Warn("refund retry", "order_id", x.Order, "error", e)
		}
	}
	rows, e = s.DB.Query(ctx, "SELECT id,user_id,message FROM notifications WHERE NOT delivered ORDER BY created_at LIMIT 20")
	if e != nil {
		return
	}
	type notification struct{ ID, User, Message string }
	list := []notification{}
	for rows.Next() {
		var n notification
		if rows.Scan(&n.ID, &n.User, &n.Message) == nil {
			list = append(list, n)
		}
	}
	rows.Close()
	for _, n := range list {
		if e = s.provider(ctx, "/notifications", map[string]string{"id": n.ID, "recipient_id": n.User, "message": n.Message}, nil); e == nil {
			if _, e = s.DB.Exec(ctx, "UPDATE notifications SET delivered=true WHERE id=$1", n.ID); e != nil {
				slog.Warn("notification persistence", "id", n.ID)
			}
		}
	}
}
func IntegrationFailure(e error) string { return fmt.Sprint(e) }
