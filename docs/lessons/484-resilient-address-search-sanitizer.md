# Lesson 484: Resilient Address Search & House Number Query Sanitizer

## Navigation Aid & Quick Reference

- **Domain**: Web UI search resilience, Vietnamese address tokenization, diacritics tolerance, street acronym aliases, AGENTS.md Rule 1 (Zero-emoji minimalist copy).
- **Relevant Issues**: Issue #484 (S15-T7), Issue #477 (Mobile Ergonomics S15-T3), Issue #476 (Corridor Routes S15-T2).
- **Key Files**:
  - `apps/web/src/utils/phuongFormatters.ts` (`sanitizeAddressSearchQuery`, `resolveStreetAlias`, `matchesVietnameseSearch`)
  - `apps/web/src/utils/phuongFormatters.test.ts` (Unit test suites covering alleys, prefixes, date streets, acronyms)
  - `apps/web/src/components/PhuongTreeFilter.tsx` (Contextual feedback hint banner beneath search input)
  - `apps/web/src/components/AddressSearchResilience.test.ts` (Empirical automated verification for Rule 10 scenarios)
- **Core Invariants**:
  - **Traffic cameras monitor intersections, not private domiciles.** Department of Transport (Sở GTVT / HTVC) cameras are permanently stationed at bridges, roundabouts, and arterial cross-streets. No camera is mounted to an individual house number.
  - **All-words matching fails on private address queries.** When users paste `34/55/67 Cách Mạng Tháng Tám`, an exact `queryWords.every(...)` conjunction returns 0 results because `"34/55/67"` is not present in camera titles. Stripping house numbers and alley prefixes while preserving the corridor entity restores instant search recall.
  - **Protect canonical date/numeric streets from false-positive stripping.** Streets such as `Đường 3/2`, `3 Tháng 2`, `30/4`, `2/9`, `19/5`, `Đường số 7` contain digits that naive address parsers mistake for house numbers. Explicit whitelisting and named road guardrails prevent stripping vital road names.

---

## 1. Context & Problem Statement

During user testing in Sprint 15, commuters frequently input complete residential addresses copied from ride-hailing orders, messaging chats, or Google Maps:
- Deep alleys with multiple slashes: `34/55/67 Cách Mạng Tháng Tám`
- Building numbers with letter suffixes: `123/4B Lê Văn Sỹ`
- Prefix markers: `Số 123 Nguyễn Trãi`, `Hẻm 456 Hoàng Văn Thụ`
- Mixed comma structures: `34/55/67 CMT8, Phường 5, Tân Bình`

In `phuongFormatters.ts`, `matchesVietnameseSearch(target, query)` previously enforced:
```typescript
const queryWords = normQuery.split(' ').filter(Boolean);
if (queryWords.length > 1 && queryWords.every(w => normTarget.includes(w))) {
  return true;
}
```
Because camera titles and corridor labels lack private house numbers, entering any number or alley notation (`34/55/67`) caused the search to return **0 results**. Commuters interpreted this as a broken search index or missing camera coverage along major corridors.

---

## 2. Root Cause Analysis & Architectural Solution

### 2.1. Regex-Based Address Parsing (`sanitizeAddressSearchQuery`)
We separate the user input into:
1. **House / Alley Identifier**: Number with optional letters and slashes (`34/55/67`, `123A`, `Hẻm 456`).
2. **Core Street Entity**: The arterial corridor name (`Cách Mạng Tháng Tám`).
3. **Ward / Administrative Segment**: Extracted from comma-separated tokens (`Phường 5`).

```typescript
export function sanitizeAddressSearchQuery(input: string): ParsedAddressQuery {
  const raw = (input || '').trim();
  if (!raw) return { rawQuery: '', cleanStreetQuery: '', isHouseNumberDetected: false };

  // Guard: Preserve "Đường số 7", "Đường số 10"
  if (/^(đường\s+số|duong\s+so)\s+\d+/i.test(raw)) {
    return { rawQuery: raw, cleanStreetQuery: raw, isHouseNumberDetected: false };
  }

  const commaParts = raw.split(',').map(p => p.trim()).filter(Boolean);
  const primaryPart = commaParts[0] || raw;
  const wardPart = commaParts.length > 1 ? commaParts[1] : undefined;

  // Guard: Protect date streets like "3/2", "30/4", "19/5"
  if (PROTECTED_DATE_STREETS.some(ps => primaryPart.toLowerCase().replace(/^(đường|duong)\s+/i, '').trim() === ps)) {
    return { rawQuery: raw, cleanStreetQuery: primaryPart, detectedWard: wardPart, isHouseNumberDetected: false };
  }

  const prefixRegex = /^(số\s+nhà|số|hẻm|ngõ|kiệt|ngách|tổ)\s+/i;
  const strippedPrefix = primaryPart.replace(prefixRegex, '');
  const hadPrefix = strippedPrefix !== primaryPart;

  const houseNumRegex = /^(\d+[a-zA-Z]?([\/\-]\d+[a-zA-Z]?)*)\s+(.+)$/;
  const match = strippedPrefix.match(houseNumRegex);

  if (match) {
    return {
      rawQuery: raw,
      cleanStreetQuery: match[3].trim(),
      houseNumber: match[1],
      detectedWard: wardPart,
      isHouseNumberDetected: true,
    };
  }
  // ...
}
```

### 2.2. Acronym & Alias Mapping (`resolveStreetAlias`)
Vietnamese commuters frequently search with abbreviations:
- `cmt8` / `cmtt` $\to$ `cách mạng tháng tám`
- `3/2` / `3 tháng 2` $\to$ `ba tháng hai`
- `vvk` $\to$ `võ văn kiệt`
- `nvt` $\to$ `nguyễn văn trỗi`
- `pvd` $\to$ `phạm văn đồng`
- `dbp` $\to$ `điện biên phủ`
- `xlhn` $\to$ `xa lộ hà nội`
- `ql1a` $\to$ `quốc lộ 1`

`resolveStreetAlias` strips optional prefixes (`Đường`, `Đại lộ`) and maps acronyms to canonical names before matching against camera targets.

### 2.3. Contextual Feedback Banner (Rule 1 Compliant)
When a house or alley number is detected, a subtle monochromatic hint appears below the search input:
- **Vietnamese (`vi`)**:
  `Đang lọc theo trục đường: Cách Mạng Tháng Tám (Hệ thống camera giao thông định vị theo giao lộ, không gắn tại số nhà riêng lẻ)`
- **English (`en`)**:
  `Filtering corridor: Cách Mạng Tháng Tám (Traffic cameras are stationed at arterial intersections, not private addresses)`

This provides immediate transparency, reassuring commuters that the system correctly understood their corridor while explaining the nature of public traffic camera assets.

---

## 3. Verification & Operational Evidence

1. **Unit & Edge-Case Test Suite**:
   - `phuongFormatters.test.ts` validates multi-slash alleys (`34/55/67`), standard building numbers (`150`), prefixes (`Hẻm`, `Số`, `Ngõ`), and protected date streets (`3/2`, `30/4`, `Đường số 7`).
2. **Empirical Scenarios (Rule 10)**:
   - `AddressSearchResilience.test.ts` runs 5 end-to-end automated scenarios verifying corridor group expansion, ward filtering, and search input clearing.
3. **Zero Regression**: 440/440 tests passing across `apps/web`.
