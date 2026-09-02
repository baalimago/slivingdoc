# Partille (partille.se) - KF retrieval notes

Target: Partille kommun, meeting type kf (Kommunfullmäktige).
Harvest run: 2026-08-20, range 2022-01-01..2026-08-20.

## Site structure
- Optimizely/EPiServer site (www.partille.se). Meeting documents are served by
  Axiell NetPublicator via a JS "public reader" (np-publicreader) embedded on
  per-body pages under /kommun--politik/protokoll/<body>/.
- KF page (canonical): https://www.partille.se/kommun--politik/protokoll/handlingar-fran-kommunfullmaktige/
  Reader config: readerName=partille, previousPath=["c7142bd91daa30"],
  rootId=15a3f0b6efe4255569, rootName=Fullmäktige.
- IMPORTANT: the site only publishes the LAST ~12 MONTHS of meeting documents.
  Page text: "På den här sidan publicerar vi protokoll från de senaste tolv
  månaderna... kontakta kundcenter för äldre protokoll." The KF channel holds
  exactly 6 meetings: 2025-09-10, 2025-10-15, 2025-12-03, 2026-02-18,
  2026-04-22, 2026-06-02. KS channel (rootId 2ab2b2a6b39511879) also ~12 months.
- NetPublicator JSON API (GET, JSON): 
  read:   https://docs.netpublicator.com/api/public/partille/read?hash=<hash>&isr=false
  search: https://docs.netpublicator.com/api/public/partille/search/?query=<q>&hash=<hash>
  document download: https://docs.netpublicator.com/api/public/partille/document/<docId>?hash=<hash>&cache=<ts>
  hash = c7142bd91daa30-15a3f0b6efe4255569-<meetingId> for docs under a meeting.
  The root channel read returns items: meetings + subchannels; each meeting read
  returns its documents (KF-protokoll, Tillkännagivande = notice, agenda item
  channels, and also KS-protokoll documents attached as related docs - skip those).

## KF minutes recorded (6, all verified by download + text)
Per meeting the main protocol ("Sammanträdesprotokoll / Kommunfullmäktige /
Sammanträdesdatum <date>" in the PDF text):
- 2025-09-10, doc 6d42fd78f846b82ba601-c6f3-46bb-b18e-9b2cdc23a438, "KF-protokoll 10 september - GDPRsäkrat för publicering"
- 2025-10-15, doc 34f1d23541c08e3f6388-ceaf-4d53-aa11-8a26d6dda7d5, "KF-protokoll 15 oktober 2025 - GDPRsäkrat för publicering"
- 2025-12-03, doc 126d59225bd0301fb404-5bd3-4df8-afc0-92897c718ca8, "KF-protokoll 3 december 2025 - GDPRsäkrat för publicering"
- 2026-02-18, doc 945a1d5c002d228d400a-5494-4d4e-a6c5-267f66f2bb54, "Protokoll kommunfullmäktige 18 februari 2026 - GDPRsäkrat för publicering"
  (API listing text has a typo "18 februari 2025"; content verified 2026-02-18 §§1-21)
- 2026-04-22, doc fee2a1a670ee8a23740c-c67c-40c4-940d-cbc8937c6c36, "KF protokoll 22 april 2026"
- 2026-06-02, doc bbc39bcb6114a8c6511c-c5c8-4204-8c19-34b6d23e098f, "KF-protokoll 2 juni 2026"
source_page for all: the KF page URL above. Confidence 0.95.
Note: some read responses list the same KF-protokoll under a DIFFERENT meeting
channel (e.g. 2026-04-22 doc under 2026-06-02 channel; 2025-10-15 doc under
2025-12-03 channel). Record each meeting's protocol from its OWN channel listing.

## Older protocols 2022..2025-08: NOT ONLINE (dead end)
- 2022-2023 (and presumably 2024/early 2025) KF protocols were published as
  static PDFs on the old same page:
  https://www.partille.se/siteassets/kommun--politik/protokoll-och-kallelser/kf-<year>/pr_kf_*.pdf
  (e.g. pr_kf_2022_02_01.pdf .. pr_kf_22_12_06.pdf; full 2022 list from Wayback
  snapshot 20230131140312 of /kommun--politik/protokoll/: 02-01, 03-01, 03-29,
  05-03, 06-07, 08-30, 09-27, 10-18, 11-15, 12-06).
- These siteassets URLs now return HTTP 500 ("Något gick fel", EPiServer error
  page) on the live site - files were removed/migrated away. Verified for
  kf-2022, ks-2022, von-2022 samples. Globalassets/contentassets variants also 500.
- Wayback Machine has NOT captured the PDF bodies (CDX "No URLs" for pr_kf_2022;
  direct wayback fetch 404). Only the linking page is archived (last snapshot of
  /kommun--politik/protokoll/ is 2023-01-31; no 2024/2025 snapshots).
- e-arkiv (https://earkiv.partille.se, iipax serviceapp): only Byggärenden,
  Geotekniska utredningar, Planhandlingar, Fastighetsregister - no protocols.
- Digital anslagstavla (https://lex.partille.se/Lex2PinBoardWasm, Blazor WASM;
  API /pinboard/searchdocument POST with tenantGuid f4856006-...): notice board
  only - Tillkännagivanden/Anslagsbevis/Kungörelser, recent postings; not minutes.
- NetPublicator search endpoint only indexes the 6 currently published meetings.

## Tips for next runs
- Fetch the KF page HTML, read data-np-config, then call the read API for the
  root channel and each meeting - no need for Playwright beyond discovering the
  config (documents come from docs.netpublicator.com JSON).
- Cookie dialog appears but does not block API access.
- If the municipality later restores an archive or the harvest needs 2022-2025,
  the old siteassets paths are the only known pattern - contact kundcenter.

## Round 2 (2026-08-20, same range) - CORRECTION on Wayback / +1 doc

Re-verified the whole harvest. The 6 NetPublicator KF docs (2025-09-10 .. 2026-06-02)
were re-downloaded, text-verified and recorded again (same URLs/titles as round 1;
record_documents accepted them, no duplicates reported in this run's view).

NEW FINDING - the notebook's claim "Wayback has NOT captured the PDF bodies" is
WRONG for at least one file. CDX + direct fetch prove:
- https://web.archive.org/web/20220301224106if_/https://www.partille.se/siteassets/kommun--politik/protokoll-och-kallelser/kf-2022/pr_kf_2022_02_01.pdf
  is a genuine capture (application/pdf, 998849 bytes) of the KF sammanträdesprotokoll
  for 2022-02-01 (Sammanträdesdatum 1 februari 2022, §1-§25, ordförande Inger René).
  RECORDED: date 2022-02-01, title "1 februari 2022", source_page the archived
  listing http://web.archive.org/web/20230131140312/https://www.partille.se/kommun--politik/protokoll/ , conf 0.9.
- The same archived listing page (20230131140312, last snapshot of that page) lists
  10 KF 2022 protocols (02-01, 03-01, 03-29, 05-03, 06-07, 08-30, 09-27, 10-18,
  11-15, 12-06) but CDX shows ONLY pr_kf_2022_02_01.pdf was ever captured.
- CDX prefix queries: kf-2023/*, kf-2024/*, kf-2025/* => zero captures. Domain-wide
  filter original:.*pr_kf.* confirms no other 2022+ KF PDF anywhere in Wayback.
- No Wayback snapshots of the current KF page URL (handlingar-fran-kommunfullmaktige).
- e-arkiv still redirect-loops; per round 1 it holds no protocols.

So for range 2022-01-01..2026-08-20 the complete recoverable set is 7 KF minutes:
2022-02-01 (Wayback), 2025-09-10, 2025-10-15, 2025-12-03, 2026-02-18, 2026-04-22,
2026-06-02 (NetPublicator). 2022-03-01..2025-08 and Sep-Dec 2026: not available
online (site keeps only ~12 months; old siteassets 500; no archive captures).

Tip: when re-checking Wayback, query CDX with matchType=prefix on the directory
(e.g. .../protokoll-och-kallelser/kf-2022/*) and try the if_ modifier for raw
bytes; the plain /web/<ts>/ URL returns the toolbar wrapper HTML.
