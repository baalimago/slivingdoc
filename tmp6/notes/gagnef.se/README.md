
# gagnef.se scanner notes (Gagnefs kommun)

## Where KF (kommunfullmäktige) minutes live
- Main page: https://www.gagnef.se/kommun-och-politik/politisk-organisation/kommunfullmaktige/
  WordPress page id 711. Holds protocol file lists for 2026, 2025, 2024, 2023
  (rendered by plugin ee-simple-file-list-pro, but the file links are embedded in
  the page HTML - fetchable directly).
- Older protocols page: https://www.gagnef.se/kommun-och-politik/politisk-organisation/kommunfullmaktige/kommunfullmaktige-aldre-protokoll/
  (page id 8792) holds 2022 and older, as plain HTML lists.
- Best retrieval: WP REST API page content: https://www.gagnef.se/wp-json/wp/v2/pages/711
  and .../pages/8792 return full HTML incl. all PDF hrefs (slim_http HTML scan gets
  truncated by the 478-link menu, so use the JSON API or Playwright instead).

## URL patterns
- Current (fillistor): https://www.gagnef.se/wp-content/uploads/fillistor/Protokoll/Kommunfullmaktige/YYYY/Protokoll-kommunfullmaktige-YYYY-MM-DD-§§-A-B.pdf
- Older (plain uploads): https://gagnef.se/wp-content/uploads/YYYY/MM/Protokoll-kommunfullmaktige-YYYY-MM-DD-§§-A-B.pdf
- Each year also has Innehallsforteckning (table of contents) - not minutes, skip.
- One PDF per meeting normally, but 2022-10-17 (constituent meeting after the
  election) is split into 4 PDFs (§§102-103, §§104-107, §108, §§109-132) on the same date.
- NOTE: the äldre protokoll page links 2022-11-14 as "...-133-198-1.pdf" but that
  URL 404s; the live file is "...-133-198.pdf" (no "-1"). Use the no-suffix URL.

## Meetings per year (in range 2022-01-01..2026-08-20)
- 2022: 03-07, 04-25, 06-13, 10-17 (4 parts), 11-14, 12-12
- 2023: 03-06, 04-24, 06-19, 10-02, 11-06, 12-11
- 2024: 03-11, 04-29, 06-17, 09-30, 11-04, 12-16
- 2025: 03-10, 05-05, 06-16, 09-29, 11-03, 12-15
- 2026: 03-09, 04-27, 06-15 (later 2026 meetings 10-19, 11-16, 12-14 out of range)
- 27 meeting dates total in range -> 27 records.

## Decision on same-date split (2022-10-17)
- Four PDFs are all parts of the single constituent meeting 2022-10-17 (§§102-132).
  Recorded ONE per the one-per-date rule: the first-listed/largest part
  "Protokoll kommunfullmäktige 2022-10-17 §§ 109-132"
  (https://gagnef.se/wp-content/uploads/2022/10/Protokoll-kommunfullmaktige-2022-10-17-§§-109-132.pdf).

## Document verification
- PDFs with extractable text (2023-04-24 and later most; plus 2022-10-17 parts)
  confirmed: header "Gagnefs kommun Sammanträdesprotokoll / Kommunfullmäktige <date>".
- Several 2022-2024 PDFs are scanned images (pdftotext empty, OCR required) but
  filenames/titles + the year TOCs confirm they are the meeting protocols.
- 2022 meeting list cross-checked against Innehållsförteckning kommunfullmäktige 2022
  (https://gagnef.se/wp-content/uploads/2022/03/innehallsforteckning-kommunfullmaktige-2022.pdf).

## Harvest result (kf minutes 2022-01-01..2026-08-20)
- 27 documents recorded (one per meeting date), 2022-03-07 .. 2026-06-15.
