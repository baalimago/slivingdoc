# trollhattan.se (Trollhättans stad) - KF retrieval notes

Target: Trollhättan, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## RESULT: 28 KF protocols recorded (2024-01-29 .. 2026-06-17), conf 0.95
2024 (11): 01-29, 02-26, 03-25, 04-22, 05-20, 06-17, 09-16, 10-14, 11-04,
11-25, 12-16.
2025 (11): 01-27, 02-24, 03-24, 04-22, 05-19, 06-18, 09-15, 10-13, 11-05,
11-24, 12-15.
2026 (6): 01-26, 02-23, 03-23, 04-20, 05-18, 06-17.

## IMPORTANT GAP: 2022-01-01 .. 2023-12-31 KF protocols NOT accessible
- The old diarium https://diariet.trollhattan.se (Evolution SPA; linked from
  site search "Tips!" box as the place for "kommunfullmäktiges handlingar och
  protokoll") is OFFLINE: both slim_http and Playwright get "no route to
  host"/ERR_ADDRESS_UNREACHABLE (194.236.216.167). It presumably held the
  2022-2023 (and older) KF protocols. Nothing recorded for those years.
- DMW archive has no KF folders for 2022/2023 (only press releases).

## Site structure (two diarium generations + DMW archive widget)
1. New diarium (2024-06-17 onwards): https://webbdiariet.trollhattan.se/meetings
   - Evolution.Internet Angular SPA. API: GET /api/units,
     /api/decisionAuthoritys/KS (KF = Kommunfullmäktige id 584),
     /api/meetings/584 (list, NOT paginated; returns 23 meetings
     2024-06-17..2026-06-17), /api/meetings/decisions/<meetingId> (per-§
     "Beslut KF <date> <rubrik>" .docx decision excerpts),
     /api/meetings/notices/<meetingId> (kallelse/underlag - skip).
   - The meeting page shows per-point decisions, NOT the full protocol; the
     full protocol lives in the DMW archive (below). Do NOT record the
     per-§ decision docx as "minutes".
2. Old diarium (2022-2024-05): https://diariet.trollhattan.se - OFFLINE (see gap).
3. DMW archive widget (protocol PDFs): embedded on
   https://trollhattan.se/startsida/kommun-och-politik/moten-handlingar-och-protokoll/
   as accordions per body ("Kommunfullmäktige" button) -> iframe
   https://dmw.trollhattan.se/?folderId=<root>.
   - API: GET https://dmw.trollhattan.se/api/folders (list of all folders),
     GET /api/folders/<id> (folder + documents), GET
     /api/folders/<id>?page=2 (ignored - full JSON returned anyway).
   - KF root folder: 9874dfac-3498-4fa9-bf73-06f4b0d68fd9 with year folders:
     2024 adab3b57-d0f7-450c-b61e-fc384b683f2a, 2025
     64855e93-d1dd-42ed-8297-bdf207464dbd, 2026
     31ec2810-45cb-48a5-b21e-86b95569a754. (Duplicates exist: 465a95f7... and
     empty 6b63a445... - ignore.)
   - Document download URL: https://dmw.trollhattan.se/api/download/<docId>/<folderId>
     (docId = document.id, folderId = year folder id). Returns application/pdf;
     download_document works. Filenames/titles = document "name"
     ("Protokoll kommunfullmäktige <date> §§ X-Y E-signerat").
   - Protocol PDF content verified: "SAMMANTRÄDESPROTOKOLL /
     Sammanträdesdatum <date> / Kommunfullmäktige" (checked 2024-01-29,
     2026-06-17, 2025-03-24). Dates = meeting date from title/PDF.
   - Närvarorapport/Omröstningsrapport/Protokollsbilaga docs in same folders:
     skip.

## Notes / gotchas
- Trollhättan KF meets ~monthly Jan-Dec (no Jul-Aug; 2022 had no Sep meeting -
  valår). First 2024 meeting 01-29, last recorded 2026-06-17 (nothing later in
  range; next would be 2026-09).
- Site search (webprod.adeptic.eu webseeker XML) does index trollhattan.se PDFs
  but KF protocol PDFs are NOT among them (search only returns planning docs);
  do not rely on it.
- Bulletin board (anslagstavla2) is a rolling NetPublicator board - no history.
- source_page used = https://dmw.trollhattan.se/?folderId=<yearFolder> (the
  widget page holding the document listing); entry point on trollhattan.se is
  the "Möten, handlingar och protokoll" page.
