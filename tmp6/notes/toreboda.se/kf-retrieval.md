# Töreboda (toreboda.se) — KF minutes retrieval

Site: SiteVision CMS. Navigation: Kommun och politik > Politik och demokrati >
Sammanträden och protokoll > Protokoll > Kommunfullmäktiges protokoll.

## Key URLs (live)
- KF protocol listing (year folders):
  https://toreboda.se/kommun-och-politik/politik-och-demokrati/sammantraden-och-protokoll/protokoll/kommunfullmaktiges-protokoll
  Year subfolders shown (2026, 2025, 2024, 2023). Folder ids:
  - 2026: folder=19.7a97deef19afcba6812449d
  - 2025: folder=19.15e5a457193decde021175
  - 2024: folder=19.39b9eecc18df28a9e4527e70
  - 2023: folder=19.39b9eecc18df28a9e4527e6e
  Pattern: <page>?folder=<id>&sv.url=12.39b9eecc18df28a9e453fba9
- KF meeting documents/agendas (excluded as agenda/notice):
  https://toreboda.se/kommun-och-politik/politik-och-demokrati/sammantraden-och-protokoll/sammantradeshandlingar-for-kommunfullmaktige
  (files named "Handlingar kommunfullmäktige YYYY-MM-DD.pdf" — agendas, not minutes)
- Documents: direct PDF links under /download/18.<id>/<timestamp>/<filename>.pdf

## Structure notes
- The protocol page lists year folders only 2023..2026. The oldest folder (2023)
  shows a "Föregående" link that goes to the PARENT folder (year list), i.e. no
  2022 folder exists — 2022 KF protokoll are NOT published online (site text says
  older protocols can be requested from kommunen@toreboda.se). Same for
  sammanträdeshandlingar (parent folder 19.39b9eecc18df28a9e453fdc7, no 2022).
- 2023 folder files were uploaded 2024-04-11; 2024 files in real time during
  2024. Site redesigned ~Dec 2024.
- Duplicate found: Protokoll-KF 2026-03-30.pdf appears twice in 2026 folder
  (uploaded 2026-03-31 and 2026-04-08); byte-identical SHA-256
  1e04fdea39696a1df867930ca0f4fd75794fdc21bd7524f455ca8f90884a9673 — recorded once
  (the 2026-04-08 upload).
- File naming varies: "Kommunfullmäktige protokoll YYYY-MM-DD.pdf" (2023),
  "Kf protokoll YYYY-MM-DD.pdf" (2024-2026), "KF ptotokoll 2026-03-06.pdf"
  (typo as published), "Protokoll-KF 2026-03-30.pdf". Meeting date is in filename.

## Harvested (2022-01-01..2026-08-20), all confidence 1.0
- 2023: 10 meetings (01-30, 02-27, 03-27, 04-24, 05-29, 06-19, 09-25, 10-30,
  11-27, 12-18)
- 2024: 8 (02-26, 03-25, 05-27, 06-17, 09-30, 10-21, 11-25, 12-16)
- 2025: 8 (02-26, 03-31, 04-28, 06-18, 09-25, 10-30, 11-27, 12-18)
- 2026: 5 (02-23, 03-06, 03-30, 05-25, 06-16)
- 2022: none published online.

## Tips for next run
- Year folder ids may shift after CMS updates; re-discover from the top-level
  protocol page (Playwright renders the file table; slim_http works too).
- Exclude "Handlingar ..." files (agendas). Record only *protokoll* PDFs.
