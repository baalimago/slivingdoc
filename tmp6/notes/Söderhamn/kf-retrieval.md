# Söderhamn (soderhamn.se) — KF retrieval log

Target: Söderhamn kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.
RESULT: 44 KF protocols recorded (12 for 2022, 32 for 2023-2026).

## Site structure (SiteVision CMS at soderhamn.se, redirects to www.soderhamn.se)
- "Möten och protokoll" entry: https://www.soderhamn.se/sidor/kommun-och-politik/politik-och-demokrati/moten-och-protokoll.html
  - "Protokoll och ärendelistor 2019-2022" -> https://www.soderhamn.se/sidor/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/arendelistor-och-protokoll-2019-2022.html
    This page lists organs (Arbetsmarknads- och socialnämnden, Barn- och utbildningsnämnden, ..., Kommunfullmäktige, ...) as
    SiteVision *folder* links: ?folder=<id>&sv.url=12.4b05d41f1879ab5644f3457 (jsessionid in the href but NOT needed).
    KF folder id: 19.3f9c3e24168a00f78083956f -> subfolders "Protokoll KF" (19.3f9c3e24168a00f78083957f) and "Ärendelistor KF" (19.3f9c3e24168a00f78083957d).
    The folder listing is server-rendered (one page, no pagination): direct PDF links like
    https://www.soderhamn.se/download/18.<node>/<ts>/KF-protokoll-YYMMDD-%C2%A7%C2%A7-<range>.pdf
  - "Protokoll och ärendelistor från 2023 och framåt" -> https://www.soderhamn.se/sidor/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/arendelistor-och-protokoll-fran-och-med-2023.html
    links ONLY to the external diarium SPA https://soderhamn-sok.ciceron.cloud/ (Ciceron / twoday "Söderhamns kommuns diarium").

## Ciceron diarium (soderhamn-sok.ciceron.cloud) — the 2023+ source (and it also mirrors 2019-2022 meetings)
- SPA at /#!/search/?t=1&i=Kommunfullm%C3%A4ktige&n=KS&expand=0&today=0 (pre-set "Kommunfullmäktige" / diary KS hotspot, 81 meeting objects 2019-02..2026-06).
- JSON-RPC endpoint: POST https://soderhamn-sok.ciceron.cloud/json  (content-type application/json).
  Flow: ReadHotspots -> get session_id (response includes it) -> ReadObject {"search_id":"ciceronsok_search","param":"{\"t\":\"1\",\"i\":\"Kommunfullmäktige\",\"n\":\"KS\",\"today\":\"0\"}"}
  -> ReadItems {"search_id":"ciceronsok_search","offset":0,"limit":100} (returns all meetings, title "Kommunfullmäktige YYYY-MM-DD", 0-based id)
  -> ReadObjectDetails {"search_id":"ciceronsok_search","id":"<n>"} returns value JSON with "documents":[{name, id, filename_b64,...}] and agenda "items".
- Document download: GET https://soderhamn-sok.ciceron.cloud/download/document?filename=<filename_b64>&id=<file-id>
  IMPORTANT: endpoint returns 404 to HEAD and to download_document's existence check (HEAD always 404); plain GET works (200, application/pdf)
  even without session_id/cookie. So use the Playwright browser download+saveAs (anchor click with a.download attr -> waitForEvent('download') -> saveAs(...))
  to get the PDFs; download_document CANNOT fetch this host.
  filename_b64 = base64(Latin-1) of "Kommunfullmäktiges protokoll <YYYY-MM-DD>.signerad.pub.pdf" (encode via page.evaluate btoa; the "ä" is one Latin-1 byte 0xE4).
- Each KF meeting object has documents: "Kallelse Kommunfullmäktige..." (agenda - skip) and "Kommunfullmäktiges protokoll <date>[ §§ a-b]" (record).
- Cancelled meetings appear as objects with a single doc "Kommunfullmäktiges sammanträde den ... ställs in" and no items -> no protocol (2026-02-23, 2025-09-29, 2025-01-27).
- 2022-09-26 meeting object exists but has NO documents/items -> no protocol published anywhere (also absent from main-site folder).

## What was recorded (44)
- 2022 (12, main-site folder URLs): 01-31, 02-28, 03-28, 04-11, 04-25, 05-30, 06-13, 08-29, 10-17, 10-31, 11-28, 12-19.
  URLs: https://www.soderhamn.se/download/18.<node>/<ts>/KF-protokoll-YYMMDD-%C2%A7%C2%A7-<range>.pdf (stable, direct).
- 2023 (11, diarium): 01-30, 02-27, 03-27, 04-24, 05-29, 06-12, 08-28, 09-25, 10-30, 11-27, 12-18. File ids 455332..455344.
- 2024 (9, diarium): 01-29, 02-26, 03-25, 04-29, 06-10, 08-26, 09-30, 10-28, 12-16. File ids 457075..457083.
- 2025 (7, diarium): 02-24, 03-31, 04-28, 06-16, 08-25, 10-27, 12-15. File ids 459018..464746.
- 2026 (5, diarium): 01-26, 02-27, 03-30, 04-27, 06-15. File ids 466292..471594.
- All verified via document_to_text: header "Kommunfullmäktige / Sammanträdesdatum <date>" + §§ range.
- Skip: all "Kallelse"/"Ärendelista" documents (agenda/notice terms), cancelled-meeting notices, and the "Kommunfullmäktiges sammanträde ... ställs in" notices.

## Dead ends / notes
- 2019-2022 folder page must be fetched with the ?folder= param (bare page shows only the organ list; slim_http HTML link extraction missed
  the folder contents — use Playwright to walk folder links).
- The diarium search SPA results must be expanded per meeting to reveal document links (client-side); the JSON-RPC ReadObjectDetails
  is the clean way to enumerate.
- No Wayback needed: everything 2022+ was live.
