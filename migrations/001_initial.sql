CREATE TABLE IF NOT EXISTS users (
 id uuid PRIMARY KEY, email text NOT NULL UNIQUE, name text NOT NULL, password_hash text NOT NULL,
 role text NOT NULL CHECK (role IN ('customer','manager','warehouse','assembler','admin')),
 active boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS refresh_tokens (
 token_hash text PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id), expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS categories (id text PRIMARY KEY, name text NOT NULL);
CREATE TABLE IF NOT EXISTS products (
 id uuid PRIMARY KEY, name text NOT NULL CHECK(length(name)>0), category text NOT NULL REFERENCES categories(id),
 description text NOT NULL DEFAULT '', price bigint NOT NULL CHECK(price>0 AND price<=10000000000),
 active boolean NOT NULL DEFAULT true, specs jsonb NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS stock (
 product_id uuid PRIMARY KEY REFERENCES products(id), available integer NOT NULL DEFAULT 0 CHECK(available>=0),
 reserved integer NOT NULL DEFAULT 0 CHECK(reserved>=0)
);
CREATE TABLE IF NOT EXISTS cart_items (
 user_id uuid NOT NULL REFERENCES users(id), product_id uuid NOT NULL REFERENCES products(id),
 quantity integer NOT NULL CHECK(quantity>0 AND quantity<=100), expected_price bigint NOT NULL,
 PRIMARY KEY(user_id,product_id)
);
CREATE TABLE IF NOT EXISTS configurations (
 user_id uuid PRIMARY KEY REFERENCES users(id), items jsonb NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS orders (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT now(),
 status text NOT NULL DEFAULT 'awaiting_payment', total bigint NOT NULL CHECK(total>0), assembly boolean NOT NULL,
 receiving text NOT NULL CHECK(receiving IN ('pickup','delivery')), contact_name text NOT NULL, phone text NOT NULL,
 address text NOT NULL DEFAULT '', stock_state text NOT NULL DEFAULT 'reserved' CHECK(stock_state IN ('reserved','consumed','released','return_pending','returned')),
 checkout_key uuid NOT NULL, UNIQUE(user_id,checkout_key)
);
CREATE TABLE IF NOT EXISTS order_items (
 order_id uuid NOT NULL REFERENCES orders(id), product_id uuid NOT NULL REFERENCES products(id),
 name text NOT NULL, specs jsonb NOT NULL, price bigint NOT NULL CHECK(price>0), quantity integer NOT NULL CHECK(quantity>0),
 PRIMARY KEY(order_id,product_id)
);
CREATE TABLE IF NOT EXISTS order_events (
 id bigserial PRIMARY KEY, order_id uuid NOT NULL REFERENCES orders(id), status text NOT NULL,
 note text NOT NULL, actor_id uuid REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS assembly_tasks (
 order_id uuid PRIMARY KEY REFERENCES orders(id), assignee_id uuid REFERENCES users(id), status text NOT NULL,
 test_result text, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS payments (
 order_id uuid PRIMARY KEY REFERENCES orders(id), provider_id text UNIQUE, status text NOT NULL DEFAULT 'created',
 amount bigint NOT NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS payment_events (
 event_id text PRIMARY KEY, order_id uuid NOT NULL REFERENCES orders(id), kind text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS shipments (
 order_id uuid PRIMARY KEY REFERENCES orders(id), provider_id text UNIQUE, tracking text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'created', updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS stock_movements (
 id bigserial PRIMARY KEY, product_id uuid NOT NULL REFERENCES products(id), quantity integer NOT NULL,
 kind text NOT NULL, note text NOT NULL, actor_id uuid REFERENCES users(id), order_id uuid REFERENCES orders(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS notifications (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id), order_id uuid NOT NULL REFERENCES orders(id),
 message text NOT NULL, delivered boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS provider_records (
 kind text NOT NULL, request_key text NOT NULL, payload jsonb NOT NULL, PRIMARY KEY(kind,request_key)
);
CREATE INDEX IF NOT EXISTS orders_user_created ON orders(user_id,created_at DESC);
CREATE INDEX IF NOT EXISTS products_category ON products(category);
CREATE INDEX IF NOT EXISTS notifications_pending ON notifications(created_at) WHERE delivered=false;
