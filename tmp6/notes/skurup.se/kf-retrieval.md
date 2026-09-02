# skurup.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — SUCCESS: 31 KF protocol documents recorded (2022-01-31 .. 2026-06-15)

## Site structure / two eras
- skurup.se is SiteVision since ~Oct 2023. KF minutes live on
  https://www.skurup.se/kommun-och-politik/politik-och-demokrati/moten-och-protokoll
  (earlier name "kallelser-och-protokoll"). The page has collapse sections
  "Kallelse till kommunfullmäktige" / "Protokoll till kommunfullmäktige"; the
  Protokoll section is a plain text portlet with PDF links, newest first.
- IMPORTANT: the live Protokoll list is TRIMMED to ~12 most recent entries
  (2025-02-10 .. 2026-06-15 as of 2026-08-20). Older protocols (2023-05-29 ..
  2024-12-16) were on the same page historically but their /download/18.* files
  now 404 on live. Use Wayback captures of the page + the old page for history.
- Pre-2023-05 era: old imCMS site page https://skurup.se/protokoll-kommunfullmaktige
  (dead now, only in Wayback). Old protocol URLs were numeric nodes
  https://skurup.se/<nodeId> which now 404; only those captured in Wayback survive.

## KF protocol inventory 2022-01-01..2026-08-20 (one per meeting date)
- 2022 (8 recorded; 3 NOT recoverable — see below): 01-31, 02-28, 03-28, 05-30,
  06-20, 11-01, 11-28, 12-12. Also 04-25, 08-29, 09-26 existed on old page but
  NO Wayback PDF capture and live node 404 -> NOT recorded.
- 2023 (10): 01-30, 02-27, 03-27, 04-24 (old site; only 04-24 in Wayback —
  01-30/02-27/03-27 NOT recoverable), 05-29, 06-19 (new-site files, in Wayback),
  09-25, 11-27, 12-18 (new-site files, in Wayback).
- 2024 (7): 01-29, 03-25, 04-29, 06-17, 09-30, 10-28, 12-16 (all in Wayback).
  NOTE 2024-06-17: filename says "2024-06-25" (admin typo) but page label +
  PDF first page = "Kommunfullmäktige 2024-06-17 KS 2024.579" -> record 06-17.
- 2025 (7): 02-10, 03-24, 04-28, 06-16, 09-08, 11-17, 12-15 (LIVE on current page).
- 2026 (4): 02-09, 03-23, 04-27, 06-15 (LIVE). Next scheduled 2026-09-07 (>range).

## URLs used
- Live era (2025-02-10..): https://www.skurup.se/download/18.<node>/<ts>/<file>.pdf
  (download_document GET 200 application/pdf works directly).
- Offline new-era (2023-05-29..2024-12-16): record Wayback URL
  https://web.archive.org/web/<wb-ts>/https://www.skurup.se/download/18.<node>/<ts>/<file>.pdf
  (verified with id_ variant -> 200 application/pdf; plain form returns the
  Wayback toolbar wrapper HTML, id_ returns raw bytes).
  wb timestamps: 2023-05-29=20241103170835, 06-19=20241103142110,
  09-25=20241103124641, 11-27=20241103132344, 12-18=20241103125655,
  2024-01-29=20241103144847, 03-25=20241103121201, 04-29=20241103162722,
  06-17=20241103152356, 09-30=20241106142232, 10-28=20251212061934,
  12-16=20251212070237.
- Old era (2022/2023-early): record Wayback URL https://web.archive.org/web/<wb-ts>/https://skurup.se/<node>:
  2022-01-31=/37216@20220816164106, 02-28=/37261@20220816170810,
  03-28=/37310@20220816172020, 05-30=/37375@20220624224836,
  06-20=/37387@20220624225645, 11-01=/37513@20221127163740,
  11-28=/37523@20230821094626, 2023-04-24=/37669@20230510015940.
  All verified 200 application/pdf via id_ form; 2022-02-28 has no text layer
  (scanned), rest text-readable first page "Kommunfullmäktige <date>".

## Dead ends / not recoverable (do NOT re-hunt blindly)
- Old nodes NOT in Wayback as PDF: 2022-04-25 (/37329), 2022-08-29 (/37454),
  2022-09-26 (/37469), 2022-12-12 (/37543), 2023-01-30 (/37586), 2023-02-27
  (/37623), 2023-03-27 (/37648). Live all 404; CDX has no application/pdf
  capture. Only trace is the old-page listing (dates + PDF sizes). Not recorded.
- Old /download/18.* files for 2023-05-29..2024-12-16 are 404 on live; only
  Wayback copies exist (recorded via Wayback URL).
- Anslagstavla (https://www.skurup.se/anslagstavla, React app fed by
  /appresource/4.1be572e3189583430b4ae46/12.1be572e3189583430b4bc72/?searchTerm=&page=N&type=...&instance=...)
  only keeps current notices (KF justeratprotokoll -> 1 article). Not an archive.
- e-portalen.skurup.se is e-services only. No diarium/Public360 portal.

## Source pages used
- Live: https://www.skurup.se/kommun-och-politik/politik-och-demokrati/moten-och-protokoll
- Historical: Wayback captures of .../kallelser-och-protokoll (20231210073031,
  20240530064026, 20241208030002, 20250327132738) and old
  https://web.archive.org/web/20230923111021/https://www.skurup.se/protokoll-kommunfullmaktige

## Verification notes
- 2022-01-31, 2022-11-28, 2023-04-24, 2023-05-29, 2023-06-19, 2023-11-27,
  2024-01-29, 2024-06-17, 2024-12-16, 2025-02-10 first-page text-verified.
- Skip per-meeting extras: "Kallelse ..." docs; "omedelbar justering"/"§ N"
  partials (2023-05-29 /37683, 2024-06-17 §81, 2025-06-16 §68); presidieberedning
  protocols (separate body); kallelse variants of same meeting.
