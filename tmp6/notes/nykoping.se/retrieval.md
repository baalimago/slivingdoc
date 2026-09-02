# nykoping.se retrieval log

## kf — 2026-08-20: SUCCESS, 39 KF protocols recorded (2022-02-08 .. 2026-06-09)

### Site structure
- nykoping.se is a custom CMS (EPiServer-ish; pages have a .md alternate:
  https://nykoping.se/<path>.md returns front-matter markdown).
- KF (Kommunfullmäktige) minutes live in TWO places:
  A) The "Kallelser och protokoll" page
     https://nykoping.se/kommun--politik/politik-och-demokrati/kallelser-och-protokoll/
     embeds a Netpublicator widget (docs.netpublicator.com) — holds meetings 2023-02-14 .. 2026-06-09.
     Registration id = r25731201, root hash = f3e23853c6e6363, KF channel id = cc3e1b3b97ae4501236.
  B) The calendar archive
     https://nykoping.se/kommun--politik/politik-och-demokrati/kommunfullmaktige/kommunfullmaktige---tidigare-protokoll-och-kallelser/
     with per-meeting pages /arkiv/kalender/<date>_1900_sammantrade_kommunfullmaktige*/ — covers 2018-2026,
     protocol PDFs under /globalassets/nykoping.se/arkiv/protokoll/import/kf/YYYY/...
     The 2022 meetings exist ONLY here (not in Netpublicator).
- 2022-10-11 and 2024-02-13 / 2024-04-09 / 2024-10-22 meetings were INSTÄLLT (cancelled) — no protocols.
- Calendar archive is INCOMPLETE for 2024 (missing 02-13, 04-09 — both cancelled, so OK) and 2025+ pages
  are stubs ("Från nu finns kallelserna på en annan sida") — use Netpublicator for 2023-2026.

### Netpublicator API (the widget's feed)
- Read API: GET https://docs.netpublicator.com/api/public/r25731201/read?hash=<pathHash>&isr=<true|false>
  pathHash = root-id + "-" + channel/meeting ids joined by "-". isr=true at root only.
  Returns {"history":[...],"items":[...]} where items are channels/meetings/documents.
- Root (hash=f3e23853c6e6363, isr=true) -> channel list; KF = cc3e1b3b97ae4501236.
- KF channel (hash=f3e23853c6e6363-cc3e1b3b97ae4501236) -> meetings 2024-02-13..2026-06-09 + "2023" subchannel
  (63e43aded4c36381206) holding 2023-02-14..2023-12-12. NO 2022 in the widget.
- Meeting read -> items: "Dagordning" (document, skip), agenda channels, "Protokoll" channel (id varies per meeting).
- Protokoll channel read -> documents: main "Protokoll ..." doc (RECORD), "Omedelbart justerade §§..." (skip),
  "Anmälningsärenden" (skip). Some 2023 protocols are partial (§§ subsets) — the main doc is still the record.
- Search API: GET .../search?query=Protokoll&hash=<meeting pathHash> returns the meeting's protocol doc(s)
  directly (with the Protokoll channel id in history) — much cheaper than two read calls.
- Download URL: GET https://docs.netpublicator.com/api/public/r25731201/document/<docId>?hash=<fullPathHash>
  (application/pdf). Verified all 31 widget docs download 200 PDF; content = "PROTOKOLL / KOMMUNFULLMÄKTIGE <date>".
- NOTE: 2023-05-09 has NO protocol in Netpublicator (only Dagordning) — use calendar PDF.

### Recorded (39)
- 2022 x9 (02-08, 03-08, 04-12, 05-10, 06-14, 09-13, 10-25, 11-08, 12-13): calendar PDFs, conf 0.95.
  URL quirks: 02-08 found via page /arkiv/kalender/2022-02-27_1900_sammantrade_kommunfullmaktige/
  (listing typo slug; the /2022-02-08_...kommunfullmaktige/ page 404s); 09-13 file = kf-2022-09-13.pdf;
  10-25 file = ..._protokoll2.pdf; 11-08 file lives under /arkiv/tillkannagivanden/kommunfullmaktiges-protokoll-2022-11-08.pdf
  but IS the full protocol (text-verified).
- 2023 x9: 8 via Netpublicator + 2023-05-09 via calendar PDF.
- 2024 x7: 03-12, 05-14, 06-11, 09-10, 10-08, 11-12, 12-10 (all Netpublicator).
- 2025 x9, 2026 x5 (Netpublicator). Last meeting in range: 2026-06-09 (no Jul/Aug meetings; 2026-08-20 has none).

### Dead ends / tips
- The widget's plain read of the KF channel returns ALL meetings (no pagination; page=2 ignored).
- Netpublicator document URLs contain no date — the only key is the opaque docId found via the read/search API.
- slim_http works fine on the JSON API (no jsoncallback needed); document endpoints return PDF (use download_document).
- Calendar "…_protokoll/" pages (vs "…_sammantrade_…/") hold only tillkännagivande (announcement) PDFs — skip.
- "Omedelbart justerade/justering §§…" docs are same-day partial minutes — skip (one set of minutes per meeting).
