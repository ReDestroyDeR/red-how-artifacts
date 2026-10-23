// Пакет serdebench — Go-половина бенчей сериализации.
//
// Контракт: JSON-пейлоад побайтово совпадает с Rust-версией — одинаковый LCG,
// одинаковый порядок полей (порядок объявления структурных полей = порядок
// ключей в JSON), только целые числа. Проверяется TestPayloadMatches.
package serdebench

import (
	"encoding/json"
	"os"
)

// N — основной размер батча, синхронизирован с Rust (serde_bench::N).
const N = 10_000

var symbols = [8]string{"SBER", "GAZP", "LKOH", "VTBR", "GMKN", "ROSN", "TATN", "MTSR"}

// Symbols — словарь тикеров для внешних потребителей (cmd/etl).
func Symbols() [8]string { return symbols }

// lcg — детерминированный LCG (Numerical Recipes), бит-в-бит совпадает с Rust.
type lcg struct{ s uint64 }

const (
	lcgSeed = 0x5EED5EED5EED5EED
	// Золотое сечение: разваливает соседние индексы при seed-выводе.
	// Совпадает с Lcg::GOLDEN в Rust.
	lcgGolden = 0x9E3779B97F4A7C15
)

func (l *lcg) next() uint64 {
	l.s = l.s*6364136223846793005 + 1442695040888963407
	return l.s >> 33
}

// Tick — доменная запись. Порядок полей менять нельзя: развалится
// побайтовое совпадение JSON с Rust.
//
//easyjson:json
type Tick struct {
	Symbol  string `json:"symbol"`
	Venue   string `json:"venue"`
	Price   uint64 `json:"price"`
	Qty     uint64 `json:"qty"`
	TsMs    int64  `json:"ts_ms"`
	Side    string `json:"side"`
	OrderID uint64 `json:"order_id"`
	Flags   uint32 `json:"flags"`
}

// Ticks — тот же батч, но с own-кодогенерацией easyjson: без именованного
// типа easyjson ушёл бы в рефлексию encoding/json.
//
//easyjson:json
type Ticks []Tick

// TickPlain — тот же JSON-контракт, но без easyjson-методов: чистая мишень
// для v1-рефлексии и v2. Нужен, потому что encoding/json (и v1, и v2)
// уважает json.Marshaler/json.Unmarshaler: бенчить «рефлексию» на Tick
// с easyjson-методами — мерить easyjson с обёрткой.
type TickPlain struct {
	Symbol  string `json:"symbol"`
	Venue   string `json:"venue"`
	Price   uint64 `json:"price"`
	Qty     uint64 `json:"qty"`
	TsMs    int64  `json:"ts_ms"`
	Side    string `json:"side"`
	OrderID uint64 `json:"order_id"`
	Flags   uint32 `json:"flags"`
}

type TicksPlain []TickPlain

// GenTicksPlain — тот же поток, типизированный под v1/v2.
func GenTicksPlain(n int) []TickPlain {
	src := GenTicks(n)
	out := make([]TickPlain, n)
	for i := range src {
		out[i] = TickPlain(src[i])
	}
	return out
}

// GenTick — тик i как чистая функция индекса: окно потока возобновляемо,
// ETL-прогон детерминирован на любом разбиении. Порядок вызовов next()
// должен совпадать с Rust-версией gen_tick.
func GenTick(i uint64) Tick {
	r := lcg{s: lcgSeed ^ i*lcgGolden}
	s := r.next()
	price := 100_000 + r.next()%900_000
	qty := 1 + r.next()%1_000
	ts := 1_729_600_000_000 + int64(i)*100 + int64(r.next()%100)
	side := "S"
	if r.next()&1 == 0 {
		side = "B"
	}
	orderID := r.next() % (1 << 48)
	flags := uint32(r.next() % 16)
	return Tick{
		Symbol:  symbols[s%uint64(len(symbols))],
		Venue:   "MOEX",
		Price:   price,
		Qty:     qty,
		TsMs:    ts,
		Side:    side,
		OrderID: orderID,
		Flags:   flags,
	}
}

// GenTicks генерирует детерминированный батч.
func GenTicks(n int) []Tick {
	ticks := make([]Tick, n)
	for i := 0; i < n; i++ {
		ticks[i] = GenTick(uint64(i))
	}
	return ticks
}

// GenTicksRange — окно потока [from, from+n).
func GenTicksRange(from, n uint64) []Tick {
	ticks := make([]Tick, n)
	for i := uint64(0); i < n; i++ {
		ticks[i] = GenTick(from + i)
	}
	return ticks
}

// GenTicksPlainRange — окно потока, типизированное под v1/v2-бенчи.
func GenTicksPlainRange(from, n uint64) []TickPlain {
	src := GenTicksRange(from, n)
	out := make([]TickPlain, n)
	for i := range src {
		out[i] = TickPlain(src[i])
	}
	return out
}

// AppendTickJSON — NDJSON-строка тика i в канонической компактной форме + '\n'.
// Байт-в-байт совпадает с Rust push_tick_ndjson — сверяется equivalence-тестом.
func AppendTickJSON(buf []byte, i uint64) []byte {
	line, err := json.Marshal(GenTick(i))
	if err != nil {
		panic(err)
	}
	return append(append(buf, line...), '\n')
}

// NDJSONWindow — окно потока [from, from+ticks) как конкатенированные строки.
func NDJSONWindow(from uint64, ticks int) []byte {
	buf := make([]byte, 0, ticks*128)
	for i := from; i < from+uint64(ticks); i++ {
		buf = AppendTickJSON(buf, i)
	}
	return buf
}

// symbolIndex — словарный индекс без map, для генерации эталонного потока.
func symbolIndex(s string) uint32 {
	for i, sym := range symbols {
		if sym == s {
			return uint32(i)
		}
	}
	panic("unknown symbol " + s)
}

func sideCode(s string) uint8 {
	if s == "B" {
		return 0
	}
	return 1
}

// PayloadJSON — канонический компактный JSON-пейлоад.
func PayloadJSON(n int) []byte {
	b, err := json.Marshal(GenTicks(n))
	if err != nil {
		panic(err)
	}
	return b
}

// CanonicalPayload читает эталонный JSON, сгенерированный Rust-стороной.
func CanonicalPayload() ([]byte, error) {
	return os.ReadFile("../payloads/payload-10k.json")
}
