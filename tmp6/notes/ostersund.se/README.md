# ostersund.se scanner notes (Östersunds kommun)

## Where the minutes live
- Entry: https://ostersund.se/kommun-och-politik/moten-handlingar-och-protokoll.html
  and https://ostersund.se/kommun-och-politik/diarium-och-arkiv.html both link to the
  diarium: **https://diariet.ostersund.se/#!/search/** (a Ciceron/e-bbot JavaScript SPA).
- The ostersund.se site itself has no protocol PDFs; everything is in the diarium.

## Diarium API (worked directly, no UI needed)
- JSON-RPC endpoint: POST https://diariet.ostersund.se/json
- Search: method `CiceronsokServer:Search`, params:
  {"search_id":"<sid>","doctype":64,"text":"","param":"{\"hasFiles\":false,\"diary\":\"KS\",\"board\":\"Kommunfullmäktige\",\"from_date\":\"2022-01-01\",\"to_date\":\"2026-08-19\"}"}
  Response result.result is JSON {"hits":N,...}. Reuse the same search_id for paging;
  using one search_id for multiple Searches keeps only the last query.
- Page results: method `CiceronsokServer:ReadItems`, params {"search_id":sid,"offset":0,"limit":10}
  -> results[] with type "Möte", title "Kommunfullmäktige YYYY-MM-DD", object_link
  "?t=1&i=Kommunfullmäktige&d=YYYY-MM-DD 00:00:00&n=KS".
- Meeting detail: method `CiceronsokServer:ReadObjectDetails`, params {"search_id":sid,"id":"<idx>"}
  -> result.value JSON with documents[] (each {name, id, filename_b64, size, type:"A"}) and items[].
  Protocol is the document whose name starts "Protokoll ...". Also present per meeting:
  Kallelse (skip - agenda), Närvaro, Talare, Omröstningsresultat.
- Session: cicsoksid cookie is the session_id; it also works as ?session_id= query param.
  Session stays valid for the browser session; download GET works without the cookie too
  (filename+id suffice -> application/pdf).

## Document download URLs
- https://diariet.ostersund.se/download/document?filename=<filename_b64>&id=<docid>
  (page renders it with &session_id=<sid> appended; the param is optional).
- download_document tool gets 404 on these (its existence check/HEAD is rejected), and
  slim_http refuses binary. Working path: trigger fetch() of the URL inside the Playwright
  page (page.evaluate), then capture that XHR via browser_network_request
  part="response-body" filename="/tmp/.playwright-mcp/downloads/<date>-protokoll.pdf".
  Then document_to_text on that file (absolute paths work).

## KF (kommunfullmäktige) harvest result 2022-01-01..2026-08-19
- Search board="Kommunfullmäktige", doctype=64 (Sammanträdesprotokoll): 40 meetings.
  doctype=1 (Möte) returns 41 - the extra one is "Kommunfullmäktige 2023-04-25" which has
  NO documents/items (empty placeholder, no protocol).
- board "Historiska protokoll - KF" has 0 hits in this range (only older stuff).
- Recorded: 40 protocol PDFs, one per meeting date (dates as titles).
  Note titles: 2025-06-16 "- rättad § 101", 2025-04-16 "§ 62 - omedelbart justerad",
  2023-10-19 "rättning § 177", 2026-04-23 had TWO protocol docs ("... § 61" supplement +
  main "Protokoll Kommunfullmäktige 2026-04-23" id 321212 - recorded the main one).
  2022 protocols are named "Protokoll KF YYYY-MM-DD" (2022-05-24/04-28/03-31 share the
  filename b64 "Protokoll.pub.pdf" but distinct ids).
- The meeting list is in the KS diary (nämnd dropdown "Kommunstyrelsen/Kommunfullmäktige",
  beslutinstans "Kommunfullmäktige").

## Decision extraction guidance (KF protocols)
- Substantive decisions appear under a "Kommunfullmäktiges beslut" heading (sometimes
  embedded as "Kommunfullmäktige medger/utser/fastställer ..." lines). Extract those.
- Information-only paragraphs to skip: "Meddelanden och informationer", "Dialog med
  revisionen", "Anmälan av motioner/medborgarförslag" without beslut (i.e. only
  meddelanden), "Allmänhetens frågestund" with no questions.
- Ceremonial "Utdelning av utmärkelse" paragraphs (e.g. Årets By, Tillgänglighetspriset)
  record no formal decision - skip.
- Procedural items are recorded decisions and were included: roll call/justerare/
  dagordning (§ 188-type) and "Kommunfullmäktige medger att frågan får ställas"
  (interpellation/fråga paragraph).
- Final adopted wording is in "Kommunfullmäktiges beslut"; when a yrkande changed a
  point, the printed beslut already reflects the amendment (e.g. date fixes, added
  tilläggsyrkanden in budget). Use the printed beslut text as full_text.
- Voting method: only record voting_method when an explicit "Omröstning" with results is
  in the text; otherwise omit.
