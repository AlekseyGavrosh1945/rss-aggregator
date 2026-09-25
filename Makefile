BINARY := rss-aggregator

.PHONY: run build test vet tidy up down logs

## run: запустить приложение локально (нужен PostgreSQL и переменные окружения)
run:
	go run ./cmd/aggregator

## build: собрать бинарник
build:
	CGO_ENABLED=0 go build -o $(BINARY) ./cmd/aggregator

## test: прогнать тесты (TEST_DATABASE_URL включает интеграционные)
test:
	go test -race ./...

## vet: статический анализ
vet:
	go vet ./...

## tidy: почистить зависимости
tidy:
	go mod tidy

## up: поднять приложение вместе с PostgreSQL в Docker
up:
	docker compose up -d --build

## down: остановить и удалить контейнеры вместе с данными
down:
	docker compose down -v

## logs: следить за логами приложения
logs:
	docker compose logs -f app
