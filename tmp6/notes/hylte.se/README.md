# hylte.se scanner notes (Hylte kommun)

## Where KF (kommunfullmäktige) minutes live — two sources split by year
- Current-year (2026) meeting documents are on the MeetingPlus/Formpipe portal:
  **https://meetings.hylte.se** (reached via hylte.se -> Kommun och politik -> Möten
  och protokoll -> "Till meetings här", also the "Kallelser, handlingar" link).
- Previous years (2022-2025) are in a file archive on hylte.se under
  "Möten och protokoll" -> "Äldre protokoll"
  (https://www.hylte.se/kommun-och-politik/moten-och-protokoll/aldre-protokoll),
  section "Protokoll 2021-2025" -> Kommunfullmäktige, with per-year folders
  "Protokoll 2021/2022/2023/2024/2025". Protocols older than 5 years are not online.

## Meeting list (portal)
- Committee page: https://meetings.hylte.se/committees/kommunfullmaktige
  lists ALL meetings server-side (Kommande + Tidigare, no pagination). Meeting URLs:
  https://meetings.hylte.se/committees/kommunfullmaktige/mote-YYYY-MM-DD
- KF meeting dates in 2022-2026-08-20: 2022: 02-17,05-05,06-16,09-01,10-20,11-15,
  12-07,12-08; 2023: 03-16,05-02,06-13,09-12,10-17,11-14,12-12; 2024: 03-12,05-07,
  06-13,09-10,10-22,11-19,12-10; 2025: 03-04,05-06,06-03,09-16,10-21,11-11,12-16;
  2026: 01-27,03-03,05-05,06-02 (33 total).

## Portal protocol links
- On each meeting page the protocol tab has "Öppna protokoll" ->
  URL pattern /committees/kommunfullmaktige/mote-<date>/protocol/<slug>?downloadMode=open
  but the slug varies (e.g. 2026-06-02 "protokoll-kf-20260602pdf", 2026-01-27
  "protokoll-kommunfullmaktige-27-januari-2026pdf"). Discover per meeting by fetching
  the meeting page with slim_http and filtering links for token "protocol".
- "Öppna protokoll" is DISABLED ("Inget protokoll har publicerats") for 2022 meetings
  (and presumably 2023-2025): those minutes are only in the hylte.se file archive.
- Kallelse (agenda) links live under /agenda/<slug>?downloadMode=open — skip those.

## File archive (hylte.se) protocol folder API
- The aldre-protokoll page embeds AppRegistry.registerInitialState with the KF file
  share module (svid12_571e1c6a19ed441d76b51e2f) listing folder ids per year:
  Protokoll 2022 = 19.8cccb0a187da6d9beb6e3, 2023 = 19.2e972c5318d1d00215ed50,
  2024 = 19.50abecd193df1e4e1512df7, 2025 = 19.150b299d19b355c6fee1ecea.
- Folder contents (JSON) via:
  GET https://www.hylte.se/appresource/4.8cccb0a187da6d9beb129c/12.571e1c6a19ed441d76b51e2f/files?folderId=<folderId>&svAjaxReqParam=ajax
  (page id 4.8cccb0a187da6d9beb129c, module id 12.571e1c6a19ed441d76b51e2f).
  Returns files[] with name "Protokoll KF YYYY-MM-DD.pdf" and url
  https://www.hylte.se/download/<id>/<ts>/<name>. Downloadable directly, no auth.
- Each year's folder has exactly the 7-8 KF protocols matching the portal meeting list.
  2022 also contains "Rättelse § 158 KF - Val till tillsynsnämnden..." (a correction,
  not minutes — skipped).

## KF harvest result 2022-01-01..2026-08-20
- Recorded 33 protocol PDFs (one per meeting date): 2022-2025 from the hylte.se file
  archive URLs, 2026 (01-27, 03-03, 05-05, 06-02) from the meetings portal protocol
  URLs. All verified as MÖTESPROTOKOLL / Kommunfullmäktige with matching dates.
- 2026-09-08 meeting exists in portal but is outside the range (after 2026-08-20).

## Dead ends
- meetings.hylte.se/api/v2.0/meetings/<id> returns 404 (no JSON meeting details API;
  only /api/v2.0/meetings/<id>/download/Agenda|Protocol "Ladda ner allt" zips exist).
- The file archive folder .html URLs (e.g. .../protokoll2022.19.8cccb0a187da6d9beb6e3.html)
  404 when fetched directly; use the appresource JSON endpoint instead.
