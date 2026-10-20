# opi-suai

## Виртуальная машина для лабораторной

Отдельная Ubuntu 24.04 в QEMU/KVM с Redmine 6.1.5, PostgreSQL,
nginx/Puma, темой Bleuclair и Git. Redmine устанавливается из исходников
внутри гостевой ОС.

```sh
make vm-up      # запустить ВМ; при первом запуске создать и установить
make vm-status  # проверить состояние
make vm-ssh     # войти в Ubuntu внутри ВМ
make vm-down    # штатно выключить ВМ с сохранением данных
```

Redmine в ВМ: http://localhost:8082. Логин `admin`, сгенерированный пароль
сохранён в локальном файле `.vm/credentials.txt`. При первом запуске дождитесь
окончания установки (`make vm-logs`).

Подробности, команды для отчёта и отличия от методички: [vm/README.md](vm/README.md).
