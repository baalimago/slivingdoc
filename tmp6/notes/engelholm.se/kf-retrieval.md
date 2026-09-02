# engelholm.se (Ängelholm) kf retrieval log

## Run 2026-08-20 — KF minutes 2022-01-01..2026-08-20

## Site structure
- Current site engelholm.se (SiteVision). "Möten och allmänna handlingar"
  (https://engelholm.se/kommun-och-politik/moten-och-allmanna-handlingar.html)
  points to the **Webbdiarium = https://diariet.engelholm.se/#!/search/**
  (Ciceron GUI, Microsoft IIS; session-based search via GET handlers, NOT JSON-RPC).
  Anslagstavla = https://anslagstavlan.engelholm.se/ (notice board, excluded).
- The **live diarium only holds 2026 records** (search InfoType=64
  "Sammanträdesprotokoll", Diarie=KS, BeslutInstans=Kommunfullmäktige,
  2022-01-01..2026-08-20 -> 6 hits, all 2026; all-nämnder search 2022..2026 ->
  78 hits, all 2026-01-12..2026-08-17). Older data is purged (like Helsingborg).

## Diarium API (session-based GET handlers on https://diariet.engelholm.se/)
- `?handler=Change&name=InfoType&value=64` (64=Sammanträdesprotokoll, 1=Möte,
  6=Hela diariet), `name=Diarie&value=KS`, `name=BeslutInstans&value=Kommunfullm%C3%A4ktige`,
  `name=FromDate&value=YYYY-MM-DD`, `name=ToDate&value=YYYY-MM-DD`, `name=Text`.
- `?handler=Search` runs search (results HTML table with `data-id`, `.item-text-inst`, `.item-text-date`);
  `?handler=LoadMore` paginates (returns accumulated list).
- `?handler=ReadDetails&id=N` expands item N; file links are
  `<a onclick="downloadFile(this)" data-id="<famId>" data-filename="<base64>">label</a>`.
- Download URL: **https://diariet.engelholm.se/?handler=FileDownload&famId=<id>&fileName=<base64>**
  (base64 decodes to e.g. "KF 2026-06-15 Protokoll.pub.pdf"). Plain GET returns the PDF.
- 2026 KF protocols: famId 2622 (2026-06-15), 1685 (05-25), 1285 (04-27), 872 (03-30),
  348 (02-23), 53 (01-26).

## Current engelholm.se meeting pages (LIVE source for 2024..2026)
- Per-meeting pages under
  /kommun-och-politik/politik-och-demokrati/kommunfullmaktige/kommunfullmaktiges-sammantraden-20XX/<slug>.html
  listed on https://engelholm.se/kommun-och-politik/politik-och-demokrati/kommunfullmaktige.html
  (sections 2026, 2025, 2024 ONLY; no 2022/2023).
- Each meeting page has "Mötets kallelse" (excluded) and "Mötets protokoll"
  with a direct PDF link **https://www.engelholm.se/download/18.<hex>/<ts>/KF%20<date>%20Protokoll[.pdf]**.
  NOTE: 2026-04-27 page links a placeholder "Platshållare_nämnders_kallelser_och_protokoll.pdf"
  (61 kB) — the real 2026-04-27 protocol is in the diarium (famId 1285).
- Some dates have TWO protocol files: a partial "omedelbar justering § N" and the main
  "KF Protokoll <date>" — record only the MAIN one (one per meeting date).
  (2024-02-26, 2025-08-25 have such partial files.)

## 2022..2023 KF protocols — only via Wayback Machine
- Old site (same URL patterns, e.g. .../kommunfullmaktiges-sammantraden-2022/2021-12-28-...-sammantrade-2022-03-28.html)
  is gone from the live site (404). Old diarium diarium.engelholm.se (pre-2023 diarium) is dead (DNS gone).
- Wayback captured many old /download/18.<id>/<ts>/KF%20<date>%20Protokoll.pdf files.
  Replay form that serves the raw PDF: **https://web.archive.org/web/<ts>if_/https://www.engelholm.se/download/...**
- IMPORTANT: some Wayback captures are exactly 1048576 bytes and corrupt (pdftotext exit 99,
  `file` says "PDF version 1.7 (zip deflate encoded)" with NO page count) — those are truncated/bad.
  Others are exactly 1048576 but VALID (`file` reports "PDF ... N page(s)") — complete PDFs with no
  readable text layer (pdftotext exit 1). Check with `file` before trusting.
  Verified-good timestamps per date are recorded in this run (see recorded list).
- Wayback meeting-page replays confirm each protocol link:
  e.g. 2022-11-14 page 20230926100325 -> "KF Protokoll 2022-11-14.pdf"
  (18.1814b0d51847df747e77e4/1669277196873, NOT captured in WB);
  2023-05-29 page 20230930071742 -> "KF 2023-05-29 Protokoll.pdf" (NOT captured in WB), etc.

## Meetings with NO online protocol (do not re-hunt; absent from live site + diarium + Wayback)
- 2022-08-29 (URL 18.3031f8b91829a13a3a71d9c/... only 404 in WB)
- 2022-09-26 (18.3103bcdc1834e782dbf14e5/... no WB capture)
- 2022-11-14 (18.1814b0d51847df747e77e4/... no WB capture)
- 2023-05-29 (18.346311d41881834e1ddb70/... no WB capture)
- 2023-06-19 (18.347470ab188cc9e95b19f3/... no WB capture)
- 2023-09-25 (18.2ddcc80618aa0cf703a4a9/... no WB capture)
- 2023-11-13 budget (18.5bbc94c618ba823037f555/... no WB capture)
- 2023-11-27 (18.6863a41118c23ae2230b7/... no WB capture)

## Recorded (47, one per meeting date)
- 2022 (10): 02-07, 02-28, 03-28, 04-25, 05-30, 06-20, 10-17, 10-31, 11-28(0.7), 12-12
- 2023 (7): 01-30, 02-27, 03-27, 04-24, 08-28(0.7), 10-30(0.7), 12-11(0.7)
- 2024 (12): 01-29, 02-26, 03-25, 04-29, 05-27, 06-17, 08-26, 09-30, 10-28, 11-11, 11-25, 12-16
- 2025 (12): 01-27, 02-24, 03-31, 04-28, 05-26, 06-16, 08-25, 09-29, 10-27, 11-10, 11-24, 12-15
- 2026 (6): 01-26, 02-23, 03-30, 04-27, 05-25, 06-15 (diarium FileDownload URLs)

## Tips for next run
- 2024-2026: extract "Mötets protokoll" links from the live meeting pages; use diarium for 2026.
- 2022-2023: only Wayback if_ replays; verify with `file` (page count) since text layer may be missing.
- The 1MB "truncated-looking" captures that still report "N page(s)" via `file` are valid PDFs (scanned/no text).
