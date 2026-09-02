# fargelanda.se retrieval log (kf = Kommunfullmäktige)

## kf — round 1 (2026-08-25): SUCCESS, 23 protocols recorded (2022-04-13 .. 2026-06-17)

- fargelanda.se is Umbraco CMS. KF protocol archive lives on ONE page:
  https://fargelanda.se/kommun-och-politik/politik-och-demokrati/kommunfullmaktige/protokoll/
  Reachable from root -> Kommun och politik -> Politik och demokrati -> Kommunfullmäktige -> Protokoll.
- The page lists ALL years inline (Årets protokoll 2026, then Äldre protokoll 2025..2010) — no
  accordion/AJAX needed. slim_http is swamped by the nav menu; filter with required_tokens
  ["media"] (max_lines ~150) to get just the PDF links. Playwright evaluate on 'a[href*=media]'
  confirms the same list.
- Document URLs: https://fargelanda.se/media/<8-char-media-id>/<url-encoded-filename>.pdf
  (Umbraco media). download_document works fine (GET 200 application/pdf); document_to_text OK.
- KF meetings are few per year (small municipality): 2022: 4 (04-13, 06-08, 10-19, 12-19),
  2023: 4 (04-12, 06-14, 09-27, 11-29), 2024: 6 (03-20, 04-24, 06-12, 10-23, 11-06, 12-11),
  2025: 6 (03-19, 05-21, 06-18, 09-24, 10-22, 12-10), 2026: 3 (03-18, 04-22, 06-17;
  next meetings 10-21 and 12-09 per sammanträdestider 2026 — nothing in Jul/Aug 2026).
  Total 23, all within 2022-01-01..2026-08-25. Cross-checked against the kallelser page
  (https://fargelanda.se/kommun-och-politik/politik-och-demokrati/kommunfullmaktige/kallelser/)
  — same meeting dates per year, so coverage is complete.
- One meeting = one protocol. SKIP: all "Kallelse..."/"Tilläggs..." (agenda/notice, on the
  kallelser page), "Bilaga till protokoll..." attachments (e.g. bilaga 2026-03-18 Återremiss
  Ödeborgs skola on the protokoll page), and partial "omedelbart justerat" docs (e.g.
  "Protokoll kommunfullmäktige Omedelbart Justerat 34 35" = §§34-35 of the 2025-06-18 meeting;
  the full "Protokoll kommunfullmäktige 2025 06 18" §§36-56 is the one recorded — same date
  conflict otherwise).
- QUIRK: "Protokoll kommunfullmäktige 2024-06-24" (media/15ojq5wr/...pdf) is misnamed — its
  content header + anslag state Sammanträdesdatum 2024-06-12 (kallelse also 2024-06-12).
  Recorded with date 2024-06-12, title as published.
- Titles recorded as published (link text); dates = meeting date from the PDF header
  (SAMMANTRÄDESPROTOKOLL YYYY-MM-DD); confidence 0.95.
- Other bodies (KS, nämnder) have the same /protokoll/ pattern under
  /kommun-och-politik/politik-och-demokrati/<organ>/protokoll/ if needed later.
