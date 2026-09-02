# skinnskatteberg.se retrieval log

## kf — round 1 (2026-08-20): SUCCESS — 34 KF protocol PDFs recorded (2022-01-01..2026-08-20)

### Site structure
- skinnskatteberg.se is a WordPress site. KF protocols live under
  "Kommun och demokrati" -> "Protokoll och dagordningar" -> "Kommunfullmäktige".
- Live KF page (protocols 2024-2026, year accordions "Protokoll"):
  https://www.skinnskatteberg.se/kommun-och-demokrati/protokoll-och-dagordningar/kommunfullmaktige/
- KF archive page (protocols 2023, 2022, 2021 + dagordningar):
  https://www.skinnskatteberg.se/kommun-och-demokrati/protokoll-och-dagordningar/kommunfullmaktige/kommunfullmaktige/
- Protocol PDFs: https://www.skinnskatteberg.se/app/uploads/<yyyy>/<mm>/<filename>.pdf
  (filename carries the meeting date, e.g. protokoll-kf-260126.pdf).
- Accordions are HTML labels; everything is server-rendered (slim_http sees nav
  only; use the Playwright browser and expand each year accordion, or read the
  snapshot files, to get the a[href*="/app/uploads/"] links).
- The live KF page also has "Kommunfullmäktige kallelser och handlingar <year>"
  sections (agenda/notice files) — NOT minutes; skip.

### Recorded (34 KF plenary minutes, date = meeting date)
- 2026 (4): 01-26, 03-30, 05-04, 06-22
- 2025 (7): 02-24, 04-22, 05-05, 06-09, 08-25, 10-13, 11-24
  (2025-03-24 kallelse exists but meeting was INSTÄLLT — "KF inställt 250324" pdf; no protocol)
- 2024 (7): 03-25, 05-06, 06-17, 08-19 (Del 1), 10-21, 11-18, 12-16
- 2023 (7): 03-06, 05-15 (Del 1), 06-19, 10-16, 11-13, 12-11 (Del 1), 12-18 (Del 2)
  (12-18 was an "extra" meeting per dagordning list; Del 2 of the series)
- 2022 (9): 01-10 (Del 1), 01-31, 02-10, 03-07, 05-16, 06-13 (Del 1), 10-17,
  11-14, 12-12 (single PDF "protokoll-kf-221212-221219.pdf" covering 12-12 &
  12-19; recorded under 12-12)

### Same-date split protocols (record ONE per date — Del 1)
Several meetings publish minutes split into "Del 1" + "Del 2" PDFs with the SAME
meeting date in both names: 220110, 220613, 230515, 240819. Recorded only Del 1
(one document per meeting date). 231211/231218 are different dates -> both recorded.
- 240819 Del 2 is 23 MB (vs Del 1 3 MB); Del 1 carries the meeting header.

### Excluded (listed under KF protocol tables but NOT KF plenary minutes)
- "Protokoll KF presidie(t) <date>" — KF presidium protocols: 260316, 250527,
  240214, 240506, 230529, 230612, 220609, 221010 (separate body, like valberedning;
  consistent with koping/orsa convention of excluding KF-internal sub-bodies).
- "Protokoll KF Valberedning 230608" — fullmäktiges valberedning, separate body.
- All "Dagordning KF ..." and "kallelser och handlingar" files — agenda/notice.
- Digital anslagstavla (https://www.skinnskatteberg.se/kommun-och-demokrati/digital-anslagstavla/)
  currently only holds kallelser for VN/SN, no KF protocols; not a KF source.

### Caveats / tips
- ALL protocol PDFs are scanned images — pdftotext yields empty output
  ("scanner: ocr required"); document_to_text cannot verify content in this
  environment. Verification relied on page titles + filenames + dagordning/
  kallelse cross-references. All downloads were 200 application/pdf, sizes match
  the page labels.
- Date format in filenames: YYMMDD (e.g. 260126 = 2026-01-26) or YYYY-MM-DD
  (protokoll-kf-2025-04-22.pdf). 250825 file is protokoll-kf-25-08-25.pdf.
- 2022-03-07 protocol URL is protokoll-kf-220307-1.pdf; its dagordning file is
  named dagordning-kf-220228_hemsidan.pdf (title says 220307).
- 2021 protocols are on the archive page (before range start — ignore).
- The live page's accordion years: 2026, 2025, 2024; archive holds 2023-2021.
