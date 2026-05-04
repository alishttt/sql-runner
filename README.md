# sql-runner

Маленький HTTP-сервис на Go: принимает SQL-запрос, ходит в БД, возвращает результат как JSON. Написал, чтобы прокачать `database/sql` — connection pooling, per-request timeouts через context, graceful shutdown HTTP-сервера. Шаблон похож на внутренний SQL-инструмент аналитиков, который делали для одной из команд.

Драйвер БД сейчас — SQLite в памяти, чтобы запускалось без Docker. Код один-в-один работает с MySQL / PostgreSQL / Oracle / ClickHouse — меняется только driver name + DSN + blank import нужного драйвера.

## Эндпоинты

| Метод | Путь | Что делает |
|---|---|---|
| GET | `/health` | health-check для k8s |
| POST | `/query` | SELECT, ответ `{columns, rows, row_count, elapsed_ms}` |
| POST | `/exec` | INSERT / UPDATE / DELETE / DDL, ответ `{rows_affected, last_insert_id, elapsed_ms}` |
| GET | `/pool/stats` | статистика пула: open / idle / in-use / wait-count |

## Запуск

```bash
make deps    # go mod tidy
make run     # слушает на :8080
make test    # все тесты с -race
```

## Примеры

```bash
# простой SELECT
curl -s -X POST localhost:8080/query -H 'content-type: application/json' \
  -d '{"sql":"SELECT id, name, email FROM users ORDER BY id"}'

# параметризованный запрос (без SQL-инъекции)
curl -s -X POST localhost:8080/query -H 'content-type: application/json' \
  -d '{"sql":"SELECT * FROM orders WHERE user_id = ?","args":[1]}'

# с per-request таймаутом
curl -s -X POST localhost:8080/query -H 'content-type: application/json' \
  -d '{"sql":"SELECT count(*) FROM users","timeout_ms":5000}'

# INSERT через /exec
curl -s -X POST localhost:8080/exec -H 'content-type: application/json' \
  -d '{"sql":"INSERT INTO users (name, email) VALUES (?, ?)","args":["Dana","dana@example.com"]}'

# статистика пула
curl -s localhost:8080/pool/stats
```

## Архитектура

```
HTTP-запрос
   │
   ▼
http.Server (read header 3s, read 10s, write 5m+)
   │
   ▼
logRequests middleware (метод, путь, статус, длительность)
   │
   ▼
ServeMux (Go 1.22+, роутинг с привязкой к HTTP-методу)
   │
   ▼
queryHandler / execHandler
   │  context.WithTimeout(req.Context(), N)
   ▼
runner.Runner
   │  db.QueryContext(ctx, ...)
   ▼
*sql.DB пул
   │  SetMaxOpenConns / SetMaxIdleConns / SetConnMaxLifetime / SetConnMaxIdleTime
   ▼
драйвер БД (modernc.org/sqlite, легко меняется)
```

## Connection pool — как тюнить

Дефолты в `main.go`:

- `MaxOpenConns = 10` — потолок одновременных соединений с БД. Должно быть **сильно ниже** `max_connections` самой БД (Postgres дефолт 100, MySQL 151), чтобы оставить место другим клиентам. На N инстансах сервиса правило: `N × MaxOpenConns < 70% от max_connections`.
- `MaxIdleConns = 5` — сколько коннектов держать в горячем резерве. Если поставить 0 — каждый запрос будет переподключаться, теряется весь смысл пула. Выше `MaxOpen` ставить бессмысленно.
- `ConnMaxLifetime = 30m` — принудительный recycle. Нужно когда перед БД стоит балансер (HAProxy, RDS Proxy, pgbouncer) — без этого долгоживущие коннекты пинуются к одной ноде и не уезжают на новые после ребаланса.
- `ConnMaxIdleTime = 5m` — закрывать idle-коннекты **раньше**, чем их закроет сама БД (у MySQL/Postgres свой idle timeout, бывает не предсказуемый). Иначе ловишь "broken pipe" на следующем запросе.

Мониторинг в проде смотрит на `db.Stats()` (endpoint `/pool/stats`):

- `WaitCount > 0` → запросы стояли в очереди за коннектом → пул мал или запросы тормозят.
- `InUse` стабильно близко к `MaxOpen` → пора скейлить (либо пул, либо сервис горизонтально).
- `WaitDuration` растёт → деградация перфоманса, нужна реакция.

## Особенности реализации

**Graceful shutdown.** По SIGINT/SIGTERM `srv.Shutdown(shutdownCtx)` перестаёт принимать новые соединения и ждёт пока in-flight запросы завершатся. Жёсткий дедлайн — 10 секунд.

**Per-request timeout.** Каждый запрос получает свой `context.WithTimeout(req.Context(), …)`. Если клиент отвалился — запрос в БД отменяется автоматически через цепочку отмены контекстов. Если запрос дольше дедлайна — 504.

**Ping at startup.** `sql.Open` ленивый, не подключается. Без `PingContext` сервис стартует "здоровым" даже с битыми кредами и падает только на первом юзер-запросе. Ping ловит проблему сразу — fail-fast.

**`[]byte → string` для текстовых колонок.** Некоторые драйверы возвращают TEXT/BLOB как `[]byte`, что в JSON сериализуется в base64. Конвертим в `string` ради читаемого ответа.

**Method-aware routing без фреймворка.** Go 1.22+ поддерживает паттерны вида `"POST /query"` в стандартном `ServeMux`. Для маленького сервиса этого хватает — chi / gorilla / echo не нужны.

## TODO

- env-config вместо хардкода (порт, DSN, пул-параметры)
- read-only режим (запрет INSERT/UPDATE/DELETE) для безопасных production-сценариев
- prometheus-метрики на `/metrics`
- structured logging через `log/slog`
