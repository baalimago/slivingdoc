# almhult.se KF retrieval log

Target: Älmhult kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.
RESULT: 44 KF protocols recorded (2022 x11, 2023 x11, 2024 x9, 2025 x9, 2026 x4). All live almhult.se URLs, no Wayback needed.

## Site structure (SiteVision CMS)
- Entry path: https://almhult.se/kommunpolitik.928.html (Kommun & politik) ->
  "Möten, handlingar & protokoll" (kommunpolitik/motenhandlingarprotokoll.1024.html) ->
  "Protokoll" (https://almhult.se/kommunpolitik/motenhandlingarprotokoll/protokoll.UN.html).
- Protokoll page is one long page with one section per organ (Kommunfullmäktige, KF:s valberedning,
  Kommunrevisionen, Kommunstyrelsen, nämnder...). Each section: "Protokoll för <current year>" list
  (static <a href> to /download/<node>/<unixms>/<name>.pdf) plus an accordion button
  "Protokoll från tidigare år" -> per-year accordions (2025, 2024, 2023, 2022) with the same style links.
- slim_http does NOT show the file links (they are inside a JS file-list webapp accordion); use the
  Playwright browser, click "Protokoll från tidigare år" then each year button, read the <a> links.
- PDF URL pattern: https://almhult.se/download/18.<hexnode>/<unix-ms>/<URL-encoded name>.pdf
  e.g. .../Kommunfullm%C3%A4ktiges%20sammantr%C3%A4desprotokoll%20YYYY-MM-DD.pdf.
  Direct download_document works (200, application/pdf) for ALL years 2022-2026 - nothing is removed.
- Filename convention: "Kommunfullmäktiges sammanträdesprotokoll YYYY-MM-DD.pdf"; a few older files say
  "Kommunfullmäktige sammanträdesprotokoll" (2024-12-16, 2025-09-29). One 2022 file is a per-§ supplement
  "Kommunfullmäktiges sammanträdesprotokoll 2022-12-12, § 182-183.pdf" (same meeting date as the main
  2022-12-12 protocol) - do NOT record both (same-day conflict).

## KF meeting dates (protocols found, all text-verified: header "Sammanträdesprotokoll <date> / Kommunfullmäktige")
- 2022 (11): 01-31, 02-28, 03-28, 04-25, 05-30, 06-20, 08-29, 09-26, 10-31, 11-28, 12-12.
- 2023 (11): 01-30, 02-27, 03-27, 04-24, 05-29, 06-19, 08-28, 09-25, 10-30, 11-27, 12-18.
- 2024 (9): 01-29, 03-25, 04-29, 05-27, 06-17, 09-30, 10-28, 11-25, 12-16. (No Feb protocol; no 07/08)
- 2025 (9): 01-27, 03-24, 04-28, 05-26, 06-23, 09-29, 10-27, 11-24, 12-15.
- 2026 (4, in range): 01-26, 03-30, 04-27, 05-25.
- 2026 sammanträdesplan (sammantradesplan.982.html): 23 feb INSTÄLLD, 29 jun INSTÄLLD (news article
  "Kommunfullmäktige 29 juni ställs in – nästa möte är 31 augusti"), 31 aug 2026 out of range.
  So 4 published 2026 protocols is complete for the range.

## Dead ends / notes
- "Handlingar" page (motenhandlingarprotokoll/handlingar.3376.html) = kallelser/agendas (excluded).
- "Anslagstavla" (anslagstavla.930.html) = tillkännagivanden (excluded).
- Kommunfullmäktige info page (kommunpolitik/kommunensorganisation/kommunfullmaktige.629.html) has no
  extra protocol files; just links back to the Protokoll page. Livestream on almhult.socialcast.se (not docs).
- All recorded URLs are live (no Wayback needed for 2022-2026). Confidence 0.95-0.97 after text verification.
