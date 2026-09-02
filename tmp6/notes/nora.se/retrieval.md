# nora.se scanner notes (kf = Kommunfullmäktige)

## Round 1 (2026-08-20): SUCCESS — 39 KF protocol PDFs recorded (2022-01-01..2026-08-20)

### Site structure
- nora.se is SiteVision CMS (kommun.nora.se is just an HTTP alias of the same
  site, canonical to www.nora.se; no separate old portal).
- KF page: https://nora.se/kommunpolitik/denpolitiskaorganisationen/kommunfullmaktige.4.539d1c491275e7d0564800016704.html
  (reachable: Kommun & politik -> Den politiska organisationen -> Kommunfullmäktige)
- Under the "Protokoll" heading the page lists year folders 2026..2021. Folder
  contents are server-rendered; navigation via URL param
  ...html?folder=<nodeId>&sv.url=12.313755fd19c47cb0457bac
  - Protokoll 2026 folder=19.4cc0067f19bb75ba97b8f62
  - Protokoll 2025 folder=19.3e8b4488194b1a376fa1663
  - Protokoll 2024 folder=19.320fce1318bfb1f88bc3126
  - Protokoll 2023 folder=19.5139e120184f66615bb7cb
  - Protokoll 2022 folder=19.2121725917e57a6b10227ca
- slim_http with required_tokens ["download"] on a folder URL returns the whole
  file listing (plus nav css/js). PDF URLs:
  https://nora.se/download/18.<nodeId>/<unix-ms>/<Filename>.pdf — opaque ids,
  no date template; take href exactly as listed.
- "Dagordning och aktuella handlingar" section on the same page holds current
  meeting documents (föredragningslista, motions, interpellations...) — NOT
  minutes; skip.
- Protokollsarkiv page (/kommunpolitik/denpolitiskaorganisationen/protokollsarkiv.*)
  only holds old utskott (social-/barn- och ungdoms-/samhällsbyggnads-/lednings-
  utskottet, active until 2022) — no KF material.
- Diarium & arkiv page has no public search; archive by e-post/beställning.

### Recorded (39 KF sammanträdesprotokoll, all https://nora.se/download/18.*)
- 2022 (7): 03-02 (KF 2022-03-02.pdf, starts §1 => first meeting of year),
  04-27, 06-07, 09-21, 10-26 (konstituerande after 2022-09-11 election),
  11-16, 12-07.
- 2023 (8): 02-22, 04-12, 05-10, 06-14, 09-20, 10-25, 11-29, 12-21.
- 2024 (9): 01-24 (ink. bilagor), 02-28, 04-17, 05-22, 06-12, 09-18, 10-23,
  11-20, 12-11 (Bortredigerad). NOTE: folder lists 2024-05-22 twice with two
  node ids (18.2ef08f9f18fa4230b0650b3/1717140391327 and ...0b4/1717140391333,
  same size 390.8kB) — same doc stored twice; recorded only the first.
- 2025 (10): 01-22, 02-12, 03-19, 04-23, 05-28, 06-18, 08-27, 09-17, 10-22,
  11-26. (No December 2025 protocol in folder; anslagstavla kungörelse only
  confirms the 11-26 meeting. 2026 first meeting is 01-14.)
- 2026 (5, all before 2026-08-20): 01-14, 02-18, 03-25, 04-29, 06-03. Matches
  the sammanträdestider 2026 table (next: 09-16 — out of range).
- Confidence 0.97. Content-verified by text extraction: every file is
  "SAMMANTRÄDESPROTOKOLL / Kommunfullmäktige / <date>".
- Source page for all: the KF page URL above.

### Dead ends / tips
- Site search (/omwebbplatsen/sok.*) indexes the protocol folders poorly
  ("Protokoll KF" -> 1 hit, the KF page itself). Use the folder URLs directly.
- The 2022-03-02 protocol begins at §1, confirming no Jan/Feb 2022 KF meeting
  was missed.
- kommuna anslagstavla shows kungörelser/kallelser (agenda/notice) — do not
  record; only useful to cross-check meeting dates.
