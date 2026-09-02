# koping.se retrieval log

## kf — round 1 (2026-08-20): SUCCESS — 44 KF protocol PDFs recorded (2022-01-01..2026-08-20)

### Site structure
- koping.se is SiteVision CMS. Meetings/protocols live under
  https://koping.se/kommun--politik/politik-ledning-och-namnder/...
- Index page (found via "Kommun & politik" -> "Politik, ledning och nämnder" ->
  "Protokoll och kallelser" page, and directly from the Kommunfullmäktige page):
  - KF protocols: https://koping.se/kommun--politik/politik-ledning-och-namnder/kommunfullmaktige/protokoll-for-kommunfullmaktige.html
  - KF kallelser (agendas — do NOT record): .../kommunfullmaktige/kallelser-for-kommunfullmaktige.html
  - Same pattern for other bodies: protokoll-for-kommunstyrelsen.html, -ksau, -kultur--och-fritidsnamnden, -samhallsbyggnadsnamnden, -social--och-arbetsmarknadsnamnden, -utbildningsnamnden, -valnamnden, -vard--och-omsorgsnamnden, -overformyndarnamnden, etc.
- The KF protocol page lists ALL years 2016-2026 on ONE page, newest first
  (2026, 2025, 2024, 2023, 2022, 2021, ...). No pagination. "Senast uppdaterad"
  for the page was 2026-07-01; newest 2026 entry is 2026-06-15.
- Direct PDFs: https://koping.se/download/18.<uuid>/<timestamp>/<filename>.pdf
  (filename percent-encoded). URLs contain no date component; the date is only
  in the filename/title.
- Caveats:
  - Some entries are "Mapp" (folder) accordion buttons that must be opened to
    reveal the PDF inside (e.g. "Kommunfullmäktige 2023-12-18" folder ->
    Kommunfullmäktige 2023-12-18.pdf; "Kommunfullmäktiges valberedning
    2022-12-12" folder -> valberednings protocol). Click to expand; the folder
    contains one protocol PDF.
  - A few meetings have TWO pdfs for the same date: a full protocol plus a
    directly-justerad partial (§) addendum (e.g. 2024-02-05 §12 vs full
    20240205.pdf; 2022-02-28 §§4-5 vs full; 2022-12-19 §127 vs full). Record
    ONLY the full protocol (one document per meeting date).
  - Voteringsbilaga/Voteringslista pdfs are voting appendices, not minutes —
    skip them. Some protocols embed the voteringsbilaga in the same PDF
    ("... med voteringsbilaga.pdf") — those ARE the minutes for that date.
  - "Kommunfullmäktiges valberedning 2024-01-10.pdf" and the valberedning
    2022-12-12 folder are the KF nominating-committee's protocols, listed under
    the KF heading but NOT KF plenary minutes — excluded from kf harvest.
  - Beware: snapshot/display can drop a character from the long SiteVision
    id (e.g. 2022-12-19 full protocol real href id is
    18.1b65c9ff185299d9b3058f8, display showed ...58f). Always take the href
    from the DOM (page.evaluate) and verify with download_document; the
    truncated id 404s.
  - One URL contains a non-breaking space: Kommunfullm%C3%A4ktige%C2%A02026-03-02.pdf
    (works as-is).
  - 2024-12-16 filename has typo "Kommunfullm%C3%A4tkige" (mätkige) — use the
    href exactly as published.
- Everything rendered server-side; no JS/API feeding needed (page.evaluate +
  slim_http both work). slim_http on this page only surfaced 2020+ links when
  filtered; use Playwright or evaluate to get the full ordered list.

### Recorded (44 KF minutes PDFs, date = meeting date):
- 2026: 02-02, 03-02, 04-13, 05-04, 06-01, 06-15 med bilaga
- 2025: 03-03 justerat, 04-14, 05-05, 06-02, 06-16, 09-08, 10-06,
  11-10 med voteringsbilaga, 12-15
- 2024: 02-05 (full "Kommunfullmäktige 20240205.pdf"), 03-04, 04-08, 05-06,
  05-15 med voteringsbilagor, 06-03 med voteringsbilaga §78, 06-17 med
  voteringsbilaga §88, 09-09 med voteringsbilaga §112, 10-07 med
  voteringsbilaga, 11-04, 12-16
- 2023: 02-14, 03-07, 04-04, 05-08, 06-08, 09-04, 10-09, 11-06, 12-18
  (in folder)
- 2022: 02-28, 03-28, 04-25, 05-30, 06-20, 09-26, 10-31, 11-28, 12-19
- All downloaded (status 200, application/pdf); spot-checked text extraction
  confirms "Protokoll" / SAMMANTRÄDESPROTOKOLL for KF meeting date.

### Dead ends / tips
- Guessing URL .../kommunfullmaktige.html sub-pages: the protokoll page exists
  at the predictable slug, but always navigate from "Protokoll och kallelser"
  index page to confirm.
- The homepage nav dump (slim_http on /) contains the whole menu; filter with
  required_tokens to find "kommun--politik" links.
- kallelser-for-kommunfullmaktige.html holds agendas only — not for kf minutes.
