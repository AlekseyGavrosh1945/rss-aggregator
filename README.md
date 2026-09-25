# RSS Aggregator

Телеграм-бот — агрегатор RSS/Atom-лент: подписывайся на фиды и получай новые
посты прямо в чат. Написан на Go.

[![CI](https://github.com/AlekseyGavrosh1945/rss-aggregator/actions/workflows/ci.yml/badge.svg)](https://github.com/AlekseyGavrosh1945/rss-aggregator/actions/workflows/ci.yml)

## Возможности

- **Telegram-бот** с командами `/add`, `/list`, `/remove`, `/help`;
  подписки привязаны к чату, уведомления приходят автоматически
- **Автопоиск ленты**: в `/add` можно дать адрес сайта — бот сам найдёт
  RSS/Atom в разметке страницы (тег `<link rel="alternate">`)
- **Планировщик на worker-пуле**: фиды опрашиваются параллельно
  ограниченным числом горутин, интервал настраивается
- **Дедупликация постов** на уровне БД: уникальный индекс `(feed_id, guid)`,
  вставка одним запросом через `unnest(...)` + `ON CONFLICT ... RETURNING`
- **Живой фид-парсер**: RSS 2.0 и Atom (gofeed), с фолбэком GUID → link
- **Мониторинг ошибок**: фид, который не удалось забрать, помечается
  в `/list` значком ⚠️
- **Миграции**, встроенные в бинарник и применяемые при старте
  (идемпотентно, в транзакциях)
- **HTTP health-check** `GET /healthz` — 200/503 в зависимости от доступности БД
- **Graceful shutdown** по SIGINT/SIGTERM: HTTP, бот и планировщик
  останавливаются корректно

## Быстрый старт (Docker)

```bash
git clone https://github.com/AlekseyGavrosh1945/rss-aggregator
cd rss-aggregator
cp .env.example .env         # впиши BOT_TOKEN от @BotFather
docker compose up -d --build
docker compose logs -f app
```

PostgreSQL поднимется вместе с приложением, миграции применятся сами.

## Команды бота

| Команда | Описание |
| --- | --- |
| `/add <url>` | подписаться на RSS/Atom-фид (можно просто адрес сайта) |
| `/list` | список подписок (нерабочие помечены ⚠️) |
| `/remove <url>` | отписаться |
| `/start`, `/help` | справка |

## Архитектура

```
Telegram ──команды──▶ Bot ──▶ Store ──▶ PostgreSQL
                                        ▲
RSS-фиды ──▶ Fetcher (gofeed) ──▶ Scheduler (worker pool, ticker)
                    │                        │
                    └── новые посты ──▶ Notifier ──▶ Telegram chat
```

- `internal/config` — конфигурация из переменных окружения
- `internal/feed` — загрузка и парсинг RSS/Atom
- `internal/storage` — PostgreSQL (pgx), встроенные миграции
- `internal/scheduler` — периодический опрос фидов, worker-пул, доставка постов
- `internal/telegram` — бот (telebot), команды и уведомления
- `internal/server` — HTTP `/healthz`

Пакеты связаны через небольшие интерфейсы (`scheduler.Store`,
`scheduler.Notifier`, `scheduler.FeedFetcher`), поэтому планировщик
тестируется на фейках без БД и Telegram.

## Конфигурация

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `BOT_TOKEN` | — | токен бота от [@BotFather](https://t.me/BotFather); пусто — бот отключён |
| `DATABASE_URL` | `postgres://rss:rss@localhost:5432/rss?sslmode=disable` | строка подключения к PostgreSQL |
| `FETCH_INTERVAL` | `1m` | как часто опрашивать фиды |
| `FETCH_TIMEOUT` | `15s` | таймаут загрузки одного фида |
| `WORKERS` | `8` | число параллельных воркеров |
| `HTTP_ADDR` | `:8080` | адрес HTTP-сервера с `/healthz` |

## Разработка

```bash
make test   # юнит-тесты (go test -race ./...)
make vet    # go vet
make run    # локальный запуск, нужен PostgreSQL
```

Интеграционные тесты хранилища требуют живой PostgreSQL:

```bash
docker run -d --name rss-pg -e POSTGRES_USER=rss -e POSTGRES_PASSWORD=rss \
  -e POSTGRES_DB=rss -p 5432:5432 postgres:16-alpine
TEST_DATABASE_URL='postgres://rss:rss@localhost:5432/rss?sslmode=disable' go test ./...
```

В CI (GitHub Actions) те же тесты гоняются против сервис-контейнера
с PostgreSQL.

## Стек

Go 1.27 · pgx v5 · telebot v3 · gofeed · Docker · GitHub Actions

## Лицензия

[MIT](LICENSE)
