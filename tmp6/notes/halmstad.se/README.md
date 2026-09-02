# halmstad.se scanner notes (kf = Kommunfullmäktige)

## Where the minutes live
- Official page (live): https://halmstad.se/kommunochpolitik/politikochdemokrati/kommunfullmaktige/sammantradesdatumkallelsehandlingarochprotokollforkommunfullmaktige.n305.html
  publishes ONLY current year + one year back (2026 and 2025 as of 2026-08-20), via a
  SiteVision "file-share" webapp. "Söker du tidigare handlingar och protokoll?" points
  to contacting the kommun (older years removed from web).
- Diarium: https://diarium.halmstad.se/ (link "Gå till kommunens diarier" from
  https://halmstad.se/kommunochpolitik/diariumocharkiv/sokidiariet.n329.html). Ciceron-style
  SPA; JSON-RPC endpoint POST https://diarium.halmstad.se/json
  (CiceronsokServer:Search/ReadItems/ReadObjectDetails, session_id in responses; methods
  listed in https://diarium.halmstad.se/iciceronsok.js).
- Old years (2022..2024-09) are NOT on the live site and NOT (as combined protocols) in the
  diarium; only retrievable through the Wayback Machine archive of the OLD KF page
  https://www.halmstad.se/kommunochpolitik/politikochdemokrati/kommunfullmaktige/sammantradesdatumdagordninghandlingarochprotokollforkommunfullmaktige.n305.html
  (note: "dagordning" in the old URL; snapshots 2022-03-19 .. 2024-11-14).

## Site structure / file-share webapp
- KF page HTML embeds folder buttons for "2026/2025 års kallelser/handlingar" and
  "2026/2025 års protokoll". Expanding a folder calls
  GET https://www.halmstad.se/appresource/4.1fda7ccb1708b6c921b14d/12.78a524f319f0dc979ee5c66b/files?folderId=<folderId>&svAjaxReqParam=ajax
  returning JSON {"files":[{name,url,...}]}.
- Folder ids (live): 2026 kallelser 19.3b3bf89619b2bf35d30f1a9d, 2026 protokoll
  19.3b3bf89619b2bf35d30f1b3c, 2025 kallelser 19.4f6b3485193deb4f1671ac8, 2025 protokoll
  19.4f6b3485193deb4f1671aca. Old folder ids (2024: 19.503c370518c8ad3a0b67bc32 protokoll /
  ...bc36 kallelser; 2023: 19.2fb05371184a4946f592056d protokoll / ...568 kallelser; 2022:
  19.1ab5eabc17db948137815ad9 protokoll / ...ad7 kallelser) return 400 on the live appresource
  (folders deleted); only visible in Wayback snapshots.
- Protocol PDF naming convention: YYMMDD-kf-protokoll.pdf (sometimes "-gdpr" suffix), e.g.
  "260212-kf-protokoll.pdf", "25061617-kf-protokoll-gdpr.pdf" (16-17 June 2025 meeting).
  Download URL shape: https://www.halmstad.se/download/18.<hex>/<ts>/<filename>.pdf

## Diarium specifics
- Diaries via CiceronsokServer:ReadDiaries: KS = "Kommunfullmäktige/kommunstyrelsen" with
  instances incl. "Kommunfullmäktige" (board value for search param).
- Search: CiceronsokServer:Search {"search_id":"x","doctype":64,"text":"","param":"{\"hasFiles\":false,\"diary\":\"KS\",\"board\":\"Kommunfullmäktige\",\"from_date\":\"...\",\"to_date\":\"...\"}"}.
  doctype 64 (Sammanträdesprotokoll) and 1 (Möte) return 0 hits — meeting objects are NOT
  indexed here (unlike ostersund's diarium). Use doctype 4 (Handling) with text e.g. "kf-protokoll".
- "kf-protokoll" text search (doctype 4, board Kommunfullmäktige) -> 17 hits, all titled
  "YYMMDD-kf-protokoll", covering 2024-10-24 .. 2026-06-16 (241024, 241121, 241212, then all
  of 2025, then all of 2026 to date). ReadObjectDetails shows files[] EMPTY for nearly all of
  them (only 251211 has a file: id 599321, filename 251211-kf-protokoll.signerad.pub.pdf);
  the diarium is thus a register, not the source of PDF bytes. PDFs live on halmstad.se.
- Older years' diarium content: only per-§ "Protokollsutdrag ... KF §N" documents and
  "Kallelse KF"/"YYMMDD-kf-kallelse" entries; no combined protocols for 2022, 2023, 2024-01..09.

## Wayback findings (old KF page snapshots)
- 2022-03-19 snapshot: 220217-kf-protokoll.pdf (2022-02-17) +
  https://www.halmstad.se/download/18.2c1c1ff017db93c146b44fd0/1645468178452/220217-kf-protokoll.pdf
- 2022-05-19: 220428-kf-protokoll-gdpr.pdf
- 2022-06-16: 220525-kf-protokoll.pdf (+ "220525-kf-protokoll OJ.pdf" immediate-justering variant - skip)
- 2022-09-11: 220616-kf-protokoll.pdf
- 2022-11-28: 221025-kf-protokoll.pdf
- CDX: 221017-kf-protokoll.pdf captured 2023-08-19 (2022-10-17 meeting)
- 2023-05-12: 230427-kf-protokoll.pdf
- 2024-02-23: 240215-kf-protokoll.pdf (also CDX 2024-04-15/05-18)
- 2024-05-18: 240425-kf-protokoll-gdpr.pdf
- 2024-11-14: 241024-kf-protokoll.pdf
- Wayback PDFs actually captured (200) and retrievable via <ts>if_ URL: 220217, 221017,
  240215, 25061617. The others exist only as links in archived pages; original URLs now 404.
- Wayback CDX: https://web.archive.org/cdx/search/cdx?url=halmstad.se&matchType=domain&filter=original:.*kf-protokoll.* (browser fetch; slim_http returns empty).

## Recorded (24 KF combined protocols, one per meeting date)
- 2025 (9) and 2026 (5): live halmstad.se /download/... URLs from the file-share folders
  (confidence 0.97). Source page = live KF page.
- 2022-02-17, 2022-10-17, 2024-02-15: Wayback if_ URLs (PDFs verified) (0.92).
- 2022-04-28, 2022-05-25, 2022-06-16, 2022-10-25, 2023-04-27, 2024-04-25, 2024-10-24:
  original halmstad.se download URLs found on archived KF pages; files now dead (0.6).
- NOT found anywhere (no URL): 2022-03-31, 2022-09-29, 2022-11-24, 2022-12-13, 2023-02-16,
  2023-03-30, 2023-05-25, 2023-06-14/15, 2023-09-21, 2023-10-26, 2023-11-23, 2023-12-14,
  2024-03-21, 2024-05-23, 2024-06-17/18, 2024-09-26, 2024-11-21, 2024-12-12 (2024-11-21 and
  2024-12-12 exist as file-less diarium registry entries only).
