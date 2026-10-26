package app

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Weit145/opi-suai/internal/domain"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const productColumns = "p.id,p.name,p.category,p.description,p.price,p.active,p.specs,s.available,s.reserved"

func scanProduct(row pgx.Row) (domain.Product, error) {
	var p domain.Product
	e := row.Scan(&p.ID, &p.Name, &p.Category, &p.Description, &p.Price, &p.Active, &p.Specs, &p.Available, &p.Reserved)
	return p, e
}
func (s *Server) products(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	spec := q.Get("spec")
	if spec == "" {
		spec = "{}"
	}
	if !json.Valid([]byte(spec)) {
		return bad("Некорректный фильтр характеристик")
	}
	return s.json(w, r, "SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT "+productColumns+" FROM products p JOIN stock s ON s.product_id=p.id WHERE p.active AND ($1='' OR p.name ILIKE '%'||$1||'%') AND ($2='' OR p.category=$2) AND p.specs @> $3::jsonb ORDER BY p.name LIMIT 200) t", q.Get("q"), q.Get("category"), spec)
}
func (s *Server) product(w http.ResponseWriter, r *http.Request) error {
	p, e := scanProduct(s.DB.QueryRow(r.Context(), "SELECT "+productColumns+" FROM products p JOIN stock s ON s.product_id=p.id WHERE p.id=$1 AND p.active", chi.URLParam(r, "id")))
	if e != nil {
		return e
	}
	write(w, 200, p)
	return nil
}
func (s *Server) categories(w http.ResponseWriter, r *http.Request) error {
	return s.json(w, r, "SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT id,name FROM categories ORDER BY name) t")
}
func (s *Server) staffProducts(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r, "manager", "warehouse") {
		return forbidden()
	}
	return s.json(w, r, "SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT "+productColumns+" FROM products p JOIN stock s ON s.product_id=p.id ORDER BY p.name) t")
}
func (s *Server) saveCategory(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r, "manager") {
		return forbidden()
	}
	var c struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	if len(c.ID) < 1 || len(c.ID) > 40 || len(trim(c.Name)) < 1 || len(c.Name) > 100 {
		return bad("Укажите код и название категории")
	}
	_, e := s.DB.Exec(r.Context(), "INSERT INTO categories(id,name) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET name=excluded.name", c.ID, trim(c.Name))
	if e != nil {
		return e
	}
	write(w, 200, c)
	return nil
}
func (s *Server) saveProduct(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r, "manager") {
		return forbidden()
	}
	var p domain.Product
	if e := body(r, &p); e != nil {
		return e
	}
	p.Name = trim(p.Name)
	if p.Name == "" || len(p.Name) > 200 || p.Price <= 0 || p.Price > 10000000000 || len(p.Description) > 5000 || len(p.Specs) > 30 {
		return bad("Проверьте название, положительную цену и характеристики")
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Specs == nil {
		p.Specs = map[string]string{}
	}
	e := s.tx(r.Context(), func(tx pgx.Tx) error {
		_, e := tx.Exec(r.Context(), "INSERT INTO products(id,name,category,description,price,active,specs) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET name=excluded.name,category=excluded.category,description=excluded.description,price=excluded.price,active=excluded.active,specs=excluded.specs", p.ID, p.Name, p.Category, p.Description, p.Price, p.Active, p.Specs)
		if e != nil {
			return e
		}
		_, e = tx.Exec(r.Context(), "INSERT INTO stock(product_id) VALUES($1) ON CONFLICT DO NOTHING", p.ID)
		return e
	})
	if e != nil {
		return e
	}
	write(w, 200, map[string]string{"id": p.ID})
	return nil
}
func (s *Server) loadProducts(r *http.Request, ids []string) ([]domain.Product, error) {
	out := []domain.Product{}
	if len(ids) > 30 {
		return nil, bad("Слишком много компонентов")
	}
	for _, id := range ids {
		p, e := scanProduct(s.DB.QueryRow(r.Context(), "SELECT "+productColumns+" FROM products p JOIN stock s ON s.product_id=p.id WHERE p.id=$1 AND p.active", id))
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *Server) compatibility(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		IDs      []string `json:"ids"`
		Complete bool     `json:"complete"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	products, e := s.loadProducts(r, c.IDs)
	if e != nil {
		return e
	}
	issues := domain.Compatibility(products, c.Complete)
	write(w, 200, map[string]any{"compatible": len(issues) == 0, "issues": issues})
	return nil
}
func (s *Server) cart(w http.ResponseWriter, r *http.Request) error {
	return s.json(w, r, "SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT c.product_id,c.quantity,c.expected_price,p.name,p.price,p.active,s.available FROM cart_items c JOIN products p ON p.id=c.product_id JOIN stock s ON s.product_id=p.id WHERE c.user_id=$1 ORDER BY p.name) t", actor(r).ID)
}
func validateItems(items []domain.Item) error {
	if len(items) > 50 {
		return bad("В заказе допускается до 50 позиций")
	}
	seen := map[string]bool{}
	for _, i := range items {
		if _, e := uuid.Parse(i.ProductID); e != nil || i.Quantity < 1 || i.Quantity > 100 || i.ExpectedPrice <= 0 || seen[i.ProductID] {
			return bad("Некорректная позиция: проверьте количество, цену и повторы")
		}
		seen[i.ProductID] = true
	}
	return nil
}
func (s *Server) saveCart(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		Items []domain.Item `json:"items"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	if e := validateItems(c.Items); e != nil {
		return e
	}
	e := s.tx(r.Context(), func(tx pgx.Tx) error {
		if _, e := tx.Exec(r.Context(), "SELECT id FROM users WHERE id=$1 FOR UPDATE", actor(r).ID); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), "DELETE FROM cart_items WHERE user_id=$1", actor(r).ID); e != nil {
			return e
		}
		for _, i := range c.Items {
			if _, e := tx.Exec(r.Context(), "INSERT INTO cart_items(user_id,product_id,quantity,expected_price) VALUES($1,$2,$3,$4)", actor(r).ID, i.ProductID, i.Quantity, i.ExpectedPrice); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	return s.cart(w, r)
}
func (s *Server) configuration(w http.ResponseWriter, r *http.Request) error {
	return s.json(w, r, "SELECT coalesce((SELECT items FROM configurations WHERE user_id=$1),'[]'::jsonb)", actor(r).ID)
}
func (s *Server) saveConfiguration(w http.ResponseWriter, r *http.Request) error {
	var c struct {
		IDs []string `json:"ids"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	if _, e := s.loadProducts(r, c.IDs); e != nil {
		return e
	}
	if c.IDs == nil {
		c.IDs = []string{}
	}
	b, _ := json.Marshal(c.IDs)
	_, e := s.DB.Exec(r.Context(), "INSERT INTO configurations(user_id,items) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET items=excluded.items", actor(r).ID, b)
	if e != nil {
		return e
	}
	return s.configuration(w, r)
}
func (s *Server) inventory(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r, "warehouse", "manager") {
		return forbidden()
	}
	return s.json(w, r, "SELECT jsonb_build_object('products',(SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT "+productColumns+" FROM products p JOIN stock s ON s.product_id=p.id ORDER BY p.name)t),'movements',(SELECT coalesce(jsonb_agg(t),'[]') FROM (SELECT m.*,p.name FROM stock_movements m JOIN products p ON p.id=m.product_id ORDER BY m.id DESC LIMIT 100)t))")
}
func (s *Server) movement(w http.ResponseWriter, r *http.Request) error {
	if !allowed(r, "warehouse") {
		return forbidden()
	}
	var c struct {
		ProductID string `json:"product_id"`
		Quantity  int    `json:"quantity"`
		Kind      string `json:"kind"`
		Note      string `json:"note"`
	}
	if e := body(r, &c); e != nil {
		return e
	}
	if c.Quantity < 1 || c.Quantity > 10000 || trim(c.Note) == "" || len(c.Note) > 500 || !strings.Contains("|receipt|writeoff|return|", "|"+c.Kind+"|") {
		return bad("Укажите тип, положительное количество и основание движения")
	}
	delta := c.Quantity
	if c.Kind == "writeoff" {
		delta = -delta
	}
	e := s.tx(r.Context(), func(tx pgx.Tx) error {
		tag, e := tx.Exec(r.Context(), "UPDATE stock SET available=available+$2 WHERE product_id=$1 AND available+$2>=0", c.ProductID, delta)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return conflict("Недостаточно свободного товара или товар не найден")
		}
		_, e = tx.Exec(r.Context(), "INSERT INTO stock_movements(product_id,quantity,kind,note,actor_id) VALUES($1,$2,$3,$4,$5)", c.ProductID, delta, c.Kind, c.Note, actor(r).ID)
		return e
	})
	if e != nil {
		return e
	}
	write(w, 200, map[string]bool{"ok": true})
	return nil
}
