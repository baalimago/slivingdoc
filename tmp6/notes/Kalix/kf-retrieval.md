# Kalix (kalix.se) KF retrieval notes

## Site structure
- KF protocol page: https://kalix.se/kommun/Moten-handlingar-och-protokoll/Kommunfullmaktige/
  (plain HTML, no JS listing). Entry from https://kalix.se/kommun/Moten-handlingar-och-protokoll/.
- The page currently lists ONLY years 2024-2026 ("Protokoll och handlingar 2026/2025/2024"
  H4 sections). 2022 and 2023 sections were REMOVED from the live page (probably during 2025
  site rework). The PDFs for 2022-2023 are however STILL LIVE on kalix.se under
  /globalassets/... — the old links work even though they are no longer linked.
- For 2022-2023 link inventory, use Wayback Machine snapshots of the same KF protocol page:
  - 2023-01-27 (web/20230127014546), 2023-12-04 (web/20231204064826), 2024-08-11
    (web/20240811153351). The 2024-08-11 snapshot has the complete 2022 and 2023 sections.
  - Old anslag pages (https://www.kalix.se/anslagstavla/anslag/kommunfullmaktiges-anslag/
    tillkannagivande-...-2022-02-07 etc.) are just announcement notices WITHOUT PDF links —
    not useful for finding protocol PDFs; the protocol page snapshots are the source.
- PDF naming: mostly /globalassets/politik-och-styrning/... and a few /globalassets/-block-/
  /contentassets/. No date-template URL; scrape the listing.

## KF meetings 2022-01-01..2026-08-20 (23 protocols recorded, one per meeting date)
2022 (5): 02-07, 04-11, 06-13, 10-17, 11-28 (28-29 Nov two-day meeting, one protocol).
2023 (5): 02-06, 04-11, 06-19, 10-16, 11-27.
2024 (6): 02-05, 04-15, 06-17, 07-04 (extra), 09-23, 11-25.
2025 (5): 02-10, 04-28, 06-16, 09-22, 11-24.
2026 (2 within range): 02-09, 04-27. 2026-06-15 meeting happened (webcast, 24 ärenden) but
its protocol is NOT published online as of 2026-08-20 — only the tillkännagivande PDF
(globalassets/.../ny-katalog/tillkannagivande-och-handlingar---kommunfullmaktige-den-15-juni-2026.pdf)
is up; candidate URL .../ny-katalog/protokoll---kommunfullmaktige-den-15-juni-2026.pdf is 404.
Recorded 23; confidence 0.97 (0.9 for 2025-06-16, which is a scanned PDF — pdftotext empty,
verified via listing title + webcast page).

## Verified by PDF text
All 2022-2025 + 2026-02/04 files start with "SAMMANTRÄDESPROTOKOLL / Sammanträdesdatum
YYYY-MM-DD / Kommunfullmäktige" matching the listed date. Note: 2026-02-09 protocol filename
says "8-februari" but document date is 2026-02-09 (title on page: den 9 februari 2026).

## Do NOT record (same-meeting excerpts / agenda-adjacent)
- Omedelbar-justering excerpts (e.g. globalassets/politik-och-styrning/kf-220613-omedelbarjustering--87-89.pdf,
  skannat-2025-02-10_10-44-31-861.pdf "Del av protokoll §18,19") — same date as main protocol.
- Tillkännagivande-och-handlingar PDFs (meeting packages/agendas) for each meeting.
- Budget/budgetberedning/årsredovisning/revisionsberättelse PDFs on the page.
- 2026-06-15: no protocol exists online.

## Notes for next time
- If 2024-2026 listings are missing again, they live on the live page; 2022-2023 require
  wayback snapshots of the same URL (2024-08-11 snapshot is the best single source).
- Webcast/recording page: https://kalix.se/kommun/Moten-handlingar-och-protokoll/Kommunfullmaktige/webbfullmaktige/
  confirms meeting dates (KF YYYY-MM-DD) and has per-meeting pages under
  /kommun/filmer/Webbfullmaktige/kf-YYYY/kf-YYYY-MM-DD/.
