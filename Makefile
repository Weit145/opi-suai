up:
	docker compose up

build:
	docker compose up --build

# delete:
# 	docker compose down -v --remove-orphans

down:
	docker compose down

.PHONY: app-init app-up app-down app-logs test

app-init:
	python3 deploy/init_local.py

app-up: app-init
	docker compose --env-file .env.app -f deploy/compose.app.yml up -d --build

app-down:
	docker compose --env-file .env.app -f deploy/compose.app.yml down

app-logs:
	docker compose --env-file .env.app -f deploy/compose.app.yml logs -f

test:
	./deploy/run-tests.sh

.PHONY: vm-up vm-down vm-status vm-ssh vm-logs vm-check

vm-up:
	python3 vm/manage.py up

vm-down:
	python3 vm/manage.py down

vm-status:
	python3 vm/manage.py status

vm-ssh:
	python3 vm/manage.py ssh

vm-logs:
	python3 vm/manage.py logs

vm-check:
	python3 vm/check.py
