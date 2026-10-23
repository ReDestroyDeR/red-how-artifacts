package serdebench

import (
	"encoding/json"
	"testing"

	easyjson "github.com/mailru/easyjson"
)

// Диагностика Г-8: кто даёт аллокации в marshal/unmarshal.
// Гипотеза: v1/v2 уважают json.Marshaler/json.Unmarshaler, поэтому «рефлексия»
// на Tick (с easyjson-методами) мерит easyjson с обёрткой.
// Запуск: go test -run TestAllocsDiag -v

func TestAllocsDiag(t *testing.T) {
	one := []Tick{GenTick(0)}
	onePlain := []TickPlain{TickPlain(GenTick(0))}

	cases := []struct {
		name string
		fn   func()
	}{
		{"v1 marshal Tick(easyjson methods) x1", func() { _, _ = json.Marshal(one) }},
		{"v1 marshal TickPlain x1", func() { _, _ = json.Marshal(onePlain) }},
		{"easyjson marshal x1", func() { _, _ = easyjson.Marshal(Ticks(one)) }},
		{"v1 unmarshal Tick x1", func() {
			var out []Tick
			_ = json.Unmarshal(PayloadJSON(1), &out)
		}},
		{"v1 unmarshal TickPlain x1", func() {
			var out []TickPlain
			_ = json.Unmarshal(PayloadJSON(1), &out)
		}},
		{"easyjson unmarshal x1", func() {
			var out Ticks
			_ = easyjson.Unmarshal(PayloadJSON(1), &out)
		}},
	}
	for _, c := range cases {
		allocs := testing.AllocsPerRun(200, c.fn)
		t.Logf("%-42s %.1f allocs/op", c.name, allocs)
	}
}
