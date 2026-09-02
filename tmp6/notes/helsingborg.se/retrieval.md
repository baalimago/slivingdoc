# helsingborg.se retrieval log (kf = Kommunfullmäktige)

## Round 1 (2026-08-20) — kf minutes 2022-01-01..2026-08-20
- Target: Helsingborg, meeting type kf (kommunfullmäktige).
- Site structure: helsingborg.se (WordPress) -> "Kommun och politik" ->
  "Diarium, handlingar och protokoll" page points to the diarium SPA
  https://diariet.helsingborg.se/ (Ciceron / CiceronsokServer platform).
  Also https://anslagstavla.helsingborg.se/ (same Ciceron app; a rolling
  3-week notice board "Justerade protokoll" — NOT an archive, useless for
  older dates).
- Diarium API: JSON-RPC POST https://diariet.helsingborg.se/json.
  Methods used: CiceronsokServer:Test (creates session_id),
  Search, ReadItems, ReadObjectDetails, ReadObject, ReadDiaries,
  ReadHotspots, ReadArendeFiles.
- The site's own shortcut button "Kommunfullmäktiges protokoll" runs
  Search(doctype=4, text="Protokoll från kommunfullmäktiges sammanträde",
  param={"hasFiles":true,"diary":"DIAKS","from_date":"2021-01-01"})
  -> exactly 22 hits = all KF combined protocols in the live diarium
  (2024-08-27 .. 2026-05-19).
- Document download URL shape:
  https://diariet.helsingborg.se/download/document?filename=<base64>&id=<n>
  (session_id param optional; plain GET returns the PDF; NOTE the server
  returns 404 for HEAD so download_document's existence check fails while
  GET works — verified with slim_http which got application/pdf).
- Live diarium only holds ~2 years ("två år bakåt i tiden" per site).
  Doctype=1 (Möte) search DIAKS 2022-01..2024-12 returns only 2024-08-20..;
  2022 and 2023 meetings are 0 hits. ReadObject({t:1,d:<date>,i:Kommunfullmäktige,n:DIAKS})
  for 2022-01-25 -> 0 hits. So the 2022..2024-08 KF protocols are GONE
  from the live backend (search index purged).
- Wayback Machine: CDX for diariet.helsingborg.se/download/document* shows
  only ~50 unique captured PDFs; the only KF ones are 2018-11-21/22, 2019-11-26,
  2021-11-02 and a "KF Kallelse" (agenda) — no 2022..2024-08 KF protocol.
  The ArchiveBot (2026-03-14) captures of per-meeting escaped-fragment URLs
  (?_escaped_fragment_=/search/?t=1&i=Kommunfullmäktige&d=YYYY-MM-DD...) are
  Angular app shells (no rendered content); the 2023-09-27 hash-URL captures
  (#!/search/?t=1&i=Kommunstyrelsen&d=2022-01-12...) are unreplayable (404).
  Old file ids still resolve on the live download endpoint (id=238845
  "Kfprotokoll 191126.pdf" -> PDF), but ids for the 2022-24 KF protocols
  are not discoverable. CONCLUSION: no online source for KF minutes
  2022-01..2024-08; nothing recorded for that span.
- 2026-06-09 KF meeting exists (doctype=1 "Kommunfullmäktige 2026-06-09";
  diarium has Kallelse + per-§ "Protokollsutdrag KF 2026-06-09 § ..."), but
  NO combined protocol "Protokoll från kommunfullmäktiges sammanträde den 9
  juni 2026" is published; anslagstavla empty for it now -> nothing recorded.
- RECORDED: 22 combined KF protocols (one per meeting, 2024-08-27,
  2024-09-17, 2024-10-15, 2024-11-19, 2024-12-17, 2025-01-21, 2025-02-11,
  2025-03-11, 2025-04-22, 2025-05-20, 2025-06-10/11, 2025-08-26, 2025-09-16,
  2025-10-22, 2025-11-18, 2025-12-09, 2025-12-17, 2026-01-27, 2026-02-24,
  2026-03-24, 2026-04-21, 2026-05-19), confidence 0.95, source page
  https://diariet.helsingborg.se/#!/search/.
- Filename patterns: "Protokoll KF YYMMDD[1].signerad.pub.pdf" and
  "KF Protokoll YYMMDD[1].signerad.pub.pdf" (occasionally editorial suffixes,
  e.g. 2025-12-17 -> "Protokoll (2).signerad.pub.pdf", 2025-12-09 ->
  "KF Protokoll 251209 2.signerad.pub.pdf").
- Tip for next round: re-run the hotspot Search (above) and ReadObjectDetails
  per hit; do not rely on date-token URL templates. If a later round needs
  2022-24 data, only the Wayback PDFs (if ever captured) or the city archive
  would help.
