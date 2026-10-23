//! FlatBuffers (библиотека, переносимость) против perfect (своя раскладка).
//! Код сгенерирован flatc из schema/tick.fbs (rust/src/serdebench/tick_generated.rs):
//! чтение — верифицированный safe-путь root_as_tick, записи — сгенерированный
//! create. Никакого ручного unsafe: это то, как библиотеку использует прод.

use std::hint::black_box;

use criterion::{criterion_group, criterion_main, Criterion};
use flatbuffers::FlatBufferBuilder;

use serde_bench::fb::serdebench;

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
    let tick = serdebench::Tick::create(
        b,
        &serdebench::TickArgs {
            symbol: Some(sym),
            venue: Some(venue),
            price: t.price,
            qty: t.qty,
            ts_ms: t.ts_ms,
            side: Some(side),
            order_id: t.order_id,
            flags: t.flags,
        },
    );
    b.finish(tick, None);
}

/// Чтение через верифицированный root_as_tick — safe-путь с проверкой формата.
fn read_fb_tick<'a>(buf: &'a [u8]) -> serdebench::Tick<'a> {
    serdebench::root_as_tick(buf).unwrap()
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
    g.finish();
}

criterion_group!(benches, bench_fb);
criterion_main!(benches);
