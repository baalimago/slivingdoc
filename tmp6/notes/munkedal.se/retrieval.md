# munkedal.se retrieval log (kf = Kommunfullmäktige)

## kf — round 1 (2026-08-19): SUCCESS, 40 protocols recorded (2022-02-28 .. 2026-06-15)

- munkedal.se is SiteVision CMS (sitevision system-resource, /download/... URLs with
  node-id/timestamp/filename shape). No Ciceron for protocols; the diarium
  (munkedal-sok.ciceron.cloud, linked from /kommun-och-politik/arenden-och-handlingar/diarium-och-arkiv)
  only serves ärenden/search, not the meeting archive.
- KF protocols live on one page:
  https://munkedal.se/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/kommunfullmaktige-kallelser-och-protokoll
  Reachable from root -> Kommun och politik -> Politik och demokrati -> Möten och protokoll ->
  "Kommunfullmäktige kallelser och protokoll".
- The page uses expandable accordion sections per year: "Kallelser och protokoll 2026/2025/2024/2023/2022"
  plus "Äldre kallelser och protokoll" (folders for 2015-2021, out of range).
  slim_http does NOT show the accordion bodies (they are rendered in the DOM; the button/section
  markup is present but content is collapsed). Use Playwright: click each year button, then
  browser_snapshot to a file and read the link/url lines.
- Each year section has two lists: "Kallelser" (agenda/notice PDFs - skip) and "Protokoll"
  (the minutes to harvest). Document links look like
  https://munkedal.se/download/<18.nodeid>/<unixms>/<URL-encoded filename>.pdf
  The <unixms> is a publish timestamp, NOT a date key. Filenames carry the meeting date.
- One meeting = one protocol. Pick the full "Protokoll KF YYYY-MM-DD ..." document; skip the
  partial "omedelbar justering" docs ("Protokoll KF YYYY-MM-DD § N omedelbar justering signed.pdf"
  for 2025-04-28 §53/§54 and 2026-04-27 §78, 2026-02-23 §18), and skip all "Kallelse...",
  "Tilläggskallelse...", "Komplettering...", "Uppdaterad föredragningslista...", "Fråga i
  kommunfullmäktige...", "Motion...", "Nya medborgarförslag...", "Handlingar ... del N",
  "Kompletterande handlingar..." items.
- 2026-06-15 has TWO protocol docs: "Protokoll KF 2026-06-15 signed.pdf" (clean digital, 56 p,
  §§105-131) and "Rättad paragrafserie Protokoll KF 2026-06-15.pdf" (scanned 5.9 MB copy with
  handwritten paragraph corrections). Recorded the signed one only (one meeting = one minutes set).
- Counts per year (meeting dates): 2022: 10 (02-28..12-19), 2023: 8 (02-27..11-27),
  2024: 9 (02-26..12-16), 2025: 8 (02-24..11-24), 2026: 5 (02-23..06-15). Total 40, all within
  2022-01-01..2026-08-19. 2026 has no protocol after 06-15 (summer break); nothing further in range.
- download_document works fine on these /download/ URLs (GET 200, application/pdf). Verified by
  document_to_text on samples: 2022-02-28, 2023-02-27, 2024-04-29 (oddly named
  "Protokoll KF 2024-04-29.pdf digital signering.pdf" but it is the full 48p protocol),
  2025-04-28 (53p full), 2026-06-15 signed (56p full).
- Dates recorded = meeting date from filename. Titles as published (link text). Confidence 0.95.
