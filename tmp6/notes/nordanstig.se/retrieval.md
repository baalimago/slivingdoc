# nordanstig.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — RESULT: 33 KF protokoll recorded (2022-01-01..2026-08-20)

## Where KF protokoll live (canonical, ALL years 2016+)
- Entry page: https://nordanstig.se/startsida/sidor/kommun-och-politik/organisation/moten-kallelser-och-protokoll.html
  ("Möten, kallelser och protokoll") -> "Protokoll" section -> "Kommunfullmäktige" accordion.
  Inside: year tabs 2026..2016, each with a SiteVision file-share webapp
  (sv-marketplace-sitevision-file-share) listing PDFs. All file links are rendered in the
  server HTML (NO AJAX; verified via network requests — only menu-service calls). The
  accordion content is in the DOM even when collapsed; default visible year is 2026.
- Direct KF page: https://nordanstig.se/startsida/sidor/kommun-och-politik/organisation/kommunfullmaktige-kf.html
  carries Kallelser (2026) + Protokoll 2026 and a "Protokoll från 2025" collapse; points to the
  meetings page for older years. 2025/2026 protokoll appear on BOTH pages (identical links).
- Download URL shape: https://nordanstig.se/download/18.<nodeId>/<ts>/<name>.pdf
- Filename pattern: "Protokoll KF YYMMDD §§ X-Y.pdf" (some 2016-2018 "KF protokoll YYMMDD ...").
  Each file = one KF meeting's minutes; header of each PDF confirms
  "Nordanstigs kommun SAMMANTRÄDESPROTOKOLL Kommunfullmäktige Sammanträdesdatum YYYY-MM-DD".
- 2023-07-10 is an extra meeting (single § 91). No meetings in Jan/Jul(except 2023)/Aug/Dec.

## Harvest shortcut (worked well)
- Open the meetings page in Playwright, then via browser_evaluate grab all
  a[href*='/download/'] inside the Kommunfullmäktige section id
  svid10_35cdc16f17077dd6848367bc (or just all /download/ links on the page for every body).
  This yields the complete per-year file list in one call, no clicking through year tabs.

## Recorded (33 docs, dates):
- 2026: 02-23, 04-27, 05-25, 06-22
- 2025: 02-24, 04-28, 05-19, 06-23, 10-27, 11-24
- 2024: 02-26, 04-29, 05-20, 06-24, 10-21, 11-25
- 2023: 02-20, 03-27, 04-24, 05-29, 06-26, 07-10, 09-25, 10-23, 11-27
- 2022: 02-28, 03-28, 04-25, 05-23, 06-27, 09-26, 10-24, 11-28
- Cross-checked against the KF page's YouTube webcast list (per-year meeting dates) — 1:1 match.
- Kallelser (agendas) and "Årsredovisning 2025.pdf" on the KF page were NOT recorded
  (agenda/notice + non-protocol).

## Dead ends / notes
- Site search and sitemap are not needed; the meetings page is the single source.
- Anslagstavlan / diarium not required for protokoll. "Förtroendevalda och diariet"
  (https://nordanstig.se/.../fortroendevalda-och-diariet.html) was not explored — if ever
  needed, it links to e-tjanster.nordanstig.se (external).
