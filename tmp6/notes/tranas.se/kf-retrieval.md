# Tranås (tranas.se) - KF retrieval notes

Target: Tranås kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- tranas.se is WordPress; its "Protokoll och handlingar" page
  (https://tranas.se/kommun-och-politik/beslut-och-handlingar/protokoll-och-handlingar/)
  points to the MeetingPlus (Formpipe) sammanträdesportal:
  https://tranaskommun.ondemand.formpipe.com/committees/
- Only Bygg- och miljönämnden and Socialnämnden PDFs live on tranas.se
  (/wp-content/dokument/louise-sparr/protokoll-och-handlingar/...) - NOT KF.
  All KF protocols live on the Formpipe portal.

## Portal structure (same MeetingPlus platform as Ragunda)
- Committee page: https://tranaskommun.ondemand.formpipe.com/committees/kommunfullmaktige
  Has tabs "Kommande" / "Tidigare"; the Tidigare tab lists ALL past meetings
  year-grouped (2026..2022), server-side in the DOM after clicking the tab
  (slim_http alone only shows "Kommande" rows - use Playwright click on
  "Tidigare" or read the #committeesRecentContent section).
- Meeting page URL: /committees/kommunfullmaktige/<slug>, slug = "kommunfullmaktige-YYYY-MM-DD"
  (cancelled meeting: "kommunfullmaktige-2025-08-18-installt" - NO protocol link).
- Each meeting page has tabs Kallelse / Protokoll; the protocol document link is
  a[href*="/protocol/"], button "Öppna protokoll":
  /committees/kommunfullmaktige/<slug>/protocol/<filename>pdf?downloadMode=open
  slim_http with required_tokens ["/protocol/"] returns it directly per meeting.
  download_document works (200, application/pdf) with ?downloadMode=open.

## Protocol files - pick ONE main protocol per date
- Filenames vary: protokoll-kommunfullmaktige-<date>pdf, protokoll-kf-<date>pdf,
  plain "protokollpdf" (many 2022-2024), some with numeric suffix
  (e.g. protokoll-kommunfullmaktige-2025-09-15pdf-18552, ...-2025-03-31pdf-66587,
  ...-2025-01-13pdf-34086).
- 2025-11-10 meeting also publishes a PARTIAL protocol "Protokoll
  kommunfullmäktige 2025-11-10 § 182.pdf" (omedelbar justering of a single
  paragraph). Main protocol = "protokoll-kf-2025-11-10pdf" (42 pp, §§183-206).
- 2023-09-18 lists two identical full-protocol files
  (protokoll-kommunfullmaktige-2023-09-18pdf and protokollpdf) - same bytes,
  record one.
- All PDFs text-layer: header "PROTOKOLL / Kommunfullmäktige / Sammanträdesdatum
  <date>". Verified content of 16 docs across 2022-2026 (all match meeting date).

## KF meetings found in range (44 protocols recorded)
2022: 01-17, 02-21, 03-28, 05-02, 06-13, 09-05, 10-03, 10-17, 11-07, 12-05
2023: 01-16, 02-20, 03-27, 05-15, 06-12, 08-21, 09-18, 10-16, 11-13, 12-04
2024: 01-15, 02-19, 03-25, 05-06, 06-10, 08-19, 09-16, 10-14, 11-11, 12-02
2025: 01-13, 02-17, 03-31, 05-05, 06-09, 09-15, 10-13, 11-10, 12-08
2026: 01-19, 02-16, 03-30, 05-11, 06-15
(2025-08-18 listed but INSTÄLLT - no protocol; 2026-08-31 & 2026-10-05 are
upcoming/after range end - exclude.)

## Tips for next runs
- Playwright: open committee page, click "Tidigare" tab to get full meeting list.
- Then per meeting use slim_http with required_tokens ["/protocol/"] - fastest.
- Ignore the §-partial protocols and duplicate named files; one minutes per date.
- Other bodies (KS, nämnder) each have /committees/<slug> - same pattern.
