package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/Weit145/opi-suai/internal/domain"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type order struct {
	ID, UserID, Status, Receiving, StockState string
	Total                                     int64
	Assembly                                  bool
}

func readOrder(ctx context.Context, q queryer, id string, lock bool) (order, error) {
	var o order
	sql := "SELECT id,user_id,status,receiving,stock_state,total,assembly FROM orders WHERE id=$1"
	if lock {
		sql += " FOR UPDATE"
	}
	e := q.QueryRow(ctx, sql, id).Scan(&o.ID, &o.UserID, &o.Status, &o.Receiving, &o.StockState, &o.Total, &o.Assembly)
	return o, e
}
func canView(r *http.Request, o order) bool {
	return o.UserID == actor(r).ID || allowed(r, "manager", "warehouse", "assembler")
}
func event(ctx context.Context, tx pgx.Tx, o order, status, note, actor string) error {
	var who any
	if actor != "" {
		who = actor
	}
	if _, e := tx.Exec(ctx, "UPDATE orders SET status=$2 WHERE id=$1", o.ID, status); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, "INSERT INTO order_events(order_id,status,note,actor_id) VALUES($1,$2,$3,$4)", o.ID, status, note, who); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, "INSERT INTO notifications(id,user_id,order_id,message) VALUES($1,$2,$3,$4)", uuid.NewString(), o.UserID, o.ID, "Заказ "+o.ID[:8]+": "+status+". "+note)
	return e
}
func (s *Server) checkout(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		Items         []domain.Item `json:"items"`
		Assembly      bool          `json:"assembly"`
		Configuration bool          `json:"configuration"`
		Receiving     string        `json:"receiving" validate:"oneof=pickup delivery"`
		ContactName   string        `json:"contact_name" validate:"required,max=120"`
		Phone         string        `json:"phone" validate:"required,min=7,max=30"`
		Address       string        `json:"address" validate:"max=500"`
		Key           string        `json:"checkout_key" validate:"required,uuid"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	if s.V.Struct(c) != nil || len(c.Items) == 0 || (c.Receiving == "delivery" && trim(c.Address) == "") {
		return bad("Заполните контакты, способ получения и адрес доставки; корзина не должна быть пустой")
	}
	if e := validateItems(c.Items); e != nil {
		return e
	}
	sort.Slice(c.Items, func(i, j int) bool { return c.Items[i].ProductID < c.Items[j].ProductID })
	id := uuid.NewString()
	e := s.tx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, e := tx.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", actor(r).ID); e != nil {
			return e
		}
		var existing string
		e := tx.QueryRow(ctx, "SELECT id FROM orders WHERE user_id=$1 AND checkout_key=$2", actor(r).ID, c.Key).Scan(&existing)
		if e == nil {
			id = existing
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		products := []domain.Product{}
		var total int64
		for _, i := range c.Items {
			p, e := scanProduct(tx.QueryRow(ctx, "SELECT "+productColumns+" FROM products p JOIN stock s ON s.product_id=p.id WHERE p.id=$1 FOR UPDATE OF p,s", i.ProductID))
			if e != nil {
				return e
			}
			if !p.Active {
				return conflict("Товар снят с продажи: " + p.Name)
			}
			if p.Price != i.ExpectedPrice {
				return conflict("Цена изменилась: " + p.Name + ". Обновите корзину и подтвердите новую стоимость.")
			}
			if p.Available < i.Quantity {
				return conflict("Недостаточный остаток: " + p.Name)
			}
			if (c.Assembly || c.Configuration) && i.Quantity != 1 {
				return bad("Для одной конфигурации выберите по одной позиции каждого компонента")
			}
			products = append(products, p)
			total += p.Price * int64(i.Quantity)
		}
		if c.Assembly || c.Configuration {
			if issues := domain.Compatibility(products, true); len(issues) > 0 {
				return conflict(strings.Join(issues, "; "))
			}
		}
		if _, e := tx.Exec(ctx, "INSERT INTO orders(id,user_id,total,assembly,receiving,contact_name,phone,address,checkout_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", id, actor(r).ID, total, c.Assembly, c.Receiving, trim(c.ContactName), trim(c.Phone), trim(c.Address), c.Key); e != nil {
			return e
		}
		for n, i := range c.Items {
			p := products[n]
			if _, e := tx.Exec(ctx, "UPDATE stock SET available=available-$2,reserved=reserved+$2 WHERE product_id=$1", i.ProductID, i.Quantity); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, "INSERT INTO order_items(order_id,product_id,name,specs,price,quantity) VALUES($1,$2,$3,$4,$5,$6)", id, p.ID, p.Name, p.Specs, p.Price, i.Quantity); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, "INSERT INTO stock_movements(product_id,quantity,kind,note,actor_id,order_id) VALUES($1,$2,'reserve','Резервирование при оформлении',$3,$4)", p.ID, -i.Quantity, actor(r).ID, id); e != nil {
				return e
			}
		}
		if _, e := tx.Exec(ctx, "DELETE FROM cart_items WHERE user_id=$1", actor(r).ID); e != nil {
			return e
		}
		return event(ctx, tx, order{ID: id, UserID: actor(r).ID}, "awaiting_payment", "Заказ оформлен, состав и цены зафиксированы", actor(r).ID)
	})
	if e != nil {
		return e
	}
	write(w, 201, map[string]string{"id": id})
	return nil
}
func (s *Server) orders(w http.ResponseWriter, r *http.Request) error {
	staff := allowed(r, "manager", "warehouse", "assembler")
	return s.json(w, r, "SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT o.*,u.name AS customer_name FROM orders o JOIN users u ON u.id=o.user_id WHERE ($2 OR o.user_id=$1) ORDER BY o.created_at DESC LIMIT 300)t", actor(r).ID, staff)
}
func (s *Server) orderDetail(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	o, e := readOrder(r.Context(), s.DB, id, false)
	if e != nil {
		return e
	}
	if !canView(r, o) {
		return forbidden()
	}
	return s.json(w, r, `SELECT to_jsonb(o)||jsonb_build_object(
 'items',(SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT * FROM order_items WHERE order_id=o.id)t),
 'history',(SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT e.*,u.name AS actor_name FROM order_events e LEFT JOIN users u ON u.id=e.actor_id WHERE e.order_id=o.id ORDER BY e.id)t),
 'payment',(SELECT to_jsonb(p) FROM payments p WHERE p.order_id=o.id),
 'shipment',(SELECT to_jsonb(p) FROM shipments p WHERE p.order_id=o.id),
 'assembly_task',(SELECT to_jsonb(a) FROM assembly_tasks a WHERE a.order_id=o.id)) FROM orders o WHERE o.id=$1`, id)
}
func (s *Server) orderAction(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	if len(c.Note) > 1000 {
		return bad("Слишком длинное примечание")
	}
	if c.Action == "fail" && trim(c.Note) == "" {
		return bad("Укажите причину неуспешного тестирования")
	}
	e := s.tx(r.Context(), func(tx pgx.Tx) error {
		o, e := readOrder(r.Context(), tx, chi.URLParam(r, "id"), true)
		if e != nil {
			return e
		}
		next, e := domain.Transition(o.Status, c.Action, actor(r).Role, o.Assembly)
		if e != nil {
			return conflict(e.Error())
		}
		if c.Action == "pickup" && o.Receiving != "pickup" {
			return bad("Для доставки требуется отправление")
		}
		if c.Action == "picked" {
			if o.StockState != "reserved" {
				return conflict("Заказ уже скомплектован")
			}
			if _, e := tx.Exec(r.Context(), "UPDATE stock s SET reserved=s.reserved-i.quantity FROM order_items i WHERE i.order_id=$1 AND i.product_id=s.product_id", o.ID); e != nil {
				return e
			}
			if _, e := tx.Exec(r.Context(), "UPDATE orders SET stock_state='consumed' WHERE id=$1", o.ID); e != nil {
				return e
			}
		}
		if o.Assembly {
			var result any
			if c.Action == "pass" {
				result = "passed"
			}
			if c.Action == "fail" {
				result = "failed"
			}
			var who any
			if allowed(r, "assembler") {
				who = actor(r).ID
			}
			if _, e := tx.Exec(r.Context(), "UPDATE assembly_tasks SET status=$2,assignee_id=coalesce($3::uuid,assignee_id),test_result=coalesce($4::text,test_result),updated_at=now() WHERE order_id=$1", o.ID, next, who, result); e != nil {
				return e
			}
		}
		return event(r.Context(), tx, o, next, c.Action+": "+trim(c.Note), actor(r).ID)
	})
	if e != nil {
		return e
	}
	return s.orderDetail(w, r)
}
func release(ctx context.Context, tx pgx.Tx, o order, who string) error {
	if o.StockState == "reserved" {
		if _, e := tx.Exec(ctx, "UPDATE stock s SET reserved=s.reserved-i.quantity,available=s.available+i.quantity FROM order_items i WHERE i.order_id=$1 AND i.product_id=s.product_id", o.ID); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "UPDATE orders SET stock_state='released' WHERE id=$1", o.ID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "INSERT INTO stock_movements(product_id,quantity,kind,note,actor_id,order_id) SELECT product_id,quantity,'release','Освобождение резерва',$2,order_id FROM order_items WHERE order_id=$1", o.ID, who)
		return e
	}
	if o.StockState == "consumed" {
		_, e := tx.Exec(ctx, "UPDATE orders SET stock_state='return_pending' WHERE id=$1", o.ID)
		return e
	}
	return nil
}
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) error {
	e := s.tx(r.Context(), func(tx pgx.Tx) error {
		o, e := readOrder(r.Context(), tx, chi.URLParam(r, "id"), true)
		if e != nil {
			return e
		}
		if o.UserID != actor(r).ID && !allowed(r, "manager") {
			return forbidden()
		}
		switch o.Status {
		case "cancelled", "refund_pending":
			return nil
		case "shipping_pending", "shipped", "completed":
			return conflict("Отмена переданного или выполненного заказа не предусмотрена")
		}
		if (o.Status == "assembling" || o.Status == "testing" || o.Status == "rework" || o.Status == "ready" && o.Assembly) && !allowed(r, "manager") {
			return forbidden()
		}
		if e := release(r.Context(), tx, o, actor(r).ID); e != nil {
			return e
		}
		status := "refund_pending"
		if o.Status == "awaiting_payment" {
			status = "cancelled"
		}
		if _, e := tx.Exec(r.Context(), "UPDATE assembly_tasks SET status='cancelled',updated_at=now() WHERE order_id=$1", o.ID); e != nil {
			return e
		}
		return event(r.Context(), tx, o, status, "Отмена заказа", actor(r).ID)
	})
	if e != nil {
		return e
	}
	return s.orderDetail(w, r)
}
func (s *Server) stockReturn(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r, "warehouse") {
		return forbidden()
	}
	var c struct {
		Note string `json:"note"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	if trim(c.Note) == "" {
		return bad("Опишите проверку возвращаемого товара")
	}
	e := s.tx(r.Context(), func(tx pgx.Tx) error {
		o, e := readOrder(r.Context(), tx, chi.URLParam(r, "id"), true)
		if e != nil {
			return e
		}
		if o.StockState != "return_pending" || (o.Status != "cancelled" && o.Status != "refund_pending") {
			return conflict("Нет товара, ожидающего проверенного возврата")
		}
		if _, e := tx.Exec(r.Context(), "UPDATE stock s SET available=s.available+i.quantity FROM order_items i WHERE i.order_id=$1 AND i.product_id=s.product_id", o.ID); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), "UPDATE orders SET stock_state='returned' WHERE id=$1", o.ID); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), "INSERT INTO stock_movements(product_id,quantity,kind,note,actor_id,order_id) SELECT product_id,quantity,'return',$2,$3,order_id FROM order_items WHERE order_id=$1", o.ID, c.Note, actor(r).ID); e != nil {
			return e
		}
		return event(r.Context(), tx, o, o.Status, "Склад проверил и вернул товар: "+c.Note, actor(r).ID)
	})
	if e != nil {
		return e
	}
	return s.orderDetail(w, r)
}
func (s *Server) notifications(w http.ResponseWriter, r *http.Request) error {
	return s.json(w, r, "SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT id,order_id,message,delivered,created_at FROM notifications WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100)t", actor(r).ID)
}
func decodeMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
