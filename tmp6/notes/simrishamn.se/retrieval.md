# simrishamn.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — SUCCESS: 44 KF protocol documents recorded (2022-01-31 .. 2026-06-15)

## Site structure / entry
- Simrishamn.se is SiteVision CMS. KF minutes live on one static page:
  **https://www.simrishamn.se/politik-och-paverkan/kallelser-och-protokoll**
  ("Kallelser och protokoll"), reached from Politik och påverkan -> Kallelser och protokoll.
- The page is plain HTML tables (no JS needed, no pagination). One section per body:
  Barn- och utbildningsnämnden, Byggnadsnämnden, **Kommunfullmäktige**, Kommunstyrelsen,
  Kultur- och fritidsnämnden, Revisionen, Samhällsplaneringsnämnden, Senior- och
  tillgänglighetsråd, Socialnämnden, Strategiska utskottet, Ungdomsrådet, Valnämnden.
- KF table columns: Kallelser | Protokoll | Länk webbsändning. Rows newest-first,
  complete from 2022-01-31 to 2026-06-15 (despite page text claiming only current+
  previous year). Cancelled meetings are plain text rows "…inställt" (no links).
- slim_http is noisy (nav menu dominates); use Playwright snapshot to read the tables,
  or fetch raw HTML and grep for /download/ links with "Protokoll".

## Download URL pattern (works directly)
- Documents: **https://www.simrishamn.se/download/<SiteVision-node>/<timestamp>/<URL-encoded filename>.pdf**
  download_document GET 200 application/pdf works directly (no session, no HEAD trap).
- KF protocol filenames: 2022-01-31..2023-12-18 use "KFprot YYYYMMDD.pdf" under
  /download/18.1f5db18d18c80b9663a...; 2024+ mostly "Protokoll KF YYYYMMDD.pdf" under
  various /download/18.* nodes. 2025-01-27 = "KF-protokoll 20250127.pdf";
  2025-06-16 = "Protokoll KF 16 juni.pdf".

## Recorded (44, one per meeting date; all verified GET 200 application/pdf, several
## first-page-verified via document_to_text = "Kommunfullmäktige Sammanträdesprotokoll <date>")
- 2022 (10): 01-31, 02-28, 03-28, 05-30, 06-20, 09-26, 10-17, 10-31, 11-28, 12-19.
- 2023 (10): 01-30, 02-27, 03-27, 05-29, 06-19, 08-28, 09-25, 10-30, 11-27, 12-18.
- 2024 (9): 01-29, 02-26, 04-29, 05-27, 06-26, 08-26, 10-28, 11-25, 12-16.
- 2025 (10): 01-27, 02-24, 03-24, 04-28, 05-26, 06-16, 08-25, 09-29, 10-27, 12-15.
- 2026 (5): 02-23, 03-30, 04-27, 05-25, 06-15.
- Total 10+10+9+10+5 = 44.

## Label/date gotchas
- 2022-03-28 row: page link text says "Protokoll 2023-03-28" and "Kallelse 202-03-28"
  (typos); file is KFprot 20220328.pdf and first page reads "2022-03-28" — recorded 2022-03-28.
- 2025-12-15 row: Kungörelse dated 2025-12-14 but Protokoll = 2025-12-15; recorded 2025-12-15.
  This PDF is scanned (only e-signature page has a text layer) — conf 0.9.
- 2022-12-19..2023-11-27 KFprot files (under 18.1f5db18d...) verified readable text.

## Meetings in range with NO protocol (cancelled/inställt — do not record)
- 2022-04-25, 2022-08-29, 2023-04-24, 2024-03-25, 2024-09-30, 2025-11-24, 2026-01-26
  ("Mötet inställt p g a väderläget").
- No KF meeting between 2026-06-15 and range end 2026-08-20 (KF has no July/August
  sessions; next after range).

## Tips for next run
- Single listing page holds everything; grep HTML for /download/ links, keep the
  "Protokoll" cell per KF row (skip Kallelse/Kungörelse and webbsändning links).
- Verify ambiguous rows (typo'd labels, scanned PDFs) by first-page text extraction.
