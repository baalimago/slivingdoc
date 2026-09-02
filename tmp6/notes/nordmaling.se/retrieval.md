# nordmaling.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — RESULT: 28 KF protocols recorded (2022-02-14 .. 2026-06-15)

## Where KF protocols live (canonical source)
- Page: "Protokoll Kommunfullmäktige"
  https://nordmaling.se/kommun/politik/kallelser-och-protokoll/protokoll-kommunfullmaktige
  (entry via https://nordmaling.se/kommun -> Genvägar "Kallelser och protokoll")
- Single static SiteVision page, grouped by year 2026..2022, one PDF link per meeting.
  No pagination, no JS. 28 links total; each is
  https://nordmaling.se/download/18.<nodeid>/<ts>/<filename>.pdf
- Source page URL for all records: the protokoll-kommunfullmaktige page.

## Meeting inventory (verified by downloading + pdftotext; text layer confirms date on p.1
## "Sammanträdesdatum" and "Plats och tid: Oasen, ...")
- 2022: 02-14, 04-11, 06-27, 09-05, 10-24, 11-07, 12-12 (7)
- 2023: 02-13, 04-03, 06-26, 09-25, 11-06, 12-11 (6)
- 2024: 02-12, 04-09, 06-24, 09-30, 11-04, 12-16 (6)
- 2025: 02-10, 04-22, 06-23, 09-29, 11-03, 12-15 (6)
- 2026: 02-09, 04-20, 06-15 (3; last within range 2026-08-20)

## Pitfalls / notes
- The 2022 link labeled "2022-05-09" on the page is a TYPO: the PDF is
  "Justerat protokoll kommunfullmäktige 20220905.pdf" and its content says
  Sammanträdesdatum 2022-09-05 (Oasen 5 sep 2022, §§83-100). Record with date
  2022-09-05, NOT the page label.
- 4 PDFs are scanned (pdftotext empty -> ocr required): 2022-02-14, 2022-11-07,
  2024-12-16, 2025-12-15. Still valid; dates confirmed from filename + page label.
- Kallelser/möteshandlingar (agendas) are on a separate page
  https://nordmaling.se/kommun/politik/kallelser-och-protokoll/kallelser-och-moteshandlingar/kallelser-kommunfullmaktige
  (all "Kallelse ...", "Handlingar ..." PDFs). EXCLUDED (agenda/notice terms).
  It confirms the same meeting dates per year -> the protocol page is complete.
- Anslagstavla https://nordmaling.se/kommun/politik/anslagstavla is a bulletin
  board SPA (webapp) listing only "Kallelse" and "Justerat protokoll" notices
  (Kommunstyrelsen, Sociala utskottet) — no KF protocol documents. Excluded.
- Site search (…/om-webbplatsen/sok?query=…) returns committee protocols and a
  duplicate KF 2022-06-27 file ("Protokollsmall KF (1).signerad27 juni.pdf",
  /download/18.19b60134181723bb23da9e1/1656448332639/…); do not double-record
  same meeting date — use the protokoll page canonical URL.
