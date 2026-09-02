# botkyrka.se KF retrieval notes (kf = Kommunfullmäktige)

Harvest run: 2026-08-22, range 2022-01-01..2026-08-22. RESULT: 45 KF protocol
documents recorded (15 on botkyrka.se old archive for 2022 + 2023-01..08; 30 on
opengov.360online.com for 2023-09..2026-06).

## Site structure - two archives
Main site is SiteVision. "Möten, handlingar och protokoll"
(https://botkyrka.se/kommun-och-politik/politik-och-organisation/moten-handlingar-och-protokoll)
splits into:
- "Nyare handlingar och protokoll" -> "Insyn i politiken" =
  https://opengov.360online.com/Meetings/BOTKYRKAkommun (OpenGov 360online
  platform). Covers nämnd meetings from 2024-01 and KF/KS meetings from 2023-09.
- "Äldre möten, handlingar och protokoll" =
  .../aldre-moten-handlingar-och-protokoll : nämnd meetings before 2024-01 and
  KF/KS meetings before 2023-09 (the opengov 2023 KF board also lists the whole
  of 2023 including Jan-Aug, but the official split puts pre-Sep-2023 on the old
  page; we recorded old-archive URLs for those dates).

## Old archive (botkyrka.se /download/... URLs)
Page has per-year sections with tabs "Handlingar" (full meeting packet = agenda,
skip) and "Protokoll" (the minutes). Protocol files are named
"Kommunfullmäktiges protokoll YYYY-MM-DD signed.pdf" under
https://botkyrka.se/download/18.<nodeid>/<timestamp>/<file> - direct PDF GET works
with download_document. slim_http with required_tokens ["Kommunfullmäktige"] lists
everything. Kungörelse files (notices) and Handlingar packets (agenda) skipped.
KF dates 2022 (10): 01-27, 02-24, 03-31, 04-28, 05-24, 06-21, 09-29, 10-27,
11-24, 12-15. KF dates 2023 Jan-Aug (5): 01-26, 02-23, 03-30, 04-27, 06-20
(2023-05-25 was Inställt/cancelled - only a Kungörelse exists).
Gotcha: one Handlingar file in the 2023 tab is mislabeled
"Kommunfullmäktige 2022-03-30.pdf" but is actually the 2023-03-30 meeting packet
(upload ts 2023-03-30); the protocol tab confirms a real 2023-03-30 meeting.
Gotcha: 2022-10-27 protocol is split into TWO files: "§ 107" (omedelbar justering,
3 pages) and "§§ 106, 108-123" (main, 28 pages). Recorded the main §§106,108-123
file (conf 0.8); the §107 partial is skipped. 2022-05-24 has 7 "Del" Handlingar
parts - skip; single signed protocol exists.

## opengov.360online.com (OpenGov 360online, sv locale)
- KF boards per year: /Meetings/BOTKYRKAkommun/Boards/Details/203998 (2023),
  205175 (2024), 206094 (2025), 206684 (2026). Board page lists all meetings for
  the year server-side (slim_http fine; count in "N möten hittades"; Inställt /
  Inte publicerat entries are listitems without links).
- Meeting detail: /Meetings/BOTKYRKAkommun/Meetings/Details/<id>. "Dokument"
  panel lists Kungörelse (skip) and Protokoll. Protocol PDF:
  /Meetings/BOTKYRKAkommun/File/Details/<fileid>.PDF?fileName=<name>&fileSize=<n>
  (GET 200 application/pdf; download_document works). Some 2026 ids use .pdf
  lowercase - fine.
- Meeting ids -> dates (all recorded): 2023: 340345=09-28, 340347=10-26,
  379633=11-06 (extra budget), 340354=11-23, 381976=12-07; 2024: 378997=02-01,
  378998=03-05, 379002=04-25, 379003=05-30, 405858=06-12, 379008=09-26,
  379011=10-24, 379012=11-28, 379013=12-19; 2025: 420821=01-30, 420822=03-04,
  420824=03-27, 420827=04-24, 420830=05-22, 420836=06-17, 420839=09-25,
  420842=10-23, 420845=11-27, 420847=12-18; 2026: 463910=01-29, 463912=03-05,
  463914=03-26, 463917=04-23, 463918=05-28, 463920=06-16.
- Cancelled (Inställt, no protocol - record nothing): 2023-05-25, 2023-12-14,
  2024-03-26, 2024-06-18, 2024-12-05. 2026 future (Inte publicerat, after range
  end): 09-24, 10-22, 11-26, 12-17.
- OJ partials to skip: 2024-06-12 has §74 OJ (484039) + §83 OJ (484282 is main);
  recorded main 484282. 2024-10-24 has §120 OJ (504422); recorded main 504595.
  2024-11-28 has TWO full protocol files (510778 677KB vs 510785 3MB); recorded
  the larger 510785 (same §§124-132, larger one carries more content/attachments).
- Confidence 0.95 for all opengov + old archive; 0.8 for 2022-10-27 split file.

## Recorded
45 KF minutes: 2022 x10, 2023 x10 (5 old + 5 opengov), 2024 x9, 2025 x10,
2026 x6 (through 2026-06-16; no later meeting before 2026-08-22). Agenda/notice
docs (Handlingar packets, Kungörelse, Kallelse) skipped.
