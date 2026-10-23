package serdebench

import (
	"encoding/json"
	"testing"

	easyjson "github.com/mailru/easyjson"
)

// Ось «JSON: наивная десериализация, DOM, codegen».
// v1 = encoding/json (рефлексия). Этот файл компилируется и без
// GOEXPERIMENT=jsonv2 (чистый v1), и с ним (v1, переимплементированный
// поверх v2) — обе точки фиксируются в results/.

func BenchmarkJSON(b *testing.B) {
	data := PayloadJSON(N)

	b.Run("v1/unmarshal_reflection", func(b *testing.B) {
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
	// DOM-подобный путь: всё в any → []any из map[string]any.
	b.Run("v1/unmarshal_into_any_dom", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var out any
			if err := json.Unmarshal(data, &out); err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
	b.Run("v1/marshal_reflection", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		// Чистая рефлексия: TickPlain без easyjson-методов, иначе v1
		// вызывает MarshalJSON и мерим codegen с обёрткой.
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
	// codegen вместо рефлексии: easyjson по именованному типу Ticks.
	b.Run("easyjson/unmarshal", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			var out Ticks
			if err := easyjson.Unmarshal(data, &out); err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
	b.Run("easyjson/marshal", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		ticks := Ticks(GenTicks(N))
		b.ResetTimer()
		for b.Loop() {
			out, err := easyjson.Marshal(ticks)
			if err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	})
}
