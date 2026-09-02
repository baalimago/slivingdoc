# Orsa (orsa.se) - KF retrieval notes

Target: Orsa kommun, meeting type kf (Kommunfullmäktige).

## Structure
- Site: SiteVision (orsa.se). Main entry: https://orsa.se/kommun-och-politik.html
  -> "Protokoll, kallelser, kungörelser och tillkännagivanden"
  -> "Protokoll och kallelser Kommunfullmäktige"
- KF listing page (canonical, single source for KF protocols):
  https://orsa.se/kommun-och-politik/protokoll-kallelser-kungorelser-och-tillkannagivanden/protokoll-och-kallelser-kommunfullmaktige.html
- The listing is year-based collapsible sections (2026..2021). Documents are all
  present in the DOM server-side (hidden by env-collapse); no XHR feed needed.
  Extracting all a[href*="/download/"] links via the Playwright browser gives
  every document for every year at once. slim_http alone does not show the
  document links (collapsed), so use the browser + evaluate.
- Document URLs: https://orsa.se/download/<nodeid>/<timestamp>/<filename>
- Retention: protocols kept 5 years then purged; oldest year (2021) still present
  in Aug 2026 but will be gone soon. Kallelser purged when obsolete.

## KF protocol documents found (2022-01-01 .. 2026-08-20)
24 KF minutes ("Protokoll ... Kommunfullmäktige") recorded, meeting dates:
2022: 02-14, 05-02, 05-30, 10-24, 11-28, 12-12
2023: 04-03, 05-29, 07-11 (extra sammanträde), 10-02, 11-27
2024: 02-19, 04-08, 05-27, 06-04, 10-07, 11-25
2025: 02-10, 04-07, 06-02, 10-06, 11-24
2026: 04-20, 06-01

## Excluded (listed on the same KF page but not KF minutes)
- "Närvaro och omröstningslista ..." (attendance/voting lists, attachments)
- "Kallelse kommunfullmäktige ..." (2025-11-24, 2026-06-01) - agenda/notice
- "Protokoll fullmäktiges valberedning 2022-11-24.pdf" - this is the VALBEREDNING
  (election committee) protocol, a separate body from Kommunfullmäktige
  (verified: header says PROTOKOLL VALBEREDNING, organ Valberedning). Not KF.
- Digital anslagstavla (https://orsa.se/arkiv/digital-anslagstavla.html) contains
  no download links; points back to the protokoll page. No KF docs there.

## Tips for next runs
- Fetch KF page with Playwright, accept cookie dialog ("Godkänn nödvändiga kakor")
  or clicks get intercepted, then evaluate to collect all /download/ links.
- Match date from the filename (YYYY-MM-DD), record one minutes per meeting date.
- Other bodies (KS, utskott, nämnder) each have their own page under the same
  protokoll index; same pattern applies.
