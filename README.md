# opi-suai
ТЕПЕРЬ ВЕРСИЯ 0.1.0 !!
## Redmine

Запуск: `docker compose up -d`. Интерфейс: http://localhost:8081
при `REDMINE_PORT=8081` в `.env` (порт 8080 занят другим экземпляром Redmine).
Если `REDMINE_PORT` не указан, используется порт 8080.

Установлена тема [Bleuclair](https://github.com/farend/redmine_theme_farend_bleuclair)
в `themes/bleuclair`. Использован тег `redmine6.0`, коммит
`f836de9a25dc801dd16700c5729a03eccb6b0692`; лицензия GPL-2.0
сохранена в каталоге темы. Файлы темы подключаются через Docker Compose.

После первого запуска выберите «Администрирование → Настройки → Отображение →
Тема → Bleuclair» и сохраните настройки. Также можно включить тему командой:

```sh
docker compose exec redmine bundle exec rails runner -e production 'Setting.ui_theme = "bleuclair"'
```

Настройка сохраняется в базе данных. Для возврата к стандартной теме выберите
её на той же странице настроек.

default:
login:admin
password:admin
