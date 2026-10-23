//! Модель данных и генератор пейлоада для бенчей сериализации.
//!
//! Контракт: пейлоад побайтово совпадает с Go-реализацией — одинаковый LCG,
//! одинаковый порядок полей, только целые числа (никакого форматирования f64).
//! Проверяется тестом эквивалентности на Go-стороне: `go test -run TestPayloadMatches`.

use std::collections::HashMap;
use std::hint::black_box;

use zerocopy::{FromBytes, Immutable, IntoBytes, KnownLayout};

/// flatc-кодогенерация из schema/*.fbs (FlatBuffers).
/// Сгенерированные файлы ссылаются друг на друга через crate::tick_generated::*.
#[path = "tick_generated.rs"]
pub mod tick_generated;
#[path = "bigdoc_generated.rs"]
pub mod bigdoc_generated;

// Аллокатор выбирается фичей на этапе сборки, ровно один на бинарь.
// Отсутствие обеих фич = системный аллокатор (macOS libmalloc).
#[cfg(all(feature = "jemalloc", feature = "mimalloc"))]
compile_error!("выбери один аллокатор: jemalloc или mimalloc");

#[cfg(feature = "jemalloc")]
#[global_allocator]
static GLOBAL_JEMALLOC: tikv_jemallocator::Jemalloc = tikv_jemallocator::Jemalloc;

#[cfg(feature = "mimalloc")]
#[global_allocator]
static GLOBAL_MIMALLOC: mimalloc::MiMalloc = mimalloc::MiMalloc;

/// Основной размер батча: ~1.1 MB JSON — помещается в L2/L3, измеримо в мс.
pub const N: usize = 10_000;

pub const SYMBOLS: [&str; 8] = [
    "SBER", "GAZP", "LKOH", "VTBR", "GMKN", "ROSN", "TATN", "MTSR",
];

/// Детерминированный LCG (Numerical Recipes). Совпадает с Go-версией бит в бит.
pub struct Lcg(u64);

impl Lcg {
    pub const SEED: u64 = 0x5EED_5EED_5EED_5EED;
    /// Золотое сечение: разваливает соседние индексы при seed-выводе.
    pub const GOLDEN: u64 = 0x9E37_79B9_7F4A_7C15;

    pub fn new(seed: u64) -> Self {
        Lcg(seed)
    }

    pub fn next(&mut self) -> u64 {
        self.0 = self
            .0
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        self.0 >> 33
    }
}

/// Доменная запись. Порядок полей = порядок ключей в JSON — менять нельзя,
/// иначе развалится побайтовое совпадение с Go.
#[derive(Debug, Clone, PartialEq, serde::Serialize, serde::Deserialize)]
#[derive(rkyv::Archive, rkyv::Serialize, rkyv::Deserialize)]
pub struct Tick {
    pub symbol: String,
    pub venue: String,
    pub price: u64,
    pub qty: u64,
    pub ts_ms: i64,
    pub side: String,
    pub order_id: u64,
    pub flags: u32,
}

/// Тот же тик без heap-полей: словарные индексы вместо строк.
/// Раскладка без дыр, естественное выравнивание 8 — это условие
/// `FromBytes` для zerocopy (неявный паддинг нельзя кастить).
#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq, FromBytes, IntoBytes, KnownLayout, Immutable)]
pub struct TickWire {
    pub ts_ms: i64,
    pub order_id: u64,
    pub price: u64,
    pub qty: u64,
    pub symbol_idx: u32,
    pub venue_idx: u32,
    pub flags: u32,
    pub side: u8,
    pub _pad: [u8; 3],
}

pub const WIRE_SIZE: usize = std::mem::size_of::<TickWire>(); // 48

/// Тик i — чистая функция индекса: окно потока возобновляемо, ETL-прогон
/// детерминирован на любом разбиении. Порядок вызовов rng.next()
/// должен совпадать с Go-версией genTick.
pub fn gen_tick(i: u64) -> Tick {
    let mut rng = Lcg::new(Lcg::SEED ^ i.wrapping_mul(Lcg::GOLDEN));
    let s = rng.next();
    Tick {
        symbol: SYMBOLS[(s % SYMBOLS.len() as u64) as usize].to_string(),
        venue: "MOEX".to_string(),
        price: 100_000 + rng.next() % 900_000,
        qty: 1 + rng.next() % 1_000,
        ts_ms: 1_729_600_000_000 + i as i64 * 100 + (rng.next() % 100) as i64,
        side: if rng.next() & 1 == 0 { "B" } else { "S" }.to_string(),
        order_id: rng.next() % (1 << 48),
        flags: (rng.next() % 16) as u32,
    }
}

pub fn gen_ticks_range(from: u64, len: usize) -> Vec<Tick> {
    (from..from + len as u64).map(gen_tick).collect()
}

pub fn gen_ticks(n: usize) -> Vec<Tick> {
    gen_ticks_range(0, n)
}

/// NDJSON-строка тика i в канонической компактной форме + '\n'.
/// Байт-в-байт совпадает с Go appendTickJSON — сверяется equivalence-тестом.
pub fn push_tick_ndjson(i: u64, buf: &mut Vec<u8>) {
    serde_json::to_writer(&mut *buf, &gen_tick(i)).unwrap();
    buf.push(b'\n');
}

pub fn ndjson_window(from: u64, ticks: usize) -> Vec<u8> {
    let mut buf = Vec::with_capacity(ticks * 128);
    for i in from..from + ticks as u64 {
        push_tick_ndjson(i, &mut buf);
    }
    buf
}

/// Плоская запись тика i напрямую из генератора — словарный индекс
/// известен до HashMap: это эталонный wire-поток, а не конверсия.
pub fn gen_wire_tick(i: u64) -> TickWire {
    let t = gen_tick(i);
    TickWire {
        ts_ms: t.ts_ms,
        order_id: t.order_id,
        price: t.price,
        qty: t.qty,
        symbol_idx: symbol_index(t.symbol.as_str()) as u32,
        venue_idx: 0,
        flags: t.flags,
        side: if t.side == "B" { 0 } else { 1 },
        _pad: [0; 3],
    }
}

/// symbol -> индекс без HashMap, для генерации эталонного потока.
pub fn symbol_index(symbol: &str) -> usize {
    SYMBOLS.iter().position(|s| *s == symbol).expect("unknown symbol")
}

/// Окно perfect: len записей по WIRE_SIZE.
pub fn wire_window(from: u64, len: usize) -> Vec<u8> {
    let mut out = Vec::with_capacity(len * WIRE_SIZE);
    for i in from..from + len as u64 {
        out.extend_from_slice(gen_wire_tick(i).as_bytes());
    }
    out
}

/// Канонический JSON-пейлоад: compact, порядок полей как в структуре.
pub fn payload_json(n: usize) -> String {
    serde_json::to_string(&gen_ticks(n)).unwrap()
}

/// Словарь symbol -> индекс для wire-формата.
pub fn symbol_table() -> HashMap<&'static str, u32> {
    SYMBOLS
        .iter()
        .enumerate()
        .map(|(i, s)| (*s, i as u32))
        .collect()
}

/// Типизированные тики -> wire-байты. Цена кодирования словаря отдельно
/// от стоимости раскладки — в бенче `zerocopy/encode`.
pub fn encode_wire(ticks: &[Tick], table: &HashMap<&str, u32>) -> Vec<u8> {
    let mut out = Vec::with_capacity(ticks.len() * WIRE_SIZE);
    for t in ticks {
        let w = TickWire {
            ts_ms: t.ts_ms,
            order_id: t.order_id,
            price: t.price,
            qty: t.qty,
            symbol_idx: *table.get(t.symbol.as_str()).expect("unknown symbol"),
            venue_idx: 0,
            flags: t.flags,
            side: if t.side == "B" { 0 } else { 1 },
            _pad: [0; 3],
        };
        out.extend_from_slice(w.as_bytes());
    }
    out
}

/// Каст байтов в &[TickWire] без валидации содержимого — только раскладка.
pub fn cast_wire(bytes: &[u8]) -> Option<&[TickWire]> {
    <[TickWire]>::ref_from_prefix(bytes).ok().map(|(s, _)| s)
}

/// Семантическая валидация поверх каста: границы словаря, домен side,
/// разумность времени. Это та стоимость, которую zerocopy-каст НЕ платит.
/// Безветвевая аккумуляция: LLVM опускает bool->u64 в setcc и векторизует;
/// ветвящаяся форма на 23% медленнее (см. results, таблица форм).
pub fn validate_wire(wire: &[TickWire]) -> bool {
    let mut bad = 0u64;
    for w in wire {
        bad |= u64::from(w.symbol_idx as u64 >= SYMBOLS.len() as u64)
            | u64::from(w.side > 1)
            | u64::from(w.flags >= 16)
            | u64::from(w.ts_ms <= 1_000_000_000_000);
    }
    bad == 0
}

// --- protobuf (prost, derive-подход без .proto) ---

#[derive(Clone, PartialEq, prost::Message)]
pub struct TickMsg {
    #[prost(string, tag = "1")]
    pub symbol: String,
    #[prost(string, tag = "2")]
    pub venue: String,
    #[prost(uint64, tag = "3")]
    pub price: u64,
    #[prost(uint64, tag = "4")]
    pub qty: u64,
    #[prost(int64, tag = "5")]
    pub ts_ms: i64,
    #[prost(string, tag = "6")]
    pub side: String,
    #[prost(uint64, tag = "7")]
    pub order_id: u64,
    #[prost(uint32, tag = "8")]
    pub flags: u32,
}

#[derive(Clone, PartialEq, prost::Message)]
pub struct BatchMsg {
    #[prost(message, repeated, tag = "1")]
    pub ticks: Vec<TickMsg>,
}

pub fn gen_batch_msg(n: usize) -> BatchMsg {
    BatchMsg {
        ticks: gen_ticks(n)
            .into_iter()
            .map(|t| TickMsg {
                symbol: t.symbol,
                venue: t.venue,
                price: t.price,
                qty: t.qty,
                ts_ms: t.ts_ms,
                side: t.side,
                order_id: t.order_id,
                flags: t.flags,
            })
            .collect(),
    }
}

// --- rkyv helpers ---

pub type RkyvError = rkyv::rancor::Error;
pub type ArchivedTicks = <Vec<Tick> as rkyv::Archive>::Archived;

pub fn rkyv_bytes(ticks: &Vec<Tick>) -> rkyv::util::AlignedVec {
    rkyv::to_bytes::<RkyvError>(ticks).unwrap()
}

/// Проверка корректности данных после разных путей декодирования
/// (sanity-проверка самих бенчей, не отдельная ось).
pub fn assert_roundtrip(json: &str, ticks: &[Tick]) {
    let parsed: Vec<Tick> = serde_json::from_str(json).unwrap();
    assert_eq!(parsed, ticks, "serde_json roundtrip mismatch");
    black_box(());
}
