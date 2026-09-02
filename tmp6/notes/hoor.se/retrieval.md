# hoor.se retrieval notes (Höörs kommun), kf = Kommunfullmäktige

## Two publication eras — no single archive covers the whole range
- Since ~2023-05-24: meeting documents live in the Ciceronsok "Sammanträdesportal" at
  **https://sok-hr.unikom.se/#!/search/** (linked from hoor.se Kommunfullmäktige page).
  The portal ONLY contains meetings from 2023-05-02 onward (search for 2022 in any diary
  returns 0 hits; the old portal mplusext-hr.unikom.se used before then is DNS-dead).
- 2022-02-02 .. 2023-04-12: KF protocols were published as plain file uploads on hoor.se
  (WordPress media library, /app/uploads/ and legacy /wp-content/uploads/ paths). Those
  files are STILL LIVE and indexed by the WP REST API.

## Portal (sok-hr.unikom.se) — JSON-RPC API
- Endpoint: POST https://sok-hr.unikom.se/json with {"jsonrpc":"2.0","method":...,"params":...,"session_id":"..."}.
  Session id returned in every response (e.g. AB22227AC78CFD63A7C97A6CCB48C4E1B1FD612441); keep reusing it.
- ReadDiaries -> diary "KSF" = Kommunstyrelsen/Kommunfullmäktige (instances: Kommunfullmäktige, Kommunstyrelsen, KSAU).
- Search: {"method":"CiceronsokServer:Search","params":{"search_id":"ciceronsok_search","doctype":1,"text":"","param":"{\"hasFiles\":false,\"diary\":\"KSF\",\"board\":\"\",\"from_date\":\"2022-01-01\",\"to_date\":\"2026-08-20\"}"}}
  -> hits count. Then ReadItems {"search_id":"ciceronsok_search","offset":0,"limit":100} -> items
  {id(index), title "Kommunfullmäktige YYYY-MM-DD", object_link}. 56 KSF meetings total in range; KF subset = 27.
- Meeting detail: {"method":"CiceronsokServer:ReadObjectDetails","params":{"search_id":"ciceronsok_search","id":"<index>"}}
  -> value JSON with documents[] {name ("Protokoll KF YYMMDD"), id, filename_b64, dok_id} and items[].
  Protocol doc = name starts "Protokoll" but NOT "Protokollsbilaga"/"Protokollsanteckning" (skip those;
  also skip "Kallelse"). One protocol per meeting date.
- Download URL (works WITHOUT session, verified 200 application/pdf):
  https://sok-hr.unikom.se/download/document?filename=<filename_b64>&id=<docid>
  NOTE: download_document tool CANNOT fetch these (its HEAD existence probe gets 404; the server
  only answers GET). Verify in-browser fetch instead. The b64 may contain "=" -> URL-encode as %3D.
- Portal KF meeting dates recorded (27): 2023: 05-24,06-14,08-30,10-11,11-08,12-06; 2024: 01-31,03-13,
  04-17,05-22,06-19,08-28,09-25,11-06,12-18; 2025: 01-29,03-12,04-23,06-11,08-27,10-01,11-05,12-17;
  2026: 02-04,03-11,04-22,06-10 (all within 2022-01-01..2026-08-20).

## hoor.se WP uploads era (2022-02-02 .. 2023-04-12)
- Enumerate the whole media library: GET https://www.hoor.se/wp-json/wp/v2/media?search=protokoll&per_page=100&page=N
  (207 "protokoll" items, 3 pages; also search=kommunfullmaktige, search=tillkannagivande).
  KF protocol files = names "protokoll-kommunfullmaktige-YYMMDD*" / "protokoll-kf-YYMMDD*", e.g.
  protokoll-kommunfullmaktige-220202-signerat.pdf, ...-220302-signerat.pdf, ...-220406-omedelbart-justerat-37-1.pdf,
  ...-220518-signerat.pdf, ...-220615.rtf, ...-220831.docx, ...-221012.docx, ...-221019.docx, ...-221116.docx,
  ...-221130.docx, ...-221214.docx, ...-230201.docx, ...-230301.docx, protokoll-kf-230412.docx.
  Live URL: https://www.hoor.se/app/uploads/<filename> (both app/uploads and wp-content/uploads work).
- Meeting dates cross-confirmed by: Wayback captures of hoor.se KF page (2022 schedule: 2 feb, 2 mar, 6 apr,
  18 maj, 15 jun, 31 aug, 12 okt, 16 nov, 30 nov, 14 dec; 2023: 1 feb, 1 mar, 12 apr),
  tillkannagivande-kf-<YYMMDD>-signerat.pdf files, and "faststalld-kf-<YYMMDD>" policy docs
  (e.g. bolagspolicy-faststallda-kf-220831, pensionsreglemente-faststallt-kf-221012,
  instruktion-for-kommunfullmaktiges-valberedning-faststalld-kf-221019 -> extra KF meeting 2022-10-19).
- 2022-10-12 has also a protokollsutdrag "kommunfullmaktige-2022-10-12-2022-10-12-kf-c2a7106.pdf" — skip
  extracts; the full protocol is protokoll-kommunfullmaktige-221012.docx.
- 2022-04-06 has 3 file versions (omedelbart-justerat-37, -37-1, signerat) — same meeting, record one.
  2022-11-30 has .rtf and .docx — same meeting, record one.
- KF protocols recorded from hoor.se uploads (14): 2022-02-02,03-02,04-06,05-18,06-15,08-31,10-12,10-19,
  11-16,11-30,12-14; 2023-02-01,03-01,04-12.

## Total harvest result 2022-01-01..2026-08-20
- 41 KF minutes recorded: 14 from hoor.se uploads (confidence 0.85-0.9; PDFs are scanned so text not
  extractable, docx/rtf not parsed by document_to_text — metadata-verified) and 27 from the portal
  (confidence 0.95, structured metadata + verified PDF responses).

## Dead ends
- mplusext-hr.unikom.se (old MeetingPlus portal) DNS dead; Wayback only has partial API captures, no KF data.
- Current portal has no 2022 data (any diary, 2022-01-01..2022-12-31 -> 0 hits).
- hoor.se news pages (nyheter/kommunfullmaktige-sammantrader-*) now 404; site search finds no protocols.
- download_document HEAD probe fails on the portal download endpoint (404 on HEAD, 200 on GET).
