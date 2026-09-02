# morbylanga.se scanner notes (Mörbylånga kommun, kf = Kommunfullmäktige)

## Harvest 2026-08-20: 34 KF protocols recorded (2022-01-31 .. 2026-06-15)

### Where KF minutes live
- Website is WordPress (www.morbylanga.se). The "Möten och protokoll" page
  (kommun-och-politik/arkiv-och-allmanna-handlingar/moten-och-protokoll/) says all
  meeting documents are on the WEB DIARIUM: https://diariet.morbylanga.se/
  (JS/Angular SPA, Ciceron "Sök" system).
- KF organisation page links "Diarium på webben" too.

### Diarium API (JSON-RPC POST to https://diariet.morbylanga.se/json)
- Ciceron JSON-RPC. Every POST returns session_id; REUSE it in the next call
  (put it top-level in the JSON body). Methods (from iciceronsok.js):
  Search(search_id, doctype, text, param), ReadItems(search_id, offset, limit),
  ReadObject(search_id, param), ReadObjectDetails(search_id, id),
  ReadDiaries(), ReadArendeFiles(...), ReadHotspots().
- Diaries (ReadDiaries): BN, KS (instances incl. "Kommunfullmäktige"),
  KTN, SN, TN, UN, VN. KF = diary KS, board/instance "Kommunfullmäktige".
- Full enumeration that works: Search doctype=64 (Sammanträdesprotokoll) with
  param {"from_date":"2022-01-01","to_date":"2026-08-20","diary":"KS",
  "board":"Kommunfullmäktige","hasFiles":false} -> 34 hits; ReadItems lists the
  meetings (title "Kommunfullmäktige <date>", object_link ?t=1&i=Kommunfullmäktige&d=<date> 00:00:00&n=KS).
  NOTE: doctype=1 (Möte) returns 35 hits — it adds 2022-06-20, whose meeting has
  NO protocol document (documents:[] in ReadObjectDetails) — excluded.
- Meeting details: ReadObjectDetails(id) returns {"documents":[{name,id,filename_b64,
  subtitle}], "items":[...]} for type 1. Documents include "Kallelse ..." (agenda,
  SKIP), "Protokoll KF <date> omedelbart justerat"/"omedelbar justering" (per-§
  supplement, SKIP), and the main "Protokoll KF <date>" (RECORD). One record per
  meeting date.
- Document download URL (GET, works WITHOUT session_id — server sets its own cookie):
  https://diariet.morbylanga.se/download/document?filename=<base64>&id=<numeric>
  e.g. filename base64 of "Protokoll KF.signerad.pub.pdf" =
  UHJvdG9rb2xsIEtGLnNpZ25lcmFkLnB1Yi5wZGY= ; id is the document id from details.
- GOTCHA: download_document CANNOT verify these URLs — server answers HEAD with 404
  while GET returns 200 PDF (same as molndal.se webbdiarium). slim_http GET refuses
  (binary). Verify/download via Playwright: page.evaluate(fetch(url)) then
  browser_network_requests filter "download/document", then
  browser_network_request index=N part="response-body"
  filename="/tmp/.playwright-mcp/downloads/<name>.pdf". document_to_text on that
  path works.

### Recorded (34), date = Sammanträdesdatum verified in each PDF
- 2026 (3): 02-23, 04-20, 06-15. (Next KF meeting after range: ~2026-09.)
- 2025 (7): 02-24, 04-28, 06-16, 09-15, 10-13, 11-17, 12-15.
- 2024 (7): 02-26, 04-29, 06-17, 09-16, 10-14, 11-18, 12-16.
- 2023 (7): 02-27, 04-24, 06-19, 09-18, 10-16, 11-20, 12-18.
- 2022 (10): 01-31, 02-28, 03-28, 04-25, 05-30, 08-29, 09-26, 10-24, 11-21,
  12-19. (2022-06-20 meeting exists but NO protocol file — see dead end below.)
- Titles as published (e.g. "Protokoll KF 260615"; 2022 ones sometimes named by
  § range: "Kommunfullmäktige den 31 januari 2022 §§ 1-24", "... 28 februari 2022
  §§ 25-68", "... 26 september §§ 168-178"; 2022-12-19 file is titled "Protokoll
  KF 221221" but content says Sammanträdesdatum 2022-12-19 — record the meeting date).
- source_page: https://diariet.morbylanga.se/#!/search/?t=1&i=Kommunfullm%C3%A4ktige&d=<date>%2000:00:00&n=KS

### 2022-06-20 protocol: NOT AVAILABLE (dead end)
- Meeting object exists (2022-06-20, 16 agenda items) but documents:[] in details.
- Case 2022000075 "Kommunfullmäktiges signerade protokoll 2022" lists
  "Kommunfullmäktiges protokoll 220620 §§ 133-148" (dok_id 21234) with exists:false
  (no file id/filename). No downloadable file anywhere in the diarium; WordPress
  media library has no KF protocol uploads from 2022; download?dokid=* 404s.
  Conclusion: 2022-06-20 protocol is not retrievable from any live URL.

### Tips
- Do NOT record Kallelse / omedelbart justerat / Tilläggslista / Handlingar docs.
- The Kommunfullmäktige page lists webcasts (qcnl.tv / screen9) per meeting date —
  useful cross-check for meeting dates 2024-2026.
- Session: browser sessionStorage session_id is what the SPA uses for downloads;
  but the URL works without it.
