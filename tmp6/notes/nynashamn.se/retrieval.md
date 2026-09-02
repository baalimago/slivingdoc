# nynashamn.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — RESULT: 49 KF protocols recorded (2022-01-13 .. 2026-06-11)

## Site structure
- SiteVision CMS. Meeting/protocol pages:
  - Current year: https://nynashamn.se/service/organisation--styrning/politik-och-organisation/sammantraden-och-protokoll
    (accordions per body; "Protokoll kommunfullmäktige" holds the current-year KF PDFs, plus
    Dagordning/Tillkännagivanden lists).
  - Previous years: .../sammantraden-och-protokoll/protokoll-fran-tidigare-ar (accordions per body,
    then per year 2023/2024/2025). Live page only keeps three years back + current year.
- Downloads are SiteVision /download/18.<nodeId>/<timestamp>/<name>.pdf URLs. 2022-era live
  download URLs now 404 (files removed from CMS), so 2022 must come from the Wayback Machine.

## 2022 protocols (Wayback only)
- The current live "tidigare år" page no longer lists 2022. The best archived source is the
  Wayback capture of the tidigare-ar page at
  https://web.archive.org/web/20250425092118id_/https://nynashamn.se/service/organisation--styrning/politik-och-organisation/sammantraden-och-protokoll/protokoll-fran-tidigare-ar
  (capture 2025-04-25, page last changed 2025-01-08; it lists KF 2022 + 2023 + 2024 sections).
- 2022 KF meetings with protocol files (12): 01-13, 02-10, 03-10, 04-21, 05-12, 06-16, 09-15,
  10-13, 10-27, 11-10 (budget), 12-05, 12-08. All downloaded and text-verified from Wayback via
  https://web.archive.org/web/20251214192647id_/<live-download-url> (resolves to archived PDF).
- Split/excerpt files for the SAME meeting date (skip per one-protocol-per-date rule):
  2022-05-12-...-§70.pdf, 2022-06-16-...-§93.pdf, 2022-10-27-...-§149.pdf, and the two
  2022-09-15 part files (§§107-109,112-126 / §§110-111) — for 2022-09-15 the §§107-109,112-126
  part (24 pp, includes meeting header) was recorded as THE meeting protocol.
- Older URL in 2022 was /service/organisation--styrning/insyn-och-paverkan/politiska-sammantraden-och-protokoll;
  its last Wayback capture (2022-10-31) lacks Nov/Dec 2022 protocols — the 2025-04-25 capture of
  the newer tidigare-ar URL is the complete source.

## 2023-2025 protocols (live)
- Live tidigare-ar page lists: 2023 x10 (01-12,02-09,03-09,05-11,06-15,09-14,10-12,11-08,11-09,12-07),
  2024 x10 (02-15,03-14,04-25,05-16,06-18,08-29,10-17,11-14,11-21,12-12), 2025 x12 KF
  (01-16,01-28,03-13,04-10,05-15,06-12,08-28,09-25,10-16,11-13,11-20,12-11).
- 2025 also lists two "Kommunfullmäktige presidium" protocols (2025-09-11, 2025-10-09) under the KF
  heading — these are the PRESIDIUM body's minutes (5 attendees), NOT plenary KF; excluded.
- Downloaded several live PDFs and text-verified sammanträdesdatum matches filenames.

## 2026 protocols (live main page)
- 2026 KF protocols: 01-15, 03-12, 04-16, 05-21, 06-11 (5). Next meetings (08-27, 10-12 etc.)
  have no published protocol yet; range end 2026-08-20 anyway.
- No 2022-2026 duplicates: all recorded once per meeting date.

## Dead ends / tips
- Wayback CDX API (web.archive.org/cdx/search/cdx) returned 503 "temporarily offline" and empty
  results during this run; replay service (/web/<ts>id_/URL) worked fine. Use replay + probes to
  map captures.
- slim_http output for nynashamn.se pages is dominated by nav links; filter with
  required_tokens=["download"] to get PDF links, or use Playwright browser_find on the accordions.
- Cookie-consent modal intercepts clicks in Playwright; click "Godkänn nödvändiga kakor" first.
