//! Генератор артефактов: побайтово канонический JSON-пейлоад (для
//! эквивалентности с Go) + таблицы размеров для results/.
//!
//! Запуск: cargo run --bin gen
//! Пишет payloads/payload-10k.json, payloads/ndjson-sample.jsonl
//! и печатает markdown-таблицы размеров.

use std::fs;
use std::path::PathBuf;

use prost::Message as _;
use serde_bench::*;

fn payloads_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../payloads")
}

fn main() {
    let dir = payloads_dir();
    fs::create_dir_all(&dir).unwrap();

    let ticks = gen_ticks(N);
    let json = serde_json::to_string(&ticks).unwrap();
    fs::write(dir.join("payload-10k.json"), &json).unwrap();

    // NDJSON-сэмпл: первые 1000 строк потока — эталон для Go equivalence-теста.
    let mut sample = Vec::with_capacity(1000 * 128);
    for i in 0..1000u64 {
        push_tick_ndjson(i, &mut sample);
    }
    fs::write(dir.join("ndjson-sample.jsonl"), &sample).unwrap();
    println!(
        "wrote payloads/payload-10k.json ({} bytes), payloads/ndjson-sample.jsonl ({} bytes)",
        json.len(),
        sample.len()
    );

    // Лестница CRUD: фактические байты на размер.
    println!("\n### CRUD-лестница (фактические размеры)\n");
    println!("| тиков | json, B | proto, B | rkyv, B | perfect, B |");
    println!("|---:|---:|---:|---:|---:|");
    let table = symbol_table();
    for n in [1usize, 4, 8, 16, 80, 10_000] {
        let ticks = gen_ticks(n);
        let j = serde_json::to_string(&ticks).unwrap().len();
        let p = gen_batch_msg(n).encode_to_vec().len();
        let r = rkyv_bytes(&ticks).len();
        let w = encode_wire(&ticks, &table).len();
        println!("| {n} | {j} | {p} | {r} | {w} |");
    }

    // Окно ETL.
    let win = ndjson_window(0, 32_768);
    let wire = wire_window(0, 32_768);
    let archived = rkyv_bytes(&gen_ticks_range(0, 32_768));
    let proto = gen_batch_msg(32_768).encode_to_vec();
    let z3 = |b: &[u8]| zstd::bulk::compress(b, 3).unwrap().len();
    println!("\n### Окно ETL (32 768 тиков)\n");
    println!("| формат | окно, B | ×ndjson | zstd-3, B |");
    println!("|---|---:|---:|---:|");
    println!(
        "| ndjson | {} | 1.00 | {} |",
        win.len(),
        z3(&win)
    );
    println!(
        "| protobuf | {} | {:.2} | {} |",
        proto.len(),
        proto.len() as f64 / win.len() as f64,
        z3(&proto)
    );
    println!(
        "| rkyv | {} | {:.2} | {} |",
        archived.len(),
        archived.len() as f64 / win.len() as f64,
        z3(&archived)
    );
    println!(
        "| perfect | {} | {:.2} | {} |",
        wire.len(),
        wire.len() as f64 / win.len() as f64,
        z3(&wire)
    );
}
