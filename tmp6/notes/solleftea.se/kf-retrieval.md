
# Sollefteå (solleftea.se) - KF retrieval notes

Target: Sollefteå kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- SiteVision site. Main entry for meetings:
  https://solleftea.se/Kommun--politik/kallelser-protokoll-och-sammantraden
  ("Kallelser, protokoll och sammanträden"). Accordion sections: Kallelser,
  Protokoll, Sammanträdesplan 2026, WebbTV.
- Protokoll section has sub-accordions: "Kommunfullmäktige protokoll",
  "Kommunstyrelsens protokoll", "Kommunstyrelsens utskott", "Nämnder",
  "Delegationer och råd".
- KF protokoll file portlet id: svid12_499760a71853e28aa2a1a334. Currently it
  renders the files of the 2026 folder DIRECTLY (no year subfolders). Files are
  server-rendered in the DOM (slim_http can see them; no XHR).
- KF kallelser portlet id: svid12_499760a71853e28aa2a1a336. Shows year folders
  "KF Kallelser 2026..2019" as folder= links; each folder still works, e.g.
  ?folder=19.61b51ad41938db4554652a&sv.url=12.499760a71853e28aa2a1a336 -> KF
  kallelser 2025 (agendas - EXCLUDED per task).
- Download URL pattern: https://solleftea.se/download/18.<nodeid>/<ts>/<name>.pdf

## KF protocols found (only 2026 published on live site)
The "Kommunfullmäktige protokoll" portlet currently holds exactly 8 files, all
2026: 3 presidium minnesanteckningar (NOT KF minutes, skip), 3 main KF
protocols, 2 "direktjustering" single-§ files (same meeting dates as main -
skip, one meeting = one set of minutes). Recorded:
- KF protokoll 2026-02-23.pdf (meeting 2026-02-23) - recorded
- KF protokoll 2026-04-27.pdf (meeting 2026-04-27) - recorded
- KF protokoll 2026-06-22, §§ 43-66, 68-76.pdf (meeting 2026-06-22) - recorded
All three verified by extraction: header PROTOKOLL, Nämnd/styrelse
Kommunfullmäktige, sammanträdesdatum matches. 2026-05-25 sammanträde appears
cancelled (June protocol starts at §43 right after April's §11-42).
Not recorded: "KF protokoll 2026-02-23 § 3 - direktjustering.pdf" and
"KF protokoll 2026-06-22 §67 direktjustering.pdf" (same-day supplements).

## BLOCKER: 2022-2025 KF protocols are GONE from the live site
- The KF protokoll portlet used to show YEAR FOLDERS (Wayback captures
  2025-08-11 and 2026-06-10 show "KF protokoll 2025/2024/2023/2022/..." with
  folder node IDs e.g. 2025=19.61b51ad41938db4554652e, 2024=19.7a78a6818d5ab9b0deefe,
  2023=19.33e2398d185cf941b951d14, 2022=19.499760a71853e28aa2a164e).
- On the live site (2026-08-20) those folder URLs now all fall back to the 2026
  files -> the KF protokoll year folders are deleted/unlinked. The folder-param
  mechanism itself still works (proven: KF Kallelser 2025 and KS protokoll 2025
  folder= params return their files), so the KF year folders are genuinely gone.
- No alternative source for 2022-2025 KF minutes: anslagstavla document pages
  (e.g. .../anslagstavla/dokument/2026-04-30-protokoll---kommunfullmaktige-2026-04-27)
  contain only notice metadata (Organ, datum, paragrafer, sekreterare) and NO
  PDF attachments; Wayback never captured the KF protokoll folder pages nor any
  KF /download/ PDFs; Common Crawl index has no KF protocol PDFs; site search
  "KF protokoll 2025" returns only the page itself + an unrelated plan PDF.

## 2nd run additions (2026-08-20, same harvest date as note above)
- Re-verified live site: KF protokoll portlet = 8 files (all 2026) - recorded
  the 3 main protocols again (see above).
- Wayback deep-dive on kallelser-protokoll page captures (timemap has ~36
  mementos 2020-09..2026-06):
  * OLD structure Jan-Aug 2022: page had SUBPAGES
    .../kallelser-protokoll-och-sammantraden/kallelser and .../protokoll.
    The protokoll subpage had /protokoll/kommunfullmaktige with its own year
    folders (KF protokoll 2022 folder=19.652cec21800277a012a48&sv.url=12.1a0ce126173bee4b6481dba).
    Those folder= URLs were never captured either (timemap empty).
  * ONLY capture showing KF protocol files directly: 2022-09-29
    (web.archive.org/web/20220929234950/...). It lists 4 KF protocols:
    - KF protokoll 2022-02-28.pdf  (18.5ffed5c117f6d4f6a1da3/1646835586290)
    - Protokoll kommunfullmäktige 2022-04-25.pdf (18.652cec21800277a012c4a/1651501260969)
    - Protokoll kommunfullmäktige 2022-05-30 signerat.pdf (18.53fab69a180ee89fa5efdb/1654839765817)
    - KF protokoll 2022-06-20.pdf (18.53fab69a180ee89fa5e1b7f/1663251630372)
    BUT: the PDFs themselves were never fetched by Wayback (web.archive.org/web/
    20220929234950/<that-url> -> 404) AND the same URLs on the live site are now
    404 too. So no recoverable 2022 PDFs.
  * All later captures (2023-01-31, 2023-05-28, 2024-09-22, 2025-08-11,
    2026-06-10) show year folders only (no direct KF files; folder content
    lazy/AJAX, not in archived HTML).
- Common Crawl CDX API unreachable from this env (404 on all index queries);
  previous run noted no KF protocol PDFs in CC anyway.
- Site search (Rekai, JS app at /om-webbplatsen/sok) "KF protokoll" -> 10 hits,
  none are KF minutes (only reglementen and the page itself). Playwright needed;
  direct navigation to solleftea.se times out at domcontentloaded but page loads
  after ~60s - retry/navigate then snapshot works.

## Tips for next runs
- Fetch the page with slim_http, filter required_tokens ["/download/","KF"] to
  get the current KF protocol files directly.
- If the municipality re-adds KF protokoll year folders (or restores the old
  folder= node IDs), record one main protocol per meeting date for 2022-2025.
- Old 2022-era subpages (…/protokoll/kommunfullmaktige etc.) now 404.
- Wayback folder= URL timemaps for the KF year folders are empty - not a path.
