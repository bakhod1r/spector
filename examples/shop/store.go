package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// store is a small SQLite backing for the shop example.
//
// Its purpose is to make the example a real API rather than a set of handlers
// returning empty slices: an admin panel generated from this service can then
// list, create, edit and delete actual rows, which is the only way to see
// whether the panel works.
//
// The nested parts of each record (addresses, tags, line items) are stored as
// JSON columns. A normalised schema would be more faithful to how a production
// service is built, but it would also triple the size of an example whose
// subject is Specter, not database design.
type store struct{ db *sql.DB }

var db *store

// openStore opens the database and seeds it when it is empty.
func openStore(dsn string) (*store, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// The pure-Go driver serialises writes; a single connection avoids
	// "database is locked" under the panel's concurrent requests.
	conn.SetMaxOpenConns(1)

	s := &store{db: conn}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.seed(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL, email TEXT NOT NULL,
  roles TEXT NOT NULL DEFAULT '[]',
  contact TEXT NOT NULL DEFAULT '{}',
  billing TEXT NOT NULL DEFAULT '{}',
  active INTEGER NOT NULL DEFAULT 1,
  metadata TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, created_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS products (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  sku TEXT NOT NULL, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
  price_amount REAL NOT NULL DEFAULT 0, price_currency TEXT NOT NULL DEFAULT 'USD',
  tags TEXT NOT NULL DEFAULT '[]', attributes TEXT NOT NULL DEFAULT '{}',
  image TEXT NOT NULL DEFAULT '',
  in_stock INTEGER NOT NULL DEFAULT 1, rating REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, created_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  items TEXT NOT NULL DEFAULT '[]',
  total_amount REAL NOT NULL DEFAULT 0, total_currency TEXT NOT NULL DEFAULT 'USD',
  status TEXT NOT NULL DEFAULT 'pending',
  ship_to TEXT NOT NULL DEFAULT '{}',
  placed TEXT NOT NULL,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, created_by TEXT NOT NULL DEFAULT ''
);`
	_, err := s.db.Exec(schema)
	return err
}

// ---- helpers ----

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(data)
}

// fromJSON decodes a JSON column. A malformed value leaves the destination at
// its zero value rather than failing the whole request: one bad row should not
// make a list endpoint unusable.
func fromJSON(raw string, dst any) {
	if raw == "" {
		return
	}
	_ = json.Unmarshal([]byte(raw), dst)
}

func stamps(a Audit) (string, string, string) {
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	return a.CreatedAt.Format(time.RFC3339), now.Format(time.RFC3339), a.CreatedBy
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ---- users ----

const userCols = `id, name, email, roles, contact, billing, active, metadata, created_at, updated_at, created_by`

func scanUser(sc interface{ Scan(...any) error }) (User, error) {
	var u User
	var roles, contact, billing, metadata, created, updated, by string
	var active int
	if err := sc.Scan(&u.ID, &u.Name, &u.Email, &roles, &contact, &billing,
		&active, &metadata, &created, &updated, &by); err != nil {
		return User{}, err
	}
	fromJSON(roles, &u.Roles)
	fromJSON(contact, &u.Contact)
	fromJSON(billing, &u.Billing)
	fromJSON(metadata, &u.Metadata)
	u.Active = active == 1
	u.Audit = Audit{CreatedAt: parseTime(created), UpdatedAt: parseTime(updated), CreatedBy: by}
	return u, nil
}

func (s *store) listUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// A non-nil empty slice matters: it marshals as [] rather than null, and a
	// client that trusts the schema should not have to handle both.
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *store) getUser(id int) (User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *store) createUser(u User) (User, error) {
	created, updated, by := stamps(u.Audit)
	res, err := s.db.Exec(
		`INSERT INTO users (name, email, roles, contact, billing, active, metadata, created_at, updated_at, created_by)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		u.Name, u.Email, mustJSON(u.Roles), mustJSON(u.Contact), mustJSON(u.Billing),
		boolInt(u.Active), mustJSON(u.Metadata), created, updated, by)
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.getUser(int(id))
}

func (s *store) updateUser(id int, u User) (User, error) {
	_, err := s.db.Exec(
		`UPDATE users SET name=?, email=?, roles=?, contact=?, billing=?, active=?, metadata=?, updated_at=?
		 WHERE id=?`,
		u.Name, u.Email, mustJSON(u.Roles), mustJSON(u.Contact), mustJSON(u.Billing),
		boolInt(u.Active), mustJSON(u.Metadata), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return User{}, err
	}
	return s.getUser(id)
}

func (s *store) deleteUser(id int) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// ---- products ----

const productCols = `id, sku, title, description, price_amount, price_currency, tags, attributes, image, in_stock, rating, created_at, updated_at, created_by`

func scanProduct(sc interface{ Scan(...any) error }) (Product, error) {
	var p Product
	var tags, attrs, created, updated, by string
	var inStock int
	if err := sc.Scan(&p.ID, &p.Sku, &p.Title, &p.Description, &p.Price.Amount, &p.Price.Currency,
		&tags, &attrs, &p.Image, &inStock, &p.Rating, &created, &updated, &by); err != nil {
		return Product{}, err
	}
	fromJSON(tags, &p.Tags)
	fromJSON(attrs, &p.Attributes)
	p.InStock = inStock == 1
	p.Audit = Audit{CreatedAt: parseTime(created), UpdatedAt: parseTime(updated), CreatedBy: by}
	return p, nil
}

func (s *store) listProducts() ([]Product, error) {
	rows, err := s.db.Query(`SELECT ` + productCols + ` FROM products ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *store) getProduct(id int) (Product, error) {
	return scanProduct(s.db.QueryRow(`SELECT `+productCols+` FROM products WHERE id = ?`, id))
}

func (s *store) createProduct(p Product) (Product, error) {
	created, updated, by := stamps(p.Audit)
	res, err := s.db.Exec(
		`INSERT INTO products (sku, title, description, price_amount, price_currency, tags, attributes, image, in_stock, rating, created_at, updated_at, created_by)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.Sku, p.Title, p.Description, p.Price.Amount, orUSD(p.Price.Currency),
		mustJSON(p.Tags), mustJSON(p.Attributes), p.Image, boolInt(p.InStock), p.Rating, created, updated, by)
	if err != nil {
		return Product{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Product{}, err
	}
	return s.getProduct(int(id))
}

func (s *store) updateProduct(id int, p Product) (Product, error) {
	_, err := s.db.Exec(
		`UPDATE products SET sku=?, title=?, description=?, price_amount=?, price_currency=?, tags=?, attributes=?, image=?, in_stock=?, rating=?, updated_at=?
		 WHERE id=?`,
		p.Sku, p.Title, p.Description, p.Price.Amount, orUSD(p.Price.Currency),
		mustJSON(p.Tags), mustJSON(p.Attributes), p.Image, boolInt(p.InStock), p.Rating,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return Product{}, err
	}
	return s.getProduct(id)
}

func (s *store) deleteProduct(id int) error {
	_, err := s.db.Exec(`DELETE FROM products WHERE id = ?`, id)
	return err
}

// ---- orders ----

const orderCols = `id, user_id, items, total_amount, total_currency, status, ship_to, placed, created_at, updated_at, created_by`

func scanOrder(sc interface{ Scan(...any) error }) (Order, error) {
	var o Order
	var items, shipTo, placed, created, updated, by string
	if err := sc.Scan(&o.ID, &o.UserID, &items, &o.Total.Amount, &o.Total.Currency,
		&o.Status, &shipTo, &placed, &created, &updated, &by); err != nil {
		return Order{}, err
	}
	fromJSON(items, &o.Items)
	fromJSON(shipTo, &o.ShipTo)
	o.Placed = parseTime(placed)
	o.Audit = Audit{CreatedAt: parseTime(created), UpdatedAt: parseTime(updated), CreatedBy: by}
	return o, nil
}

func (s *store) listOrders() ([]Order, error) {
	rows, err := s.db.Query(`SELECT ` + orderCols + ` FROM orders ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *store) getOrder(id int) (Order, error) {
	return scanOrder(s.db.QueryRow(`SELECT `+orderCols+` FROM orders WHERE id = ?`, id))
}

func (s *store) createOrder(o Order) (Order, error) {
	created, updated, by := stamps(o.Audit)
	if o.Placed.IsZero() {
		o.Placed = time.Now().UTC()
	}
	if o.Status == "" {
		o.Status = "pending"
	}
	// The total is derived rather than accepted: a client that can name its
	// own total can name the wrong one.
	o.Total = totalOf(o.Items)

	res, err := s.db.Exec(
		`INSERT INTO orders (user_id, items, total_amount, total_currency, status, ship_to, placed, created_at, updated_at, created_by)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		o.UserID, mustJSON(o.Items), o.Total.Amount, orUSD(o.Total.Currency),
		o.Status, mustJSON(o.ShipTo), o.Placed.Format(time.RFC3339), created, updated, by)
	if err != nil {
		return Order{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Order{}, err
	}
	return s.getOrder(int(id))
}

func (s *store) updateOrder(id int, o Order) (Order, error) {
	o.Total = totalOf(o.Items)
	_, err := s.db.Exec(
		`UPDATE orders SET user_id=?, items=?, total_amount=?, total_currency=?, status=?, ship_to=?, updated_at=?
		 WHERE id=?`,
		o.UserID, mustJSON(o.Items), o.Total.Amount, orUSD(o.Total.Currency),
		o.Status, mustJSON(o.ShipTo), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return Order{}, err
	}
	return s.getOrder(id)
}

func (s *store) deleteOrder(id int) error {
	_, err := s.db.Exec(`DELETE FROM orders WHERE id = ?`, id)
	return err
}

func totalOf(items []LineItem) Money {
	total := Money{Currency: "USD"}
	for _, it := range items {
		total.Amount += it.Price.Amount * float64(it.Quantity)
		if it.Price.Currency != "" {
			total.Currency = it.Price.Currency
		}
	}
	return total
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func orUSD(c string) string {
	if c == "" {
		return "USD"
	}
	return c
}

// ---- seed ----

// seed fills an empty database with ten of each record, so the API has
// something to serve the first time it runs.
func (s *store) seed() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	names := []struct{ name, email, city, country string }{
		{"Ada Lovelace", "ada@example.com", "London", "GB"},
		{"Grace Hopper", "grace@example.com", "New York", "US"},
		{"Alan Turing", "alan@example.com", "Manchester", "GB"},
		{"Katherine Johnson", "katherine@example.com", "Hampton", "US"},
		{"Barbara Liskov", "barbara@example.com", "Boston", "US"},
		{"Donald Knuth", "donald@example.com", "Stanford", "US"},
		{"Margaret Hamilton", "margaret@example.com", "Cambridge", "US"},
		{"Edsger Dijkstra", "edsger@example.com", "Nuenen", "NL"},
		{"Ken Thompson", "ken@example.com", "Berkeley", "US"},
		{"Radia Perlman", "radia@example.com", "Seattle", "US"},
	}
	roles := [][]string{{"admin", "dev"}, {"dev"}, {"dev", "ops"}, {"viewer"}, {"dev"},
		{"admin"}, {"ops"}, {"dev"}, {"admin", "ops"}, {"viewer", "dev"}}

	for i, n := range names {
		_, err := s.createUser(User{
			Name: n.name, Email: n.email, Roles: roles[i],
			Contact: Contact{Email: n.email, Phone: fmt.Sprintf("+1-555-01%02d", i+1)},
			Billing: Address{Line1: fmt.Sprintf("%d Main St", (i+1)*10), City: n.city,
				Region: n.country, Country: n.country, Zip: fmt.Sprintf("%05d", 10000+i*7)},
			Active:   i%4 != 3,
			Metadata: map[string]string{"tier": []string{"free", "pro", "enterprise"}[i%3]},
			Audit:    Audit{CreatedBy: "seed"},
		})
		if err != nil {
			return fmt.Errorf("seeding user %d: %w", i, err)
		}
	}

	products := []struct {
		sku, title, desc string
		price            float64
		rating           float64
		tags             []string
	}{
		{"KB-01", "Mechanical Keyboard", "Tactile switches, 87 keys", 129.00, 4.6, []string{"input", "desk"}},
		{"MS-02", "Wireless Mouse", "Six buttons, USB-C", 59.50, 4.2, []string{"input"}},
		{"MN-03", "27\" 4K Monitor", "IPS panel, 60Hz", 429.99, 4.7, []string{"display", "desk"}},
		{"HD-04", "Noise Cancelling Headphones", "Over-ear, 30h battery", 249.00, 4.5, []string{"audio"}},
		{"DK-05", "Docking Station", "11 ports, 100W PD", 189.00, 4.0, []string{"desk", "power"}},
		{"CH-06", "Ergonomic Chair", "Lumbar support, mesh back", 549.00, 4.8, []string{"furniture"}},
		{"DS-07", "Standing Desk", "Electric, 120x70cm", 699.00, 4.4, []string{"furniture", "desk"}},
		{"WC-08", "1080p Webcam", "Autofocus, dual mic", 89.90, 3.9, []string{"video"}},
		{"SS-09", "1TB Portable SSD", "USB 3.2, 1050MB/s", 119.00, 4.6, []string{"storage"}},
		{"LP-10", "Laptop Stand", "Aluminium, adjustable", 45.00, 4.1, []string{"desk"}},
	}
	for i, p := range products {
		_, err := s.createProduct(Product{
			Sku: p.sku, Title: p.title, Description: p.desc,
			Image:      swatch(i),
			Price:      Money{Amount: p.price, Currency: "USD"},
			Tags:       p.tags,
			Attributes: map[string]string{"warranty": "24m"},
			InStock:    i%5 != 4,
			Rating:     p.rating,
			Audit:      Audit{CreatedBy: "seed"},
		})
		if err != nil {
			return fmt.Errorf("seeding product %d: %w", i, err)
		}
	}

	statuses := []string{"pending", "paid", "shipped", "delivered", "cancelled"}
	for i := 0; i < 10; i++ {
		productID := (i % 10) + 1
		_, err := s.createOrder(Order{
			UserID: (i % 10) + 1,
			Items: []LineItem{{
				ProductID: productID,
				Quantity:  (i % 3) + 1,
				Price:     Money{Amount: products[productID-1].price, Currency: "USD"},
			}},
			Status: statuses[i%len(statuses)],
			ShipTo: Address{Line1: fmt.Sprintf("%d Market St", (i+1)*5), City: names[i].city,
				Region: names[i].country, Country: names[i].country, Zip: fmt.Sprintf("%05d", 20000+i*3)},
			Placed: time.Now().UTC().AddDate(0, 0, -i),
			Audit:  Audit{CreatedBy: "seed"},
		})
		if err != nil {
			return fmt.Errorf("seeding order %d: %w", i, err)
		}
	}

	log.Printf("shop: seeded %d users, %d products, %d orders", len(names), len(products), 10)
	return nil
}

// swatch builds a small inline SVG so the seeded products have pictures without
// the example depending on a network or on files shipped alongside it.
func swatch(i int) string {
	colours := []string{"4f46e5", "0891b2", "16a34a", "ca8a04", "dc2626",
		"7c3aed", "db2777", "0d9488", "ea580c", "475569"}
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="120">`+
			`<rect width="120" height="120" fill="#%s"/>`+
			`<text x="60" y="72" font-family="sans-serif" font-size="44" fill="#fff" text-anchor="middle">%d</text></svg>`,
		colours[i%len(colours)], i+1)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}
