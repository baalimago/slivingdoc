# sotenas.se retrieval log (kf = Kommunfullmäktige)

## kf — round 1 (2026-08-20): SUCCESS, 35 protocols recorded (2022-02-16 .. 2026-06-17)

- sotenas.se is SiteVision CMS. No Ciceron diarium for the meeting archive; the
  "Kallelser och protokoll" hub lives at
  https://sotenas.se/kommun--politik/politik-och-demokrati/kallelser-och-protokoll
  (root -> Kommun & politik -> Politik och demokrati -> Kallelser och protokoll),
  with one subpage per body. KF archive:
  https://sotenas.se/kommun--politik/politik-och-demokrati/kallelser-och-protokoll/kommunfullmaktige
- Page structure: the CURRENT year (2026) is rendered inline as two sections
  "Kallelser 2026" and "Protokoll 2026" (links visible to slim_http). Older
  years are behind folder links:
  "Äldre kallelser" (2025, 2024, 2023, 2022, 2021) and
  "Äldre protokoll" (2025..2016). Folder links are of the form
  https://sotenas.se/kommun--politik/politik-och-demokrati/kallelser-och-protokoll/kommunfullmaktige?folder=19.<nodeid>&sv.url=12.212215de15cc395f032be74d
  The ;jsessionid=... token in the rendered hrefs is NOT needed — the folder=
  param alone works with slim_http GET, and the folder content is appended to
  the same page (so the 2026 section links appear too; filter with
  required_tokens to narrow).
- Folder ids (protokoll): 2022 folder=19.639e6c3617de5194f12d0892,
  2023 folder=19.2e2c7de91850dd1709942ccb, 2024 folder=19.4238e10118c6050f88a384c8,
  2025 folder=19.4202a77c193b2d6578de188b. Kallelse folders (for cross-checking
  meeting dates): 2022 ../d0880, 2023 ..ca7, 2024 ..84b6, 2025 ..87d.
- Document links: https://sotenas.se/download/<18.nodeid>/<unixms>/<URL-encoded filename>.pdf
  <unixms> is a publish timestamp, not a date key. Filenames carry the meeting date.
  download_document works fine (GET 200 application/pdf); document_to_text extracts OK.
- One meeting = one protocol. Filenames "KF Protokoll YYYY-MM-DD [signed|extra|ver 2].pdf".
  SKIP all "Kallelse ..." / "KF Kallelse ..." / "KF kallelse ..." (agenda/notice),
  and SKIP partial "omedelbar justering" docs. Exception seen: 2026-06-17 has
  "KF Protokoll 2026-06-17 § 74-81 83-100 signed.pdf" (full minutes, §82 excluded)
  plus "KF Protokoll 2026-06-17 omg justering § 82 signed.pdf" (6p partial) —
  recorded the §74-81/83-100 one only.
- Counts per year (meeting dates): 2022: 8 (02-16..12-14), 2023: 7 (02-01..12-13),
  2024: 8 (01-16..12-11), 2025: 8 (02-26..12-10), 2026: 4 (02-25..06-17). Total 35,
  all in 2022-01-01..2026-08-20. Cross-checked kallelse folders per year — counts
  match (multiple kallelse versions per meeting, one protocol each).
- 2026 schedule (Sammanträdesdagar 2026 PDF, /download/18.56b6801719b0ef58883266b4/1766053822858/...):
  KF 25 feb, 17 mar, 22 apr, 17 jun, then 23 sep / 28 okt / 11 nov / 16 dec — nothing
  in Jul-Aug 2026, so range is fully covered by the 4 recorded 2026 protocols.
- All 35 PDFs verified via document_to_text: header line
  "Kommunfullmäktige | Sammanträdesprotokoll |YYYY-MM-DD §§ ..." matches filename date.
  Dates recorded = meeting date from filename; titles as published (link text);
  confidence 0.95.
