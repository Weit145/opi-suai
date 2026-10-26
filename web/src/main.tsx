import React, {
  useState,
  useEffect,
  useRef,
  createContext,
  useContext,
  useCallback,
} from "react";
import ReactDOM from "react-dom/client";
import {
  BrowserRouter,
  Routes,
  Route,
  NavLink,
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import {
  api,
  setAccess,
  errorText,
  money,
  date,
  roles,
  states,
  specs,
  type User,
  type Product,
  type CartItem,
  type Order,
} from "./api";
import "./style.css";

type Context = {
  user: User | null;
  tick: number;
  refresh: () => void;
  setUser: (u: User | null) => void;
  notify: (s: string) => void;
  demo: boolean;
};
const AppContext = createContext<Context>(null!);
const useApp = () => useContext(AppContext);
function useData<T>(path: string | null) {
  const { tick } = useApp();
  const [data, setData] = useState<T | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(true);
  const load = useCallback(() => {
    if (!path) {
      setLoading(false);
      return;
    }
    setLoading(true);
    api
      .get<T>(path)
      .then((r) => {
        setData(r.data);
        setError("");
      })
      .catch((e) => setError(errorText(e)))
      .finally(() => setLoading(false));
  }, [path]);
  useEffect(load, [load, tick]);
  return { data, error, loading, reload: load };
}
function Alert({ text }: { text: string }) {
  return text ? (
    <div className="alert" role="alert">
      {text}
    </div>
  ) : null;
}
function Badge({ status }: { status: string }) {
  return <span className={`badge ${status}`}>{states[status] || status}</span>;
}
function Title({
  eyebrow,
  title,
  children,
}: {
  eyebrow?: string;
  title: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="page-title">
      {eyebrow && <p className="eyebrow">{eyebrow}</p>}
      <h1>{title}</h1>
      {children}
    </div>
  );
}
function Empty({ children }: { children: React.ReactNode }) {
  return <div className="empty">{children}</div>;
}
function Require({
  children,
  only,
}: {
  children: React.ReactNode;
  only?: string[];
}) {
  const { user } = useApp();
  if (!user)
    return (
      <Empty>
        Для этого раздела <Link to="/login">войдите в аккаунт</Link>.
      </Empty>
    );
  if (only && !only.includes(user.role) && user.role !== "admin")
    return <Empty>Этот раздел недоступен вашей роли.</Empty>;
  return <>{children}</>;
}
function App() {
  const [user, setUser] = useState<User | null>(null),
    [tick, setTick] = useState(0),
    [toast, setToast] = useState(""),
    [ready, setReady] = useState(false),
    [demo, setDemo] = useState(false);
  const refresh = useCallback(() => setTick((x) => x + 1), []);
  useEffect(() => {
    api
      .get("/info")
      .then((r) => setDemo(r.data.demo))
      .catch(() => {});
    api
      .post("/auth/refresh")
      .then((r) => {
        setAccess(r.data.access_token);
        setUser(r.data.user);
      })
      .catch(() => {})
      .finally(() => setReady(true));
  }, []);
  useEffect(() => {
    if (toast) {
      const t = setTimeout(() => setToast(""), 5000);
      return () => clearTimeout(t);
    }
  }, [toast]);
  const logout = async () => {
    await api.post("/auth/logout");
    setAccess("");
    setUser(null);
    refresh();
  };
  if (!ready)
    return <div className="boot">Загружаем рабочее пространство…</div>;
  return (
    <AppContext.Provider
      value={{ user, setUser, tick, refresh, notify: setToast, demo }}
    >
      <BrowserRouter>
        <header>
          <Link className="brand" to="/">
            <span className="brand-icon">К</span>комплект
            <span className="brand-dot">.</span>
          </Link>
          <nav aria-label="Основная навигация">
            <NavLink to="/" end>
              Каталог
            </NavLink>
            <NavLink to="/builder">Собрать ПК</NavLink>
            {user && (
              <>
                <NavLink to="/orders">Заказы</NavLink>
                <NavLink to="/cart">Корзина</NavLink>
              </>
            )}
            {user && ["manager", "warehouse", "admin"].includes(user.role) && (
              <NavLink to="/inventory">Склад</NavLink>
            )}
            {user && ["manager", "admin"].includes(user.role) && (
              <NavLink to="/catalog-admin">Товары</NavLink>
            )}
            {user?.role === "admin" && <NavLink to="/users">Команда</NavLink>}
          </nav>
          <div className="account">
            {user ? (
              <>
                <Link to="/notifications" title="Уведомления">
                  ◎
                </Link>
                <span>
                  {user.name}
                  <small>{roles[user.role]}</small>
                </span>
                <button
                  className="text-button"
                  onClick={() => logout().catch((e) => setToast(errorText(e)))}
                >
                  Выйти
                </button>
              </>
            ) : (
              <Link className="button secondary" to="/login">
                Войти
              </Link>
            )}
          </div>
        </header>
        {demo && (
          <div className="demo-strip">
            Учебная система · демонстрационные товары · платежи и доставка без
            реальных операций
          </div>
        )}
        <main>
          <Routes>
            <Route path="/" element={<Catalog />} />
            <Route path="/builder" element={<Builder />} />
            <Route path="/login" element={<Login />} />
            <Route
              path="/cart"
              element={
                <Require>
                  <Cart />
                </Require>
              }
            />
            <Route
              path="/orders"
              element={
                <Require>
                  <Orders />
                </Require>
              }
            />
            <Route
              path="/orders/:id"
              element={
                <Require>
                  <OrderDetail />
                </Require>
              }
            />
            <Route
              path="/inventory"
              element={
                <Require only={["warehouse", "manager"]}>
                  <Inventory />
                </Require>
              }
            />
            <Route
              path="/catalog-admin"
              element={
                <Require only={["manager"]}>
                  <CatalogAdmin />
                </Require>
              }
            />
            <Route
              path="/users"
              element={
                <Require only={[]}>
                  <Users />
                </Require>
              }
            />
            <Route
              path="/notifications"
              element={
                <Require>
                  <Notifications />
                </Require>
              }
            />
            <Route
              path="*"
              element={
                <Empty>
                  Страница не найдена. <Link to="/">Перейти в каталог</Link>
                </Empty>
              }
            />
          </Routes>
        </main>
        <footer>
          <strong>Комплект</strong>
          <span>Каталог → конфигурация → заказ → готовый компьютер</span>
          <span>ОПИ · вариант 34 · группа 4431</span>
        </footer>
        {toast && (
          <div className="toast" role="status">
            {toast}
          </div>
        )}
      </BrowserRouter>
    </AppContext.Provider>
  );
}

function Login() {
  const { setUser, refresh } = useApp(),
    navigate = useNavigate();
  const [register, setRegister] = useState(false),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const f = new FormData(e.currentTarget);
    try {
      const r = await api.post(
        register ? "/auth/register" : "/auth/login",
        register
          ? {
              email: f.get("email"),
              password: f.get("password"),
              name: f.get("name"),
            }
          : { email: f.get("email"), password: f.get("password") },
      );
      setAccess(r.data.access_token);
      setUser(r.data.user);
      refresh();
      navigate("/");
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="auth-layout">
      <div>
        <p className="eyebrow">Ваш следующий компьютер</p>
        <h1>
          Все детали.
          <br />В одном месте.
        </h1>
        <p className="muted">
          Подберите совместимые компоненты, оформите заказ и следите за каждым
          этапом сборки.
        </p>
        <div className="auth-art">
          <span>CPU</span>
          <span>RAM</span>
          <span>SSD</span>
          <span>GPU</span>
        </div>
      </div>
      <form className="panel auth-form" onSubmit={submit}>
        <h2>{register ? "Создать аккаунт" : "Вход в систему"}</h2>
        <Alert text={error} />
        {register && (
          <label>
            Ваше имя
            <input name="name" required maxLength={120} autoComplete="name" />
          </label>
        )}
        <label>
          Email
          <input name="email" type="email" required autoComplete="username" />
        </label>
        <label>
          Пароль
          <input
            name="password"
            type="password"
            required
            minLength={register ? 10 : undefined}
            maxLength={72}
            autoComplete={register ? "new-password" : "current-password"}
          />
        </label>
        <button disabled={busy}>
          {busy ? "Подождите…" : register ? "Зарегистрироваться" : "Войти"}
        </button>
        <button
          className="text-button"
          type="button"
          onClick={() => {
            setRegister(!register);
            setError("");
          }}
        >
          {register
            ? "Уже есть аккаунт? Войти"
            : "Нет аккаунта? Зарегистрироваться"}
        </button>
      </form>
    </div>
  );
}

const symbols: Record<string, string> = {
  cpu: "CPU",
  motherboard: "MB",
  ram: "RAM",
  storage: "SSD",
  gpu: "GPU",
  psu: "PSU",
  case: "PC",
  accessory: "USB",
};
function Catalog() {
  const { user, notify, refresh } = useApp(),
    navigate = useNavigate();
  const [q, setQ] = useState(""),
    [category, setCategory] = useState(""),
    [filter, setFilter] = useState(""),
    [value, setValue] = useState(""),
    [selected, setSelected] = useState<Product | null>(null),
    [error, setError] = useState("");
  const cats = useData<{ id: string; name: string }[]>("/categories");
  const query = new URLSearchParams({
    q,
    category,
    ...(filter && value ? { spec: JSON.stringify({ [filter]: value }) } : {}),
  });
  const {
    data,
    loading,
    error: loadError,
  } = useData<Product[]>("/products?" + query.toString());
  async function add(p: Product) {
    if (!user) {
      navigate("/login");
      return;
    }
    try {
      const r = await api.get<CartItem[]>("/cart");
      const items = r.data.map((i) => ({
        product_id: i.product_id,
        quantity: i.quantity,
        expected_price: i.expected_price,
      }));
      const old = items.find((x) => x.product_id === p.id);
      if (old) old.quantity++;
      else
        items.push({ product_id: p.id, quantity: 1, expected_price: p.price });
      await api.put("/cart", { items });
      notify("Товар добавлен в корзину");
      refresh();
    } catch (e) {
      setError(errorText(e));
    }
  }
  return (
    <>
      <section className="hero">
        <div>
          <p className="eyebrow">Комплектующие и сборка</p>
          <h1>
            Компьютер под
            <br />
            ваши задачи.
          </h1>
          <p>
            Выберите детали сами или соберите совместимую конфигурацию. Мы
            сохраним состав, цену и историю вашего заказа.
          </p>
          <Link className="button" to="/builder">
            Открыть конфигуратор <span>↗</span>
          </Link>
        </div>
        <div className="hero-device" aria-hidden="true">
          <div className="device-label">YOUR NEXT BUILD</div>
          <div className="device-fan">
            <span />
          </div>
          <div className="device-fan small">
            <span />
          </div>
          <div className="device-lines" />
          <div className="device-badge">
            СОВМЕСТИМОСТЬ
            <br />
            <strong>под контролем</strong>
          </div>
        </div>
      </section>
      <div className="section-heading">
        <h2>Каталог комплектующих</h2>
        <span className="muted">{data?.length || 0} товаров</span>
      </div>
      <div className="filters">
        <label className="search">
          Поиск
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Название компонента"
          />
        </label>
        <label>
          Категория
          <select
            value={category}
            onChange={(e) => setCategory(e.target.value)}
          >
            <option value="">Все категории</option>
            {cats.data?.map((c) => (
              <option value={c.id} key={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Характеристика
          <select value={filter} onChange={(e) => setFilter(e.target.value)}>
            <option value="">Без фильтра</option>
            {Object.entries(specs).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </label>
        {filter && (
          <label>
            Значение
            <input
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="Например, AM5"
            />
          </label>
        )}
      </div>
      <Alert text={error || loadError} />
      {loading && !data ? (
        <Empty>Загружаем каталог…</Empty>
      ) : data?.length === 0 ? (
        <Empty>По этим условиям товары не найдены.</Empty>
      ) : (
        <div className="product-grid">
          {data?.map((p) => (
            <article className="product-card" key={p.id}>
              <button
                className={"product-art " + p.category}
                onClick={() => setSelected(p)}
                aria-label={"Подробнее: " + p.name}
              >
                <span>{symbols[p.category] || "PC"}</span>
                <i />
              </button>
              <div className="product-copy">
                <span className="category-label">
                  {cats.data?.find((c) => c.id === p.category)?.name}
                </span>
                <button className="product-name" onClick={() => setSelected(p)}>
                  {p.name}
                </button>
                <p className="spec-preview">
                  {Object.entries(p.specs)
                    .slice(0, 3)
                    .map(([k, v]) => `${specs[k] || k}: ${v}`)
                    .join(" · ")}
                </p>
                <span className={p.available ? "stock" : "muted"}>
                  {p.available
                    ? "● В наличии: " + p.available
                    : "Нет в наличии"}
                </span>
                <div className="product-bottom">
                  <strong>{money(p.price)}</strong>
                  <button
                    className="small-button"
                    disabled={!p.available}
                    onClick={() => add(p)}
                  >
                    В корзину
                  </button>
                </div>
              </div>
            </article>
          ))}
        </div>
      )}
      {selected && (
        <div className="modal-backdrop" onClick={() => setSelected(null)}>
          <section
            className="modal panel"
            role="dialog"
            aria-modal="true"
            aria-label={selected.name}
            onClick={(e) => e.stopPropagation()}
          >
            <button
              className="close"
              onClick={() => setSelected(null)}
              aria-label="Закрыть"
            >
              ×
            </button>
            <p className="eyebrow">Карточка товара</p>
            <h2>{selected.name}</h2>
            <p>{selected.description}</p>
            <dl className="spec-list">
              {Object.entries(selected.specs).map(([k, v]) => (
                <React.Fragment key={k}>
                  <dt>{specs[k] || k}</dt>
                  <dd>{v}</dd>
                </React.Fragment>
              ))}
            </dl>
            <div className="product-bottom">
              <strong>{money(selected.price)}</strong>
              <button
                disabled={!selected.available}
                onClick={() => add(selected)}
              >
                Добавить в корзину
              </button>
            </div>
          </section>
        </div>
      )}
    </>
  );
}

function Builder() {
  const edited = useRef(false);
  const { user, notify } = useApp(),
    navigate = useNavigate();
  const { data: products } = useData<Product[]>("/products");
  const [chosen, setChosen] = useState<Record<string, string>>({}),
    [issues, setIssues] = useState<string[]>([]),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const categories = [
    ["cpu", "Процессор"],
    ["motherboard", "Материнская плата"],
    ["ram", "Оперативная память"],
    ["storage", "Накопитель"],
    ["psu", "Блок питания"],
    ["case", "Корпус"],
    ["gpu", "Видеокарта · необязательно"],
  ];
  useEffect(() => {
    if (user && products)
      api
        .get<string[]>("/configuration")
        .then((r) => {
          const next: Record<string, string> = {};
          for (const id of r.data) {
            const p = products.find((p) => p.id === id);
            if (p) next[p.category] = id;
          }
          if (!edited.current) setChosen(next);
        })
        .catch((e) => setError(errorText(e)));
  }, [user, products]);
  const ids = Object.values(chosen).filter(Boolean);
  useEffect(() => {
    let active = true;
    api
      .post("/compatibility", { ids, complete: true })
      .then((r) => {
        if (active) setIssues(r.data.issues);
      })
      .catch((e) => {
        if (active) setError(errorText(e));
      });
    return () => {
      active = false;
    };
  }, [JSON.stringify(ids)]);
  const total = ids.reduce(
    (sum, id) => sum + (products?.find((p) => p.id === id)?.price || 0),
    0,
  );
  async function save(cart: boolean) {
    if (!user) {
      navigate("/login");
      return;
    }
    setBusy(true);
    try {
      await api.put("/configuration", { ids });
      if (cart) {
        await api.put("/cart", {
          items: ids.map((id) => ({
            product_id: id,
            quantity: 1,
            expected_price: products!.find((p) => p.id === id)!.price,
          })),
        });
        navigate("/cart?assembly=1");
      } else notify("Конфигурация сохранена");
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <Title eyebrow="Конфигуратор" title="Соберите свою конфигурацию">
        <p className="muted">
          Проверяем сокет, память, габариты, мощность и разъёмы питания.
        </p>
      </Title>
      <Alert text={error} />
      <div className="split">
        <section className="panel builder-list">
          {categories.map(([id, name], index) => (
            <label className="builder-row" key={id}>
              <span className="step">{String(index + 1).padStart(2, "0")}</span>
              <span>
                <strong>{name}</strong>
                <select
                  value={chosen[id] || ""}
                  onChange={(e) => {
                    edited.current = true;
                    const value = e.target.value;
                    setChosen((previous) => ({ ...previous, [id]: value }));
                  }}
                >
                  <option value="">Выберите компонент</option>
                  {products
                    ?.filter((p) => p.category === id)
                    .map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name} — {money(p.price)}
                      </option>
                    ))}
                </select>
              </span>
            </label>
          ))}
        </section>
        <aside className="panel summary">
          <p className="eyebrow">Ваша сборка</p>
          <div className="big-price">{money(total)}</div>
          <p>{ids.length} компонентов</p>
          <div className={issues.length ? "compat warning" : "compat success"}>
            <strong>
              {issues.length ? "Нужно проверить" : "Компоненты совместимы"}
            </strong>
            {issues.length > 0 && (
              <ul>
                {issues.map((x, i) => (
                  <li key={i}>{x}</li>
                ))}
              </ul>
            )}
          </div>
          <button
            disabled={busy || issues.length > 0}
            onClick={() => save(true)}
          >
            Перенести сборку в корзину
          </button>
          <small className="muted">Заменит текущее содержимое корзины.</small>
          <button
            className="secondary"
            disabled={busy}
            onClick={() => save(false)}
          >
            Сохранить конфигурацию
          </button>
        </aside>
      </div>
    </>
  );
}

function Cart() {
  const { refresh } = useApp(),
    navigate = useNavigate(),
    [params] = useSearchParams();
  const { data: items, error: loadError } = useData<CartItem[]>("/cart");
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [assembly, setAssembly] = useState(params.get("assembly") === "1"),
    [receiving, setReceiving] = useState("pickup");
  const [key, setKey] = useState(() => crypto.randomUUID());
  const changed = items?.some((i) => i.price !== i.expected_price);
  async function update(next: CartItem[], prices = false) {
    try {
      await api.put("/cart", {
        items: next.map((i) => ({
          product_id: i.product_id,
          quantity: i.quantity,
          expected_price: prices ? i.price : i.expected_price,
        })),
      });
      setKey(crypto.randomUUID());
      refresh();
    } catch (e) {
      setError(errorText(e));
    }
  }
  async function checkout(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const f = new FormData(e.currentTarget);
    try {
      const r = await api.post("/orders", {
        items: items?.map((i) => ({
          product_id: i.product_id,
          quantity: i.quantity,
          expected_price: i.expected_price,
        })),
        assembly,
        configuration: assembly,
        receiving,
        contact_name: f.get("contact_name"),
        phone: f.get("phone"),
        address: f.get("address") || "",
        checkout_key: key,
      });
      refresh();
      navigate("/orders/" + r.data.id);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <Title eyebrow="Оформление" title="Ваша корзина" />
      <Alert text={error || loadError} />
      {!items?.length ? (
        <Empty>
          В корзине пока пусто. <Link to="/">Выбрать комплектующие</Link>
        </Empty>
      ) : (
        <div className="split">
          <section className="panel">
            {items.map((i) => (
              <div className="cart-row" key={i.product_id}>
                <div>
                  <strong>{i.name}</strong>
                  <p className="muted">
                    {money(i.expected_price)} за штуку · доступно {i.available}
                  </p>
                  {i.price !== i.expected_price && (
                    <p className="danger">Новая цена: {money(i.price)}</p>
                  )}
                  {!i.active && <p className="danger">Товар снят с продажи</p>}
                </div>
                <label>
                  Количество
                  <input
                    aria-label={"Количество: " + i.name}
                    type="number"
                    min={1}
                    max={100}
                    value={i.quantity}
                    onChange={(e) => {
                      const qty = Number(e.target.value);
                      if (qty >= 1)
                        update(
                          items.map((x) =>
                            x.product_id === i.product_id
                              ? { ...x, quantity: qty }
                              : x,
                          ),
                        );
                    }}
                  />
                </label>
                <button
                  className="text-button"
                  onClick={() =>
                    update(items.filter((x) => x.product_id !== i.product_id))
                  }
                >
                  Удалить
                </button>
              </div>
            ))}
            {changed && (
              <div className="compat warning">
                <p>
                  Цена некоторых товаров изменилась. Подтвердите актуальную
                  стоимость перед оформлением.
                </p>
                <button onClick={() => update(items, true)}>
                  Подтвердить новые цены
                </button>
              </div>
            )}
          </section>
          <form className="panel summary" onSubmit={checkout}>
            <h2>Оформить заказ</h2>
            <div className="big-price">
              {money(
                items.reduce((s, i) => s + i.expected_price * i.quantity, 0),
              )}
            </div>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={assembly}
                onChange={(e) => setAssembly(e.target.checked)}
              />
              Сборка и тестирование ПК
            </label>
            <label>
              Получение
              <select
                value={receiving}
                onChange={(e) => setReceiving(e.target.value)}
              >
                <option value="pickup">Самовывоз</option>
                <option value="delivery">Доставка</option>
              </select>
            </label>
            <label>
              Контактное имя
              <input name="contact_name" required maxLength={120} />
            </label>
            <label>
              Телефон
              <input
                name="phone"
                type="tel"
                required
                minLength={7}
                maxLength={30}
              />
            </label>
            {receiving === "delivery" && (
              <label>
                Адрес доставки
                <textarea name="address" required maxLength={500} />
              </label>
            )}
            <p className="muted">
              При оформлении цена фиксируется, товары резервируются. Оплата
              выполняется следующим шагом.
            </p>
            <button disabled={busy || changed || items.some((i) => !i.active)}>
              {busy ? "Создаём заказ…" : "Подтвердить заказ"}
            </button>
          </form>
        </div>
      )}
    </>
  );
}

function Orders() {
  const { user } = useApp();
  const { data, error } = useData<Order[]>("/orders");
  return (
    <>
      <Title
        eyebrow="Заказы"
        title={
          user?.role === "customer" ? "История ваших заказов" : "Рабочие заказы"
        }
      />
      <Alert text={error} />
      {data?.length ? (
        <div className="order-list">
          {data.map((o) => (
            <Link
              className="panel order-card"
              to={"/orders/" + o.id}
              key={o.id}
            >
              <div>
                <span className="eyebrow">№ {o.id.slice(0, 8)}</span>
                <h3>{o.assembly ? "Компьютер со сборкой" : "Комплектующие"}</h3>
                <p className="muted">
                  {date(o.created_at)} ·{" "}
                  {o.receiving === "delivery" ? "Доставка" : "Самовывоз"}
                  {user?.role !== "customer" ? " · " + o.customer_name : ""}
                </p>
              </div>
              <Badge status={o.status} />
              <strong>{money(o.total)}</strong>
              <span>→</span>
            </Link>
          ))}
        </div>
      ) : (
        <Empty>Заказов пока нет.</Empty>
      )}
    </>
  );
}

function OrderDetail() {
  const { id } = useParams(),
    { user, refresh, demo, notify } = useApp();
  const { data: o, error: loadError, reload } = useData<Order>("/orders/" + id);
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [note, setNote] = useState("");
  const can = (...r: string[]) =>
    user?.role === "admin" || r.includes(user?.role || "");
  async function action(path: string, payload: unknown = {}) {
    setBusy(true);
    setError("");
    try {
      await api.post("/orders/" + id + path, payload);
      reload();
      refresh();
      notify("Действие выполнено");
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    if (o?.status === "refund_pending") {
      const t = setInterval(reload, 4000);
      return () => clearInterval(t);
    }
  }, [o?.status, reload]);
  if (!o)
    return (
      <>
        <Alert text={loadError} />
        <Empty>Загрузка заказа…</Empty>
      </>
    );
  const actions: { code: string; label: string; show: boolean }[] = [
    {
      code: "pick",
      label: "Начать комплектацию",
      show: o.status === "paid" && can("warehouse"),
    },
    {
      code: "picked",
      label: "Комплектация завершена",
      show: o.status === "picking" && can("warehouse"),
    },
    {
      code: "assemble",
      label: "Начать сборку",
      show: ["picked", "rework"].includes(o.status) && can("assembler"),
    },
    {
      code: "test",
      label: "Передать на тестирование",
      show: o.status === "assembling" && can("assembler"),
    },
    {
      code: "pass",
      label: "Тест пройден",
      show: o.status === "testing" && can("assembler"),
    },
    {
      code: "fail",
      label: "Тест не пройден",
      show: o.status === "testing" && can("assembler"),
    },
    {
      code: "pickup",
      label: "Подтвердить выдачу",
      show:
        o.status === "ready" &&
        o.receiving === "pickup" &&
        can("warehouse", "manager"),
    },
  ];
  return (
    <>
      <Link className="back" to="/orders">
        ← Все заказы
      </Link>
      <Title
        eyebrow={"Заказ № " + o.id.slice(0, 8)}
        title={o.assembly ? "Компьютер со сборкой" : "Комплектующие"}
      >
        <Badge status={o.status} />
      </Title>
      <Alert text={error || loadError} />
      <div className="split">
        <div>
          <section className="panel">
            <h2>Состав заказа</h2>
            {o.items?.map((i, n) => (
              <div className="line-item" key={n}>
                <span>
                  {i.name}
                  <small>
                    {i.quantity} шт. × {money(i.price)}
                  </small>
                </span>
                <strong>{money(i.price * i.quantity)}</strong>
              </div>
            ))}
            <div className="line-item total">
              <strong>Итого</strong>
              <strong>{money(o.total)}</strong>
            </div>
            <p className="muted">Цены зафиксированы при оформлении.</p>
          </section>
          <section className="panel">
            <h2>История заказа</h2>
            <ol className="timeline">
              {o.history?.map((h) => (
                <li key={h.id}>
                  <div>
                    <strong>{states[h.status] || h.status}</strong>
                    <time>{date(h.created_at)}</time>
                  </div>
                  <p>{h.note}</p>
                  {h.actor_name && <small>{h.actor_name}</small>}
                </li>
              ))}
            </ol>
          </section>
        </div>
        <aside>
          <section className="panel summary">
            <h2>Получение</h2>
            <p>
              {o.contact_name}
              <br />
              {o.phone}
            </p>
            <p>{o.receiving === "pickup" ? "Самовывоз" : o.address}</p>
            {o.shipment && (
              <>
                <strong>Трек: {o.shipment.tracking}</strong>
                <Badge status={o.shipment.status} />
                <button
                  className="secondary"
                  disabled={busy}
                  onClick={() => action("/shipment/sync")}
                >
                  Обновить доставку
                </button>
                {demo &&
                  can("manager", "warehouse") &&
                  o.status === "shipped" && (
                    <button
                      disabled={busy}
                      onClick={() => action("/demo-delivery")}
                    >
                      Учебная доставка завершена
                    </button>
                  )}
              </>
            )}
            {o.assembly_task && (
              <div className="compat">
                <strong>Задание на сборку</strong>
                <p>
                  {states[o.assembly_task.status] || o.assembly_task.status}
                </p>
                {o.assembly_task.test_result && (
                  <Badge status={o.assembly_task.test_result} />
                )}
              </div>
            )}
            {o.payment && (
              <p>
                Оплата: <Badge status={o.payment.status} />
              </p>
            )}
            {o.status === "awaiting_payment" &&
              (user?.role === "customer" || can("manager")) && (
                <>
                  {!o.payment?.provider_id ? (
                    <button disabled={busy} onClick={() => action("/payment")}>
                      Создать {demo ? "учебный " : ""}платёж
                    </button>
                  ) : (
                    demo && (
                      <button
                        disabled={busy}
                        onClick={() => action("/demo-payment")}
                      >
                        Подтвердить учебную оплату
                      </button>
                    )
                  )}
                </>
              )}
            {actions.some((a) => a.show) && (
              <label>
                Примечание
                <textarea
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder="Результат работы или причина доработки"
                />
              </label>
            )}
            {actions
              .filter((a) => a.show)
              .map((a) => (
                <button
                  key={a.code}
                  disabled={busy}
                  className={a.code === "fail" ? "secondary" : ""}
                  onClick={() => action("/action", { action: a.code, note })}
                >
                  {a.label}
                </button>
              ))}
            {["ready", "shipping_pending"].includes(o.status) &&
              o.receiving === "delivery" &&
              can("manager", "warehouse") && (
                <button disabled={busy} onClick={() => action("/shipment")}>
                  {o.status === "shipping_pending"
                    ? "Повторить оформление доставки"
                    : "Передать в доставку"}
                </button>
              )}
            {![
              "cancelled",
              "refund_pending",
              "shipping_pending",
              "shipped",
              "completed",
            ].includes(o.status) &&
              (can("manager") ||
                (user?.role === "customer" &&
                  !(
                    o.assembly &&
                    ["assembling", "testing", "rework", "ready"].includes(
                      o.status,
                    )
                  ))) && (
                <button
                  className="secondary danger"
                  disabled={busy}
                  onClick={() => action("/cancel")}
                >
                  Отменить заказ
                </button>
              )}
            {o.stock_state === "return_pending" && can("warehouse") && (
              <>
                <label>
                  Проверка возвращённого товара
                  <textarea
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                    required
                  />
                </label>
                <button
                  disabled={busy || !note.trim()}
                  onClick={() => action("/stock-return", { note })}
                >
                  Вернуть проверенный товар на склад
                </button>
              </>
            )}
          </section>
        </aside>
      </div>
    </>
  );
}

function Inventory() {
  const { user, refresh, notify } = useApp();
  const { data, error: loadError } = useData<{
    products: Product[];
    movements: {
      id: number;
      name: string;
      quantity: number;
      kind: string;
      note: string;
      created_at: string;
    }[];
  }>("/inventory");
  const [error, setError] = useState("");
  async function movement(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget,
      f = new FormData(form);
    try {
      await api.post("/inventory/movement", {
        product_id: f.get("product_id"),
        kind: f.get("kind"),
        quantity: Number(f.get("quantity")),
        note: f.get("note"),
      });
      form.reset();
      refresh();
      notify("Движение зарегистрировано");
    } catch (e) {
      setError(errorText(e));
    }
  }
  return (
    <>
      <Title eyebrow="Склад" title="Остатки и движения">
        <p className="muted">
          Свободный остаток и резерв учитываются отдельно. Возврат из
          отменённого заказа — в карточке заказа.
        </p>
      </Title>
      <Alert text={error || loadError} />
      {["warehouse", "admin"].includes(user?.role || "") && (
        <form className="panel inline-form" onSubmit={movement}>
          <label>
            Товар
            <select name="product_id" required>
              {data?.products.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Операция
            <select name="kind">
              <option value="receipt">Приход</option>
              <option value="writeoff">Списание</option>
              <option value="return">Прочий проверенный возврат</option>
            </select>
          </label>
          <label>
            Количество
            <input
              name="quantity"
              type="number"
              min={1}
              max={10000}
              required
              defaultValue={1}
            />
          </label>
          <label>
            Основание
            <input name="note" required maxLength={500} />
          </label>
          <button>Провести</button>
        </form>
      )}
      <div className="panel table-wrap">
        <table>
          <thead>
            <tr>
              <th>Товар</th>
              <th>Доступно</th>
              <th>В резерве</th>
              <th>В продаже</th>
            </tr>
          </thead>
          <tbody>
            {data?.products.map((p) => (
              <tr key={p.id}>
                <td>{p.name}</td>
                <td>{p.available}</td>
                <td>{p.reserved}</td>
                <td>{p.active ? "Да" : "Нет"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <section className="panel">
        <h2>Последние движения</h2>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Когда</th>
                <th>Товар</th>
                <th>Изменение свободного остатка</th>
                <th>Основание</th>
              </tr>
            </thead>
            <tbody>
              {data?.movements.map((m) => (
                <tr key={m.id}>
                  <td>{date(m.created_at)}</td>
                  <td>{m.name}</td>
                  <td>
                    {m.quantity > 0 ? "+" : ""}
                    {m.quantity}
                  </td>
                  <td>{m.note}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}

const blankProduct: Product = {
  id: "",
  name: "",
  category: "cpu",
  description: "",
  price: 100,
  active: true,
  specs: {},
  available: 0,
  reserved: 0,
};
function CatalogAdmin() {
  const { refresh, notify } = useApp();
  const { data: products, error: loadError } =
      useData<Product[]>("/staff/products"),
    { data: cats } = useData<{ id: string; name: string }[]>("/categories");
  const [edit, setEdit] = useState<Product | null>(null),
    [error, setError] = useState(""),
    [attribute, setAttribute] = useState("socket");
  async function save(e: React.FormEvent) {
    e.preventDefault();
    try {
      await api.post("/staff/products", edit);
      setEdit(null);
      refresh();
      notify("Товар сохранён");
    } catch (e) {
      setError(errorText(e));
    }
  }
  async function category(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget,
      f = new FormData(form);
    try {
      await api.post("/staff/categories", {
        id: f.get("id"),
        name: f.get("name"),
      });
      form.reset();
      refresh();
      notify("Категория сохранена");
    } catch (e) {
      setError(errorText(e));
    }
  }
  return (
    <>
      <Title eyebrow="Управление каталогом" title="Товары и категории" />
      <Alert text={error || loadError} />
      <div className="toolbar">
        <button onClick={() => setEdit({ ...blankProduct, specs: {} })}>
          Добавить товар
        </button>
        <span className="muted">
          Исторические цены заказов сохраняются при изменении каталога.
        </span>
      </div>
      {edit && (
        <form className="panel" onSubmit={save}>
          <h2>{edit.id ? "Редактировать товар" : "Новый товар"}</h2>
          <div className="form-grid">
            <label>
              Название
              <input
                required
                value={edit.name}
                onChange={(e) => setEdit({ ...edit, name: e.target.value })}
              />
            </label>
            <label>
              Категория
              <select
                value={edit.category}
                onChange={(e) => setEdit({ ...edit, category: e.target.value })}
              >
                {cats?.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Цена, ₽
              <input
                type="number"
                min="0.01"
                step="0.01"
                required
                value={edit.price / 100}
                onChange={(e) =>
                  setEdit({
                    ...edit,
                    price: Math.round(Number(e.target.value) * 100),
                  })
                }
              />
            </label>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={edit.active}
                onChange={(e) => setEdit({ ...edit, active: e.target.checked })}
              />
              Показывать в каталоге
            </label>
          </div>
          <label>
            Описание
            <textarea
              value={edit.description}
              onChange={(e) =>
                setEdit({ ...edit, description: e.target.value })
              }
            />
          </label>
          <h3>Характеристики</h3>
          <div className="form-grid">
            {Object.entries(edit.specs).map(([k, v]) => (
              <label key={k}>
                {specs[k] || k}
                <div className="field-action">
                  <input
                    value={v}
                    onChange={(e) =>
                      setEdit({
                        ...edit,
                        specs: { ...edit.specs, [k]: e.target.value },
                      })
                    }
                  />
                  <button
                    type="button"
                    className="secondary"
                    aria-label={"Удалить характеристику " + (specs[k] || k)}
                    onClick={() => {
                      const next = { ...edit.specs };
                      delete next[k];
                      setEdit({ ...edit, specs: next });
                    }}
                  >
                    ×
                  </button>
                </div>
              </label>
            ))}
          </div>
          <div className="toolbar">
            <label>
              Добавить характеристику
              <select
                value={attribute}
                onChange={(e) => setAttribute(e.target.value)}
              >
                {Object.entries(specs).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </label>
            <button
              type="button"
              className="secondary"
              onClick={() =>
                setEdit({
                  ...edit,
                  specs: {
                    ...edit.specs,
                    [attribute]: edit.specs[attribute] || "",
                  },
                })
              }
            >
              Добавить
            </button>
          </div>
          <div className="toolbar">
            <button>Сохранить товар</button>
            <button
              type="button"
              className="secondary"
              onClick={() => setEdit(null)}
            >
              Закрыть
            </button>
          </div>
        </form>
      )}
      <div className="panel table-wrap">
        <table>
          <thead>
            <tr>
              <th>Название</th>
              <th>Категория</th>
              <th>Цена</th>
              <th>Статус</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {products?.map((p) => (
              <tr key={p.id}>
                <td>{p.name}</td>
                <td>{cats?.find((c) => c.id === p.category)?.name}</td>
                <td>{money(p.price)}</td>
                <td>{p.active ? "В продаже" : "Скрыт"}</td>
                <td>
                  <button
                    className="text-button"
                    onClick={() => setEdit({ ...p, specs: { ...p.specs } })}
                  >
                    Изменить
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <form className="panel inline-form" onSubmit={category}>
        <h3>Категория</h3>
        <label>
          Код
          <input name="id" required placeholder="accessory" maxLength={40} />
        </label>
        <label>
          Название
          <input
            name="name"
            required
            placeholder="Аксессуары"
            maxLength={100}
          />
        </label>
        <button>Создать / переименовать</button>
        <p className="muted">
          Для переименования укажите существующий код. Коды базовых категорий
          используются конфигуратором.
        </p>
      </form>
    </>
  );
}

function Users() {
  const { user, refresh, notify } = useApp();
  const { data, error: loadError } = useData<User[]>("/users");
  const [error, setError] = useState("");
  async function create(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget,
      f = new FormData(form);
    try {
      await api.post("/users", {
        name: f.get("name"),
        email: f.get("email"),
        password: f.get("password"),
        role: f.get("role"),
      });
      form.reset();
      refresh();
      notify("Учётная запись создана");
    } catch (e) {
      setError(errorText(e));
    }
  }
  async function update(u: User, role: string, active: boolean) {
    try {
      await api.patch("/users/" + u.id, { role, active });
      refresh();
    } catch (e) {
      setError(errorText(e));
    }
  }
  return (
    <>
      <Title eyebrow="Администрирование" title="Пользователи и права" />
      <Alert text={error || loadError} />
      <form className="panel inline-form" onSubmit={create}>
        <label>
          Имя
          <input name="name" required maxLength={120} />
        </label>
        <label>
          Email
          <input name="email" type="email" required />
        </label>
        <label>
          Начальный пароль
          <input
            name="password"
            type="password"
            required
            minLength={10}
            maxLength={72}
            autoComplete="new-password"
          />
        </label>
        <label>
          Роль
          <select name="role">
            {Object.entries(roles).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <button>Создать</button>
      </form>
      <div className="panel table-wrap">
        <table>
          <thead>
            <tr>
              <th>Пользователь</th>
              <th>Email</th>
              <th>Роль</th>
              <th>Доступ</th>
            </tr>
          </thead>
          <tbody>
            {data?.map((u) => (
              <tr key={u.id}>
                <td>{u.name}</td>
                <td>{u.email}</td>
                <td>
                  <select
                    aria-label={"Роль " + u.name}
                    disabled={u.id === user?.id}
                    value={u.role}
                    onChange={(e) => update(u, e.target.value, u.active)}
                  >
                    {Object.entries(roles).map(([k, v]) => (
                      <option key={k} value={k}>
                        {v}
                      </option>
                    ))}
                  </select>
                </td>
                <td>
                  <label className="checkbox">
                    <input
                      type="checkbox"
                      disabled={u.id === user?.id}
                      checked={u.active}
                      onChange={(e) => update(u, u.role, e.target.checked)}
                    />
                    Активен
                  </label>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
function Notifications() {
  const { data, error } = useData<
    {
      id: string;
      order_id: string;
      message: string;
      created_at: string;
      delivered: boolean;
    }[]
  >("/notifications");
  return (
    <>
      <Title eyebrow="Личный кабинет" title="Уведомления" />
      <Alert text={error} />
      {data?.length ? (
        <div className="order-list">
          {data.map((n) => (
            <Link
              className="panel notification"
              key={n.id}
              to={"/orders/" + n.order_id}
            >
              <span className="notification-dot" />
              <div>
                <p>{n.message}</p>
                <small>
                  {date(n.created_at)} ·{" "}
                  {n.delivered
                    ? "Учебная доставка уведомления подтверждена"
                    : "Ожидает доставки"}
                </small>
              </div>
              <span>→</span>
            </Link>
          ))}
        </div>
      ) : (
        <Empty>Уведомлений пока нет.</Empty>
      )}
    </>
  );
}
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
