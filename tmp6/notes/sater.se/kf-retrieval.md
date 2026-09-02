# Säter (sater.se) - KF retrieval notes

Target: Säters kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Structure
- Site: WordPress (municipio theme), host sater.se.
- Current KF listing page (2024->):
  https://sater.se/kommun-demokrati/politiska-moten/kallelser-och-protokoll/kommunfullmaktiges-kallelser-och-protokoll/handlingar-och-protokoll/
  (reachable from https://sater.se/kommun-demokrati/politiska-moten/kallelser-och-protokoll/ -> Kommunfullmäktige -> Kommunfullmäktiges protokoll)
- The page is plain server-side HTML; year sections 2026/2025/2024, each row a
  document link. slim_http with required_tokens ["wp-content/uploads"] lists all
  PDF links directly. No XHR/JS feed for this page (content in DOM).
- Kallelser (agendas, exclude) live at:
  .../kommunfullmaktiges-kallelse-och-dagordning/ (same year-section layout).
- KF protokoll page currently only covers 2024-2026 (6+6+3 docs). The OLD KF page
  (pre-2025) at https://sater.se/kommun-demokrati/handlingar-och-protokoll-2-2/handlingar-och-protokoll/
  used to list 2016-2023 protocols but is now 404 (removed mid/late 2025).

## Finding 2022-2023 KF protocols (key trick)
- The PDFs still exist on sater.se at wp-content/uploads/... even though the page
  is gone and they are NOT in the WordPress media library (wp-json/wp/v2/media).
- Use the Wayback Machine CDX to recover the old listing page, e.g.
  https://web.archive.org/web/20240301080048/https://www.sater.se/kommun-demokrati/handlingar-och-protokoll-2-2/handlingar-och-protokoll/
  (captures: 20231005024859 shows 2022+early-2023; 20240301080048 shows all of 2023).
  The snapshot lists each year's KF minutes with direct /wp-content/uploads/ links
  which still resolve (200, application/pdf) on the live host.
- CDX API tip: plain-text CDX responses come back empty through slim_http; use the
  Playwright browser to open the CDX URL and read document.body.innerText instead.
- Media library (wp-json/wp/v2/media?search=kf&per_page=100&page=N) contains the
  kf-prefixed attachments that ARE attached; only kf231026/kf231130 exist there for
  2023 (re-uploaded at /2024/01/), none for 2022. So media search alone is NOT
  sufficient for the full range; the wayback old-page listing is authoritative.

## KF minutes recorded (28) - meeting dates
2022: 02-24, 04-28, 06-16, 09-29, 10-17, 12-01, 12-15
2023: 02-23, 04-27, 06-15, 09-28, 10-26, 11-30
2024: 02-22, 04-25, 06-13, 09-26, 10-24, 11-28
2025: 02-20, 04-10 (docx!), 06-12, 09-25, 10-23, 11-27
2026: 02-19, 04-23, 06-11

## Document URL patterns
- 2022-2023: https://sater.se/wp-content/uploads/<2022|2023>/03/kfYYMMDD.pdf
  exceptions: kf221017nya.pdf; kf231026.pdf & kf231130.pdf only live at
  /wp-content/uploads/2024/01/ (old /2023/03/ copies are 404).
- 2024+: https://sater.se/wp-content/uploads/<yyyy>/<mm>/kf*.pdf
  (2025-09-25 = kf-protokoll-250925.pdf; 2025-04-10 = kf250410.docx; 2024-11-28 =
  kf241128-webb.pdf). Meeting date = YYYY-MM-DD inside the PDF header
  ("Protokoll / Kommunfullmäktige / <date>"), verified via document_to_text.

## Excluded / traps
- Kallelser (agendas) on the kallelse page and in media library (names like
  kallelse-*, hela-kallelsen-*, 00_kallelse-*, Kungörelse) - agenda/notice, skip.
- Same-name trap: 2025-09-25 "kf250925.pdf" in the media library is the KALLELSE
  (16 MB); the minutes are "kf-protokoll-250925.pdf" (1.2 MB). Always verify content.
- Media library "kf" search also returns non-KF docs (delårsrapporter, evenemangsstrategi,
  protokoll of other bodies). Filter by slug/prefix kfYYMMDD + header check.
- Other bodies (KS, nämnder) have their own pages under the same kallelser-och-protokoll
  index; same year-section pattern. Old-body protocols (2021-2023) survive in the
  media library under /2024/03/ or /2024/01/ re-uploads.

## Tips for next runs
- Live KF page via slim_http suffices for 2024+; for older years use the wayback
  old-page listing (URL above) and test the wp-content/uploads URLs on the live host.
- Record one minutes per meeting date; no same-day duplicates seen in 2022-2026.
