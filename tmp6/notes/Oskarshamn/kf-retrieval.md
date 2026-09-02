# Oskarshamn (oskarshamn.se) - KF retrieval notes

Target: Oskarshamn kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure (EPiServer CMS)
- IMPORTANT: https://oskarshamn.se/... (no www) 302-redirects EVERYTHING to the homepage
  https://www.oskarshamn.se/. Always use www host for browsing. slim_http/Playwright on
  non-www shows only the homepage.
- KF protocol page: https://www.oskarshamn.se/mer-om-kommunen/politik-och-forvaltning/politik/kommunfullmaktige/protokoll-kommunfullmaktige/
  (reachable via "Mer om kommunen" -> "Politik och förvaltning" -> "Styrande dokument" -> "Protokoll" -> "Protokoll kommunfullmäktige").
- The page is a server-rendered accordion ("Protokoll KF" -> year folders). Only the CURRENT year
  (2026) folder is present; older years are removed. Text on page: "På oskarshamn.se publicerar vi
  protokoll från det innevarande året. För äldre protokoll kontakta kommunstyrelsens kansli."
  => 2022-2025 protocols are NOT on the live site anymore.
- 2026 live PDFs live under /globalassets/mer-om-kommunen/dokument/protokoll-kf/2026/
  named ks-2022_001162-<nr>-protokoll--kommunfullmaktige-YYYY-MM-DD-<id>_<v>-1.pdf
  (diarium series KS 2022:1162). All 2026 folder PDFs still downloadable as real PDFs.
- Related pages (skip for kf minutes): Kallelser till kommunfullmäktige (agendas/notice),
  Anslagstavla (kungörelser/anslagsbevis), Sammanträden (meeting dates only).

## KF meetings 2026 (from sammantraden page, box #22126)
9 feb (INSTÄLLT), 9 mar, 13 apr, 11 maj, 8 jun, 21 sep, 19 okt, 16 nov, 7 dec.

## What was recorded (18 KF protocols)
- 2026 (4, live oskarshamn.se URLs): 2026-03-09, 2026-04-13, 2026-05-11, 2026-06-08.
- 2025 (2, Wayback): 2025-02-10, 2025-04-14.
- 2024 (7, Wayback): 2024-03-11, 04-08, 05-06, 06-03, 09-09, 10-21, 11-25.
- 2022 (5, Wayback): 2022-02-14, 03-14, 04-11, 05-09, 06-13.

## Retrieval of old protocols via Wayback Machine (web.archive.org)
- Old PDFs (2022-2025) were on oskarshamn.se/globalassets/mer-om-kommunen/dokument/protokoll-kf/...
  with year folders; 2022 used protokoll-kf-YYYY-MM-DD.pdf naming, 2024/2025 mixed old naming
  (protokoll-kf-2024-03-11.pdf, protokoll--kf-2024-05-06.pdf) and diarium naming
  (ks-2022_001162-56-protokoll--kommunfullmaktige-2024-09-09-572441_6_1.pdf).
- CDX enumeration that works:
  https://web.archive.org/cdx/search/cdx?url=oskarshamn.se&matchType=domain&filter=original:.*fullmaktige.*&fl=timestamp,original&collapse=urlkey&limit=2000
  (Wayback rate-limits: got 503 "Temporarily Offline" when hammering; space requests out.)
- To GET the actual PDF from Wayback, must use the if_ modifier:
  https://web.archive.org/web/<timestamp>if_/<original-url>
  The plain /web/<ts>/ URL returns the HTML wrapper (toolbar) or 404 for the content.
- Verified PDF text for all recorded docs: header "PROTOKOLL Kommunfullmäktige / Sammanträdesdatum YYYY-MM-DD".
- 2022-03-14 PDF is a 48-page scanned image PDF; pdftotext fails (no text layer). File itself is a
  genuine PDF (file(1) says PDF v1.5, 48 pages); recorded with confidence 0.75.

## Missing / not retrievable (documented, NOT recorded)
- 2022-09-19 and 2022-10-17 protocols: listed on archived protokoll page (e.g. snapshot
  20221129011521) but the PDFs were never captured by Wayback (no CDX entry). No live copy.
- 2022-11-28 and 2022-12-12: 12-12 PDF (protokoll-kf/protokoll-kf-2022-12-12.pdf) has a single
  Wayback capture (20240704193932) which is a 404 text/html capture - PDF content never archived.
  Nov 2022 protocol not seen on archived page.
- 2023 (all): 2023-02-13 listed on 2023-03-29 snapshot; 2023-11-27, 2023-12-11 on 2024-10-05
  snapshot (protokoll-kf-2023-11-272.pdf, protokoll-kf-2023-12-11.pdf at protokoll-kf root).
  None archived (CDX empty; direct if_ fetch 404). Not retrievable from any public URL.
- 2025-05-12, 2025-06-09, 2025-09-15, 2025-10-20, 2025-11-17, 2025-11-24: listed on 2025-12-10
  snapshot of the page (ks-2022_001162-<nr>-...-2025-... naming) but never archived; live URLs now
  serve soft-404 HTML (200, text/html, 37356 bytes).
- 2026-01/02: no meetings (9 Feb inställt). 2026-09-21+ out of range.

## Notes / tips
- Old live URLs (e.g. /2024/protokoll-kf-2024-06-03.pdf) now return soft-404 HTML with 200 -
  download_document reports content-type text/html, size 37356. That's the site's 404 page.
- The Wayback snapshots of the protokoll page across time (list from CDX for
  oskarshamn.se/mer-om-kommunen/politik-och-forvaltning/politik/kommunfullmaktige/protokoll-kommunfullmaktige/):
  20220302, 20220510, 20220603, 20220627, 20220628, 20221126, 20221129, 20230321, 20230329,
  20241005, 20250125, 20250513, 20250618, 20250911, 20251210, 20260122, 20260513.
- Kallelser (mötesböcker) are under /globalassets/mer-om-kommunen/dokument/kallelser-kf/... -
  agenda/notice, skip for kf minutes.
- Archive Team crawls (2022-08-12, 2025-06-25) are the main sources of the PDF captures.

## Run log 2026-08-20 (agent rerun)
- Re-verified the live protocol page: only 2026 folder, 4 PDFs (03-09, 04-13, 05-11, 06-08),
  all downloaded as real PDFs (200, application/pdf), text header verified.
- Re-enumerated Wayback CDX (domain-wide, filters .*protokoll-kf.* and .*fullmaktige.* and
  .*protokoll.*) - capture set unchanged from the above: 5 x 2022, 7 x 2024, 2 x 2025, 1 x 2026(05-11).
  NOTE: slim_http returns empty for CDX URLs; use Playwright navigate + body.innerText.
- Downloaded all 14 archived PDFs via <ts>if_ URLs; all real PDFs, text-verified except 2022-03-14
  (scanned, no text layer; genuine PDF v1.5, 48 pages).
- Confirmed non-retrievable: 2022-09-19, 2022-10-17, 2023-*, 2025-05-12..11-24 (exact-URL CDX
  queries empty), 2022-12-12 (only 404 capture). 2025 diarium-named live URLs now hard-404.
- Recorded 18 KF protocols (same set as previous run). No agenda/notice docs recorded.
