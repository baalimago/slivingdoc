# haparanda.se scanner notes (kf = Kommunfullmäktige)

## Site structure
- Entry page: https://haparanda.se/kommun-och-politik/demokrati-och-insyn/diarium-handlingar-och-protokoll
  links to the public diarium **https://haparanda-sok.ciceron.cloud/#!/search/** (Ciceron SPA).
- "Protokoll 2023-2024" page (https://haparanda.se/kommun-och-politik/demokrati-och-insyn/protokoll-2023-2024)
  hosts direct PDFs only for nämnder (BUN, KS, SBN, SN) — NO KF protocols there.
- The Ciceron diarium only contains records from ~2024-05-01. KS diary has 65 meetings
  (2024-06-03 .. 2026-06-15); KF (board "Kommunfullmäktige", doctype=64) = exactly 12 meetings,
  all 2024-08-29 .. 2026-06-15.

## Diarium API (Ciceron JSON-RPC, same as ostersund.se)
- POST https://haparanda-sok.ciceron.cloud/json
- Search: method CiceronsokServer:Search, params
  {"search_id":"<sid>","doctype":64,"text":"","param":"{\"hasFiles\":false,\"diary\":\"KS\",\"board\":\"Kommunfullmäktige\",\"from_date\":\"2022-01-01\",\"to_date\":\"2026-08-20\"}"}
  Response gives session_id; reuse for ReadItems/ReadObjectDetails (pass in body).
- ReadItems: {"search_id":sid,"offset":0,"limit":50,"session_id":...} -> meetings, type "Möte",
  title "Kommunfullmäktige YYYY-MM-DD", object_link "?t=1&i=Kommunfullmäktige&d=...&n=KS".
- ReadObjectDetails: {"search_id":sid,"id":"<idx>","session_id":...} -> value JSON with
  documents[] ({id, dok_id, name, filename_b64, ...}) and items[]. Protocol is the document
  whose name contains "Protokoll"/"Sammanträdesprotokoll ... Kommunfullmäktige"; skip "Kallelse".
  Some meetings have multiple variants (e.g. 2025-06-16 "Omedelbart justerat ..." + main
  "Sammanträdesprotokoll"; 2024-08-29 main + "valberedningen"; 2024-12-16 E-signerat + pub).
  Pick ONE main protocol per date.
- Download URL: https://haparanda-sok.ciceron.cloud/download/document?filename=<b64>&id=<doc id>
  (doc id = the small "id" field, e.g. 69 for 2024-08-29; NOT dok_id).
  download_document tool gets 404 on these (HEAD rejected) — fetch() inside the Playwright
  page and capture the XHR via browser_network_request part="response-body"
  filename="/tmp/.playwright-mcp/downloads/<date>.pdf" (mkdir that dir first). Then
  document_to_text works on the absolute path.
- API gotcha (re-verified 2026-08-20): the JSON-RPC request must carry session_id at the TOP
  level of the request object ({"jsonrpc","method","params",...,"session_id":...}), NOT inside
  params — otherwise ReadItems returns error -1. Also pass the session_id returned by Search
  (it echoes the one you sent). A bare Search without session_id creates a new session whose
  ReadItems fails; first call ReadDiaries or reuse the session returned by any call.

## KF harvest result 2022-01-01..2026-08-20
- 12 protocol PDFs recorded, one per meeting:
  2024-08-29, 2024-10-21, 2024-12-16, 2025-02-24, 2025-03-10, 2025-04-28, 2025-06-16,
  2025-10-20, 2025-12-15, 2026-02-23, 2026-04-20, 2026-06-15. Confidence 0.95.
- Meeting 2025-03-10 is a short protocol (2 items: upprop + inriktningsbeslut skola).
- 2025-04-28 filename has typo "2025-04-28l" in b64 name; content/date verified in PDF.
- 2026-08-20 run: re-verified all 12 via API + PDF text (headers "Protokoll / Beslutande organ
  Kommunfullmäktige / Mötesdatum <date>"). Doc ids current: 2024-08-29 id 69, 2024-10-21 id 1379,
  2024-12-16 id 4142, 2025-02-24 id 5550, 2025-03-10 id 5695, 2025-04-28 id 6926,
  2025-06-16 id 8173, 2025-10-20 id 10627, 2025-12-15 id 12743, 2026-02-23 id 14724,
  2026-04-20 id 16347, 2026-06-15 id 17856. All accepted by record_documents.

## Dead ends / gap 2022-01-01..2024-08-28
- No KF protocols available online for that period from official sources:
  - Old diarium searchport.haparanda.se ("Sök i diariet") and old MeetingsPlus
    meetingsplus.haparanda.se (login publik/insyn, via old page
    /kommun-och-politik/moten-och-protokoll.html) are both offline (dead host 194.117.170.103).
  - Wayback Machine has only login-page snapshots of meetingsplus, no protocol documents;
    CDX for searchport has no docs. CDX for haparanda.se "protokoll" shows only SBN/KPR PDFs.
  - Current site search (https://haparanda.se/sokresultat?query=...) yields only KS/nämnd
    protocols and reglementen, no KF.
- Conclusion: for kf in the requested range only the 12 diarium meetings above exist; the
  2022-2023-early-2024 KF protocols are not retrievable online anymore.
