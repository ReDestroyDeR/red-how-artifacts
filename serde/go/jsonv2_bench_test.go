//go:build jsonv2

package serdebench

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"io"
	"testing"
)

// Ось «JSON v2»: GOEXPERIMENT=jsonv2, -tags jsonv2.
// - json (v2).Unmarshal — рефлексия заменена кодогенерацией на лету компилятором;
// - jsontext — потоковый токенизатор, AST-free проход;
// - v1onv2/unmarshal — v1, переимплементированный поверх v2 (интересен сам по себе).

func BenchmarkJSONV2(b *testing.B) {
	data := PayloadJSON(N)

	b.Run("v2/unmarshal", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var out []TickPlain
			if err := json.Unmarshal(data, &out); err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
	// AST-free: только токены, ни одного значения в памяти.
	b.Run("v2/token_walk", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			dec := jsontext.NewDecoder(bytes.NewReader(data))
			for {
				_, err := dec.ReadToken()
				if err == io.EOF {
					break
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("v2/marshal", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		// TickPlain без easyjson-методов: иначе v2 уважает json.Marshaler
		// и мерим easyjson с parse-back'ом.
		ticks := GenTicksPlain(N)
		b.ResetTimer()
		for b.Loop() {
			out, err := json.Marshal(ticks)
			if err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
	// v1 поверх v2: тот же сценарий, что BenchmarkJSON/v1/unmarshal_reflection.
	b.Run("v1onv2/unmarshal_reflection", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var out []TickPlain
			if err := jsonv1.Unmarshal(data, &out); err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
}
