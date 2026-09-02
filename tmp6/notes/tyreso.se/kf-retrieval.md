# Tyresö (tyreso.se) - KF retrieval notes

Target: Tyresö kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20. RESULT: 40 KF protocol
documents recorded (26 on insynsverige.se for 2022-2024, 14 on lexext.tyreso.se
for 2025-2026).

## Site structure - two platforms
Main site tyreso.se -> Organisation & styrning -> Insyn och påverkan ->
"Sammanträdeshandlingar och protokoll"
(https://tyreso.se/organisation--styrning/insyn-och-paverkan/sammantradeshandlingar-och-protokoll.html).
- Up to 2024-12-31: archive on **insynsverige.se/tyreso** (Insyn Sverige/Open24
  platform, ASP.NET WebForms). KF committee page https://insynsverige.se/tyreso-kf
  with "Senaste sammanträden" year filter (combobox #ctl00_ctrMain_ctlPreviousMeetings_ctrFilterByYear)
  and "Möteskalender" per year. All meetings listed server-side; no JS needed.
- From 2025-01-01: **lexext.tyreso.se/Lex2PublishWasm** (Lex2Publish, Blazor
  WebAssembly SPA, tenant guid 8cc90cec-fba7-4ca0-9021-e9702c209213). No direct
  HTML; use the JSON API (below).

## insynsverige.se (2022-2024)
- Meeting list: https://insynsverige.se/tyreso-kf (year filter for full list).
  2022 (9): 02-03, 03-24, 04-28, 06-02, 06-21, 08-25, 10-27, 11-24, 12-20.
  2023 (8): 03-30, 04-20, 05-25, 06-21, 08-31, 10-26, 11-23, 12-19 (12-21 has
  dagordning only, no protokoll). 2024 (9): 02-01, 03-21, 04-25, 05-23, 06-19,
  08-29, 10-24, 11-21, 12-12.
- Protocol page: https://insynsverige.se/tyreso-kf/protokoll?date=YYYY-MM-DD.
  It contains TWO "Ladda ner som PDF" links: the FIRST is the Kallelse (skip),
  the SECOND (in the Protokoll tab) is the protocol PDF
  https://insynsverige.se/documentHandler.ashx?did=<id>. Get both with
  required_tokens=["Ladda"].
- did mapping (protocols): 2022-02-03=2020880, 03-24=2021071, 04-28=2023281,
  06-02=2026134, 06-21=2027242, 08-25=2029084, 10-27=2031816, 11-24=2033076,
  12-20=2034068; 2023-03-30=2037409, 04-20=2038193, 06-21=2040296, 08-31=2041427,
  10-26=2043564, 11-23=2044814, 12-19=2046012; 2024-02-01=2047589, 03-21=2050263,
  04-25=2051586, 05-23=2052703, 06-19=2053691, 10-24=2055630, 11-21=2056869,
  12-12=2056859. Verified text: header "Protokoll" + Mötesdatum.
- BROKEN protocol PDF links (404): 2023-05-25 (did 2039491) and 2024-08-29
  (did 2054231). Full protocol content still rendered as HTML on the protokoll
  page; recorded the protokoll page URL itself for those two (conf 0.85).
  No Wayback captures of the dead PDFs.

## lexext.tyreso.se (2025-2026)
- API (JSON POST, all under /Lex2PublishWasm/publish/, tenant guid in path):
  - POST .../publish/search/{tenantGuid} body
    {"infoTypes":{"document":false,"case":false,"meeting":true},"subjects":[...],
    "fromDate":"2025-01-01T00:00:00","toDate":"...","searchText":"","maxResultRows":32767}
    -> meetings[] with id, uniqueId, unit, description, type.name.
    subjects = opaque encoded guids of checked diarier; the KF+KS diarium is one
    of the 10. Filter meetings by unit=="Kommunfullmäktige".
  - POST .../publish/getmeeting/{tenantGuid} body {"uniqueId":meetingUid,
    "fetchAmount":"Complete","includeParameter":true} -> documents[] with
    description, type.name ("Kallelse"/"Kungörelse KF"/"Protokoll"), id, uniqueId.
  - POST .../publish/getdocument/{tenantGuid} body {"uniqueId":docUid,
    "includeFile":true} -> file.data = base64 PDF (for verification).
- Document PDF URL (what the UI "Läs handling" buttons open, in a new tab):
  https://lexext.tyreso.se/Lex2PublishWasm/docs/{TypeName}_{meetingId}.pdf
  e.g. docs/Protokoll__1698.pdf (Protokoll has double underscore;
  docs/Kung%C3%B6relse_1702.pdf single underscore). Serves the FIRST document
  of that type for the meeting.
- KF meetings (id -> date): 1698=2025-01-30, 1700=2025-03-20, 1701=2025-04-24,
  1702=2025-05-22, 1703=2025-06-17, 1704=2025-09-25, 1705=2025-10-23,
  1706=2025-11-20, 1707=2025-12-18, 1708=2026-02-05, 2420=2026-03-19,
  2421=2026-04-23, 2433=2026-05-21, 2440=2026-06-16. Recorded
  docs/Protokoll__<id>.pdf for each.
- GOTCHA: meetings 1702 (2025-05-22) and 1705 (2025-10-23) have TWO Protokoll
  documents; docs/Protokoll__<id>.pdf serves the omedelbar-justering partial
  (§§47,51 for 1702; §110 for 1705). The full/main protocol doc (238331 for
  1702; 246003 for 1705) has no docs URL - only via getdocument API with
  includeFile (base64). Recorded the docs URL with conf 0.8; if a complete
  protocol is required, fetch the second Protokoll doc via the API.
- UI quirk: Blazor date inputs crash without
  window.blazorBootstrap.dateInput.setValue stub (addInitScript) - but the JSON
  API works fine without the UI, so prefer the API.

## Recorded
40 KF minutes: 2022 x9, 2023 x8, 2024 x9, 2025 x9, 2026 x5 (through 2026-06-16;
no later KF meeting before 2026-08-20). Agenda/notice docs (Kallelse,
Kungörelse) skipped.
