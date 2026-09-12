# Phase 01 — Core và cấu hình

Phạm vi: **MVP**. Trạng thái: **Planned.

Schema, snapshot và quyết định fault kiểm thử được độc lập với HTTP I/O.

## Các plan

| Plan | Tính năng | Phụ thuộc |
| --- | --- | --- |
| [P01](01-config-schema.md) | Schema và validation YAML | Scaffold |
| [P02](02-runtime-snapshots.md) | Snapshot và trạng thái injection | P01 |
| [P03](03-rule-engine.md) | Matcher, selector và sequence | P01, P02 |

## Điều kiện hoàn tất phase

Hoàn tất các plan trong bảng và có bằng chứng VERIFY/REVIEW cho phạm vi phase. MVP chỉ hoàn tất sau phase 05 và đủ AC1–AC24.

Xem [lộ trình và quy tắc thực hiện](../README.md).
