# Söderköping (soderkoping.se) - KF retrieval notes

Target: Söderköpings kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- CMS site soderkoping.se (EPiServer). KF page:
  https://soderkoping.se/kommun-politik/protokoll-kallelser/protokoll-kommunfullmaktige/
  states: "Från 2022 publicerar vi alla nya protokoll från kommunfullmäktiger i vårt
  e-diarium" - ALL KF protocols from 2022 live in the e-diarium, linked as
  "Sammanträdesprotokoll - Söderköpings kommuns diarier":
  https://diariet.soderkoping.se/#!/search/?t=1&i=Kommunfullm%C3%A4ktige&n=KS&expand=0&today=0
- Diarium = Ciceron platform (same family as ostersund.se). JSON-RPC endpoint:
  POST https://diariet.soderkoping.se/json  (Content-Type application/json; charset=UTF-8)

## Ciceron API flow (all plain POST JSON, works via slim_http)
1. CiceronsokServer:ReadHotspots -> hotspots: id 1 "Sammanträdesprotokoll" (info_type 64),
   id 2 "Kallelse och handlingar till sammanträde" (info_type 1). Returns session_id.
2. CiceronsokServer:ReadObject {search_id, param} with
   param={"t":"1","i":"Kommunfullmäktige","n":"KS","today":"0"} -> {"hits":41}
   (t=1 = Sammanträdesprotokoll hotspot; i=instance; n=diary KS)
3. CiceronsokServer:ReadItems {search_id, offset, limit} -> list of "Möte" (doctype 1)
   items, each {id:0..40, title:"Kommunfullmäktige <date>", object_link:"?t=1&i=...&d=<date>&n=KS"}
   limit=100 returns all 41 in one call.
4. CiceronsokServer:ReadObjectDetails {search_id, id} -> meeting detail value JSON with
   "documents":[{name, id, filename_b64, ...}] (the meeting-level files) + "items" (agenda).
5. Download URL: https://diariet.soderkoping.se/download/document?filename=<filename_b64>&id=<file_id>
   -> application/pdf. session_id NOT required (browser/slim_http GET with or without it
   returns 200). The file ids are opaque (e.g. 250..30772), not derivable from date.
- Search by free text: CiceronsokServer:Search {search_id, doctype, text, param} then
  ReadItems. doctype: 1=Möte, 2=Ärende, 4=Handling (document), 64=Sammanträdesprotokoll.

## Meeting dates found (41, one protocol file each), recorded conf 0.95 (0.85 for two)
2026: 02-02, 03-16, 04-20, 05-18, 06-15 (5)
2025: 02-03, 03-17, 04-28, 05-19, 06-16, 09-15, 10-20, 11-17, 12-15 (9)
2024: 02-05, 04-15, 05-06, 05-13, 06-17, 09-16, 10-21, 11-18, 12-16 (9)
2023: 02-13, 02-27, 03-13, 04-17, 05-08, 06-19, 09-11, 10-16, 11-13, 12-11 (10)
2022: 02-02, 03-30, 05-25, 06-22, 09-28, 10-26, 11-22, 12-14 (8)

## Per-meeting protocol selection (one per date)
- Pick the document named exactly "Protokoll [K]ommunfullm[ä]ktige <date>" linked in the
  meeting. Several meetings ALSO publish a "Direktjusterat protokoll ..." partial or a
  "Protokoll ... direktjusterat §X" partial - record ONLY the full protocol.
  Affected: 2026-04-20 (§38/§44 partial), 2025-05-19, 2025-06-16, 2023-10-16, 2023-02-13,
  2022-12-14 (§158 partial), 2024-12-16, 2026-05-18, 2026-03-16, 2026-06-15 (all have
  both full + direktjusterat partial).
- Meetings 2024-10-21 and 2023-04-17 have NO full protocol file in the diarium: the
  meeting detail links only a direktjusterat partial
  ("Protokoll Kommunfullmäktige direktjusterat § 123 2024-10-21" id 21702;
  "Direktjusterat protokoll, Kommunfullmäktige 2023-04-17, §§ 39, 50" id 4385).
  The full protocol exists only as a file-less document stub (dok_id 47824 for
  2024-10-21; 43237 "Protokollsutdrag" for 2023-04-17 - both files:[]). Recorded the
  direktjusterat partial as the minutes for those dates (conf 0.85).
- Kallelse (agenda) files appear in several meeting details - NOT recorded.

## Verification notes
- download_document FAILS on this server: the download endpoint returns 404 to HEAD
  requests (only GET returns 200 PDF). download_document's existence check (HEAD) thus
  reports "definitively not found (404/410)" for every valid URL. Verified: browser
  fetch GET -> 200 application/pdf %PDF-1.5 for all 41; slim_http GET also 200.
  Workaround used: verify via Playwright fetch (status/CT/%PDF magic/byte size + PDF
  Info title/CreationDate). All creation dates are days after the meeting date
  (e.g. 2026-06-15 protocol created D:20260623...). Byte sizes 275KB-719KB (real PDFs).
- The download URL contains URL-safe base64 filename (may contain '_'), padding '=' or
  '%3D' both work; no session needed.

## Tips for next round
- Re-run ReadObject (t=1, i=Kommunfullmäktige, n=KS) -> ReadItems -> ReadObjectDetails
  per id; pick the full protocol file per meeting; build download URL from filename_b64+id.
- Dead ends: no /api/ endpoints (that's norrkoping-style); document stubs (doctype 4
  "Protokoll ..." without files) exist for some meetings and are NOT downloadable;
  t=2 (Kallelse) hotspot does not honour the i=instance filter server-side.
