# Stockholm (stockholm.se) - KF retrieval notes

Target: Stockholms stad, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- Main site stockholm.se -> "Kommun och politik". Meeting documents are NOT on
  stockholm.se itself; they live on the MeetingPlus-by-Formpipe installation
  "Stockholms Stad eDok Meetings":
  - https://edokmeetings.stockholm.se (entry; /overview, /committees,
    /digital-bulletin-board) and mirror host https://meetingspublic.stockholm.se
    (the URLs the committee pages actually link to).
- KF committee page (canonical listing, all years on one page, newest first):
  https://meetingspublic.stockholm.se/committees/kommunfullmaktige
  - slim_http shows the entire list server-side (tabs Kommande/Tidigare;
    "Tidigare" grouped by year 2026..2021). Meeting links are date-based slugs:
    /committees/kommunfullmaktige/mote-<YYYY-MM-DD> (e.g. mote-2026-06-15).
- Meeting page: /committees/kommunfullmaktige/mote-<date> has Kallelse + Protokoll
  tabs; protocol PDF links are on the same page:
  .../mote-<date>/protocol/<slug>?downloadMode=open (GET returns application/pdf;
  use with required_tokens ["/protocol/"] to isolate protocol links). Also
  API /api/v2.0/meetings/<id>/download/Protocol?downloadMode=download (zip of all
  protocol docs; meeting ids opaque, not derivable).

## KF minutes recorded (70, dates below), conf 0.95
One per meeting date = the signed full protocol "(Signerad) Protokoll KF <date>.pdf"
(or "(Signerad) Kommunfullmäktiges protokoll <date>.pdf" 2025-01..2026-05 period).
Exceptions: 2022-01-31 and 2022-02-21 have the signed protocol SPLIT into two
§-range PDFs -> recorded the single complete "kommunfullmäktiges protokoll med
debatt <date>.pdf" instead (verified complete §§1-28 / §§1-42).
2022: 01-31*, 02-21*, 03-14, 04-04, 04-25, 05-09, 05-30, 06-13, 10-03, 10-17,
11-07, 11-28, 12-12, 12-13, 12-19 (15; * = med debatt)
2023: 01-30, 02-20, 03-06, 03-20, 04-24, 05-08, 05-29, 06-19, 09-04, 09-25,
10-16, 11-06, 11-22, 11-23, 11-27, 12-11 (16)
2024: 01-29, 02-19, 03-11, 03-25, 04-22, 05-06, 05-27, 06-17, 09-02, 09-23,
10-14, 11-04, 11-18 (dag 1 budget), 11-19 (dag 2 budget), 12-02, 12-16 (16)
2025: 01-27, 02-17, 03-10, 03-24, 04-07, 05-05, 05-26, 06-16, 09-01, 09-22,
10-13, 11-03, 11-18, 11-19 (budget), 12-01, 12-15 (16)
2026: 01-26, 02-16, 03-09, 04-13, 05-04, 05-25, 06-15 (7)
(2026-09-21 and 2026-10-19 are upcoming, after range end, no protocols.)
Content verified by download+text for 2026-06-15, 2026-01-26, 2025-06-16,
2025-11-19, 2024-06-17, 2023-05-08, 2022-12-19, 2022-10-17, 2022-01-31,
2022-02-21: all "Protokoll fört vid Stockholms kommunfullmäktiges sammanträde...".
source_page = the meeting page URL for each.

## Notes / gotchas
- Meeting page also carries "Protokoll ... med debatt.pdf" (full protocol +
  debate, one per meeting) and partial "(Signerad) ... § N omedelbar
  justering.pdf" docs - skip the partials; record only the main protocol.
- Title/slug variants: "(Signerad) Protokoll KF YYYY-MM-DD.pdf" most years;
  "Kommunfullmäktiges protokoll" naming in 2025-2026; 2022-10-17 is plain
  "Protokoll KF 2022-10-17.pdf" (no Signerad); some slugs carry numeric
  suffixes (-43608, -75766, -50661, -19272) - use the href as found.
- Budget days are separate meetings with own dates and own protocols
  (2024-11-18/19, 2025-11-18/19, 2022-12-12/13). Record each date separately.
- Election year 2022: no KF meetings Jul-Sep (val 2022-09-11); first 2022
  meeting 01-31, last 12-19.
- Digital anslagstavla (edokmeetings.stockholm.se/digital-bulletin-board) is a
  rolling notice board, not an archive - no historical minutes there.

## Reusable pattern
Other bodies (kommunstyrelsen, nämnder) are the same platform:
/committees/<slug> with /mote-<date> pages and /protocol/<slug> downloads -
same approach. Search endpoint: FullTextSearchControl.js; no need for it.
