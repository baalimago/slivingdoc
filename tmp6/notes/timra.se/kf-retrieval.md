# Timrå (timra.se) - KF retrieval log (Kommunfullmäktige)

Harvest run: 2026-08-20, range 2022-01-01..2026-08-20. RESULT: 38 KF protokoll recorded.

## Site structure (SiteVision)
- Entry: https://timra.se/kommunpolitik.4.714dad16d46439ef96d2e.html
  -> "Sammanträdesinformation" (https://timra.se/kommunpolitik/sammantradesinformation.4.714dad16d46439ef92519.html)
  -> "Kommunfullmäktiges sammanträden"
  (https://timra.se/kommunpolitik/sammantradesinformation/kommunfullmaktigessammantraden.4.714dad16d46439ef941e4.html)
- The KF page itself carries the CURRENT year (2026) protocol list + a "Sammanträdeshandlingar 2026" section,
  and submenu pages for previous years:
  - .../kommunfullmaktigesprotokoll2025.4.18d5936c19aa37ae85e376.html
  - .../kommunfullmaktigesprotokoll2024.4.2de21b92193e12a62a672.html
  - .../kommunfullmaktigesprotokoll2023.4.1e19378518c8eb0097969c.html
  - 2022 page (…/kommunfullmaktigesprotokoll2022.4.4aaf5c8b1853bb4b0f565a.html) is 410 Gone on live site.
- All file lists are server-rendered in the DOM (slim_http sees them; no XHR needed).
- Download URL pattern: https://timra.se/download/18.<nodeId>/<ts>/<name>.pdf
- Filename pattern: "Kommunfullmäktiges protokoll <YYMMDD or YYYY-MM-DD> §§ X-Y.pdf".
  PDF header confirms: "Protokoll / Sammanträdesdatum / Kommunfullmäktige <date>" (2022-2024 format)
  or "Protokoll Kommunfullmäktige / Plats och tid: ... <date> / Paragrafer §X-§Y" (2025-2026 format).

## What to exclude
- "Samlingshandling för KF_YYYY-MM-DD.pdf" / "Sammanträdeshandlingar 2026" = meeting handlingar
  (agenda/notice material), NOT minutes -> exclude.
- Anything named kallelse/föredragningslista -> exclude.

## 2022 protocols: NOT on live site, recovered via Wayback + still-live /download/ URLs
- Live 2022 listing page returns 410. The year pages 2021/2022 etc. were deleted.
- Wayback CDX (web.archive.org/cdx/search/cdx?url=timra.se&matchType=domain&filter=original:.*protokoll.*)
  + capture https://web.archive.org/web/20231001182526/https://www.timra.se/.../kommunfullmaktigesprotokoll2022...html
  gave the 7-file list. All 7 PDFs still served 200 application/pdf from the LIVE /download/ URLs
  (verified by download + text extraction; sammanträdesdatum matches). Recorded with source_page =
  the Wayback capture URL.
- 2022 KF meetings: 02-28, 03-28, 04-25, 06-13, 09-26, 10-31, 11-28 (no Jan meeting; numbering starts §1 at 02-28).

## Recorded meeting dates (38)
- 2026 (6): 02-06 (§1-5), 02-23 (§6-44), 03-30 (§45-73), 04-27 (§74-111), 06-08 (§112-131), 08-17 (§132-138)
- 2025 (9): 01-07 (§1-5), 02-24 (§31-45), 03-31 (§46-61), 04-28 (§62-93), 06-09 (§94-134), 08-25 (§135-151),
  09-29 (§152-186), 10-27 (§187-214), 11-24 (§215-243)
- 2024 (7): 01-29 (§1-45), 03-25 (§46-62), 04-29 (§63-91), 06-10 (§92-114), 09-30 (§115-145), 10-28 (§146-167), 12-16 (§168-195)
- 2023 (9): 01-30 (§1-21), 02-27 (§22-47), 03-27 (§48-64), 04-24 (§65-100), 06-12 (§101-149), 08-28 (§150-158),
  09-25 (§159-184), 10-30 (§185-207), 11-27 (§208-236)
- 2022 (7): 02-28 (§1-28), 03-28 (§29-50), 04-25 (§51-89), 06-13 (§90-130), 09-26 (§131-149), 10-31 (§150-185), 11-28 (§186-263)

## Notes / dead ends
- § numbering gap in 2025 (§§6-30 between 01-07 and 02-24) - no published protocol found on live page,
  Wayback captures (2026-01-22, 2026-02-09) or CDX. Do not fabricate.
- 2025-08-25 PDF internal header says §135-151 though filename says §§131-151 (minor naming inconsistency).
- Wayback CDX via slim_http returned empty for some queries; use download_document to get raw CDX text.
- Site search (SiteVision, JS) not needed; the KF pages are the single source. Webbkarta has no 2022 page.
