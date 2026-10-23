package serdebench

import (
	"os"
	"testing"
)

// Контракт эквивалентности языков: Go-маршалинг детерминированного потока
// обязан совпасть байт-в-байт с эталоном, сгенерированным Rust
// (cargo run --bin gen в ../rust). Без этих тестов сравнение бенчей —
// сравнение разных данных.

func TestPayloadMatches(t *testing.T) {
	canonical, err := os.ReadFile("../payloads/payload-10k.json")
	if err != nil {
		t.Skipf("нет эталонного пейлоада, сначала запусти cargo run --bin gen в ../rust: %v", err)
	}
	ours := PayloadJSON(N)
	if string(canonical) != string(ours) {
		d := firstDiff(canonical, ours)
		t.Fatalf("пейлоады расходятся на байте %d:\nrust: %.160q\ngo:   %.160q", d,
			slice(canonical, d), slice(ours, d))
	}
	// TickPlain обязан давать те же байты: это мишень v1/v2-бенчей,
	// расхождение тегов сломало бы сравнение «рефлексия vs codegen».
	plain, err := jsonPlainMarshal(GenTicksPlain(N))
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(plain) {
		d := firstDiff(canonical, plain)
		t.Fatalf("TickPlain расходится с эталоном на байте %d:\nrust: %.160q\nplain: %.160q", d,
			slice(canonical, d), slice(plain, d))
	}
}

// TestNDJSONMatches — то же для ETL-потока: первые 1000 строк NDJSON
// обязаны совпасть с payloads/ndjson-sample.jsonl.
func TestNDJSONMatches(t *testing.T) {
	canonical, err := os.ReadFile("../payloads/ndjson-sample.jsonl")
	if err != nil {
		t.Skipf("нет NDJSON-сэмпла, сначала запусти cargo run --bin gen в ../rust: %v", err)
	}
	ours := NDJSONWindow(0, 1000)
	if string(canonical) != string(ours) {
		d := firstDiff(canonical, ours)
		t.Fatalf("NDJSON-потоки расходятся на байте %d:\nrust: %.160q\ngo:   %.160q", d,
			slice(canonical, d), slice(ours, d))
	}
}

func firstDiff(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func slice(b []byte, from int) string {
	lo := max(0, from-40)
	hi := min(len(b), from+120)
	return string(b[lo:hi])
}
