# halmstad.se retrieval log

## kf — round 1 (2026-08-20)
- Live page https://halmstad.se/.../kommunfullmaktige.n300.html ->
  "Sammanträdesdatum, kallelse, handlingar och protokoll för kommunfullmäktige" page
  (n305.html) holds a SiteVision file-share webapp with year folders (only current year + 1
  back). Expanded via AJAX appresource endpoint (see README). Collected 2025 (9) + 2026 (5)
  protocol PDFs, verified 260212 and 250213 content (Kommunfullmäktige Sammanträdesprotokoll).
- Diarium diarium.halmstad.se: Ciceron JSON-RPC at /json. Tried doctype 64 (Sammanträdesprotokoll)
  and 1 (Möte) with diary KS / board Kommunfullmäktige -> 0 hits. Only doctype 4 (Handling)
  text search works. "kf-protokoll" gives the combined-protocol registry entries (17,
  2024-10-24+); files[] empty (register only). Combined protocols for 2022-2024-09 not in
  diarium at all.
- For 2022-2024 the combined protocols existed only on the old halmstad.se page
  (sammantradesdatumdagordninghandlingarochprotokoll... n305.html). Wayback snapshots of that
  page give direct PDF links for the latest meeting per snapshot + year-folder buttons whose
  contents were AJAX (not archived). CDX domain query found captured PDFs 220217, 221017,
  240215, 25061617. Recorded 3 Wayback-if_ URLs (verified PDFs) + 7 original URLs from
  archived pages (now 404) + 14 live URLs = 24 records, one per meeting date.
- Dead ends: old folder ids on live appresource (400); ?folder= URL param ignored on live
  page; Wayback did not archive folder-contents AJAX; Wayback has no 2022-03-31/2022-09-29/
  2022-11-24/2022-12-13/2023-* (except 230427)/2024-03-21/2024-05-23/2024-06-17-18/2024-09-26/
  2024-11-21/2024-12-12 KF protocols.
