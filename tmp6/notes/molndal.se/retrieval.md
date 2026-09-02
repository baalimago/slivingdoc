# molndal.se retrieval log (Mölndals stad), kf = Kommunfullmäktige

## kf — round 1 (2026-08-20): SUCCESS, 41 KF minutes recorded (2022-02-16 .. 2026-06-17)

### Site structure
- molndal.se is SiteVision CMS. KF (Kommunfullmäktige) protocols are split across TWO
  sources:
  - 2022–2024 (and older): the "Mölndals protokoll 1971 och framåt" archive page
    https://molndal.se/kommun-och-politik/arkiv/historiska-protokoll/molndals-protokoll-1971-och-framat
    (reachable via Kommun och politik -> Arkiv -> Historiska protokoll, or from the
    "Möten, protokoll, kallelser" page footer "Äldre protokoll").
  - 2025+ (current): the web diarium at https://webbdiarium.molndal.se/ (JS SPA).
- The "Möten, protokoll, kallelser" page
  (https://molndal.se/kommun-och-politik/moten-protokoll-kallelser) has tables of
  meeting dates per body per year (2025, 2026 shown); KF dates link into the
  webbdiarium search URLs. It also states the archive covers "kommunfullmäktige och
  kommunstyrelsen 1971 och framåt" and the webbdiarium covers nämnder from 2025.

### Archive page (2022-2024 KF protocols) — SiteVision "Mapp" (folder) portlet
- The archive page has two folder buttons "Kommunfullmäktige 1971-" and
  "Kommunstyrelsen 1971-". The KF folder opens to year subfolders (1970..2024);
  year subfolder lists PDFs. All folder expansion happens client-side; the content
  is present in the DOM after clicking (no AJAX observed). Buttons are
  aria-label "<year>, öppna mapp"; "gå tillbaka till föregående mapp" returns to the
  parent list. Playwright click may fail with "element is outside of the viewport"
  — use page.evaluate(btn.click()) instead.
- Protocol PDFs are SiteVision downloads:
  https://molndal.se/download/18.<nodeid>/<unixms>/<filename>.pdf, filename =
  <YYYYMMDD>_protokoll_kf.pdf (one file is <date>_Protokoll_KF.pdf — 2022-10-19).
  Dates are the meeting dates embedded in the filename; verified content =
  "SAMMANTRÄDESPROTOKOLL / Kommunfullmäktige / Sammanträdesdatum <date>".
- Per-year folder contents (unique dates; main protocol only):
  - 2022 (9): 02-16, 03-16, 04-13, 05-18, 06-15, 09-21, 10-19, 11-30, 12-14
  - 2023 (9): 02-22, 03-15, 04-19, 05-24, 06-21, 09-20, 10-18, 11-22, 12-13
    (+ 20230419_protokoll_kf_83.pdf = same-day §-supplement, skip)
  - 2024 (9): 02-21, 03-20, 04-17, 05-15, 06-19, 09-18, 10-16, 11-27, 12-18
    (+ 20241127_protokoll_kf_192-193.pdf = same-day §-supplement, skip)
- No January KF meetings in 2022–2024 (first of year is mid/late Feb). No Jul/Aug.
- KF URL node prefixes: 2022 = 18.5ba77e9a19a7d80c6e0b90b..913, 2023 =
  18.707ac4a619b2bed27ae753f3..fc, 2024 = 18.49cdcf7319d43accaf824f18..f21.
  Timestamps 1763642443xxx (2025-11-20 upload), 1768204930xxx (2026-01-12),
  1775640041xxx (2026-04-08).

### Webbdiarium (2025-2026 KF protocols) — Ciceron SPA on webbdiarium.molndal.se
- SPA reads data via JSON-RPC POST https://webbdiarium.molndal.se/json
  (methods CiceronsokServer:Search / ReadItems / ReadObject / ReadObjectDetails /
  ReadArendeFiles). slim_http POST works; each POST returns a session_id, reuse it.
  Example full enumeration: Search doctype=1 (Möte) with
  param {"board":"Kommunfullmäktige","from_date":"2025-01-01","to_date":"2026-08-20"}
  -> 14 hits; ReadItems offset/limit lists them (title "Kommunfullmäktige <date>",
  object_link ?t=1&i=Kommunfullmäktige&d=<date> 00:00:00&n=KS).
- Meeting search URL: https://webbdiarium.molndal.se/#!/search/?t=1&i=Kommunfullm%C3%A4ktige&d=<date>%2000:00:00&n=KS
  renders the meeting with its files: "Kallelse ..." (agenda - skip), "Handlingar ..."
  (skip), "Tilläggslista ..." (skip), "Omedelbart justerat protokoll ..." (same-day
  partial - skip), and the full "Protokoll kommunfullmäktige <date>" (RECORD).
- Protocol document URL:
  https://webbdiarium.molndal.se/download/document?filename=<base64>&id=<numeric>
  filename base64-decodes to "<date>_protokoll_kf.pub.pdf" (e.g. id=5504 ->
  20260617_protokoll_kf.pub.pdf). The href in the DOM includes &session_id=... —
  DROP the session_id: GET works without it (server sets cicsoksid cookie itself).
- Verified all 14 URLs via Playwright page.request: 200 OK, application/pdf,
  content-disposition filename == <date>_protokoll_kf.pub.pdf. Content is a real
  PDF (%PDF-1.5; e.g. 2026-06-17 is 40 pages).
- KF meetings 2025 (9): 02-19, 03-19, 04-23, 05-21, 06-18, 09-17, 10-15, 11-19,
  12-10. KF meetings 2026 through 2026-08-20 (5): 02-18, 03-18, 04-22, 05-20,
  06-17. (Sep-Dec 2026 out of range.)

### Gotcha: download_document CANNOT verify webbdiarium URLs
- The webbdiarium server answers HEAD /download/document?* with 404 while GET
  returns 200 PDF (HEAD not implemented for that location). download_document does
  an existence check first, so it reports "definitively not found (404/410)" for
  every webbdiarium document URL. slim_http GET also refuses (binary PDF).
  Verify such URLs with Playwright page.request.get (200 + content-type +
  content-disposition), not with download_document. molndal.se /download/18.* URLs
  pass download_document normally.

### Recorded (41), date = meeting date, conf 0.95 archive / 0.9 webbdiarium
- 2022: 9 (02-16..12-14), 2023: 9 (02-22..12-13), 2024: 9 (02-21..12-18),
  2025: 9 (02-19..12-10), 2026: 5 (02-18..06-17). Total 41.
- Titles: archive = filename as listed; webbdiarium = "Protokoll kommunfullmäktige <date>".
- source_page: archive page for 2022-24; the meeting #!/search/ URL for 2025-26.

### Dead ends / tips
- The webbdiarium only holds KF meetings from 2024-09 onward (4 meetings 2024-09/10/11/12);
  it is NOT complete for 2022-2024 — use the archive for those years.
- Site search on molndal.se does not surface the protocol PDFs; don't rely on it.
- The KF organisation page (kommunfullmaktige) links "Protokoll och kallelser" ->
  webbdiarium and "Protokoll från 2018 och äldre" -> archive; no per-meeting pages.
- "Kallelse"/"Handlingar"/"Tilläggslista"/"Omedelbart justerat protokoll" files are
  agenda/notice or partial minutes — excluded.
