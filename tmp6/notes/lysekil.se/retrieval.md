# lysekil.se retrieval log (kf = Kommunfullmäktige)

## kf — round 1 (2026-08-19): SUCCESS, 36 protocols recorded
- lysekil.se is SiteVision CMS. The "Möten, handlingar och protokoll" page
  (https://lysekil.se/kommun-och-politik/politik-och-demokrati/moten-handlingar-och-protokoll)
  links every committee's archive to the Ciceron diarium
  https://lysekil-sok.ciceron.cloud (link text "Kommunfullmäktiges handlingar
  och protokoll" -> .../#!/search/?t=1&i=Kommunfullm%C3%A4ktige&n=LKS&expand=0&today=0).
- The diarium is an AngularJS SPA; the API is JSON-RPC at
  https://lysekil-sok.ciceron.cloud/json (POST, application/json). Note: GET
  /json is 404; only POST works.
- API flow (all against /json):
  1. CiceronsokServer:ReadObject
     {"search_id":"ciceronsok_search","param":"{\"t\":\"1\",\"i\":\"Kommunfullmäktige\",\"n\":\"LKS\",\"today\":\"0\"}"}
     -> {"result":"{\"hits\":63}"} + session_id (echoed on every reply).
  2. CiceronsokServer:ReadItems {search_id, offset, limit} -> "Möte" rows
     (doctype 1), title "Kommunfullmäktige YYYY-MM-DD", newest first.
  3. CiceronsokServer:ReadObjectDetails {search_id, id:<index>} -> value JSON
     with "documents"[] (Kallelse / Protokoll ...) and "items"[] (agenda).
  - Meeting ids 0..36 fall in 2022-01-01..2026-08-19 (2022-02-16..2026-06-17).
    id 9 (2025-03-19) has documents:[] — meeting exists but NO protocol
    published; nothing recorded.
- Document download: https://lysekil-sok.ciceron.cloud/download/document?filename=<b64>&id=<doc_id>
  where <b64> is the document's filename_b64 (e.g. "UHJvdG9rb2xsLnNpZ25lcmFkLnB1Yi5wZGY="
  = "Protokoll.signerad.pub.pdf"). No session_id needed. The app adds
  &session_id=<session> but it is ignored (bogus/absent session still 200s).
- IMPORTANT dead end: download_document cannot fetch these PDFs — the server
  returns 404 to HEAD (GET is 200). download_document's existence check
  (HEAD) therefore always fails ("definitively not found (404/410)").
  Workaround that worked: trigger a Playwright download
  (page.waitForEvent('download') + synthetic <a href download> click), read
  download.path() (e.g. /tmp/playwright-artifacts-*/<uuid>), then
  document_to_text on that absolute path (it accepts paths outside scratch)
  and cat the extracted text.
- One meeting = one protocol. Where several "Protokoll..." docs exist for a
  date, pick the one named "Protokoll KF YYYY-MM-DD" with the full paragraph
  range; skip "Protokoll ... omedelbar justering", "Protokoll §X ...",
  "Protokoll ... - § N" (partial/adjourned-items docs), "Protokoll
  valberedningen ...", "Webb handlingar ...", and all "Kallelse ..." docs.
  Checked by downloading: 2023-12-13 full=35853 (42p), partial 35852 (6p);
  2023-04-26 full=35846 (38p), partial 23261 (3p); 2025-05-14 full=63161 (46p),
  partial 63160 (5p); 2024-03-20 full=50414 (50p, §§27-53), partial 50413
  (26p, §§29-39); 2025-02-12 has THREE protocol docs (63158 §10 partial 5p,
  63159 full 28p, 66662 full 28p) -> recorded 63159; 2024-02-14 has two full
  duplicates (50412, 37972) -> recorded 50412; 2023-02-08 has two duplicates
  (35844 text PDF, 35854 scanned/OCR) -> recorded 35844.
- Recorded: 36 protocols, 2022-02-16 .. 2026-06-17 (ids/dok ids: 20616..71062),
  all at https://lysekil-sok.ciceron.cloud/download/document?filename=...&id=...
  Titles as published ("Protokoll KF YYYY-MM-DD" 2023+; "Protokoll från
  kommunfullmäktige YYYY-MM-DD" 2022). Confidence 0.95.
- 2026 dates from sammanträdeskalender (25 feb, 25 mar, 29 apr, 17 jun, 23 sep,
  28 okt, 16 dec): Sep-Dec 2026 are after the range end; nothing to harvest.
