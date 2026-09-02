# Ragunda (ragunda.se) - KF retrieval notes

Target: Ragunda kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- ragunda.se is SiteVision, but KF documents live on MeetingPlus by Formpipe:
  https://ragundakommun.ondemand.formpipe.com/ ("Möten, handlingar och protokoll"
  link from https://ragunda.se/kommunochpolitik.609.html -> "Kallelser och protokoll").
- Committee page (canonical list of ALL KF meetings, back to 2019):
  https://ragundakommun.ondemand.formpipe.com/committees/kommunfullmaktige
  Year-grouped sections; meetings render server-side (slim_http sees them).
- Meeting page URL: /committees/kommunfullmaktige/<meeting-slug> where slug is
  "kommunfullmaktige-<id>" or "extra-kommunfullmaktige-<id>".
- Each meeting page has tabs "Kallelse" (agenda) and "Protokoll" (minutes).
  The protocol documents are server-rendered in the DOM under the Protokoll tab:
  a[href*="/protocol/"] links. slim_http with required_tokens ["/protocol/"]
  returns them directly - no Playwright needed.

## Protocol document URLs
- Pattern: https://ragundakommun.ondemand.formpipe.com/committees/kommunfullmaktige/<slug>/protocol/<slug2>pdf?downloadMode=open
  e.g. .../kommunfullmaktige-50402/protocol/protokoll-kf-2026-06-25pdf?downloadMode=open
  ?downloadMode=download returns the same PDF bytes (verified identical).
- Also an API download-all: /api/v2.0/meetings/<mid>/download/Protocol?downloadMode=download
- download_document works directly (200, application/pdf).

## Per-meeting protocol files - pick ONE main protocol per date
Several meetings publish BOTH a "Protokoll § N.pdf" (omedelbar justering, single
paragraph) and the main "Protokoll.pdf" / "Protokoll KF <date>.pdf". Also present:
Omröstningsbilaga, Reservation - X.pdf (attachments). Record only the MAIN protocol.
Known main files: "protokollpdf" (most meetings), "protokoll-kf-2026-06-25pdf",
"protokoll-kf-2024-02-22pdf", "protokoll-kf-2023-05-16pdf" (extra meeting),
"protokolpdf" (typo, no second 'l', 2025-08-28 extra meeting).

## KF meeting dates found (in range 2022-01-01..2026-08-20) - 30 listed, 29 with protocols
2022: 02-24, 04-07, 05-10 (extra), 06-16, 09-29, 10-20, 11-24
2023: 03-02, 04-13, 05-16 (extra), 06-08, 09-28, 11-09; 12-21 INSTÄLLT (cancelled,
      page /committees/kommunfullmaktige/installt-kommunfullmaktige has NO protocol)
2024: 02-22, 04-11, 06-13, 09-26, 11-07, 12-06 (extra), 12-19
2025: 02-20, 04-10, 06-12, 08-28 (extra), 11-06, 12-10
2026: 02-19, 04-16, 06-25 (2026-09-24 exists but is after range end - exclude)

29 KF protocols recorded (verified content of 5: 2022-05-10, 2022-11-24, 2024-12-06,
2026-04-16, 2026-06-25 - all "Beslutande organ: Kommunfullmäktige" text-layer PDFs).

## Gotchas
- extra-kommunfullmaktige-54131 (2024-12-06) renders protocol links pointing to the
  canonical meeting kommunfullmaktige-17074 - use the canonical URL for record/source.
- 2023-12-21 meeting was inställt (cancelled): page title "INSTÄLLT Kommunfullmäktige",
  no protocol documents at all.
- 2021-12-22/2021-12-14 and older meetings are before range start - ignore.

## Tips for next runs
- Fetch /committees/kommunfullmaktige with slim_http for the full meeting list, then
  fetch each meeting page with required_tokens ["/protocol/"] to get the protocol link.
- Match date from the committee listing (meeting date = protocol date).
- Other bodies (KS, nämnder, utskott) each have their own /committees/<slug> page -
  same pattern.
