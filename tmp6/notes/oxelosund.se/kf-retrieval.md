# Oxelösund (oxelosund.se) - KF retrieval notes

Target: Oxelösunds kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- SiteVision site (www.oxelosund.se / oxelosund.se).
- KF page (single canonical listing for ALL KF meetings back to 2021):
  https://oxelosund.se/kommun-och-politik/moten-kallelser-och-protokoll/kommunfullmaktige
- The page lists one collapsible accordion per meeting date (a.env-collapse-header
  <p> elements, e.g. "10 juni 2026"). Section content is server-rendered in the DOM
  (hidden by env-collapse), so slim_http does NOT show the document links; use
  Playwright browser_evaluate: for each `a.env-collapse-header`, read the date text
  and `data-target` id, then query the matching section for `a[href*="/download/"]`.
  This yields every meeting + its documents in one pass.
- Document URLs: https://www.oxelosund.se/download/18.<nodeid>/<timestamp>/<filename>
- "Kallelse och sammanträdesprotokoll finns tillgängligt i fem år på webbplatsen" -
  protocols kept ~5 years; page reaches back to 2021 (2021-02-10..2021-12-08), so the
  2022+ range is fully covered.
- Digital anslagstavla (https://oxelosund.se/ovrigt/digital-anslagstavla) holds only
  recently posted announcements (KSAU, bygglov kungörelser) - no KF protocols there.

## Meeting dates found on the KF page (accordions)
2026: 02-11, 04-01, 06-10 (3 meetings)
2025: 02-12, 04-02, 05-07, 06-11, 09-10, 10-22, 12-10 (7)
2024: 02-07, 04-03, 05-15, 06-12, 09-11 (CANCELLED - "Beslut om att ställa in
      sammanträde" only, no protocol), 10-16, 11-06, 12-11 (7 protocols)
2023: 02-08, 03-29, 05-10, 06-14, 09-13, 10-18, 11-08, 12-13 (8)
2022: 02-09 (CANCELLED - section text "inställt på grund av för få ärenden", 0 links),
      03-30, 05-11, 06-15, 09-14, 10-19, 11-09, 12-07 (7 protocols)
Older (outside range, ignore): 2021-02-10, 03-31, 05-11, 06-16, 09-15, 11-10, 12-08.

## KF minutes recorded (32 total, all verified/spot-checked as
"Sammanträdesprotokoll - Kommunfullmäktige - date" text-layer PDFs; 8 opened and
content-checked incl. 2026-06-10, 2026-04-01, 2026-02-11, 2025-12-10, 2025-04-02,
2024-12-11, 2023-02-08, 2022-03-30)
- Per meeting pick ONE protocol file: several meetings publish BOTH an
  "omedelbar justering §..." file and the main "Sammanträdesprotokoll §..-.." file
  (2024-12-11 §§119-120 omedelbar + §§121-133 main; 2024-10-16 §76 omedelbar + §§77-99
  main; 2023-12-13 §168 omedelbar + main; 2023-06-14 §90 omedelbar + main;
  2023-05-10 §§69-70 omedelbar + main; 2023-03-29 §§65-68 omedelbar + main;
  2023-02-08 §1 omedelbar + §§2-30 main; 2022-10-19 §49 omedelbar + §§50-55 main).
  Recorded the MAIN protocol only (one per date). Do not record both.

## Excluded (on the same KF page, not minutes)
- "Kallelse" pdfs (per meeting) - agenda/notice terms.
- Interpellation / Svar på interpellation / motion attachments, budgets,
  "Kompletterande handlingar", "Bilaga till ärende" pdfs.
- "Beslut om att ställa in sammanträde 2024-09-11.pdf" and the empty 2022-02-09
  accordion (cancelled meetings - no minutes exist).
- 2021 documents (before range start).

## Tips for next runs
- Use the Playwright browser on the KF page; cookie dialog appears ("Godkänn
  nödvändiga kakor") but does not block DOM extraction via evaluate.
- Date for each record = the accordion header date (meeting date), which matches the
  PDF's "Sammanträdesdatum".
- Conf 0.95 used (pattern fully consistent + 8 content-verified).
- Other bodies (KS, nämnder) each have their own subpage under
  /kommun-och-politik/moten-kallelser-och-protokoll/ - same accordion pattern.
