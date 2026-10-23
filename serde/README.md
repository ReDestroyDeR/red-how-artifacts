# red-how-artifacts: бенчмарки сериализации (Rust + Go)

Стенд к статье «Десериализация может быть бесплатной, но вы не готовы
столько заплатить». Два языка, побайтово идентичный пейлоад, три кейса
нагрузки: CRUD/WS (120 B – 10 KB), bigdoc (12 MB одним документом),
ETL-поток (~1 GB окнами 4 MB).

Контракт честности: тик `i` — чистая функция индекса (LCG), JSON-вывод
Rust и Go сверен байт-в-байт тестами (`TestPayloadMatches`,
`TestNDJSONMatches`); раскладка perfect (48 байт/тик) сверяется assert'ом
в обоих языках. Только целые числа — никакого форматирования f64.

## Quick start

```bash
# Rust: эталонные пейлоады + таблицы размеров, потом бенчи
cd rust
cargo run --bin gen
cargo bench                          # json, proto, zerocopy, compression, sizes, etl, fb

# Rust: аллокаторы — ровно одна фича на бинарь
cargo run --release --features jemalloc --bin etl -- --windows 250
cargo run --release --features mimalloc --bin etl -- --windows 250

# Go: обычный режим (encoding/json v1) и эксперимент json v2
cd ../go
go test -run XXX -bench . -benchtime 3s
GOEXPERIMENT=jsonv2 go test -tags jsonv2 -run XXX -bench . -benchtime 3s

# ETL-поток: 250 окон ≈ 1 GB NDJSON (дефолт), 2500 — демонстрация на 10 GB
go run ./cmd/etl
cargo run --release --bin etl -- --windows 250   # из rust/

# Контракт эквивалентности
go test -run 'TestPayloadMatches|TestNDJSONMatches'
```

## Структура

| путь | что |
|---|---|
| `rust/` | serde_json, simd-json, prost, rkyv, zerocopy, flatbuffers, zstd; criterion |
| `go/` | encoding/json v1 + jsonv2, easyjson, protobuf-go, unsafe-каст, zstd |
| `schema/tick.fbs` | схема FlatBuffers (кодоген: `flatc --rust/--go`) |
| `methodology.md` | что и как меряем: кейсы, гипотезы, ограничения, протокол |
| `results/` | сырые прогоны: один файл = один запуск |

## Что важно знать перед интерпретацией

- Между языками сравнивайте порядки, внутри языка — проценты.
- Однопоточно, одна машина (Apple M2 Max), AC-питание.
- FlatBuffers-бенчи — сгенерированный safe-путь (root_as_tick/GetRootAsTick),
  верификация формата включена в цену чтения.
- perfect — самописный формат стенда, не библиотека; unsafe-каст без
  верификации структуры, валидация доменов — в цене.

Полная методология и правила честности — `methodology.md`.
