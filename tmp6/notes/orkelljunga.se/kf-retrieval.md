# orkelljunga.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — SUCCESS: 46 KF protocol documents recorded (2022-01-31 .. 2026-06-22)

## Entry / site structure
- Orkelljunga.se is SiteVision CMS. KF minutes live on one static page:
  **https://www.orkelljunga.se/16/kommun-och-politik/demokrati-och-medborgardialog/protokoll/kommunfullmaktige.html**
  ("Kommunfullmäktige" under Protokoll, reached from Kommun och politik -> Demokrati och
  medborgardialog -> Protokoll). The page embeds the full archive as JSON in the HTML and renders
  an expandable folder tree (SiteVision filearchive webapp).
- Top-level listing shows the current year (2026) files directly; older years are folders
  (2025..2013). There is also a separate "Kommunfullmäktiges valberedning" section — SKIP it.

## Archive webapp AJAX endpoint (JSON, works directly with slim_http)
- `GET https://www.orkelljunga.se/appresource/4.61942eb414c12a7acc980c/12.5e484f4b19ed4844be356b1c/files?folderId=<folderId>&svAjaxReqParam=ajax`
  returns {"files":[{name, uri, url, id, ...}]} for a folder.
- KF root folder (holds 2026 files + year subfolders): **19.61942eb414c12a7acc9888**
- Year folder ids (parentFolder = root): 2025=19.66d3b8da19afd0e09aa87a6,
  2024=19.48ffd21193dec8a9e15938, 2023=19.2b20300818cc7ce870421c46,
  2022=19.285ec1781856d4ee98e4c824. (ids parseable from the page HTML or the root folder JSON.)

## Download URL pattern (WORKS directly with download_document, GET 200 application/pdf)
- `https://www.orkelljunga.se/download/<node-id>/<timestamp>/<URL-encoded name>.pdf`
  e.g. https://www.orkelljunga.se/download/18.5e484f4b19ed4844be31150c/1782464103891/Kommunfullm%C3%A4ktige%202026-06-22.pdf
- All protocol files are "Kommunfullmäktige YYYY-MM-DD.pdf"; every one verified page 1 =
  "Sammanträdesprotokoll <date> / Kommunfullmäktige".

## Recorded (46, one per meeting date)
- 2022 (11): 01-31, 02-28, 03-28, 04-25, 05-30, 06-27, 08-29, 09-26, 10-31, 11-28, 12-12.
- 2023 (11): 01-30, 02-27, 03-27, 04-24, 05-29, 06-26, 08-28, 09-25, 10-30, 11-27, 12-18.
- 2024 (9): 01-29, 02-26, 03-25, 04-29, 05-27, 06-24, 09-30, 10-28, 11-25.
- 2025 (10): 01-27, 03-24, 04-28, 05-26, 06-23, 08-25, 09-29, 10-27, 11-24, 12-22.
- 2026 (5): 01-26, 03-23, 04-27, 05-25, 06-22.
- No KF meeting between 2026-06-22 and range end 2026-08-20 (KF has no July/August session).

## Duplicate/partial files in the same folder — skip, record the main protocol
- 2022 folder: "Kommunfullmäktige 2022-02-28 direktjusterat.pdf" (partial, §8 only) vs main
  "Kommunfullmäktige 2022-02-28.pdf" (§§9-16). "Kommunfullmäktige 2022-06-27 - omedelbar
  justering §§ 53-54.pdf" (partial) vs main "Kommunfullmäktige 2022-06-27.pdf".
- Rule: prefer the plain "Kommunfullmäktige <date>.pdf" (full protocol); skip anything with
  "direktjusterat"/"omedelbar justering"/"Justering" suffix.

## Tips for next run
- Query the appresource endpoint for root + year folders (no pagination); take every
  "Kommunfullmäktige <date>.pdf" file in range. Direct download_document works (no HEAD trap).
- source_page used: the Kommunfullmäktige protokoll page URL.
