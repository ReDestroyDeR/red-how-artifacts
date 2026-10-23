//! FlatBuffers (библиотека, переносимость) против perfect (своя раскладка).
//! Код сгенерирован flatc из schema/tick.fbs (rust/src/serdebench/tick_generated.rs):
//! чтение — верифицированный safe-путь root_as_tick, записи — сгенерированный
//! create. Никакого ручного unsafe: это то, как библиотеку использует прод.

use std::hint::black_box;

use criterion::{criterion_group, criterion_main, Criterion};
use flatbuffers::{FlatBufferBuilder, ForwardsUOffset};

use serde_bench::bigdoc_generated as bigdoc_ns;
use serde_bench::tick_generated as tick_ns;

#[derive(Clone, Debug, PartialEq)]
struct FbTick {
    symbol: String,
    venue: String,
    price: u64,
    qty: u64,
    ts_ms: i64,
    side: String,
    order_id: u64,
    flags: u32,
}

fn sample() -> FbTick {
    FbTick {
        symbol: "SBER".into(),
        venue: "MOEX".into(),
        price: 277_350,
        qty: 15,
        ts_ms: 1_729_600_000_123,
        side: "B".into(),
        order_id: 882_374,
        flags: 3,
    }
}

/// Запись через сгенерированный Tick::create — safe-путь.
fn write_fb_tick<'bldr: 'args, 'args: 'mut_bldr, 'mut_bldr>(
    b: &'mut_bldr mut FlatBufferBuilder<'bldr>,
    t: &'args FbTick,
) {
    let sym = b.create_string(&t.symbol);
    let venue = b.create_string(&t.venue);
    let side = b.create_string(&t.side);
    let start = b.start_table();
    // порядок слотов зеркален Go buildFBTick — иначе байт-идентичность буферов
    b.push_slot(16, t.order_id, 0u64);
    b.push_slot(12, t.ts_ms, 0i64);
    b.push_slot(10, t.qty, 0u64);
    b.push_slot(8, t.price, 0u64);
    b.push_slot(18, t.flags, 0u32);
    b.push_slot_always(14, side);
    b.push_slot_always(6, venue);
    b.push_slot_always(4, sym);
    let root = b.end_table(start);
    b.finish(root, None);
}

/// bigdoc: 100 000 тиков одним документом — вектор таблиц под общим корнем.
fn build_fb_bigdoc<'bldr: 'args, 'args: 'mut_bldr, 'mut_bldr>(
    b: &'mut_bldr mut FlatBufferBuilder<'bldr>,
    ticks: &'args [FbTick],
) {
    let mut offs = Vec::with_capacity(ticks.len());
    for t in ticks {
        let sym = b.create_string(&t.symbol);
        let venue = b.create_string(&t.venue);
        let side = b.create_string(&t.side);
        let start = b.start_table();
        // порядок слотов зеркален Go buildFBTick — иначе байт-идентичность буферов
        b.push_slot(16, t.order_id, 0u64);
        b.push_slot(12, t.ts_ms, 0i64);
        b.push_slot(10, t.qty, 0u64);
        b.push_slot(8, t.price, 0u64);
        b.push_slot(18, t.flags, 0u32);
        b.push_slot_always(14, side);
        b.push_slot_always(6, venue);
        b.push_slot_always(4, sym);
        offs.push(b.end_table(start));
    }
    b.start_vector::<ForwardsUOffset<tick_ns::Tick>>(offs.len());
    for off in offs.iter().rev() {
        b.push(*off);
    }
    let vec = b.end_vector(offs.len());
    let doc = bigdoc_ns::BigDoc::create(b, &bigdoc_ns::BigDocArgs { ticks: Some(vec) });
    b.finish(doc, None);
}

/// Итерация вектора: все поля каждого тика.
fn read_fb_bigdoc(buf: &[u8]) -> u64 {
    let doc = bigdoc_ns::root_as_big_doc(buf).unwrap();
    let mut sum = 0u64;
    for t in doc.ticks().unwrap().iter() {
        sum += t.price();
        black_box(t.symbol());
    }
    sum
}

/// Чтение через верифицированный root_as_tick — safe-путь с проверкой формата.
fn read_fb_tick<'a>(buf: &'a [u8]) -> tick_ns::Tick<'a> {
    tick_ns::root_as_tick(buf).unwrap()
}

fn bench_fb(c: &mut Criterion) {
    let mut b = FlatBufferBuilder::new();
    write_fb_tick(&mut b, &sample());
    let buf: Vec<u8> = b.finished_data().to_vec();

    // корректность: прочитанное обязано совпасть с записанным
    let t = read_fb_tick(&buf);
    let s = sample();
    assert_eq!(t.symbol(), Some(s.symbol.as_str()));
    assert_eq!(t.price(), s.price);
    assert_eq!(t.flags(), s.flags);

    // hex для сверки байт-идентичности с Go-буфером (go/fb_bench_test.go)
    let hex: String = buf.iter().map(|byte| format!("{byte:02x}")).collect();
    println!("fb hex: {hex}");

    let mut g = c.benchmark_group("flatbuffers");
    g.bench_function("read_tick", |b| {
        b.iter(|| black_box(read_fb_tick(black_box(&buf))))
    });
    g.bench_function("write_tick", |b| {
        b.iter(|| {
            let mut b = FlatBufferBuilder::new();
            write_fb_tick(&mut b, &sample());
            black_box(b.finished_data().len())
        })
    });

    // bigdoc: 100 000 тиков одним документом (кейс bigdoc).
    let owned = serde_bench::gen_ticks_range(0, 100_000);
    let big: Vec<FbTick> = owned
        .iter()
        .map(|t| FbTick {
            symbol: t.symbol.clone(),
            venue: t.venue.clone(),
            price: t.price,
            qty: t.qty,
            ts_ms: t.ts_ms,
            side: t.side.clone(),
            order_id: t.order_id,
            flags: t.flags,
        })
        .collect();
    let mut bb = FlatBufferBuilder::new();
    build_fb_bigdoc(&mut bb, &big);
    let big_buf = bb.finished_data().to_vec();
    let read_sum = read_fb_bigdoc(&big_buf);
    assert_eq!(read_sum, big.iter().map(|t| t.price).sum::<u64>());

    g.bench_function("read_bigdoc", |b| {
        b.iter(|| black_box(read_fb_bigdoc(black_box(&big_buf))))
    });
    g.bench_function("write_bigdoc", |b| {
        b.iter(|| {
            let mut b = FlatBufferBuilder::new();
            build_fb_bigdoc(&mut b, &big);
            black_box(b.finished_data().len())
        })
    });
    g.finish();
}

criterion_group!(benches, bench_fb);
criterion_main!(benches);
