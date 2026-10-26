import axios from "axios";
export const api = axios.create({
  baseURL: "/api",
  withCredentials: true,
  timeout: 20000,
});
let access = "";
export function setAccess(value: string) {
  access = value;
}
api.interceptors.request.use((c) => {
  if (access) c.headers.Authorization = `Bearer ${access}`;
  return c;
});
let refreshing: Promise<string> | null = null;
api.interceptors.response.use(
  (r) => r,
  async (error) => {
    const c = error.config;
    if (
      error.response?.status === 401 &&
      c &&
      !c._retry &&
      !c.url.includes("/auth/")
    ) {
      c._retry = true;
      if (!refreshing)
        refreshing = api
          .post("/auth/refresh")
          .then((r) => {
            setAccess(r.data.access_token);
            return r.data.access_token;
          })
          .finally(() => {
            refreshing = null;
          });
      try {
        await refreshing;
        return api(c);
      } catch {
        setAccess("");
      }
    }
    return Promise.reject(error);
  },
);
export function errorText(e: unknown) {
  return axios.isAxiosError(e)
    ? e.response?.data?.error || "Не удалось связаться с сервером"
    : e instanceof Error
      ? e.message
      : "Не удалось выполнить действие";
}
export const money = (n: number) =>
  new Intl.NumberFormat("ru-RU", {
    style: "currency",
    currency: "RUB",
    maximumFractionDigits: 2,
  }).format(n / 100);
export const date = (s: string) => new Date(s).toLocaleString("ru-RU");
export type User = {
  id: string;
  name: string;
  email: string;
  role: string;
  active: boolean;
};
export type Product = {
  id: string;
  name: string;
  category: string;
  description: string;
  price: number;
  active: boolean;
  specs: Record<string, string>;
  available: number;
  reserved: number;
};
export type CartItem = {
  product_id: string;
  name: string;
  quantity: number;
  price: number;
  expected_price: number;
  available: number;
  active: boolean;
};
export type Order = {
  id: string;
  status: string;
  created_at: string;
  total: number;
  assembly: boolean;
  receiving: string;
  contact_name: string;
  phone: string;
  address: string;
  stock_state: string;
  customer_name?: string;
  items?: {
    name: string;
    price: number;
    quantity: number;
    specs: Record<string, string>;
  }[];
  history?: {
    id: number;
    status: string;
    note: string;
    created_at: string;
    actor_name?: string;
  }[];
  payment?: { provider_id: string; status: string; amount: number };
  shipment?: { tracking: string; status: string };
  assembly_task?: { status: string; test_result: string };
};
export const roles: Record<string, string> = {
  customer: "Покупатель",
  manager: "Менеджер",
  warehouse: "Сотрудник склада",
  assembler: "Сборщик",
  admin: "Администратор",
};
export const states: Record<string, string> = {
  awaiting_payment: "Ожидает оплаты",
  paid: "Оплачен",
  picking: "Комплектуется",
  picked: "Скомплектован",
  assembling: "В сборке",
  testing: "На тестировании",
  rework: "Требует доработки",
  ready: "Готов",
  shipping_pending: "Оформляется доставка",
  shipped: "Передан в доставку",
  completed: "Выполнен",
  cancelled: "Отменён",
  refund_pending: "Возврат средств",
  created: "Создан",
  refunded: "Средства возвращены",
  delivered: "Доставлен",
  planned: "Запланировано",
  passed: "Тест пройден",
  failed: "Тест не пройден",
};
export const specs: Record<string, string> = {
  socket: "Сокет",
  memory_type: "Тип памяти",
  form_factor: "Форм-фактор",
  tdp_w: "Потребление, Вт",
  power_w: "Мощность, Вт",
  length_mm: "Длина, мм",
  gpu_max_mm: "Макс. длина видеокарты, мм",
  board_formats: "Форматы плат",
  power_connector: "Разъём питания",
  connectors: "Разъёмы БП",
  capacity_gb: "Объём, ГБ",
  cores: "Ядер",
  vram_gb: "Видеопамять, ГБ",
  interface: "Интерфейс",
};
