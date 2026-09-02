# Smedjebacken (smedjebacken.se) - KF retrieval notes

Target: Smedjebackens kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Structure
- Site: SiteVision (smedjebacken.se). Main entry: https://smedjebacken.se/kommun-och-politik.html
  -> Politik och demokrati -> Möten och protokoll -> Kommunfullmäktige.
- KF protocol listing page (single source for KF minutes):
  https://smedjebacken.se/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/kommunfullmaktige.html
- The page has year-section tables (2026..2019), each row = one meeting date with two
  columns: "Protokoll" (minutes, record) and "Kallelse/handlingar" (agenda/notice,
  exclude). All PDF links are plain server-side <a href="/download/...">; no XHR/JS feed.
  slim_http (filter token "download") already returns all links; the Playwright browser
  was used to map table rows -> columns reliably.
- Document URL pattern: https://smedjebacken.se/download/<nodeid>/<timestamp>/<filename>
  (also works via www.smedjebacken.se, which is the canonical host).
- Cancelled meetings are marked "inställt" on the page (2026-02-23, 2025-02-24,
  2021-12-20) - no protocol exists for those dates.
- 2024-12-16 has only "Handlingar kommunfullmäktige 2024-12-16.pdf" (no Protokoll
  column entry) - no KF minutes for that date on the page.
- Older protocols: page says "Kontakta förvaltningen för att ta del av äldre protokoll."
- Digital anslagstavla page (digital-anslagstavla.html) holds only non-KF docs
  (e.g. valnämnden); no KF minutes there. "Kommunfullmäktige" info page links back
  to the same protokoll page.

## KF minutes recorded (25) - meeting dates
2022: 02-21, 04-25, 06-20, 09-19, 10-17, 11-21, 12-19
2023: 02-20, 04-24, 06-26, 09-25, 11-27
2024: 02-19, 04-22, 05-20, 06-24, 09-23, 11-25
2025: 04-28, 06-23, 09-22, 11-24, 12-15
2026: 04-27, 06-22

## Excluded (same page, Kallelse/handlingar column)
- "Handlingar ..." (agenda documents), "Kungörelse ..." (2023-11-27) - agenda/notice.
- Note 2023-02-20 Kallelse/handlingar PDF is named "Kf 2023-02-20.pdf" (712.6 kB) -
  identical filename to the protocol (235.4 kB); verified by content that the first
  link in the row is the SAMMANTRÄDESPROTOKOLL and the second is KUNGÖRELSE+handlingar.
  Same trap for 2023-06-26 and 2023-09-25 (both "Kf ...pdf" pairs). Always use table
  column order (Protokoll first) rather than filename alone.

## Tips for next runs
- Fetch the listing page (slim_http is enough), take the first /download/ link per
  date row = minutes. Verify with document_to_text that header says SAMMANTRÄDESPROTOKOLL
  Kommunfullmäktige.
- Meeting date = date in filename (YYYY-MM-DD); one minutes per date.
- Other bodies (KS, nämnder) each have their own page under the same
  moten-och-protokoll index; same table pattern applies.
