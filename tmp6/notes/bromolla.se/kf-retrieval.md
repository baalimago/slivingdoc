# bromolla.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-22 — SUCCESS: 42 KF protocol documents recorded (2022-02-07 .. 2026-06-15)

## Site structure / entry
- Bromolla.se is an Optimizely (EPiServer) CMS. Single listing page holds ALL KF protocols:
  **https://bromolla.se/kommun-och-politik/politik-och-demokrati/protokoll/**
  (Kommun och politik -> Politik och demokrati -> Protokoll). No JS needed to read the links;
  the page is accordions per body+year. slim_http output is nav-menu-noise dominated; filter on
  "globalassets" + "kommunfullmaktige" to get the KF links (all accordions are in the raw HTML).
- KF accordions currently on the page: 2026, 2025, 2024, 2023 only (2022 section was REMOVED
  from the live page — see below). Other KF-adjacent bodies (arvodesberedning, valberedning,
  tillfälliga beredningar) are SEPARATE bodies — skip for kf.

## Download URL pattern (live files, 2023-2026)
- `https://bromolla.se/globalassets/kommun-och-politik/protokoll/kommunfullmaktige-<YEAR>/kommunfullmaktiges-protokoll-<YYYY-MM-DD>.pdf/download`
- download_document GET 200 application/pdf works directly (no session, no HEAD trap).
- All first pages verified: "SAMMANTRÄDESPROTOKOLL / Datum / <date> / Kommunfullmäktige".

## 2022 KF protocols — NOT on the live site anymore
- The live protokoll page dropped the "Kommunfullmäktige 2022" accordion (keeps 2023+ only) and
  the 2022 globalassets files 404 ("definitively not found").
- Diarium (https://diarie.bromolla.se, Evolution Internet 3) only lists recent KF meetings
  (dropdown starts 2025-02-10; closed cases kept ~1 year), so no 2022/2023/2024 there either.
- 2022 KF protocols survive ONLY in the Wayback Machine. The archived protokoll pages
  (e.g. https://web.archive.org/web/20231211092354/https://www.bromolla.se/kommun-och-politik/politik-och-demokrati/protokoll/)
  linked all 9, under the SAME /globalassets/kommunfullmaktige-2022/ paths.
- IMPORTANT wayback gotchas:
  * The plain /web/<ts>/ URL serves an HTML toolbar wrapper (content-type text/html, 10KB) —
    NOT the PDF. Use the raw form `https://web.archive.org/web/<ts>if_/https://bromolla.se/...` which
    returns application/pdf directly.
  * Multiple captures exist per file and SOME ARE TRUNCATED to exactly 1048576 bytes (pdftotext
    then fails). Pick the largest capture. CDX (query via browser fetch, slim_http 503s/429s):
    `https://web.archive.org/cdx/search/cdx?url=bromolla.se/globalassets/.../download&output=text&fl=timestamp,statuscode,length`
  * The 2023-05-14 crawl (20230514022930 / ...22931) holds the largest copy of every 2022 file —
    recorded those timestamps.

## Recorded (42, one per meeting date)
- 2022 (9, wayback raw if_ URLs, ts 20230514022930/31): 02-07, 03-14, 04-11, 05-16, 06-13,
  09-19, 10-24, 11-28, 12-12. (2022-12-06 wayback snapshot of the page confirms the same 9;
  CDX wildcard on the 2022 dir shows no other files. No KF meeting in 2022-01/07/08.)
- 2023 (10, live): 01-30, 02-27, 04-03, 04-24, 05-29, 06-12, 09-18, 10-30, 11-27, 12-11.
- 2024 (10, live): 02-12, 03-18, 04-08, 04-29, 05-27, 06-17, 09-16, 10-28, 11-25, 12-16.
- 2025 (9, live): 02-10, 03-31, 04-28, 05-26, 06-16, 09-15, 10-27, 11-24, 12-15.
- 2026 (4, live): 02-09, 03-30, 05-25, 06-15. (Nothing 2026-06-16..2026-08-22; KF summer break.)
- All 42 first-page text-verified (date matches filename); conf 0.95.

## Dead ends
- Site search /sok/?q=... only indexes pages, not the globalassets PDFs — useless for documents.
- Anslagstavla and webbkarta have no older KF archive.
- source_page for live = protokoll page; for 2022 wayback = the 2023-12-11 archived protokoll page.

## Tips for next run
- Enumerate the live protokoll page (filter globalassets+kommunfullmaktige); for the KF years
  only take /kommunfullmaktige-YYYY/ paths (not -arvodesberedning, -valberedning, beredningar).
- If 2022 (or earlier) is wanted again: use the wayback if_ form + CDX largest-capture trick above.
