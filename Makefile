up:
	docker compose up

build:
	docker compose up --build

# delete:
# 	docker compose down -v --remove-orphans

down:
	docker compose down

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
