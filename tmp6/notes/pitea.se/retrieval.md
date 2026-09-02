# pitea.se retrieval log (kf = Kommunfullmäktige)

## Site structure
- pitea.se is an Episerver site. Entry to KF: Invånare > Kommun & politik > Politik >
  Kommunfullmäktige (https://www.pitea.se/invanare/Kommun-politik/politik/Kommunfullmaktige/).
- That page links to the protocol archive:
  https://www.pitea.se/invanare/Kommun-politik/politik/Kommunfullmaktige/Protokoll/
- The Protokoll page is a flat directory listing ("Bokhyllan") of the file share:
  https://www.pitea.se/Bokhyllan/Protokoll%20-%20n%C3%A4mnder/Kommunfullm%C3%A4ktige/Protokoll/{YYYY}/{Kommunfullm%C3%A4ktiges%20sammantr%C3%A4de%20YYYY-MM-DD}/{file.pdf}
  (URL-encoded: %20 = space, %C3%A4 = ä, %C2%A7 = §). No JS needed; slim_http lists all links.
- Each meeting folder holds the protocol PDF (sometimes plus "Protokollsbilagor.pdf"/"Bilagor ..." =
  attachments only — skip those). Download via download_document works fine (200, application/pdf).

## KF harvest result 2022-01-01..2026-08-20 (39 protocols, one per meeting date)
- 2022 (8): 02-21, 03-28, 05-30, 06-20, 09-26, 10-31, 11-28, 12-12
- 2023 (8): 02-13, 03-20, 05-29, 06-26, 09-11, 10-30, 11-27, 12-14
- 2024 (9): 02-12, 03-18, 03-25, 05-27, 06-24, 09-16, 10-28, 11-25, 12-16
- 2025 (9): 01-30, 02-10, 03-17, 05-27, 06-18, 09-15, 10-27, 11-24, 12-15
- 2026 (5, all within range): 02-05, 02-23, 04-27, 05-25, 06-15
- Confidence 0.97 (sampled 2022-02-21, 2022-12-12, 2023-02-13, 2023-10-30, 2024-02-12,
  2025-01-30, 2026-06-15 — all "Sammanträdesprotokoll Kommunfullmäktige").
- Filenames are inconsistent: 2022-2023 use "Kommunfullmäktiges protokoll [och bilagor] [date].pdf"
  per-folder; 2024-2026 are single files "Kommunfullmäktiges sammanträde YYYY-MM-DD.pdf" directly
  under the year folder. No stable date key in URL besides the folder/filename.

## Special cases
- Two meetings have minutes split into two PDFs by § range (only the main/larger part recorded,
  per one-document-per-date rule):
  - 2023-02-13: "Kommunfullmäktiges protokoll och bilagor §§ 1-7, 9-79, 82-86.pdf" (main, 90 pp,
    §§2-7,9-79,82-86) + "… §§ 8, 80-81.pdf" (complement).
  - 2023-10-30: "Kommunfullmäktiges protokoll och bilagor §§ 239-264, 267-284.pdf" (main, 86 pp)
    + "… §§ 265-266.pdf" (complement).
- 2022-12-12 "Kommunfullmäktiges protokoll och bilagor.pdf" is protocol+attachments combined — fine.
- No KF meetings in Jan 2022, Jan/Jul 2026 etc. The listing is the authoritative archive; no
  Ciceron/diarium needed. Upcoming 2026 meetings (09-15, 10-26, 11-23, 12-14) are out of range.

## Dead ends
- No need for Playwright/API: plain HTML directory listing covers everything.
