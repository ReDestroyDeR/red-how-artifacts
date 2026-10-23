package serdebench

import (
	"encoding/json"
	"testing"
)

// TestAllocsPerTick — куда уходят аллокации при decode одного тика.
// Ответ на вопрос рецензента: почему 20 025 на 10k тиков, а не 30 000+.
func TestAllocsPerTick(t *testing.T) {
	tick := GenTick(0)
	data, _ := json.Marshal(tick)
	t.Logf("tick JSON: %d bytes, strings: symbol=%q venue=%q side=%q",
		len(data), tick.Symbol, tick.Venue, tick.Side)
	one := testing.AllocsPerRun(100, func() {
		var out Tick
		_ = json.Unmarshal(data, &out)
	})
	t.Logf("v1 unmarshal 1 tick: %.0f allocs", one)
	batch := testing.AllocsPerRun(20, func() {
		var out []Tick
		_ = json.Unmarshal(PayloadJSON(100), &out)
	})
	t.Logf("v1 unmarshal []Tick x100: %.0f total, %.2f/tick", batch, batch/100)
}

func TestAllocsPerTickBatch10k(t *testing.T) {
	data := PayloadJSON(10_000)
	n := testing.AllocsPerRun(5, func() {
		var out []Tick
		_ = json.Unmarshal(data, &out)
	})
	t.Logf("v1 unmarshal []Tick x10000: %.0f total, %.2f/tick", n, n/10_000)
}
