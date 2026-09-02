# norrkoping.se retrieval log (kf = Kommunfullmäktige)

## kf — round 1 (2026-08-20): SUCCESS — 50 KF protocol PDFs recorded (2022-01-31..2026-06-15)

### Site structure
- norrkoping.se is SiteVision CMS. "Kommun och politik" -> "Politik och demokrati" ->
  "Kommunfullmäktige" -> "Kommunfullmäktiges protokoll"
  (https://norrkoping.se/kommun-och-politik/politik-och-demokrati/kommunfullmaktige/kommunfullmaktiges-protokoll)
  states plainly: "Protokollen från kommunfullmäktiges sammanträden finns i kommunens
  diarium" — protocols live ONLY in the diarium SPA at https://diariet.norrkoping.se/
  ("Sök i diariet"). No per-meeting PDFs on the CMS site itself.
- The norrkoping.se "Allmänna handlingar/diarium" page
  (https://norrkoping.se/kommun-och-politik/beslut-dokument-taxor-och-avgifter/diariet)
  links to https://diariet.norrkoping.se/ (diarium covers registrations since 2007-01-01).

### Diarium SPA / API (diariet.norrkoping.se)
- SPA is plain fetch-based (no JS framework API SDK): bundle.js + /api/ JSON endpoints.
- Endpoints discovered:
  - GET /api/organisations/ -> org-level list (Kommunfullmäktige/Kommunstyrelsen id 200017)
  - GET /api/boards/      -> actual board list used by the search (Kommunfullmäktige id 271490,
    Kommunstyrelsen 271491, ...). NOTE: using the org id 200017 in the notes search returns
    HTTP 500 ("Ett fel uppstod") — always use the /api/boards/ id.
  - GET /api/notes/?from=YYYY-MM-DD&to=YYYY-MM-DD&board=<id>&offset=&limit=
    -> {"notes":[{id,board,date}...],"total":N} (dates are JS Date strings; board label carries
    the meeting date + "Inställt" for cancelled meetings). limit=100 returned all 53 KF notes
    2022-01-31..2026-06-15.
  - GET /api/notes/<note_id> -> {"note":{"id","file":{"id","name","size","comment","accessCode"}}}
    where comment is e.g. "Protokoll  Kommunfullmäktige 2026-06-15". Returns 404 when the meeting
    has no protocol file (cancelled/unpublished meetings).
  - Download URL (from bundle.js: /api/notes/${file.id}/${file.name}):
    https://diariet.norrkoping.se/api/notes/<file_id>/<file_name>  (e.g.
    https://diariet.norrkoping.se/api/notes/2365150/2365150_11_1.PDF) -> application/pdf.
    file ids are opaque 7-digit numbers, NOT derivable from the meeting date.
- Search URL construction in bundle.js: documentType select (Dokument/Föredragningslista/
  Protokoll/Ärende) maps to /api/documents/, /api/agendas/, /api/notes/, /api/cases/;
  "Protokoll" -> /api/notes/. Params: query, from, to, board (omitted if ALL), offset, limit.

### Result
- 53 KF notes in range; 50 have protocol files -> 50 recorded (one per meeting date),
  confidence 0.97, source_page https://diariet.norrkoping.se/.
- Meetings with NO protocol file (note detail 404, not recorded):
  - 2024-12-30 (note 1879022)
  - 2025-07-30 (note 1933835)
  - 2026-02-23 (note 1960461, labelled "Inställt" = cancelled)
- Meeting dates recorded: 2022: 01-31, 02-28, 03-28, 04-25, 05-30, 06-20, 08-29, 09-26,
  10-17, 11-28, 12-19; 2023: 01-30, 02-27, 03-27, 04-24, 05-29, 06-19, 08-28, 09-25,
  10-30, 11-27, 12-18; 2024: 01-29, 02-26, 03-25, 04-29, 05-27, 06-17, 08-26, 09-04,
  09-30, 10-28, 11-25, 12-16; 2025: 01-27, 02-24, 03-31, 04-28, 05-26, 06-16, 08-25,
  09-29, 10-27, 11-24, 12-15; 2026: 01-26, 03-30, 04-27, 05-25, 06-15.
- Spot-checked text of 6 PDFs (2022-01-31, 2023-01-30, 2024-06-17, 2025-06-16,
  2026-01-26, 2026-06-15): all are "SAMMANTRÄDESPROTOKOLL" headed "KOMMUNFULLMÄKTIGE"
  with the correct meeting date in "Plats och tid". Filenames are <fileid>_<n>_1.PDF
  (opaque, not dates).

### Tips for next round
- Re-run GET /api/notes/?from=...&to=...&board=271490&limit=100 (Kommunfullmäktige),
  then GET /api/notes/<id> per hit; skip 404s. Construct download URL as
  /api/notes/<file.id>/<file.name>. The whole flow is plain GET JSON — slim_http can
  drive it; no session/cookies needed.
- Dead ends: /api/notes/<note_id>/file (404); /api/files/<id> (returns SPA shell);
  any date-token URL template cannot work (opaque file ids).
