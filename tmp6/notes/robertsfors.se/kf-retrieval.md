# robertsfors.se — KF (Kommunfullmäktige) retrieval log

Run 2026-08-20, range 2022-01-01..2026-08-20.
RESULT: 26 KF protocols recorded (7×2022, 6×2023, 5×2024, 5×2025, 3×2026). One per meeting date.

## Site structure (SiteVision CMS + Ciceron diarium)
- Entry page "Sammanträden och protokoll":
  https://robertsfors.se/kommunochpolitik/politikochsammantraden/sammantradenochprotokoll.1111.html
  States: protocols adjusted after 2024-01-01 live in the Ciceron diarium
  (https://robertsfors-sok.ciceron.cloud/#!/search/); protocols from 2023-12-31 and earlier
  live under "Äldre sammanträdesprotokoll" -> "Protokollarkiv kommunfullmäktige":
  https://robertsfors.se/kommunochpolitik/politikochsammantraden/aldresammantradesprotokoll/protokollarkivkommunfullmaktige.1929.html

## Source 1 — Old archive 2022-2023 (13 protocols)
- protokollarkivkommunfullmaktige.1929.html is an accordion ("Protokoll 2023", "Protokoll 2022", ...).
  Expand each year to reveal a table of /download/18.<node>/<ts>/<filename>.pdf links.
  Plain HTML in DOM; slim_http misses the expanded content -> use Playwright to click the year button.
- 2022 (7): 03-14, 05-02, 06-20, 09-05 (extra), 10-31, 12-12, 12-19
- 2023 (6): 03-06, 05-02, 06-19, 10-02, 11-20, 12-18
- § ranges run continuously (2022 §§1-178, 2023 §§1-150) -> archive is complete.
- Wayback CDX news posts (robertsfors.se/2022/03/14/kommunfullmaktige-14-mars/ etc.) confirm the
  same meeting dates; no missing early-2022 or early-2023 meetings.
- Note: 2022-12-12 "Kommunfullmäktigesammanträde §110-142" is genuine KF (verified header), despite
  a Wayback news page URL saying "valfullmaktige-12-december".
- download_document works fine on these robertsfors.se/download URLs.

## Source 2 — Ciceron diarium 2024-2026 (13 protocols)
- SPA at https://robertsfors-sok.ciceron.cloud/#!/search/ ; JSON-RPC endpoint:
  POST https://robertsfors-sok.ciceron.cloud/json (content-type application/json).
  Flow (session_id from ReadDiaries response is reused in later params):
  1. ReadDiaries -> session_id
  2. Search {"search_id":"ciceronsok_search","doctype":64,"text":"","param":"{\"hasFiles\":false,\"diary\":\"KS\",\"board\":\"Kommunfullmäktige\",\"from_date\":\"\",\"to_date\":\"\"}","session_id":...} -> hits:13
  3. ReadItems {"search_id":"ciceronsok_search","offset":0,"limit":100,"session_id":...} -> 13 meetings
     ("Kommunfullmäktige YYYY-MM-DD", ids 0..12, newest first)
  4. ReadObjectDetails {"search_id":"ciceronsok_search","id":"<n>","session_id":...} -> value.documents[]
     gives per meeting: "Kallelse..." (agenda, skip) and "Protokoll..." (record) with dok id + filename_b64.
- Document download: GET https://robertsfors-sok.ciceron.cloud/download/document?filename=<filename_b64>&id=<file-id>
  - Content-Disposition is inline; server sends a broken content-encoding: deflate header, so
    download_document FAILS (HEAD -> 404) and API request decompression fails.
  - Works via Playwright: navigate to https://robertsfors-sok.ciceron.cloud/#!/search/ (same origin),
    create <a href=url download=...>, click, page.waitForEvent('download'), download.saveAs(<abs path>).
- Meetings 2024-2026: 2024: 03-04, 04-22, 06-17, 11-04, 12-16 | 2025: 02-24, 04-14, 06-16, 10-27, 12-15
  | 2026: 02-23, 04-13, 06-15. Matches sammanträdestider table on the 1111 page (Feb/Apr/Jun 2026).
- TRAP: several meetings have TWO protocol documents; one is a 3-page omedelbar-justering excerpt,
  the other the full protocol. Picked the full one (verified by text size/pages):
  - 2025-12-15: full = id 2460 ("Protokoll KF 2025-12-15.signerad.pub.pdf"); id 2456 = "Protokoll § 137" excerpt.
  - 2025-10-27: full = id 2319 (34 pp §§80-105); id 2302 = §94 direktjustering excerpt (3 pp).
  - 2024-06-17: full = id 281 ("protokoll. Kommunfullmäktige 2024-06-17docx.signerad.pub.pdf", 32 pp);
    id 273 = "Direktjustering kf protokoll" excerpt (3 pp).
- All 26 recorded PDFs verified via document_to_text: header "SAMMANTRÄDESPROTOKOLL / Kommunfullmäktige /
  Sammanträdesdatum YYYY-MM-DD". Note 2026-06-15 protocol FILE name says 2026-06-16 (typo) but content
  is 2026-06-15 §§52-83.
- Skip: all "Kallelse ..." docs (agenda/notice), the omedelbar-justering/direktjustering excerpts
  (same meeting date as full protocol), "Protokoll § 137" excerpt.

## Dead ends / tips
- robertsfors.se/sok -> 404; use the Ciceron diarium + old archive pages only.
- Wayback CDX via slim_http returns empty body; use Playwright to read document.body.innerText
  (e.g. filter original:.*[Ff]ullmaktig.*).
- Anslagstavlan (robertsfors-anslagstavla.ciceron.cloud) is a notice board; no KF protocol PDFs needed there.
- No missing protocols found; nothing needed from Wayback for 2022+ (all live).
