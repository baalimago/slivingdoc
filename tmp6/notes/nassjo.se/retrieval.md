# nassjo.se retrieval log

## kf — 2026-08-20: SUCCESS, 44 protocols recorded (2022-01-27 .. 2026-06-11)
- Source: diariet.nassjo.se (EvoInternet public diarium). Discovered via
  https://nassjo.se/kommun-och-politik/kommunens-handlingar.html -> "Handlingar, ärenden,
  kallelser och protokoll i diariet" -> https://diariet.nassjo.se/documents.
- Playwright (browser) CAN load diariet.nassjo.se (unlike nassjo.se which 403s). Used it to
  discover the API: search UI calls GET /api/documents?page=&pageSize=&filter=...&orderBy=...;
  "Hämta fil" downloads from GET /api/documents/{guid}.bin (application/pdf).
- Enumerated with slim_http:
  - filter=unitCode:KS;description:protokoll%20kommunfullm%C3%A4ktige; (120 rows, all years back to 2013)
  - filter=unitCode:KS;description:kommunfullm%C3%A4ktige;documentTypes:PRO / PRO2 / PRKOPI /
    INKPR%20NY (to catch public protocols whose description lacks "protokoll", e.g.
    2026-02-26 "Kommunfullmäktige den 26 februari 2026, utan personuppgifter" INKPR NY).
  - PROT NY / PROT / PRO3 types hold nothing in 2022+ range (pre-2022 only).
- Recorded one public protocol per meeting date (44 total): 2022 x10, 2023 x10, 2024 x10,
  2025 x9, 2026 x5. URLs all https://diariet.nassjo.se/api/documents/<guid>.bin, source_page
  https://diariet.nassjo.se/documents, conf 0.9-0.97. Sample-verified PDFs across years
  (2022-01-27, 2022-12-08, 2023-11-23, 2024-09-26, 2024-10-31, 2025-11-27, 2025-12-11,
  2026-02-26, 2026-06-11): all SAMMANTRÄDESPROTOKOLL Kommunfullmäktige, dates match.
- Gotchas: per-§ split files skipped (recorded main part only for 2023/2024 split meetings);
  "Arkivbeständigt protokoll" (PRO2, no fileId) skipped; kallelser/voteringslistor/incoming
  lists skipped; 2025 has no Feb meeting; 2026-01-29 cancelled (no protocol).

## Notebook infra note
- notes_pull/notes_commit fail with "local private state operation failed" this run too
  (same as eksjo.se 2026-08-20 run). Notes were written to disk under notes/nassjo.se/;
  the service state would not accept them. Recorded documents themselves are unaffected
  (44 accepted by record_documents).
