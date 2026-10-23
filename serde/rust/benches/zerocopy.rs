//! Ось «zerocopy»: rkyv (архивный формат с реляционной раскладкой строк)
//! и zerocopy-крейт (голый каст фиксированных структур).
//!
//! Ключевые пары:
//! - access_unchecked vs access_validated — цена валидации;
//! - access vs deserialize_full — цена отказа от материализации;
//! - cast vs cast_validated — то же для плоского wire-формата;
//! - symbol_lookup — конверсия словарный int -> &str поверх каста
//!   (ось «string -> int и обратно»).

use std::hint::black_box;

use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use serde_bench::*;

fn bench_zerocopy(c: &mut Criterion) {
    let ticks = gen_ticks(N);
    let archived = rkyv_bytes(&ticks);
    let table = symbol_table();
    let wire = encode_wire(&ticks, &table);
    let symbols: Vec<&'static str> = SYMBOLS.to_vec();

    let mut g = c.benchmark_group("zerocopy");
    g.throughput(Throughput::Bytes(archived.len() as u64));

    g.bench_function(BenchmarkId::new("rkyv/serialize", N), |b| {
        b.iter(|| black_box(rkyv_bytes(black_box(&ticks))))
    });
    // Нулевое копирование без проверки структуры: доверяем байтам.
    g.bench_function(BenchmarkId::new("rkyv/access_unchecked", N), |b| {
        b.iter(|| {
            let a: &ArchivedTicks = unsafe { rkyv::access_unchecked(black_box(&archived)) };
            black_box(a.len())
        })
    });
    // Тот же доступ с полной валидацией структуры.
    g.bench_function(BenchmarkId::new("rkyv/access_validated", N), |b| {
        b.iter(|| {
            let a: &ArchivedTicks = rkyv::access::<ArchivedTicks, RkyvError>(black_box(&archived))
                .unwrap();
            black_box(a.len())
        })
    });
    // Полная материализация обратно в Vec<Tick> — «зачем мы всё это затевали» пункт сравнения.
    g.bench_function(BenchmarkId::new("rkyv/deserialize_full", N), |b| {
        b.iter(|| {
            let a: &ArchivedTicks = unsafe { rkyv::access_unchecked(black_box(&archived)) };
            black_box(rkyv::deserialize::<Vec<Tick>, RkyvError>(a).unwrap())
        })
    });

    g.throughput(Throughput::Bytes(wire.len() as u64));
    g.bench_function(BenchmarkId::new("perfect/encode", N), |b| {
        b.iter(|| black_box(encode_wire(black_box(&ticks), &table)))
    });
    // Голый каст: только раскладка, содержимому верим.
    g.bench_function(BenchmarkId::new("perfect/cast", N), |b| {
        b.iter(|| black_box(cast_wire(black_box(&wire)).unwrap()))
    });
    // Каст + семантическая валидация полей.
    g.bench_function(BenchmarkId::new("perfect/cast_validated", N), |b| {
        b.iter(|| {
            let w = cast_wire(black_box(&wire)).unwrap();
            black_box(validate_wire(w))
        })
    });
    // Конверсия int -> &str поверх каста: что стоит «превратить байты в строки»
    // без аллокаций (интернированный словарь).
    g.bench_function(BenchmarkId::new("perfect/symbol_lookup", N), |b| {
        b.iter(|| {
            let w = cast_wire(black_box(&wire)).unwrap();
            let total: usize = w
                .iter()
                .map(|t| black_box(symbols[t.symbol_idx as usize]).len())
                .sum();
            black_box(total)
        })
    });

    g.finish();
}

criterion_group!(benches, bench_zerocopy);
criterion_main!(benches);
