# bollnas.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — RESULT: nothing recorded (protocol library unreachable)
- Target: Bollnäs, kf, 2022-01-01..2026-08-20.

## Where the KF protocols live (the ONLY source)
- Entry: https://bollnas.se/kommun-och-politik/politik-och-demokrati/moten-och-protokoll
  -> subpage "Protokoll" (…/protokoll) says "När protokollen är justerade kan du ta del av
  dem via vårt protokollsbibliotek" and links to **https://protokoll.bollnas.se/** (external).
- Also linked from KF news items and the anslagstavla page. There is no other publication
  channel for KF protokoll: site search (https://bollnas.se/ovrigt/sok?query=...) returns
  only informational pages; sitemap (sitemap1.xml.gz, 2726 URLs) contains zero .pdf and no
  protocol pages; news archive (…/arkiv/nyheter) has no protocol posts (only kallelser and
  meeting notes). No evolutionTree/Evolution webapp is embedded on bollnas.se (checked raw
  HTML of the Protokoll page: webapps are only goToContent/textAdjuster/searchField/
  treeMenu/topMenu/relatedInformation etc.).

## protokoll.bollnas.se platform (from Wayback 2021 captures, all we have)
- "Bollnäs Kommun Protokollbibliotek", a VFM file manager (vfm-admin, app.min.js v3.7.5).
- Front end: SPA listing; directory browsing via `?dir=Start/`; AJAX endpoints under
  /vfm-admin/ajax/ (get-files.php, get-dirs.php, get-search.php, get-filetree.php,
  zip.php, sendfiles.php, vfm-move.php); download links look like `/?dl=<path>`.
- Uploads under /vfm-admin/_content/uploads/. Wayback CDX: only 2021 snapshots of the
  root (2021-07-15 .. 2021-12-09), NO captures 2022+ and no deep file captures.
- Anslagstavlan https://anslagstavlan.bollnas.se/#!/billboard/ (Netpublicator-style SPA)
  is a sibling on the same host.

## BLOCKER this run
- protokoll.bollnas.se and anslagstavlan.bollnas.se both resolve to **194.14.103.77**
  and are unreachable from the harvest environment: TCP connect timeout on ports 80 and
  443 (slim_http and Playwright, retried over ~40 min). bollnas.se itself resolves to
  185.84.52.17 and works. No alternative IP (checked dns.google A/AAAA; NXDOMAIN for
  medborgare./handlingar./diarium./dokument.bollnas.se).
- Wayback Machine: no captures of protokoll.bollnas.se between 2022-01-01 and 2026-12-31,
  so no archive route either.

## Run 2026-08-20 (second pass) — RESULT: nothing recorded again; blocker confirmed as site-down
- Re-verified today:
  - protokoll.bollnas.se and anslagstavlan.bollnas.se still unreachable (TCP timeout to
    194.14.103.77 on :80 and :443) from both slim_http and Playwright. Also no IPv6 route
    (ERR_ADDRESS_UNREACHABLE to 2001:67c:1784:2d6::99).
  - sjalvservice.bollnas.se (self-service portal) also resolves to 194.14.103.77 and times out.
  - Wayback CDX (web.archive.org/cdx/search/cdx): protokoll.bollnas.se* and
    anslagstavlan.bollnas.se* have NO captures 2022-2026 (last: 2021-12-09 / 2021-12-17).
  - Common Crawl index (index.commoncrawl.org, CC-MAIN-2022-05, -2022-49, -2023-50,
    -2024-10, -2025-30, -2026-30): zero captures of protokoll.bollnas.se* (404 "no results";
    wildcard validated against bollnas.se which returns hits).
  - Wayback Save Page Now (web.archive.org/save/…) for protokoll.bollnas.se returns
    **504/523 "target server didn't respond in time"** — i.e. IA's crawler cannot reach the
    host either. The protocol library is genuinely down, not just geo-blocked from us.
  - bollnas.se itself: sammanträdeskalender page is a static accordion of meeting dates
    (no links to protokoll); moteshandlingar-och-kallelser page has only kallelser
    (agendas, excluded); site search for "protokoll"/"kommunfullmäktige protokoll pdf"
    returns only informational pages; news archive months contain no "protokoll" links.
- CONCLUSION: no KF protokoll documents for Bollnäs are reachable in 2022-01-01..2026-08-20
  from this environment. Nothing recorded.

## Next run advice
- Re-check https://protokoll.bollnas.se/ (VFM `?dir=Start/` -> navigate to
  Kommunfullmäktige/2022..2026 folders -> `/?dl=<file>` PDFs). If it is back, walk the
  tree and download one protokoll PDF per KF meeting date; skip kallelser/föredragningslistor.
- If still down, try the anslagstavla (anslagstavlan.bollnas.se) which normally carries
  the justerings-tillkännagivande with a link to each protokoll, and the Wayback Machine
  again (IA had "temporarily offline" windows this run).
- bollnas.se download URLs for kallelser follow
  https://bollnas.se/download/18.<id>/<timestamp>/<name>.pdf (seen:
  F%C3%B6redragningslista%20VN%202026-08-19.pdf) — but those are agendas (excluded).
- Tip for a future run: SPN (Save Page Now) is confirmed NOT a workaround while the host is
  down; re-test SPN after the site is back, since SPN captures the rendered VFM file tree.
