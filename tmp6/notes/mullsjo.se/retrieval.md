# mullsjo.se scanner notes (Mullsjö kommun), kf = Kommunfullmäktige

## Where KF (Kommunfullmäktige) minutes live
- Entry page: https://mullsjo.se/kommun-och-politik/politik-och-demokrati/moten-kallelser-och-protokoll
  ("Möten, kallelser och protokoll", SiteVision CMS). It says it holds protocols for the
  current year + two years back; older material only from the administrative office.
- The page renders one accordion per committee via the SiteVision webapp
  **se.soleil.evolutionTree** ("Evolutionträd", Public 360/Evolution document web).
  Each accordion is a separate webapp instance (portlet id 12.28d463de196664212e0ec2e
  = Kommunfullmäktige on page 4.2e50256416a58d18ea9a3e37). Top-level folders are in the
  AppRegistry initial state; document lists load lazily via
  `GET /appresource/4.2e50256416a58d18ea9a3e37/<portlet>/folderContent?id=<folderId>`
  -> JSON {"documents":[...],"folders":[...]}.
- Download URLs for documents come from the folderContent "downloadLink" field
  (appresource-proxied to the Evolution WCF service; internal host ess.evolution.se
  does NOT resolve publicly - only reachable through the mullsjo.se proxy).

## BLOCKER (harvest on 2026-08-19)
- The site announced maintenance 17-19 Aug 2026 ("Underhållsarbete pågår ... du kan inte
  komma åt dokument i mapparna"). During this window the evolutionTree initial states are
  ALL EMPTY and folderContent returns {"documents":[],"folders":[]} for every folder id
  (tested Protokoll 2024/2025/2026 + Kallelser 2026 + old root ids). The whole KF protocol
  archive is unreachable through the official listing while the maintenance runs.
- Only ONE full KF protocol is currently accessible, as a SiteVision /download/ file
  surfaced by the site search (see below): Protokoll kommunfullmäktige 2025-11-18.

## Working alternative: the site search (indexes /download/ PDFs)
- Search URL pattern (SiteVision search, page id 12.3c688b2117d46eaaad533d5):
  https://mullsjo.se/ovrigt/sok?sv.url=12.3c688b2117d46eaaad533d5&query=...&100.2a7bca6017cbcdd713625b37=Dokument
  with &startAtHit=10..120 for paging.
- Query "protokoll kommunfullmäktige" -> 129 Dokument hits, but the ONLY full meeting
  protocol among them is 2025-11-18. The rest are per-§ "Beslut KF ..." decision
  extracts (e.g. "Beslut KF 2026-06-23 § 77 ...") - NOT meeting minutes; skip them
  (one meeting = one protocol).
- Full protocol found:
  https://mullsjo.se/download/18.209f4cb19ab97c996b685e/1764080831575/Protokoll%20kommunfullm%C3%A4ktige%202025-11-18.pdf
  (verified SAMMANTRÄDESPROTOKOLL, Kommunfullmäktige, sammanträdesdatum 2025-11-18, 45 pages).
- Filename pattern for full protocols: "Protokoll kommunfullmäktige YYYY-MM-DD.pdf".
  Other queries tried with same result (only 2025-11-18): "Protokoll kommunfullmäktige
  2022/2023/2024/2025/2026", "Protokoll KF", "Protokoll från kommunfullmäktige",
  "SAMMANTRÄDESPROTOKOLL Kommunfullmäktige", sorted by Relevans and Datum.

## Wayback Machine findings (for a post-maintenance re-run)
- 9 captures of the listing page between 2024-03-19 and 2026-04-21 (timemap available).
- 2026-04-21 capture contains the evolutionTree initial states with the CURRENT KF folder ids:
  Kallelser 2024 8d3b82dc-f29b-4987-b638-35d8aac40de0
  Kallelser 2025 7baedd93-57db-4677-b043-ef26cba0dbe1
  Kallelser 2026 a7d62e82-ee0a-4ebb-8597-f85d3e9e34c2
  Protokoll 2024 965c7ee8-3d75-454e-b453-75a327a6ef9a
  Protokoll 2025 eeff1241-bf49-41e1-92ee-d81dc71ef3f3
  Protokoll 2026 fe0a4b3f-00f5-4f23-910e-550e938c72f4
  -> call folderContent?id=<id> on the live appresource once maintenance ends.
- 2024-03-19 and 2024-10-06 captures use the OLD webapp se.soleilit.evoulution
  (portlet 12.2e087b5d16d62320d3d411c, filter ["Protokoll"]) which fetches top-folders
  at runtime; those captures show the widget offline ("Något gick snett!") and contain no
  folder ids in HTML. Wayback did NOT capture the folderContent/top-folders JSON for the
  KF portlet, nor any KF protocol documents.
- Old (2021-04-21) top-folders capture on page 4.2e50256416a58d18ea9a3e43/portlet
  12.2e087b5d16d62320d3d411f lists old KF root folder 74d29441-14c3-4861-be03-beddace3ee35
  (folderContent on it returns empty now). Old download pattern: dl-document?documentId=<id>&folderId=<id>.
- Wayback CDX filter urlkey:.*kommunfullm.* for mullsjo.se/download* shows only
  planeringsagendor and one small "Protokoll, kommunfullmäktige.pdf" (2023-09) - no full
  KF protocols.

## Dead ends
- Anslagstavla (https://mullsjo.se/arkiv/anslagstavla): only kungörelser/underrättelser
  (e.g. 2026-06-29 underrättelse detaljplan), no protocol PDFs attached.
- /appresource top-folders, folder-info, file/ endpoints on other pages are 404 or belong
  to Styrande dokument (not KF).
- ess.evolution.se does not resolve publicly (internal host of the appresource proxy).
- The "Kommunfullmäktiges sammanträden 2026" accordions on the listing page are empty
  during maintenance too (schedules only anyway).

## Next run advice
- Re-run after 2026-08-19 maintenance ends: expand the Kommunfullmäktige tree on the live
  page (or call folderContent directly with the folder ids above), collect each
  "Protokoll ..." document (one per meeting), skip "Kallelse ..." and any partial/§ docs.
- For 2022-2023 KF protocols (outside the current 3-year window), the only public source
  was the Evolution tree before 2024; try contacting the administrative office or check
  Wayback captures of the old evoulution page after maintenance.
- KF meeting pattern (from news archive/schedule): ~monthly, roughly Jan-Dec except
  July/August; 2026 meetings held 24 feb, 24 mar, 21 apr, 19 maj, 23 jun (next outside range).

## Harvest round 2026-08-20 (kf, 2022-01-01..2026-08-20) — maintenance STILL blocking
- Re-ran the full investigation on 2026-08-20 ~13:00 UTC. The maintenance has NOT ended:
  - Live page "Möten, kallelser och protokoll" still shows the "Underhållsarbete pågår"
    banner and ALL evolutionTree initial states are still {"documents":[],"folders":[]}.
  - folderContent?id=... still returns {"documents":[],"folders":[]} for every KF
    Protokoll/Kallelser folder id, even with cache-buster param (tried 2023/2024/2025/2026
    Protokoll ids). Same for the "Styrande dokument" tree on
    https://mullsjo.se/kommun-och-politik/styrande-dokument (portlet 12.ff15aa419899429b9c209b6,
    also empty) — the whole Evolution integration is down, not just KF.
- RECORDED (the only full KF protocol reachable):
  https://mullsjo.se/download/18.209f4cb19ab97c996b685e/1764080831575/Protokoll%20kommunfullm%C3%A4ktige%202025-11-18.pdf
  date 2025-11-18, title "Protokoll kommunfullmäktige 2025-11-18", verified 45-page
  SAMMANTRÄDESPROTOKOLL (§103-126), found via site search (Dokument filter).
- Confirmed again: every other hit of "protokoll kommunfullmäktige" (129 Dokument hits,
  incl. sorted by Datum) and "protokoll kommunfullmäktige 2026" (16 hits) is a per-§
  "Beslut KF ..." extract (e.g. "Beslut KF 2026-06-23 § 77 ..."). NOT minutes; skipped.
- No other full KF protocols are indexed; the archive lives only in the Evolution tree.

## NEW folder-id findings (from Wayback captures, ids STABLE over time)
- KF folder ids are stable across 2025-05-22, 2025-10-14, 2026-01-19, 2026-04-21 captures
  (Protokoll 2024 = 965c7ee8-3d75-454e-b453-75a327a6ef9a and Protokoll 2025 =
  eeff1241-bf49-41e1-92ee-d81dc71ef3f3 identical in all four).
- The 2025-05-22 and 2025-10-14 captures ALSO expose the 2023 folders (tree then held
  2023-2025). Extra KF ids to query after maintenance:
  Kallelser 2023 d02ea216-d7f0-4374-bdfb-8cee7fa4ad97
  Protokoll 2023 ba00185c-74ee-4cbf-bb23-eea0d713a416
- 2026-01-19 capture = same 2024-2026 ids as 2026-04-21 (Kallelser 2024/2025/2026 +
  Protokoll 2024/2025/2026).
- 2025-01-21 capture has NO evolutionTree instances at all (page captured without the tree).
- No capture ever shows "Protokoll 2022" or "Kallelser 2022" folders; 2022 material was
  only in the pre-2024 old webapp (se.soleilit.evoulution), which Wayback captured without
  folder ids (widget offline). 2022-2023 KF minutes (2022 fully, 2023 partially) therefore
  have NO public URL known to us — the pre-maintenance tree only held 2023+.

## Wayback "no KF protocols" verification (round 2)
- appresource CDX (url=mullsjo.se/appresource*) has NO folderContent captures. Only:
  old top-folders (2021), old folder-info (2023, = "Styrande dokument" tree), old
  dl-document (2023, verified = "Delegationsordning för socialnämnden", not KF), and
  file/ endpoints (2026, verified = "Inackorderingstillägg riktlinje" + "Tillsynsplan
  alkohol/tobak", styrande dokument, not KF).
- download CDX filter urlkey:.*protokoll.* = only Styrelseprotokoll MBAB/Gyljeryd
  (bolagsstyrelser, not KF) + "Protokoll, kommunfullmäktige.pdf" (2023-09-19, verified a
  per-§ extract for §116, NOT the full protocol).
- Conclusion unchanged: Wayback holds no full Mullsjö KF minutes.

## For the next run
- First re-test folderContent with the ids above (2023/2024/2025/2026 Protokoll). If the
  update changed folder ids, re-read the live initial state and walk the tree normally.
- Expect to harvest: Protokoll 2023 (~10 meetings), 2024 (~10), 2025 (~10), 2026
  (24 feb, 24 mar, 21 apr, 19 maj, 23 jun — within range through 2026-08-20). 2022 is
  only obtainable from the administrative office (not online).
- 2026-06-23 appears to be the last KF meeting inside the range (next scheduled outside).


## Harvest round 2026-08-20 (kf, 2022-01-01..2026-08-20) — MAINTENANCE OVER, full KF archive harvested
- At ~17:00 UTC the Evolution integration is serving data again. The page STILL displays the
  "OBS! Underhållsarbete pågår" banner, but folderContent now returns real document lists
  (the banner is just stale page copy). First working call:
  GET https://mullsjo.se/appresource/4.2e50256416a58d18ea9a3e37/12.28d463de196664212e0ec2e/folderContent?id=<folderId>
- Live Kommunfullmäktige initial state (from page HTML AppRegistry) still lists only
  Kallelser 2024/2025/2026 + Protokoll 2024/2025/2026 with the SAME folder ids as before
  (Protokoll 2024 = 965c7ee8-3d75-454e-b453-75a327a6ef9a, 2025 = eeff1241-bf49-41e1-92ee-d81dc71ef3f3,
  2026 = fe0a4b3f-00f5-4f23-910e-550e938c72f4). No Protokoll 2023/2022 folders on the live site.
- Protokoll 2023 folder id from old captures (ba00185c-74ee-4cbf-bb23-eea0d713a416) now returns
  {"documents":[],"folders":[]} — 2023 is gone from the site (page text: only current year + 2 back).
- RECORDED all 23 full KF protocols reachable in range 2022-01-01..2026-08-20:
  2026 (5): 02-24, 03-24, 04-21, 05-19, 06-23
  2025 (9): 02-25, 03-25, 04-22, 05-27, 06-24, 09-23, 10-21, 11-18, 12-16
  2024 (9): 02-27, 03-26, 04-24, 05-28, 06-25, 09-24, 10-22, 11-19, 12-17
  URL pattern: https://mullsjo.se/appresource/4.2e50256416a58d18ea9a3e37/12.28d463de196664212e0ec2e/file/<uuid>
  (downloadLink field from folderContent JSON). All verified as full SAMMANTRÄDESPROTOKOLL
  (Kommunfullmäktige, sammanträdesdatum = label date). 2025-11-18 now has an appresource URL too
  (3e8ad3a4-2b4a-4f3e-b827-e0870b3b3881) - prefer it over the old /download/ URL; same meeting, don't double-record.
- Kallelser folders were NOT harvested (agenda/notice, out of scope). 2022-2023 KF minutes remain
  unavailable online; only from the administrative office.
