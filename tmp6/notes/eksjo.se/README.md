# eksjo.se scanner notes (Eksjö kommun), kf = Kommunfullmäktige

## Where KF (Kommunfullmäktige) minutes live
- SiteVision CMS. Single KF protocol page:
  https://eksjo.se/kommun-och-politik/politik-och-paverkan/kommunfullmaktige-namnder-utskott-styrelser-och-revision/kommunfullmaktige
- Accordions: Protokoll (year sections), Tillkännagivande/ärendelista (agenda - SKIP),
  Webbsändning, Mötestider, Ledamöter, Medborgarförslag, Parlamentarisk beredning (different
  body - NOT KF minutes, skip), Valberedning.

## Link embedding (important)
- 2023/2024/2025 protocols: static sv-file-portlet <a href> to
  https://eksjo.se/download/<18.node>/<unixms>/Kommunfullm%C3%A4ktiges%20protokoll%20YYYY-MM-DD.pdf
  -> visible to slim_http.
- 2026 protocols: SiteVision marketplace webapp "file-share" (portlet 12.7e11c26f19e3df8f0b93134c,
  folderId 19.61dbeb4317ddd5cd047e71a). Links are NOT in <a> tags; they are in the
  AppRegistry.registerInitialState(...) JSON blob in the raw HTML ("files":[{id,name,uri,url,...}]).
  slim_http misses them -> download page HTML with download_document and read the JSON.
- Playwright browser is BLOCKED by eksjo.se (403 "Request forbidden by administrative rules").
  Use slim_http + download_document only.

## 2022 protocols: removed from live site, only in Wayback Machine
- As of 2026-08-20 all 2022 KF protocol /download/ URLs 404 on eksjo.se. They existed until at
  least 2024-03/05 (Wayback captures list the same node URLs and the PDFs returned 200 then).
- Recovery: Wayback CDX (url=eksjo.se/download*, filter urlkey:.*kommunfullm.*) -> capture
  timestamps; fetch raw PDF via https://web.archive.org/web/<ts>id_/<original eksjo URL>.
  Some captures are truncated at ~1 MiB (2022-03-24 and 2022-06-14 @20240303 timestamps failed
  pdftotext; use 20220616/20220619 captures for those). Verified all 10 via pdftotext.

## KF harvest 2022-01-01..2026-08-20 (42 meetings, one protocol per date)
- 2022 (10, via Wayback): 02-24, 03-24, 04-21, 05-19, 06-14, 09-29, 10-17 (extra), 10-27,
  11-24, 12-15. (2022-01-20 cancelled - no protocol.)
- 2023 (9): 02-23, 03-23, 04-20, 05-16, 06-15, 09-21, 11-02, 11-30, 12-19
- 2024 (8): 02-22, 03-21, 04-25, 05-23, 06-18, 09-19, 10-31, 11-28
- 2025 (9): 01-09, 02-20, 03-20, 04-24, 05-22, 06-17, 09-18, 11-06, 12-04
- 2026 (6): 01-08, 02-19, 03-19, 04-23, 05-21, 06-16 (2026-08-20 meeting today: only
  tillkännagivande published, no protocol yet; 17 sep+ outside range)

## Gotchas
- "Kommunfullmäktiges protokoll 2024-03-19.pdf" is the 2024-03-21 meeting (sammanträdesdatum
  inside = 2024-03-21; tillkännagivande 2024-03-21; webb-tv news 21 mars). Filename typo by
  municipality. Record 2024-03-21, title as published, conf 0.9.
- 2025-12-04 has main protocol + per-§ supplement "2025-12-04 paragraf 228.pdf" (skip the
  supplement; one meeting = one protocol).
- 2022-05-19 also had per-§ file "2022-05-19 Kf § 74.pdf" (skip).
- 2026 webapp names vary ("Kommunfullmäktige protokoll 2026-06-16.pdf" vs
  "Kommunfullmäktiges Protokoll 2026-03-19 .pdf") - use webapp "name" as title.

## Dead ends
- Site search (https://eksjo.se/ovrigt/soktraffsida?query=...) no longer returns 2022 KF
  protocols (they are gone from the site); finds 2023+ PDFs.
- Anslagstavla / netpublicator links are for ledamöter, not protocols.
