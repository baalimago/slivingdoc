# uddevalla.se retrieval log (kf = Kommunfullmäktige)

## kf — round 1 (2026-08-20): 46 protocols recorded (2022-01-12 .. 2026-06-10)

- uddevalla.se is SiteVision CMS. KF archive page:
  https://www.uddevalla.se/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/kallelse-och-protokoll/kommunfullmaktiges-kallelse-och-protokoll.html
  (root -> Kommun och Politik -> Politik och demokrati -> Möten och protokoll ->
  Kallelse och protokoll -> Kommunfullmäktiges kallelse och protokoll).
- The page is a SiteVision "file-share" webapp. Current year sections (Kallelse
  2026 / Protokoll 2026) render inline; "Äldre" year folders are buttons that
  load via AJAX:
  https://www.uddevalla.se/appresource/4.7d70873f152fa01dc9b185a2/12.638fa37819a7ba190c74d31/files?folderId=<19.nodeid>&svAjaxReqParam=ajax
  (GET works with slim_http; returns JSON list of files/folders).
- The LIVE page only shows folders for 2025 (19.6ef3cbe819b354a3eddca4c) and
  2024 (19.79136937193decd7dd2170ba) under "Äldre protokoll" — older years are
  NOT linked. BUT the old folder ids still work on the same appresource
  endpoint and return the full file lists:
  - 2022 protokoll folder: 19.250d597318529f1c4b05208 (10 full protocols)
  - 2023 protokoll folder: 19.fbe1d18c8ad0decd9745 (10 full protocols)
  - 2022/2023 folder ids found via Wayback snapshots of the KF page
    (web/20230528215618 and web/20240522164534), which also link the same
    /download/ URLs. Wayback CDX (via browser; slim_http gets rate-limited 429)
    confirms the 2022/2023 URLs returned 200 when captured.
- KF meetings are monthly, ~10/year (Jan-Jun, Sep-Dec). Meeting dates for
  2022-2025 cross-checked against nyhetsarkiv "kommunfullmäktige sammanträder"
  articles; 2026 dates from sammanträdeskalender. No Jul/Aug meetings.
- Document URL pattern: https://www.uddevalla.se/download/18.<nodeid>/<unixms>/<URL-encoded filename>.pdf
  Filename carries the meeting date ("Kommunfullmäktiges protokoll YYYY-MM-DD.pdf").
- IMPORTANT quirks:
  - download_document fails on ALL these PDFs (HEAD -> 404, and the server
    later started soft-404ing direct GETs for the old 2022/2023 nodes during
    this run — likely session/anti-bot gating; 2026-06-10 direct GET was 200).
    The URLs are the canonical published links (live webapp + Wayback 200s).
  - One meeting = one protocol: skip "omedelbar justering"/"beslut §N"
    partial docs (e.g. 2022-12-14 §294 partial, 2023-06-14 §127 partial,
    2022-06-08 §122 beslut) and all Kallelse docs.
- Recorded 46 protocols: 2022 (10), 2023 (10), 2024 (10), 2025 (10),
  2026 (6, Jan 14..Jun 10; Sep-Dec 2026 after range end). Confidence 0.95
  (2024-2026, live-linked) / 0.85-0.9 (2022-2023, via file archive + Wayback).
- Dead ends: LEX diarium (lexpublish.uddevalla.se) has no KF subject area;
  Inblicken requires login; site search only indexes pages not PDFs.
