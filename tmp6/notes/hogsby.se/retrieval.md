# hogsby.se scanner notes (Högsby kommun, kf = Kommunfullmäktige)

## Tooling blocker (harvest 2026-08-20)
- slim_http and download_document FAIL on hogsby.se: TLS error "certificate signed by
  unknown authority" (Go client rejects the cert chain). Playwright browser works fine.
- PDF retrieval pattern that works: navigate to any hogsby.se page, run
  page.evaluate(fetch(<pdf url>)) to trigger the GET, then capture via
  browser_network_requests (filter by filename) + browser_network_request
  index=N part="response-body" filename="/tmp/.playwright-mcp/downloads/<name>.pdf".
  Then document_to_text on that absolute path works.

## Where KF (Kommunfullmäktige) minutes live
- WordPress site (theme/plugins: Breakdance, novitell-library [modded], Complianz).
- Entry: https://hogsby.se/kommun-och-politik/demokrati-och-politik/protokoll/
  ("Protokoll", under Demokrati och politik). Page has an archive widget
  (plugin "novitell-document-archive-block") listing committees by ?archive=<id>.
- Kommunfullmäktige = archive 28505; subfolders per year:
  KF-2026=28506, KF-2025=28507, KF-2024=28508, KF-2023=28509
  URL shape: /kommun-och-politik/demokrati-och-politik/protokoll/?archive=<id>&prev=28500_28505
- Each year folder = a table of PDF links in https://hogsby.se/wp-content/uploads/<file>.pdf.
- Titles are link texts "KF YYYY-MM-DD"; meeting date = date in title; filenames vary:
  2023-2025: KF_YYYY_MM_DD_<old-ez-contentid>.pdf (e.g. KF_2023_02_06_226120.pdf,
  KF_2025_12_08_263313.pdf); 2026: kf-2026-06-08.pdf, kf-2026-05-04.pdf,
  kf-2026-04-13.pdf, KF-2026-03-02-1.pdf, KF_2026_02_02_265398.pdf.
- One meeting = one protocol. Skip per-§ supplements (e.g. "KF 2024-11-25, § 159 omedelbar
  justering" -> KF_2024_11_25____159_omedelbar_250308.pdf) and all Kallelse/anslagsbevis docs.

## REST API (all public, handy)
- /wp-json/wp/v2/media?search=KF  (media; 2022 KF absent)
- /wp-json/wp/v2/dokument?search=KF (custom post type "Dokument"; slugs kf-2023-02-06 ... kf-2026-06-08, no 2022)
- /wp-json/wp/v2/hf_cat_dokument (term tree; shows archive ids incl. 28505/28506-28509; NO KF-2022 term)
- /wp-json/wp/v2/types, /wp-json/wp/v2/taxonomies

## KF harvest result 2022-01-01..2026-08-20 (33 protocols recorded, 2023-02-06..2026-06-08)
- 2026: 02-02, 03-02, 04-13, 05-04, 06-08 (5). Next meeting 2026-09-07 is "Inställt möte"
  (kf_2026_09_07_-installt-mote.pdf in media) - outside range anyway.
- 2025: 02-03, 03-03, 04-07, 05-05, 06-09, 09-08, 10-06, 11-24, 12-08 (9)
- 2024: 02-05, 03-04, 04-08, 05-06, 06-10, 09-09, 10-07, 11-25, 12-09, 12-18 (10; two in Dec)
- 2023: 02-06, 03-06, 04-03, 05-08, 06-12, 09-04, 10-02, 11-27, 12-04 (9)
- Pattern: no KF in Jan/Jul/Aug. Verified PDFs (document_to_text) for 2023-02-06,
  2023-12-04, 2024-11-25 (main), 2024-12-18, 2025-11-24, 2025-12-08, 2026-02-02,
  2026-03-02, 2026-04-13, 2026-05-04, 2026-06-08: all "HÖGSBY KOMMUN
  SAMMANTRÄDESPROTOKOLL / Kommunfullmäktige" with matching Sammanträdesdatum.

## 2022 KF protocols: NOT AVAILABLE (dead end, document in detail)
- Old site was eZ Publish/Novitell at www.hogsby.se with pages
  /Kommun-och-politik/Demokrati-och-politik/Protokoll/Kommunfullmaektige/KF-2022
  listing 9 protocols (2022-02-07, 03-07, 04-04, 05-02, 06-13, 09-05, 10-17,
  11-28, 12-16) as https://www.hogsby.se/content/download/<contentid>/<version>
  (e.g. 22338/212048, 23856/224149). These URLs now return WordPress 404 on the
  new site.
- Not migrated: no KF-2022 term in hf_cat_dokument, no 2022 KF in wp media library
  (search=KF_2022/KF-2022/KF 2022 all []), no 2022 in /wp-json/wp/v2/dokument.
  Guessed uploads names (KF_2022_02_07_22338.pdf, kf-2022-02-07.pdf,
  KF_2022_02_07.pdf) all 404.
- Wayback Machine: KF-2022 page captured (20230820185814, 20251011173949) but the
  content/download PDFs themselves were never archived (CDX for
  hogsby.se/content/download/22338*, /23856* etc = []). No archived copies.
- Conclusion: 2022 KF protocols are not retrievable from any live/public URL.
  Only 2023-2026 recorded (33). If 2022 is required later, contact the
  municipality (kommun@hogsby.se) or check the internal Politikerwebb (401 login).

## Notes
- Anslagstavla (officiell) page holds kungörelser, not protocols. Kallelser page
  (demokrati-och-politik/kallelser/) holds recent kallelser PDFs (agenda/notice, skip).
- Site search (?s=...) does not index the uploads PDFs.
- Politikerwebb (www.hogsby.se/Politikerwebb) is 401 (login) in Wayback - not public.
