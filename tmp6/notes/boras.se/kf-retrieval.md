# boras.se (Borås Stad) - KF retrieval notes

Target: Borås, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-22, range 2022-01-01..2026-08-22.

## RESULT: 50 KF protocol records (2022-01-20 .. 2026-06-17), conf 0.95
2022 (11): 01-20, 02-24, 03-24, 04-28, 05-12, 06-22, 08-25, 09-29, 10-20,
11-24, 12-07. 2023 (11): 01-19, 02-23, 03-16, 04-27, 05-11, 06-21, 08-24,
09-28, 10-19, 11-22, 12-21. 2024 (11): 01-18, 02-22, 03-21, 04-25, 05-23,
06-19, 08-22, 09-25, 10-24, 11-20, 12-12. 2025 (11): 01-16, 02-20, 03-27,
04-24, 05-22, 06-18, 08-21, 09-25, 10-23, 11-26, 12-11.
2026 (6): 01-22, 02-26, 03-26, 04-23, 05-07, 06-17.

## Site structure (SiteVision CMS)
- Entry: "Möten, handlingar och beslut" page
  https://www.boras.se/kommunochpolitik/motenhandlingarochbeslut.4.1fef6289155e1318edf38e56.html
  -> "Beslut och protokoll" and, per body, "Handlingar och protokoll" pages.
- KF listing page (past meetings by year, 2026 current section + "tidigarear"):
  https://www.boras.se/kommunochpolitik/kommunensorganisation/kommunfullmaktige/handlingarochprotokollkommunfullmaktige.4.1fef6289155e1318edf1824d.html
  Lists "Sammanträdet den <date>" links per year (2026..2019). One row per
  meeting; multi-day meetings (Dec 2022, Nov 2023, Nov 2024, Nov 2025) show
  as ONE row under the start date.
- Each meeting has its own page: .../kommunfullmaktige/kommunfullmaktige/kommunfullmaktigesammantrade<YYYYMMDD><HHMM>.5.<nodeid>.html
  for 2026+, and .../tidigarear/kommunfullmaktigesammantrade<date>... for
  2025 and earlier. The page's "Beslut och protokoll" section holds the
  protocol PDF: "Kommunfullmäktige <date> protokoll.pdf" (naming varies:
  "Protokoll.pdf", "-Protokoll.pdf", "protokoll-2.pdf", "Protkoll ..." typo,
  "Protokoll Kommunfullmäktige <date>.pdf", combined "2022-12-07-08",
  "2023-11-22-23", "2024-11-20-21", "2025-11-26-27").
- Protocol URL pattern: https://www.boras.se/download/18.<nodeid>/<unixms>/<filename>.pdf

## GOTCHA: newer pages (and some older ones) render file lists via JS webapp
- 2022..2025 (most) pages: protocol link is in server-rendered HTML; slim_http
  token filter on "Protokoll.pdf" works.
- 2026 pages (and 2023-03-16, 2024-06-19, 2024-08-22, 2025-11-26): link NOT in
  raw HTML - slim_http finds nothing; must render with Playwright and collect
  <a href=...download/...> links. 2024-06-19 file is named "Protkoll
  Kommunfullmäktige 2024-06-19.pdf" (missing 'o') so a "protokoll" regex also
  misses it - grep /download/ links generically.
- 2026-08-20 meeting: no protocol published yet ("Protokollet publiceras här
  efter att det har justerats") as of harvest date; nothing to record.
- Meeting pages with per-§ supplements (e.g. "2024-11-20 § 165 Protokoll.pdf",
  "2025-11-26-27 Protokoll § 169.pdf") - record only the FULL protocol
  (combined date range file) for the meeting's start date.

## Verification
- Sampled downloads: 2022-01-20, 2022-12-07-08 (Dag 1 07/12 + Dag 2 08/12),
  2023-03-16, 2024-06-19, 2025-11-26-27 (Dag 1 26/11 + Dag 2 27/11), 2026-06-17
  - all "SAMMANTRÄDESPROTOKOLL / Kommunfullmäktige" with matching
  sammanträdesdatum. Combined protocols recorded once under the start date
  (as listed on the index page).

## Dead ends / tips
- webbdiarium https://diarier.boras.se/ exists (link from Webbdiarium page) but
  was NOT needed: all KF protocols 2022-2026-06 are on the meeting pages.
- "Sammanträden, Kommunfullmäktige" page only lists future meetings.
- Bilagor/Voteringar/handlingar.pdf files on meeting pages are attachments -
  skip. Protokollsanteckning files are notes - skip.
- KF meets monthly-ish Jan/Feb-Dec, no July; 2022 had 12 meetings (extra
  budget continuation 12-07/08), 2023-2025 had 11 each (one combined
  multi-day in Nov/Dec), 2026 had 6 in range.
