package app

import (
	"context"
	"fmt"

	schema "github.com/Weit145/opi-suai"
	"github.com/Weit145/opi-suai/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Server) Initialize(ctx context.Context, seed bool, password string) error {
	if e := validConfig(s.C); e != nil {
		return e
	}
	if e := s.tx(ctx, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(34002)"); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_versions(version integer PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())"); e != nil {
			return e
		}
		var exists bool
		if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_versions WHERE version=1)").Scan(&exists); e != nil {
			return e
		}
		if exists {
			return nil
		}
		if _, e := tx.Exec(ctx, schema.Initial); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "INSERT INTO schema_versions(version) VALUES(1)")
		return e
	}); e != nil {
		return e
	}
	if !seed {
		return nil
	}
	if !s.C.Demo {
		return fmt.Errorf("demo seed requires DEMO_MODE=true")
	}
	if len(password) < 10 || len(password) > 72 {
		return fmt.Errorf("DEMO_PASSWORD must be 10..72 characters")
	}
	for _, role := range []string{"customer", "manager", "warehouse", "assembler", "admin"} {
		var exists bool
		if e := s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)", role+"@lab.invalid").Scan(&exists); e != nil {
			return e
		}
		if !exists {
			if _, e := s.addUser(ctx, credentials{Email: role + "@lab.invalid", Password: password, Name: "Учебный " + role, Role: role}); e != nil {
				return e
			}
		}
	}
	categories := map[string]string{"cpu": "Процессоры", "motherboard": "Материнские платы", "ram": "Оперативная память", "storage": "Накопители", "psu": "Блоки питания", "case": "Корпуса", "gpu": "Видеокарты", "accessory": "Аксессуары"}
	for id, name := range categories {
		if _, e := s.DB.Exec(ctx, "INSERT INTO categories(id,name) VALUES($1,$2) ON CONFLICT DO NOTHING", id, name); e != nil {
			return e
		}
	}
	products := []domain.Product{
		{Name: "Учебный процессор A6", Category: "cpu", Price: 1490000, Specs: map[string]string{"socket": "AM5", "tdp_w": "65", "cores": "6"}},
		{Name: "Учебный процессор I6", Category: "cpu", Price: 1590000, Specs: map[string]string{"socket": "LGA1700", "tdp_w": "65", "cores": "6"}},
		{Name: "Учебная плата A620", Category: "motherboard", Price: 890000, Specs: map[string]string{"socket": "AM5", "memory_type": "DDR5", "form_factor": "mATX", "power_connector": "24pin+8pin"}},
		{Name: "Учебная плата B760", Category: "motherboard", Price: 990000, Specs: map[string]string{"socket": "LGA1700", "memory_type": "DDR4", "form_factor": "ATX", "power_connector": "24pin+8pin"}},
		{Name: "Учебная память DDR5 16 ГБ", Category: "ram", Price: 490000, Specs: map[string]string{"memory_type": "DDR5", "capacity_gb": "16"}},
		{Name: "Учебная память DDR4 16 ГБ", Category: "ram", Price: 390000, Specs: map[string]string{"memory_type": "DDR4", "capacity_gb": "16"}},
		{Name: "Учебный SSD 1 ТБ", Category: "storage", Price: 590000, Specs: map[string]string{"capacity_gb": "1024", "interface": "SATA"}},
		{Name: "Учебный БП 650 Вт", Category: "psu", Price: 590000, Specs: map[string]string{"power_w": "650", "connectors": "24pin+8pin,8pin-pcie"}},
		{Name: "Учебный БП 200 Вт", Category: "psu", Price: 190000, Specs: map[string]string{"power_w": "200", "connectors": "24pin+8pin"}},
		{Name: "Учебный корпус Compact", Category: "case", Price: 490000, Specs: map[string]string{"board_formats": "mATX,Mini-ITX", "gpu_max_mm": "280"}},
		{Name: "Учебный корпус Tower", Category: "case", Price: 690000, Specs: map[string]string{"board_formats": "ATX,mATX,Mini-ITX", "gpu_max_mm": "360"}},
		{Name: "Учебная видеокарта G8", Category: "gpu", Price: 2490000, Specs: map[string]string{"tdp_w": "160", "length_mm": "300", "power_connector": "8pin-pcie", "vram_gb": "8"}},
		{Name: "Учебная клавиатура", Category: "accessory", Price: 149000, Specs: map[string]string{"interface": "USB"}},
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		for _, p := range products {
			p.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(p.Name)).String()
			if _, e := tx.Exec(ctx, "INSERT INTO products(id,name,category,description,price,active,specs) VALUES($1,$2,$3,$4,$5,true,$6) ON CONFLICT DO NOTHING", p.ID, p.Name, p.Category, "Демонстрационный товар для лабораторной работы. Название, характеристики и цена являются учебными данными.", p.Price, p.Specs); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, "INSERT INTO stock(product_id,available) VALUES($1,100) ON CONFLICT DO NOTHING", p.ID); e != nil {
				return e
			}
		}
		return nil
	})
}
