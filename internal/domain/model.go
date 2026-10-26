package domain

import (
	"fmt"
	"strings"
)

type Product struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Category    string            `json:"category"`
	Description string            `json:"description"`
	Price       int64             `json:"price"` // integer kopecks
	Active      bool              `json:"active"`
	Specs       map[string]string `json:"specs"`
	Available   int               `json:"available"`
	Reserved    int               `json:"reserved"`
}

type Item struct {
	ProductID     string `json:"product_id"`
	Quantity      int    `json:"quantity"`
	ExpectedPrice int64  `json:"expected_price"`
}

type User struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Active bool   `json:"active"`
}

var Roles = map[string]bool{"customer": true, "manager": true, "warehouse": true, "assembler": true, "admin": true}

// Compatibility uses explicit technical attributes and rejects incomplete builds.
func Compatibility(products []Product, complete bool) []string {
	by := map[string][]Product{}
	for _, p := range products {
		by[p.Category] = append(by[p.Category], p)
	}
	issues := []string{}
	for _, cat := range []string{"cpu", "motherboard", "ram", "storage", "psu", "case"} {
		if complete && len(by[cat]) == 0 {
			issues = append(issues, "Не выбран компонент: "+cat)
		}
		if cat != "ram" && cat != "storage" && len(by[cat]) > 1 {
			issues = append(issues, "Несколько компонентов категории: "+cat)
		}
	}
	first := func(cat string) Product {
		if len(by[cat]) > 0 {
			return by[cat][0]
		}
		return Product{}
	}
	cpu, board, ram, box, psu, gpu := first("cpu"), first("motherboard"), first("ram"), first("case"), first("psu"), first("gpu")
	match := func(a Product, ak string, b Product, bk string, msg string) {
		if a.ID != "" && b.ID != "" && (a.Specs[ak] == "" || b.Specs[bk] == "" || a.Specs[ak] != b.Specs[bk]) {
			issues = append(issues, msg)
		}
	}
	match(cpu, "socket", board, "socket", "Сокет процессора и платы не совпадает или не указан")
	for _, r := range by["ram"] {
		match(r, "memory_type", board, "memory_type", "Тип памяти и платы не совпадает или не указан")
	}
	_ = ram
	if box.ID != "" && board.ID != "" && !contains(box.Specs["board_formats"], board.Specs["form_factor"]) {
		issues = append(issues, "Корпус не поддерживает форм-фактор платы")
	}
	if gpu.ID != "" && box.ID != "" {
		g, ok1 := positive(gpu.Specs["length_mm"])
		c, ok2 := positive(box.Specs["gpu_max_mm"])
		if !ok1 || !ok2 || g > c {
			issues = append(issues, "Видеокарта не помещается в корпус или габариты не указаны")
		}
	}
	if psu.ID != "" && cpu.ID != "" {
		power, ok1 := positive(psu.Specs["power_w"])
		need, ok2 := positive(cpu.Specs["tdp_w"])
		need += 100
		if gpu.ID != "" {
			g, ok := positive(gpu.Specs["tdp_w"])
			need += g
			ok2 = ok2 && ok
		}
		if !ok1 || !ok2 || power*10 < need*12 {
			issues = append(issues, "Недостаточная мощность БП с запасом 20% или неизвестное энергопотребление")
		}
		if board.ID != "" && !contains(psu.Specs["connectors"], board.Specs["power_connector"]) {
			issues = append(issues, "БП не поддерживает разъём питания платы")
		}
		if gpu.ID != "" && gpu.Specs["power_connector"] != "none" && !contains(psu.Specs["connectors"], gpu.Specs["power_connector"]) {
			issues = append(issues, "БП не поддерживает разъём питания видеокарты")
		}
	}
	return issues
}

func contains(csv, value string) bool {
	if value == "" {
		return false
	}
	for _, v := range strings.Split(csv, ",") {
		if strings.TrimSpace(v) == value {
			return true
		}
	}
	return false
}
func positive(v string) (int, bool) {
	var n int
	_, e := fmt.Sscanf(v, "%d", &n)
	return n, e == nil && n > 0
}

func Transition(status, action, role string, assembly bool) (string, error) {
	staff := func(want string) bool { return role == want || role == "admin" }
	switch action {
	case "pick":
		if status == "paid" && staff("warehouse") {
			return "picking", nil
		}
	case "picked":
		if status == "picking" && staff("warehouse") {
			if assembly {
				return "picked", nil
			}
			return "ready", nil
		}
	case "assemble":
		if (status == "picked" || status == "rework") && staff("assembler") {
			return "assembling", nil
		}
	case "test":
		if status == "assembling" && staff("assembler") {
			return "testing", nil
		}
	case "pass":
		if status == "testing" && staff("assembler") {
			return "ready", nil
		}
	case "fail":
		if status == "testing" && staff("assembler") {
			return "rework", nil
		}
	case "pickup":
		if status == "ready" && (staff("warehouse") || staff("manager")) {
			return "completed", nil
		}
	}
	return "", fmt.Errorf("переход %s из %s недоступен роли %s", action, status, role)
}
