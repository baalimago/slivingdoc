# monsteras.se scanner notes (Mönsterås kommun, kf = Kommunfullmäktige)

## Site structure / where KF minutes live
- WordPress site (theme "Monsteras", ver 20220506). Static HTML pages; no JS listing, no diarium.
- Protocol archive root: https://www.monsteras.se/kommun-och-politik/politik-och-demokrati/protokoll/
  lists one subpage per body. KF subpage (canonical, still live):
  https://www.monsteras.se/kommun-och-politik/politik-och-demokrati/protokoll/protokoll-fran-kommunfullmaktige/
  "Dokumentlista" table: column "Publicerat" (publish date) + link text "Kommunfullmäktiges protokoll YYYY-MM-DD (pdf)".
- IMPORTANT: the live KF page only lists ~34 rows, newest first, ending at 2023-02-27.
  It does NOT include 2022 protocols. 2022 KF protocols are NOT linked anywhere on the live site but the
  PDFs still exist on the server under /app/uploads/. Recovered the exact 2022 URLs from the old
  (pre-2023) site structure via Wayback Machine CDX: old page
  https://www.monsteras.se/kommun-och-politik/politik-och-demokrati/moten-och-protokoll/protokoll/
  capture 20230208033734 listed all 2022 KF protocols. All 9 URLs still resolve 200 on the live server.

## URL / naming notes
- 2022 protocols: /app/uploads/YYYY/MM/Protokoll-KF-2022-MM-DD.pdf mostly
  (exact names: 2022-02-28 -> 2022/03/Protokoll-KF-2022-02-28.pdf; 2022-03-28 -> 2022/04/Protokoll-KF-2022-03-28.pdf-1.pdf;
   2022-04-25 -> 2022/05/Protokoll-KF-2022-04-25.pdf; 2022-05-23 -> 2022/06/Protokoll-KF-2022-05-23-14.pdf;
   2022-06-13 -> 2022/06/Protokoll-KF-2022-06-13-1.pdf; 2022-09-19 -> 2022/09/Protokoll-KF-2022-09-19.pdf;
   2022-10-24 -> 2022/10/Protokoll-KF-2022-10-24.pdf; 2022-11-28 -> 2022/12/Protokoll-kommunfullmaktige-2022-11-28.pdf;
   2022-12-19 -> 2023/01/Protokoll-kommunfullmaktige-2022-12-19.pdf)
- 2023-2026: filenames vary a lot (Protokoll-KF-..., Protokoll-kommunfullmaktige-..., Protokoll-Alla-.pdf,
  Protokoll-250324.pdf, ...). Always trust the link TEXT for title/date, not the filename.
- Upload dir = month of publication, usually 1-2 months after the meeting (e.g. 2026-06-15 meeting published 2026-08-19 under /2026/08/).
- Some meeting dates appear twice on the page (re-uploads): 2023-02-27 (2 URLs), 2023-04-24 (2 URLs) - record once.
- Skip "Kommunfullmäktiges presidiums protokoll ..." rows (presidium, not a KF meeting).

## KF harvest result 2022-01-01..2026-08-20 (40 meetings, one protocol per date)
- 2022: 02-28, 03-28, 04-25, 05-23, 06-13, 09-19, 10-24, 11-28, 12-19 (9)
- 2023: 02-27, 03-27, 04-24, 05-29, 06-12, 09-25, 10-23, 11-27, 12-18 (9)
- 2024: 02-26, 03-25, 04-29, 05-27, 06-10, 09-23, 10-28, 11-25, 12-16 (9)
- 2025: 02-24, 03-24, 04-28, 06-16, 09-22, 10-27, 11-24, 12-15 (8)
- 2026: 02-23, 03-23, 04-27, 05-25, 06-15 (5)
- No KF meetings in Jan, Jul, Aug. A kallelse exists for 2025-05-26 (Wayback anslag page) but NO protocol/tillkännagivande
  ever appeared - that meeting was evidently cancelled; do not invent a 2025-05-26 record.
- All 40 PDFs verified: "Kommunfullmäktige / Sammanträdesprotokoll / <date>" on first page; download 200 application/pdf.
  Recorded with confidence 1.0 for the 2023-2026 (live listing) and 0.9 for the 2022 (recovered via Wayback, still live).

## Dead ends / tips
- Site search (?s=...) and anslagstavla only surface recent items; anslag (tillkännagivande/kallelse) pages for 2022-2025
  have been deleted from the live site (404), old URLs only in Wayback.
- Old anslag URL patterns (dead): /anslagstavla/kallelse/..., /anslagstavla/tillkannagivande/..., /anslagstavla/anslag/...
- If a 2022+ protocol is missing from the live page again, recover exact URLs from Wayback capture 20230208033734
  of the old /moten-och-protokoll/protokoll/ page, or CDX filter original:.*(Protokoll|kommunfullmaktige).* on monsteras.se.
- sitemap: page-sitemap.xml / anslag-sitemap.xml exist (Yoast); anslag-sitemap only has ~12 recent notices.
