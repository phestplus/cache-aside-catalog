.PHONY: build test up down simulate logs

build:
	go build ./...

test:
	go test ./... -v

up:
	docker compose up -d --build

down:
	docker compose down -v

simulate:
	bash scripts/simulate.sh

logs:
	docker compose logs -f
