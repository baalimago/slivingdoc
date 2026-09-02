# kumla.se scanner notes (kf = Kommunfullmäktige)

## Site structure (live, 2026-08-20)
- KF protocols live on: /kommun-och-politik/demokrati-och-insyn/moten-handlingar-och-protokoll/kommunfullmaktige-och-kommunstyrelsen.html
- The page has 5 accordions (SiteVision webapp, server-rendered tables hidden in collapsed accordions):
  "Kommunfullmäktige, protokoll 2022-2026", "Kommunfullmäktige, kallelser 2022-2026",
  "Kommunstyrelsen, protokoll 2022-2026", "Kommunstyrelsen, kallelser 2022-2026",
  "Kommunstyrelsens arbetsutskott, protokoll 2022-2026".
- Each accordion lists year folders. Folder navigation via URL param: ...html?folder=<folderId>&sv.url=12.7aaf5ec191e4e214ee8879
  (sv.url differs per accordion: KF protokoll=12.7aaf5ec191e4e214ee8879, KF kallelser=...8b84, KS protokoll=...88b3, KS kallelser=...8bab, KSAU=...88d4)
- KF protokoll year folders: 2026=19.7932b3db19c9887db6c3836, 2025=19.4eb7a993194d84a46d73164,
  2024=19.7aaf5ec191e4e214ee88a0, 2023=19.7aaf5ec191e4e214ee889e, 2022=19.7aaf5ec191e4e214ee8882.
- File links: https://kumla.se/download/18.<nodeId>/<ts>/<filename>.pdf — fetchable directly (200, application/pdf).
- slim_http with required_tokens ["download"] on a folder URL returns the full file listing (name, size, upload date).
- KF kallelser folders mostly empty (2022/2023 empty; 2024 has only 2024-10-21; 2025/2026 have kallelser for each meeting).

## KF meetings per year (cross-verified with cinestream.se/kumla broadcast archive + PDF metadata scan dates)
- KF meets ~7-9x/year, Mondays. 2022: 8 meetings (Apr 25 .. Dec 12; none Jan-Mar per cinestream).
  2023: 7 (02-06, 03-06, 04-24, 06-12, 09-25, 10-23, 11-27). 2024: 7 (02-05, 03-11, 04-22, 06-10, 08-26, 10-21, 11-25).
  2025: 8 (02-03, 03-17, 04-28, 06-09, 09-08, 10-20, 11-24, 12-15) — but the 12-15 protocol is NOT published
  (kallelse exists, no protocol file); only 7 protocols recorded. 2026: 3 (03-16, 04-27, 06-08).
- cinestream.se/kumla lists broadcast dates (reverse-chron), useful to pin meeting dates.

## Scan-name/"(N)" files
- Bulk upload 2025-01-21 renamed many scanned protocols to "Justerat protokoll (N).pdf".
  Mapping (via content verification + PDF metadata CreationDate/scan + cinestream meeting dates):
  (3)=2022-03-21 (ESTIMATED: Konica scan 2022-03-22, 29pp; no cinestream broadcast; conf 0.5),
  (26)=2022-09-07 (verified), (27)=2023-03-06 (verified), (28)=2023-04-24, (29)=2023-09-25,
  (30)=2023-10-23, (31)=2023-11-27, (32)=2024-03-11, (33)=2024-04-22, (34)=2024-06-10 (scan 06-13),
  (35)=2024-08-26. skannat_kritor_2024-02-12 = 2024-02-05 meeting (scan 02-12).
- "Justerat protokoll KS 20220829.pdf" is CONTENT-VERIFIED to be a KF protocol dated 2022-08-29
  (filename "KS" is misleading; diarienummer KS 2021/655).
- Fasad.pdf in the 2022 folder is a 1-page building drawing (uploaded 2024-09-20), NOT a protocol — skipped.

## Recorded (33 KF protocols, all https://kumla.se/download/18.*)
- 2022 (9): 2022-03-21 (3), 04-25 KF220425, 05-16 16maj, 06-13 220613, 08-29 KS20220829(=KF),
  09-07 (26), 10-17 kf221017, 11-21 KF221121, 12-12 KF221212.
- 2023 (7): 02-06 KF230206, 03-06 (27), 04-24 (28), 06-12 Signerat KF 2023-06-12, 09-25 (29), 10-23 (30), 11-27 (31).
- 2024 (7): 02-05 skannat, 03-11 (32), 04-22 (33), 06-10 (34), 08-26 (35), 10-21 Protokoll 2024-10-21, 11-25 KF 25 nov.
- 2025 (7): 02-03, 03-17, 04-28, 06-09, 09-08, 10-20, 11-24.
- 2026 (3): 03-16, 04-27, 06-08 (Protokoll KF signerat - med rättelse).
- Confidence 0.9-0.95 for named/text-verified files; 0.7-0.8 for scan-date/cinestream-inferred; 0.5 for (3).

## Dead ends / tips
- politik.kumla.se (old D2/MeetingsPlus portal, embedded via iframe in 2022-2023) is DEAD (no DNS).
  Wayback has only a few __D2_DOWNLOAD__ captures (mostly masked versions, tiny 748-883 byte error pages for 2024).
- diarium (searchport.kumla.se/search.aspx) covers nämnder only, no KF; iframe on /demokrati-och-insyn/diarium.html.
- kumla.se site search (/om-webbplatsen/sok.html?query=...) indexes TEXT-LAYER PDFs only; fully-scanned PDFs
  (most 2022-2024 KF protocols) are not searchable by content. It did confirm 16maj via scan title SKM_C25822052307180.
- Wayback CDX rate-limits hard (503/429) after a few requests — space requests out.
- sammanträdestider page only shows current year (2026).
