# norsjo.se scanner notes (Norsjö kommun, kf = Kommunfullmäktige)

## Run 2026-08-20 — RESULT: 18 KF protocol PDFs recorded (2022-01-01..2026-08-20)

## Where KF minutes live (the ONLY source needed)
- WordPress site (uploads under /wp-content/uploads/YYYY/MM/...). Static HTML, no JS
  listing, no diarium/anslag protocol archive.
- Entry: https://norsjo.se/kommun-politik/politik/ (Politik, möten och protokoll)
  -> Kommunfullmäktige -> **Protokoll kommunfullmäktige**
  https://norsjo.se/kommun-politik/politik/kommunfullmaktige/protokoll/
- The page is one long list, newest first, one year-section per year ("Protokoll 2026",
  "Protokoll 2025", ... back to 2018). Each section = "Innehållsförteckning" PDF +
  one link per meeting with link text = the meeting date "YYYY-MM-DD".
- KF meets 4x/year (quarterly): ~end of Mar, ~end of Jun, ~end of Oct, ~mid Dec.
  Cross-checked with the 2026 sammanträdesdatum page
  (https://norsjo.se/kommun-politik/politik/sammantradesdatum/): 2026 KF dates
  30 mar, 22 jun, 26 okt, 14 dec (Oct/Dec outside harvest range).
- Each year's "Innehållsförteckning" PDF (linked on the same page) lists exactly the
  same meetings -> completeness check possible offline. 2022..2025 each have 4
  meetings; 2026 has 2 so far (03-30, 06-22).

## KF harvest result 2022-01-01..2026-08-20 (18 protocols, one per date)
- 2022: 03-28 (uploads/2022/04/2022-03-28.pdf), 06-20 (2022/06/2022-06-20.pdf),
  10-24 (2022/11/2022-10-24.pdf, konstituerande after the election), 12-12
  (2022/12/2022-12-12.pdf)
- 2023: 03-27 (2023/04/2023-03-27.pdf), 06-19 (2023/06/2023-06-19.pdf),
  10-23 (2023/11/2023-10-23.pdf), 12-11 (2023/12/231211.pdf)
- 2024: 03-25 (2024/04/2024-03-25.pdf), 06-24 (2024/06/2024-06-24.pdf),
  10-28 (2024/10/2024-10-28.pdf), 12-16 (2024/12/2024-12-16.pdf)
- 2025: 03-24 (2025/03/2025-03-24.pdf), 06-23 (2025/06/250623.pdf),
  10-27 (2025/11/251027.pdf), 12-15 (2025/12/251215.pdf)
- 2026: 03-30 (2026/04/protokoll-260330.pdf), 06-22 (2026/06/protokoll-260622.pdf)
- All 18 verified: download 200 application/pdf, text extraction shows "NORSJÖ KOMMUN
  PROTOKOLL / Kommunfullmäktige <date>" on page 1. Recorded with confidence 1.0.
- Filenames are inconsistent (2022-03-28.pdf, 231211.pdf, 250623.pdf,
  protokoll-260622.pdf) - always trust the link text date + PDF first page.

## Notes / dead ends
- Kallelser page (https://norsjo.se/kommun-politik/politik/kommunfullmaktige/kallelser/)
  only holds the current kallelse (agenda/notice -> excluded).
- Digital anslagstavla (https://norsjo.se/kommun-politik/digital-anslagstavla, canon
  /kommun-politik/beslut-insyn-och-rattssakerhet/digital-anslagstavla/) holds
  kungörelser/anslag, not KF protocols.
- Site search (?s=protokoll+kommunfullmaktige) surfaces the protokoll page + aktuellt
  news posts about meetings (not minutes).
- WP REST API /wp-json/wp/v2/media?search=protokoll lists KF attachments incl. the
  2026 ones but is not a complete archive index for older years; the page listing is
  authoritative.
- Media attachment entries confirm titles like "Protokoll 260622"; page link text is
  the plain date.

## Run 2026-08-20 (re-run) — verified & re-recorded 18 KF protocols
- Re-fetched https://norsjo.se/kommun-politik/politik/kommunfullmaktige/protokoll/:
  listing unchanged, same 18 KF meeting links for 2022-2026 (2026 shows only 03-30 and
  06-22; 2026-10-26/2026-12-14 outside range). Innehållsförteckning PDFs are TOCs, not
  minutes -> not recorded.
- Downloaded all 18 PDFs (200, application/pdf), extracted text: page 1 of each reads
  "NORSJÖ KOMMUN PROTOKOLL / Kommunfullmäktige <date>". All 18 recorded via
  record_documents with confidence 1.0, source page
  https://norsjo.se/kommun-politik/politik/kommunfullmaktige/protokoll/.
- Same 18 dates/URLs as the previous run — no changes on the site.
