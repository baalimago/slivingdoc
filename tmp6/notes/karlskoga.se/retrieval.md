
# karlskoga.se retrieval log

## kf (Kommunfullmäktige) — round 1 (2026-08-20)
- Target: Karlskoga, meeting type kf, range 2022-01-01..2026-08-20.
- Site: karlskoga.se is SiteVision. KF page:
  https://karlskoga.se/kommun--politik/politik-och-demokrati/kommunfullmaktige.html
  has an accordion "Protokoll" (button) containing a folder file-list
  (Mapp 2025, Mapp 2026). Each folder lists PDFs; download URL shape
  https://karlskoga.se/download/18.<nodeId>/<unix-ms>/<Filename>.pdf
  (opaque nodeId+timestamp, no date template possible).
- Page text: "Kommunfullmäktiges protokoll från och med den 1 januari 2025
  finns publicerade... Äldre protokoll begärs ut via kommunstyrelsen@karlskoga.se".
  So the live site only publishes KF protocols 2025-01-01 onward; earlier
  ones are NOT online on karlskoga.se.
- Folder 2025 contains 7 protocols: KF 2025-03-25, 04-22, 06-10, 09-16,
  10-14, 11-11, 12-09. Folder 2026 contains 5: 2026-02-03, 2026-03-03,
  2026-04-28 (named date-only), KF 2026-05-26, KF 2026-06-09 inkl.
  protokollsanteckning. No 2025-01-28/2025-02-25 protocol published.
- Diarium: https://diariet.karlskoga.se/#!/search/ (Ciceron SPA; JSON-RPC
  POST https://diariet.karlskoga.se/json). Methods: ReadHotspots,
  ReadDiaries, Search(search_id, doctype, text, param={"hasFiles","diary",
  "from_date","to_date"}), ReadItems(search_id, offset, limit),
  ReadObjectDetails(search_id, id), ReadObject (needs search_id AND param).
  Session id returned per call. Diariet only indexes from 2025-01-01
  ("I diariet kan du söka efter handlingar från och med 1 januari 2025").
- Search: doctype=1 text=Kommunfullmäktige diary=KS -> 8 meetings
  (2025-01-28, 2025-03-25, 2025-09-16, 2025-10-14, 2025-11-11, 2026-03-03,
  2026-04-28, 2026-05-26) — incomplete index; meetings like 2026-02-03 and
  2026-06-09 have protocols on the site but aren't returned.
  ReadObjectDetails on meeting 2025-01-28 -> items have files:[] (no
  protocol attached). So early-2025 protocols are absent from diarium too.
- doctype=4 text="KF" diary=KS -> 148 Handling hits: mostly per-§
  "Protokollsutdrag KF ..." plus combined protocols "Protokoll KF YYYY-MM-DD
  §§ a-b" / "Kommunfullmäktiges protokoll KF ..." (only 2025-10-14,
  2025-11-11, 2026-02-03, 2026-04-28 found). Same documents as the site
  folder; no extra combined KF protocols in diarium.
- 2022-2024 KF protocols: NOT on live site/diarium. Wayback Machine check:
  CDX for karlskoga.se domain, filter original:.*protokoll.* and
  original:.*KF.* -> only ONE KF protocol captured in 2022-2024 window:
  KF 2022-03-01.pdf (captured 2022-03-20) at
  https://www.karlskoga.se/download/18.3f25bdf217f4c6187ae2822/1646924741431/KF%202022-03-01.pdf
  Replay URL (verified, application/pdf, 214KB, header "Sammanträdesprotokoll
  Kommunfullmäktige 2022-03-01"):
  https://web.archive.org/web/20220320101841if_/https://www.karlskoga.se/download/18.3f25bdf217f4c6187ae2822/1646924741431/KF%202022-03-01.pdf
  (NOTE: use the if_ variant; the plain /web/... URL returns the Wayback
  toolbar HTML shell). Old site (2022 capture) had "Protokoll 2022/2021/..."
  folders on the KF page, but the folder listings are dynamic
  (?folder=...&sv.url=...) and were never archived (404); individual
  protocol PDFs from 2022-2024 are otherwise not in the Wayback.
- RECORDED 13 KF sammanträdesprotokoll: 2022-03-01 (Wayback), 2025-03-25,
  2025-04-22, 2025-06-10, 2025-09-16, 2025-10-14, 2025-11-11, 2025-12-09,
  2026-02-03, 2026-03-03, 2026-04-28, 2026-05-26, 2026-06-09. Confidence
  0.95 (site) / 0.9 (Wayback). Source page for site docs:
  https://karlskoga.se/kommun--politik/politik-och-demokrati/kommunfullmaktige.html
- Not recorded: Kallelse PDFs (agenda/notice), "Inkomna handlingar KF ...",
  per-§ Protokollsutdrag (not the full minutes), partistöd redovisning.
- Tip for next round: re-open the "Protokoll" accordion on the KF page and
  read the Mapp 2025/2026 lists; no URL template possible. For 2022-2024,
  only the Wayback PDF (2022-03-01) exists online; everything else would
  need the city archive / e-post request.

## kf (Kommunfullmäktige) — round 2 (2026-08-20) verification
- Re-verified the full document set; result identical to round 1, 13 KF
  protocols recorded again (all accepted). No new documents.
- Site folder Mapp 2025 (7 PDFs, dates 2025-03-25..2025-12-09) and Mapp
  2026 (5 PDFs, 2026-02-03..2026-06-09) unchanged. The 2026 sammanträdestider
  list shows no meeting between 2026-06-09 and 2026-08-20 (8 sep inställt,
  next 6 okt), so nothing expected.
- Downloaded + text-verified 4 PDFs as KF sammanträdesprotokoll:
  KF-2022-03-01 (Wayback, §§24-51), KF-2025-03-25 (§§28-47),
  2026-02-03 (§§1-15), KF-2026-06-09 inkl. protokollsanteckning (§§64-76).
- CDX via Playwright (slim_http got 429/504 timeouts on archive.org; use
  browser page.goto for CDX). Queries run:
  - url=karlskoga.se/download* & url=www.karlskoga.se/download*, 2022-2024,
    filter original:.*KF.* -> only KF 2022-03-01.pdf (200, application/pdf).
    Others: Kallelse KF 2019/2022 (404 or agenda), "Ärende N. Inkomna
    handlingar KF ..." (received-docs, skip), KF 2021-06-22 (out of range).
  - filter original:.*rotokoll.* 2022-2024 -> only non-KF bodies (KS, BUN,
    SBN, VN, FHN, SvFR, Tillsyn etc). No KF.
  - filter original:.*fullmaktige.* 2022-2024 -> news/kungörelse pages and
    the dynamic folder URLs (https://karlskoga.se/kommun--politik/.../
    kommunfullmaktige.html?folder=19....&sv.url=...) captured 2024-09-15;
    raw (id_) HTML of those captures has NO protocol PDF links (folder
    portlet was never archived). 2025-2026 KF filter -> only a reglemente.
  - Conclusion stands: for 2022-2024 only KF 2022-03-01 is available online.
- Diarium re-check via JSON-RPC (POST https://diariet.karlskoga.se/json,
  content_type application/json; session from ReadHotspots). Search
  doctype=4 text=KF diary=KS hasFiles=true from 2022-01-01 to 2026-08-20 ->
  148 hits, ReadItems(limit=150) shows: combined protocols only for
  2025-10-14 (§§106-121), 2025-11-11 (§§122-134), 2026-02-03 (§§1-15),
  2026-04-28 (§§31-47) — exactly the same 4 PDFs as the site folder; every
  other hit is a per-§ "Protokollsutdrag KF ..." (excerpts, NOT the minutes).
  No 2025-01-28/2025-02-25 combined protocol anywhere.
- Extra noise seen in diarium hits: "Reglemente SvFR (Fastställt KF 41
  2025)", KFN/KS/SAU protokollsutdrag mentioning KF in the title — skip.
