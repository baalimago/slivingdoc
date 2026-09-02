# atvidaberg.se retrieval log (kf = Kommunfullmäktige)

## kf — 2026-08-20: SUCCESS, 37 protocols recorded (2022-02-23 .. 2026-06-15)

### Site structure
- atvidaberg.se is SiteVision CMS. The KF protocols page is
  https://atvidaberg.se/kommun-och-politik/politik-och-demokrati/moten-handlingar-och-protokoll
  ("Möten, handlingar och protokoll"). The "Handlingar till politiska sammanträden"
  section is a NetPublicator public reader widget (np-publicreader.js). The widget
  is the ONLY public source of KF protocols (covers 2020-2026).
- Widget API: docs.netpublicator.com/api/public/r15196469/read?hash=...&isr=...
  - Root: hash=be0f29eb34c5302&isr=true -> channel list. KF channel id =
    658981f891c33884865.
  - KF channel: hash=be0f29eb34c5302-658981f891c33884865&isr=false -> ALL meetings
    2020-2026 in one response (no pagination), each {id, text:<date>, type:meeting}.
  - Meeting detail: hash=<root>-<channel>-<meetingId>&isr=false -> documents at
    meeting level ("Protokoll KF <date>", "Tillkännagivande KF <date>") + agenda
    item channels (skip; those hold kallelse/handlingar/underlag per §).
  - Search: .../search?query=Protokoll&hash=<meeting hash> -> protocol doc(s) of that
    meeting (handy; first hit with history length 3 = the main protocol).
  - Download: https://docs.netpublicator.com/api/public/r15196469/document/<docId>?hash=<meetingPathHash>
    -> application/pdf (works with slim_http's download_document; docId is a UUID,
    opaque; hash must be the full root-channel-meeting path).
- isr=true is required at the root; for any deeper read isr=false (or the API
  returns the root list again).

### What was recorded (37, one protocol PDF per meeting, all text-verified
SAMMANTRÄDESPROTOKOLL Kommunfullmäktige with matching sammanträdesdatum)
- 2022 (8): 02-23, 03-30 (published title typo "Protokoll KF 2022-30-30", content ok),
  05-25, 06-15, 09-28, 10-26, 11-30 ("utan signatur"), 12-14 ("utan signatur")
- 2023 (9): 02-22, 03-29, 04-26, 05-24, 06-14, 09-27, 10-25, 11-29, 12-13
- 2024 (8): 02-26, 04-08, 05-13, 06-17, 09-09, 10-07, 11-11, 12-16
- 2025 (8): 02-24, 04-07, 05-05, 06-16, 09-08, 10-06, 11-17, 12-15
- 2026 (4): 02-23, 04-13 ("Protokoll 26-04-13"), 05-11 ("Protokoll 2026-05-11"),
  06-15 (last meeting in range; no Jul/Aug 2026 meeting exists)
- Confidences 0.97 (0.95 for the typo/unsigned/short-title docs).
- Skipped per meeting: "Tillkännagivande ..." notices, "Kallelse förstasida",
  "Info brev KF", agenda-item documents, "Protokoll Omedelbar justering §X"
  partials (e.g. 2023-12-13 §103, 2024-06-17 §46).

### Meetings WITHOUT protocols (confirmed absent, not recorded)
- 2022-04-27: regular meeting (9 agenda items + Tillkännagivande in widget) but NO
  protocol document anywhere: widget read+search show none; Wayback CDX has no
  2022-04-27 PDF (only "beslutsprotokoll-kommunfullmaktige.pdf" from 2022-03-02 for
  the Feb meeting); the EvoInternet diarium (evo-internet.atvidaberg.se) only has
  units KS and Valnämnden — no KF. Protocol exists only in the municipal archive
  (per site text: contact kommunledningsförvaltningen).
- 2025-03-10 and 2026-03-09: "Kunskapssammanträde" (knowledge sessions, at
  Industrigallerian / gamla kommunhuset) — meeting contains only an "Inbjudan"
  document; no decisions, no protocol.

### Dead ends / tips
- evo-internet.atvidaberg.se (footer "Allmänna handlingar") = EvoInternet Angular SPA;
  /api/units returns only KS + VN; /api/folders 404s (unlike ekero.se). Not a KF source.
- Wayback CDX on atvidaberg.se works but the old site (2022) published KF protocols
  only through the same NetPublicator widget; no extra PDFs in /download/.
- The widget read URL pattern is the reliable path: read channel -> read each
  meeting (isr=false) -> pick document whose text starts "Protokoll" at meeting level
  -> download via /document/<uuid>?hash=<root-channel-meeting>.
