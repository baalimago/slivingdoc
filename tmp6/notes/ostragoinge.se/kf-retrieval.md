# ostragoinge.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — SUCCESS: 36 KF protocol documents recorded (2022-04-28 .. 2026-05-28)

## Site structure / entry
- Ostragoinge.se is SiteVision CMS. The protocol page is
  https://ostragoinge.se/kommun-och-politik/politik-och-demokrati/protokoll
  (Kommun och politik -> Politik och demokrati -> Protokoll).
- That page hosts OLD protocols up to and including 2021 (accordion sections per body
  and per year: Kommunstyrelsen 2022..2015, Kommunfullmäktige 2021..2015, etc.).
  IMPORTANT: the KF section on the old page has NO 2022 protocols (sections start at
  2021); 2022 KF protocols live in the Unikom diary instead.
- Page text: "Nya protokoll, från år 2023, hittar du i kommunens diariesystem" ->
  link https://sok-og.unikom.se/#!/search/ (Unikom Medborgarportal / Ciceron).

## Medborgarportalen API (Ciceron JSON-RPC at https://sok-og.unikom.se/json)
- POST JSON-RPC, content-type application/json. NOTE: session_id must be passed at the
  TOP LEVEL of the request ({"jsonrpc":"2.0","method":...,"params":...,"session_id":"..."});
  Search returns a session_id; reuse it for ReadItems/ReadObjectDetails.
- ReadDiaries -> diaries: KS (Kommunstyrelsen; instance "Kommunfullmäktige" lives here),
  REV (Revisionen), TT (Tillsyns- och tillståndsnämnden), VN (Valnämnden).
- Search: {"method":"CiceronsokServer:Search","params":{"search_id":"ciceronsok_search",
  "doctype":1,"text":"","param":"{\"hasFiles\":false,\"diary\":\"KS\",\"from_date\":\"2022-01-01\",
  \"to_date\":\"2026-08-20\"}"}} -> 156 hits for diary KS in range (hasFiles true -> 119).
- ReadItems: {"method":"CiceronsokServer:ReadItems","params":{"search_id":"...","offset":0,
  "limit":200}} -> all meetings, newest first, title "Kommunfullmäktige YYYY-MM-DD",
  object_link "?t=1&i=Kommunfullmäktige&d=YYYY-MM-DD 00:00:00&n=KS".
- ReadObjectDetails: {"method":"CiceronsokServer:ReadObjectDetails","params":{"search_id":"...",
  "id":"<result index>"}} -> meeting JSON with top-level "documents"[] (Kallelse/Protokoll/
  Anslagsbevis) and "items"[] (agenda). Protocol = top-level document whose name starts with
  "Protokoll" (NOT Kallelse, NOT Anslagsbevis, NOT bilagor). Some meetings have only the
  protocol doc; some have Kallelse+Protokoll; 2025-11-20 also has Anslagsbevis (skip).
- KF meetings in range 2022-01-01..2026-08-20 = 37; 36 have a published protocol.
  Earliest KF in diary = 2022-04-28 (verified: no KF meeting records Jan-Mar 2022 in diary,
  and no 2022 KF protocols on the old site). 2026-06-16 has NO documents (not yet published).

## Download URL (IMPORTANT)
- Document files: **https://sok-og.unikom.se/download/document?filename=<filename_b64>&id=<doc id>**
  (filename_b64 URL-encoded with encodeURIComponent; id = document's "id" from ReadObjectDetails,
  NOT dok_id). GET 200 application/pdf works WITHOUT session (verified for all 36).
- WARNING: same HEAD-404 trap as sok-hby.unikom.se — download_document fails ("definitively
  not found"). Verify via browser fetch + pdf.js instead (see below). Recorded URLs are the
  no-session GET form.
- Most KF protocols share filename_b64 "UHJvdG9rb2xsLnNpZ25lcmFkLnB1Yi5wZGY="
  (Protokoll.signerad.pub.pdf); 2022-04-28..2022-09-22 use
  "cHJvdG9rb2xsLWtvbW11bmZ1bGxtYWt0aWdlLTIwMjIt...-*.pdf", 2022-11-24/12-15 and 2023-01-26 use
  "UHJvdG9rb2xsLnB1Yi5wZGY=" (Protokoll.pub.pdf), 2022-10-20/2023-09-28 use
  "UHJvdG9rb2xsIEtvbW11bmZ1bGxt5Gt0aWdlLCAyMDIyLTEwLTIwLnB1Yi5wZGY=" etc. Keep id+fb pairs intact
  (mismatched id+fb 500s).

## pdf.js verification trick (works in Playwright evaluate here)
- window.pdfjsLib never appears after loading pdf.min.js via script tag or plain eval in this
  harness; capture it with a defineProperty setter:
    Object.defineProperty(globalThis,'pdfjsLib',{set(v){window.__pdfjs=v;},configurable:true});
    (0,eval)(pdfminjs_text);
  then window.__pdfjs.GlobalWorkerOptions.workerSrc =
  'https://cdnjs.cloudflare.com/ajax/libs/pdf.js/3.11.174/pdf.worker.min.js';
  then getDocument({data}) -> getPage(1) -> getTextContent() for first-page verification.
- All 36 PDFs verified: first page = "Protokoll Kommunfullmäktige YYYY-MM-DD" (2022-04..09)
  or "Instans: Kommunfullmäktige ... <date>" (signed format, 2022-10 onward); page counts
  11..46.

## Recorded (36, one per meeting date; all GET 200 application/pdf, first-page verified)
- 2022 (7): 04-28, 05-25, 06-22, 09-22, 10-20, 11-24, 12-15.
- 2023 (10): 01-26, 03-02, 03-30, 04-20, 05-25, 06-21, 09-28, 10-26, 11-23, 12-14.
- 2024 (8): 01-25, 03-27, 04-25, 05-23, 06-19, 10-24, 11-28, 12-19.
- 2025 (7): 02-27, 03-27, 05-22, 06-17, 09-25, 11-20, 12-18.
- 2026 (4): 02-26, 03-26, 04-29, 05-28.
- Total 7+10+8+7+4 = 36.

## Meetings in range with NO protocol (do not re-hunt)
- 2026-06-16: meeting record exists (diary KS) but documents[] empty (protocol not published).
- No KF meetings 2022-01-01..2022-04-27 and none 2022-07/08, 2023-02, 2023-07/08, 2024-02,
  2024-08/09, 2025-01/04/08, 2026-01 or 2026-06-17..2026-08-20 (KF summer break; next after range).

## Tips for next run
- Use the JSON-RPC at sok-og.unikom.se/json (top-level session_id!), Search doctype 1 diary KS
  hasFiles false for the whole range, ReadItems offset 0 limit 200, filter title
  "Kommunfullmäktige ", ReadObjectDetails per id, pick documents[] name starting "Protokoll",
  build no-session download URL, verify with pdf.js capture trick above.
- Kallelse/Anslagsbevis documents are the agenda/notice terms — skip them.
