# hultsfred.se scanner notes (Hultsfreds kommun, kf = Kommunfullmäktige)

## Site structure / where KF minutes live
- WordPress site (theme hultsfredskommun), static HTML pages, no JS listing, no diarium.
- KF archive page (single long page, grouped by year then month, newest first):
  https://www.hultsfred.se/kategori/invanare/kommun-politik/sammantraden-protokoll/kommunfullmaktige/
  (also reachable via https://www.hultsfred.se/artikel/kommunfullmaktiges-protokoll/ — same content).
- Each meeting row = two PDFs: "Protokoll kommunfullmäktige YYYY-MM-DD" + "Kallelse till
  kommunfullmäktige YYYY-MM-DD". Links are plain <a href> to
  https://www.hultsfred.se/files/YYYY/MM/<filename>.pdf.
- slim_http: filter required_tokens ["pdf"] (or ["files"]) and read the link TEXT; kallelser
  are agenda/notice (skip); some years also carry extra "omedelbar justering" / "§ N" PDFs
  for the SAME meeting date (e.g. Protokoll_kf_20260525-omedelbar-justering_webb.pdf,
  Protokoll_kf_-20250414_-paragraf-54_webb.pdf, Protokoll_KF_20231211_Omedelbar-justering-151_webb.pdf,
  Protokoll_kf_20250616_omedelbar-justering.pdf) — skip them: they are per-§ supplements;
  the main "Protokoll kommunfullmäktige" PDF is the full minutes. One record per meeting date.

## URL / naming notes
- Filenames vary across years (KF2022-03-21webb.pdf, Kf2022-02-07webb.pdf, Protokoll_kf_250317.pdf,
  Protokoll_-kf_20241209_webb.pdf, KF2023-06-19webb.pdf, ...) but the link text is always the
  reliable title/date. Protocol PDFs are usually uploaded in the month AFTER the meeting
  (e.g. 2026-04-27 protocol under /files/2026/05/), so the URL's YYYY/MM dir is publish month,
  not meeting month.
- Site typo: the 2022-02-07 meeting's protocol link text reads "Protokoll kommunfullmäktige
  2022-02-27" but the PDF itself is "Sammanträdesdatum 2022-02-07" (Rock City, 7 feb 2022);
  the kallelse for that meeting says 2022-02-07. Recorded with date 2022-02-07, title kept as
  published, confidence 0.85.

## KF harvest result 2022-01-01..2026-08-20 (38 meetings, one protocol per date)
- 2022: 02-07, 03-21, 04-25, 05-23, 06-20, 09-26, 10-24, 11-28, 12-19 (9)
- 2023: 02-06, 03-20, 04-24, 05-22, 06-19, 09-25, 11-06, 12-11 (8)
- 2024: 02-05, 03-18, 04-22, 05-20, 06-17, 09-23, 11-04, 12-09 (8)
- 2025: 02-03, 03-17, 04-14, 05-19, 06-16, 09-29, 11-03, 12-08 (8)
- 2026: 02-09, 03-23, 04-27, 05-25, 06-22 (5; no Jan, no Jul/Aug within range)
- Pattern: no KF meetings in January, July or August (summer/winter break); months with
  meetings: Feb, Mar, Apr, May, Jun, Sep, Oct, Nov, Dec.
- Verified sample PDFs (2022-02-07, 2022-06-20, 2023-04-24, 2023-09-25, 2024-12-09,
  2025-03-17, 2026-06-22): all "Sammanträdesprotokoll / Kommunfullmäktige" with
  "Sammanträdesdatum" matching the recorded date. download_document: 200, application/pdf.

## Dead ends / tips
- Site search (?s=protokoll+kommunfullmäktige) only points back to the same article/listing.
- "Sammanträden & protokoll" root page lists all committees; KF subpage is the one to use.
- Digital anslagstavla (https://www.hultsfred.se/artikel/anslagstavla/) holds kungörelser,
  not the KF protocol archive itself.
