# Hoplites와의 경계

2026-10-23 BOSS와 합의했다.

## 책임 분담

| 영역 | 담당 |
|---|---|
| 아티팩트 분석, 구성요소 식별, 원문 보존 | Hoplites |
| 포함 여부(`inclusion`): 코드가 배포물에 실제로 들어가는가 | Hoplites |
| 원문 해석, 라이선스 선택, 승인 | Thesmothetai |
| 적용 판단(`applicability`): 이 배포 조건에서 의무가 생기는가 | Thesmothetai |
| 의무 도출, 이행, `fulfillment` | Thesmothetai |

Thesmothetai는 Dependency-Track에 직접 연결하지 않는다. 인벤토리와 CVE는 Hoplites 영역이며, Dependency-Track에는 Thesmothetai의 재사용 판단에 필요한 원문도 없다.

Hoplites가 기록한 `applicability`와 `fulfillment`는 관측값(`observed_applicability`)으로만 보관하며 판단으로 쓰지 않는다.

## 입력 계약

`thesmo ingest hoplites <run-dir>`은 Hoplites 이미지 실행 디렉터리를 읽는다.

| 파일 | 용도 |
|---|---|
| `license-scopes.json` | 기록 단위 입력. `rule_version`은 `license-scope-v1`만 허용 |
| `sources/` | 보존된 원문. 경로가 아니라 Hoplites가 기록한 SHA-256으로 찾음 |
| `normalized.cdx.json` | `owner_bom_ref` → purl. Syft의 `package-id` 한정자는 실행마다 바뀌므로 제거 |
| `image.json` | 아티팩트 식별(`image_ref`, `platform`) |
| `summary.json` | Hoplites 코드 커밋 |

같은 `license-scopes.json`(SHA-256 기준)을 다시 넣으면 거부하고 기존 run ID를 알려준다.

## 해석 단위

원문 해시가 해석 단위다. 관계(relation)별로 원문을 다음과 같이 잘라낸다.

| relation | 원문 |
|---|---|
| `source-file-declaration` | DEP-5 파일에서 `source_label`에 해당하는 `License` 문단. 저작권자는 원문에서 빼서 고지 데이터(`holders`)로 보관 |
| `source-document`, `assembly-exception-document`, `module-legal-document` | 파일 전체 |
| 바이트 범위가 있는 기록 (`source-notice` 등) | 범위만 잘라내고 `text_sha256`으로 검증 |
| `scanner-evidence-reference` | 라이선스 조건이 아니므로 skipped |

DEP-5 문단 조회 순서: 같은 Files 문단의 인라인 본문 → 독립 License 문단 → 헤더 문단 → 다른 Files 문단의 인라인 본문(본문이 모두 같을 때만). 복합 표현식(`GPL-2+ or Artistic`)은 이름별 문단을 이어 붙인다.

## 미해결 처리

원문을 확정할 수 없는 기록은 버리지 않고 `unresolved`와 사유로 남긴다(`thesmo ingest records <run-id> --outcome unresolved`).

| 사유 | 의미 | 다음 조치 |
|---|---|---|
| `dep5-license-missing` | 라이선스 이름만 있고 본문이 없음 (예: `public-domain`) | 사람 검토 |
| `dep5-not-machine-readable` | DEP-5가 아닌 서술형 고지에서 이름을 찾지 못함 | 사람 검토 |
| `notice-range-not-provided` | 고지가 소스 파일 안에 있으나 범위가 없음. 소스 파일 전체는 라이선스 원문이 아님 | Hoplites 미해결 목록에서 범위 특정 상태 확인 |
| `no-text-evidence` | 원문 증거 자체가 없음 | Hoplites 확인 |

## 고지 범위

해결됨(2026-10-23). 소스 파일 안에 고지가 있는 기록(`installed-source`, `compiled-extension`, `installed-source-or-generated-module`, `binary-embedded-code` 등)에 Hoplites가 바이트 범위(`notice_path`, `notice_sha256`, `source_byte_start`, `source_byte_end`, `text_sha256`)를 제공한다. 기존 evidence 필드는 유지하고 추가만 했으므로 `rule_version`은 `license-scope-v1` 그대로다.

Hoplites가 고지 위치를 근거 있게 특정하지 못한 파일은 범위를 추측하지 않고 Hoplites 미해결 목록(`source_path`, `reason`, `next_action`)에 남긴다. 이 기록은 Thesmothetai에서 `notice-range-not-provided`로 남는다.

## 계약 상태와 위험

입력 계약은 Hoplites main(`dbb24e0`)에 반영되었다. `license-scopes.json` 생성 코드는 Hoplites `scripts/license_scope.py`, 표현 규칙은 Hoplites `docs/license-scope-correction.md`에 있다.

- 형식이 호환되지 않게 바뀌면 Hoplites가 `rule_version`을 올리고, Thesmothetai는 모르는 버전을 거부한다. 조용히 잘못 읽지는 않는다.
- 필드 추가는 버전을 올리지 않는다. Thesmothetai가 새 필드를 써야 할 때는 이 문서와 `internal/collector/hoplites`를 함께 갱신한다.
- 고지 블록에는 저작권 연도가 포함되어, 조건이 같아도 연도만 다른 원문이 따로 해석 대상이 된다. 저작권 줄을 조건과 분리할지는 별도로 결정한다.
