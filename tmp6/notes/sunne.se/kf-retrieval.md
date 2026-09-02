# Sunne (sunne.se) - KF retrieval notes

Target: Sunne kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- EPiServer site (www.sunne.se). Politics entry:
  /kommun/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/
- That page embeds iframe https://lex2api.evarmland.se/Lex2PublishWasm (webbdiarium,
  Blazor WASM). Searchable from 2023-01-01 only (GET
  /publish/getsearchingfromdate/cbb50d5b-80d0-498a-86ba-215e3743d50f returns "2023-01-01").
- Older protocols (2021-2022) live on static page "Äldre protokoll":
  https://www.sunne.se/kommun/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/protokoll-2022/
  (orphaned - not linked from moten-och-protokoll page; found via site search).
- Official notice board (anslagstavla) embeds https://lex2api.evarmland.se/Lex2PinBoardWeb
  (SignalR Blazor; not scrapable via HTTP).

## Webbdiarium API (Lex2PublishWasm) - KEY
- tenant id (Sunne): cbb50d5b-80d0-498a-86ba-215e3743d50f
- Search: POST /publish/search/<tenant>  body:
  {"infoTypes":{"document":true,"case":true,"meeting":true},"subjects":["<subjId>",...],
   "fromDate":"YYYY-MM-DDT00:00:00","toDate":"...","searchText":"","maxResultRows":32767}
  Subject ids come from GET /publish/getsubjectareas/<tenant> (KS+Kf diarium id
  10 = "{4730314541514741414141496848546E...}", MBN id 11).
  NOTE: subject ids are NOT stable between sessions - re-fetch getsubjectareas first.
  Wide ranges (e.g. a full year) return 504 Gateway Time-out; query month-by-month via
  the browser (iframe) - it worked for one month (~17s). slim_http POST works for one month.
- Response: {"documents":[...],"cases":[],"meetings":[...],"objects":[]}
  KF minutes appear in documents as e.g. "Kommunfullmäktiges protokoll 2026-06-15",
  type "Protokoll", owners=[{type:"Meeting",name:"KF/2026-06-15"}], with date = publish date.
  Meetings array lists "KF/2026-06-15" etc (Sammanträde och beslut entries).
- Document detail: POST /publish/getdocument/<tenant> body
  {"uniqueId":"{...}","fetchAmount":"Complete","includeFile":true,"includeMinis":false,
   "includePdf":true,"includePdfA":false,"includeParameter":true}
  Returns JSON; file.data = base64 PDF when a file is attached.
- GOTCHA: KS/other protocols have attached PDFs, but the KF protocol documents in the
  diarium had file:null (checked 2026-06-15 doc id 369475) - KF protocol PDFs appear to be
  posted on the notice board instead (parameter Visa_pa_anslagstavla_prot_from/tom).
  The anslagstavla (Lex2PinBoardWeb) is SignalR-based and was NOT harvested - so the
  actual KF 2023+ PDFs were not downloadable this run.

## KF minutes recorded (9, all 2022, from protokoll-2022 page)
- URLs: https://www.sunne.se/link/<guid>.aspx (redirects to PDF, verified 13MB pdf for 2022-02-21)
- dates: 2022-02-21, 2022-03-21, 2022-05-02, 2022-05-23, 2022-06-27, 2022-09-19,
  2022-10-17, 2022-11-28, 2022-12-19 (9 meetings; no Jan/Jul/Aug - election summer break)
- guids: 60c60754d75a49d18b9e6e005c3b61e6, 5bb34598149946f4ab4167f14ba00e2a,
  77f07dc21de641738278754e8dd380b2, cce723f507144c44a2db81affa60d4cb,
  f9b098247f174ab69a2579d37b723642, f9edcb0dedf24fb4a982ac64b5f4cb3b,
  68639dd27d97494dac93d77fa5c67531, b43a8a0312fa4cfc8b773da208cce154,
  d31375dac5c94510b882cf57db53f11b
- source_page: the protokoll-2022 URL above. Confidence 0.95.

## 2023-2026 KF minutes: NOT recorded this run
- They exist in the webbdiarium (e.g. "Kommunfullmäktiges protokoll 2026-06-15",
  meeting KF/2026-06-15) but no downloadable PDF was found via getdocument, and the
  notice board (where they are posted) is SignalR-only. A future run should target
  Lex2PinBoardWeb API (check /publish/* endpoints on lex2api.evarmland.se) or fetch
  per-month search results and getdocument for each "Kommunfullmäktiges protokoll" doc
  to see if file appears later. 2026 KF meeting seen so far: 2026-06-15.
- 2022 page also contains KS 2022, MBN 2022, Valnämnd 2022, and 2021 protocols - same
  link/<guid> pattern if needed.

## Tips
- Site search (sunne.se/kommun/sok/) is the only way to discover the orphaned
  protokoll-2022 page; no other year pages (protokoll-2023 etc. 404 -> homepage).
- getsubjectareas ids differ per session - always re-fetch.


## Second run 2026-08-20 (harvest, budget-limited) - KF minutes 2023-2026 in webbdiarium

KEY discovery (previous run was wrong that KF 2023+ PDFs are only on the notice board):
- The webbdiarium search API endpoint is under /Lex2PublishWasm/publish/... (NOT /publish/...):
  POST https://lex2api.evarmland.se/Lex2PublishWasm/publish/search/{tenant}
  body {"infoTypes":{"document":true,"case":true,"meeting":true},"subjects":[fresh subject ids],"fromDate":"YYYY-MM-DDT00:00:00","toDate":"...","searchText":"...","maxResultRows":32767}
  Fresh subject ids from GET /Lex2PublishWasm/publish/getsubjectareas/{tenant} (rotate per call; use whatever comes back).
- MOST KF protocol documents DO have attached PDFs (search listing "file":{"format":"pdf",...}); the
  2026-05-25 and 2026-06-15 KF protocols are the exceptions (file:null, only on notice board via
  Visa_pa_anslagstavla_prot_from/tom parameter).
- PDF download URL mechanism: POST /Lex2PublishWasm/publish/savedoctodisk/{tenant}
  body {"uniqueId":"<fresh uniqueId from search>","fileName":"<any name>.pdf","docFolder":null}
  writes the file to the server disk; then GET https://lex2api.evarmland.se/Lex2PublishWasm/docs/<fileName>.pdf
  returns the PDF. Works with arbitrary custom fileName. Files persist at least hours (Protokoll__363555.pdf still live).
  uniqueIds from slim_http searches work for savedoctodisk.
- searchText is TOKEN-based: "Protokoll kommunfullmäktige" misses docs titled "Kommunfullmäktiges protokoll ..."
  (token "Kommunfullmäktiges" != "kommunfullmäktige") and vice versa. Use searchText "protokoll" or
  "kommunfullmäktige" and filter in code. Also one KF protocol is titled "Kommunfullmäkiges protokoll 2025-12-15" (typo).

KF meetings 2023-2026 (28): 
2023: 02-20, 03-20, 05-02, 05-22, 06-19, 09-18, 11-06, 12-18
2024: 02-19, 03-25, 04-22, 05-27, 06-17, 09-16, 11-04, 12-16
2025: 02-17, 03-24, 04-14, 05-26, 06-16, 09-15, 11-17, 12-15
2026: 04-13, 04-27, 05-25, 06-15

KF protocol docs with attached PDFs (id; description): 2023: 315114,316791,321739,323197,324811,328488,331722,332763(Kommunfullmäktiges protokoll); 2024: 334025,336003,336733,337837,338409,341500,343989,345016; 2025: 347114,349088,350719,352767,353399,354562,358732(typo Kommfullmäkiges); 2026: 363555,366108. NO file: 357181 (2025-11-17, Kommunfullmäktiges protokoll), 366847 (2026-05-25), 369475 (2026-06-15) - notice board only.

Recorded this run (13): 9 x 2022 link/<guid>.aspx URLs + docs/kf-2023-12-18.pdf, docs/kf-2025-12-15.pdf, docs/Protokoll__363555.pdf (2026-04-13), docs/kf-2026-04-27.pdf. Remaining 2023-2025 protocols need per-doc savedoctodisk to materialize docs/<name>.pdf URLs (next run: loop over the doc ids above, savedoctodisk, record).
