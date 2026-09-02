# horby.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — SUCCESS: 44 KF protocol documents recorded (2022-01-31 .. 2026-05-25)

## Entry / site structure
- Hörby.se is WordPress (Municipio). The page "Möten, kallelser och protokoll"
  (https://www.horby.se/kommun-och-politik/politik-och-demokrati/moten-kallelser-och-protokoll/)
  does NOT host documents; it points to the Unikom Medborgarportal
  **https://sok-hby.unikom.se/#!/search/** ("Hörby kommun protokoll och handlingar").
  Anslagstavla (notices, excluded) is https://anslagstavla-hby.unikom.se/#!/billboard/.

## Medborgarportalen API (Ciceron JSON-RPC at https://sok-hby.unikom.se/json)
- POST JSON-RPC, content-type application/json. Methods used:
  - CiceronsokServer:Search {"search_id":"x","doctype":1,"text":"Kommunfullmäktige","param":"{\"hasFiles\":false,\"diary\":\"KS\",\"from_date\":\"\",\"to_date\":\"\"}"} -> hits
  - CiceronsokServer:ReadItems {"search_id":"x","offset":0,"limit":100} -> meeting list, newest first.
    Meeting title "Kommunfullmäktige YYYY-MM-DD", object_link "?t=1&i=Kommunfullmäktige&d=YYYY-MM-DD 00:00:00&n=KS".
  - CiceronsokServer:ReadObjectDetails {"search_id":"x","id":"<result index>"} -> meeting JSON: top-level
    "documents"[] (Kallelse/Protokoll/Protokollsbilagor) and "items"[] (agenda). The protocol PDF is the
    top-level document whose name contains "Protokoll" but NOT "bilagor"; prefer the plain "Protokoll" over
    "(Justering)" when both exist, else use the (Justering)/(Signering) one. Skip Kallelse/Kungörelse/
    Protokollsbilagor.
- All 79 KF meeting records fetched (back to 2020-01-27); range 2022-01-01..2026-08-20 = ids 2..50.

## Download URL (IMPORTANT)
- Document files: **https://sok-hby.unikom.se/download/document?filename=<filename_b64>&id=<doc id>**
  (filename_b64 is the document's "filename_b64" from ReadObjectDetails). Works WITHOUT session_id
  (GET 200 application/pdf; session not validated).
- WARNING: the server answers **HEAD 404** (GET 200) on /download/document, so **download_document fails**
  ("definitively not found") on this site. Workaround used: Playwright page fetch + pdf.js
  (cdnjs pdf.min.js) to read first-page text for verification. Recorded URLs are the no-session GET form.
- The same endpoint 500s if the id and filename are mismatched (e.g. protocol filename with the
  Protokollsbilagor id) — keep id+filename_b64 pairs intact.

## Recorded (44, one per meeting date; verified first page = "Kommunfullmäktige ... <date>" via pdf.js)
- 2022 (11): 01-31, 03-28, 04-25, 06-20, 08-29, 09-26, 10-17, 10-31, 11-28, 12-19. (10? count again)
  Actually 2022: 01-31, 03-28, 04-25, 06-20, 08-29, 09-26, 10-17, 10-31, 11-28, 12-19 = 10.
- 2023 (10): 01-30, 02-27, 04-24, 05-29, 06-19, 08-28, 09-25, 10-30, 11-27, 12-18.
- 2024 (9): 02-26, 03-25, 04-22, 06-17, 08-26, 09-30, 10-28, 11-25, 12-16.
- 2025 (11): 01-27, 02-24, 03-24, 04-28, 05-26, 06-16, 08-25, 09-29, 10-27, 11-24, 12-15.
- 2026 (4): 01-26, 03-23, 04-27, 05-25.
- Total 10+10+9+11+4 = 44.
- 2023-06-19 protocol filename_b64 decodes to "…2023-06-14.pdf" (admin typo) but portal title + meeting
  date = 2023-06-19; recorded as 2023-06-19. 2023-04-24/05-29/06-19 and 2024-02-26 PDFs have no readable
  text layer (scanned/encoded) — verified as valid PDFs w/ page counts + portal metadata only (conf 0.9).

## Meetings in range with NO protocol published (do not re-hunt)
- 2022-02-28: record exists (tid 18:00) but documents[] and items[] empty.
- 2023-03-03: empty record (extra Friday meeting, no docs).
- 2023-03-27: kallelse + 14 agenda items exist but NO protocol document.
- 2026-03-02: empty record (second Monday in March 2026, no docs).
- 2026-06-15: item "01. Sammanträdet är inställt" (cancelled).
- 2026-08-24 and 2026-09-28 exist but are outside range (after 2026-08-20).

## Garbled text layer of scanned/encoded KF PDFs (2023-04-24, 05-29, 06-19, 2024-02-26)
- The garbled extraction is broken per-page font encoding (ToUnicode corruption), not OCR noise. Within a
  page it behaves like a many-to-one substitution (several ciphertext glyphs can map to the same letter),
  so short snippets ARE decodable by pattern/frequency (e.g. "2)'h&(+*" -> "beslutar", "le(/e," ->
  "motion", "4eff&,g&hhfi4(/0)ÿ2)'h&(+*" -> "kommunfullmäktig[e] beslutar", "q*-g|*+,-),ÿ3*4+*" ->
  "ordföranden yrkar"). § numbers and page numbers in the TOC use a digit substitution (;=1, B=2, W=3,
  [=4, A=5, V=6, Z=7, \=8, _=9, a=0).
- Each page/font needs its own key, and the mapping is lossy, so full reliable extraction of all §
  decisions is impractical. For these dates: if the task requires formal decisions, prefer submitting
  rejected:true with a rejection_reason ("unreadable text layer") over fabricating decisions.

## Tips for next run
- Search doctype 1 keyword "Kommunfullmäktige" diary KS, ReadItems offset 0..79, ReadObjectDetails per id.
- Pick top-level documents[] with name ~ "Protokoll" (skip bilagor/kallelse); build no-session download URL.
- Verify via browser fetch + pdf.js (download_document is unusable here due to HEAD 404).
