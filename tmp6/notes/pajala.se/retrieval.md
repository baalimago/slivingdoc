# Pajala (pajala.se) kf retrieval notes

## Site structure
- Entry point for protocols: https://pajala.se/kommun-politik-och-service/anslagstavla/protokoll/
  (reachable from front page menu Kommun, politik & service -> Anslagstavla -> Protokoll, or
  direkt via /kommun-politik-och-service/anslagstavla/protokoll/).
- Page is a plain HTML (Umbraco) listing — no JS rendering, no pagination. One page holds
  ALL bodies and ALL years: H2 sections per body (Kommunfullmäktige, Kommunstyrelsen, Allmänna
  utskottet, Barn- och utbildningsnämnden, ..., Valnämnden, Revisorerna, ...), each with H3
  year subsections ("Protokoll 2026" ... back to 2012 for KF/KS).
- Each entry is a direct PDF link under /media/<hash>/<filename>.pdf, e.g.
  https://pajala.se/media/w14n3kxe/protokoll-2022-02-28.pdf
- Filename conventions: "protokoll-YYYY-MM-DD.pdf" (2022-2024), "protokoll-kf-YYMMDD-kompl.pdf"
  (2026), "protokoll-kf-YYYY-MM-DD-inkl-voteringslista.pdf" (some 2023), "protokoll-kf-251103.pdf"
  (late 2025). The media hash is opaque — no date-template URL possible; scrape the listing page.

## KF harvest 2022-01-01..2026-08-20 (30 protocols, all on the protokoll page, KF section)
2022: 02-28, 04-11, 06-13, 08-15, 10-31, 11-28, 12-19 (7)
2023: 02-27, 04-11, 06-19, 09-25, 10-30, 11-27 (6)
2024: 02-26, 04-08, 06-17, 07-03, 09-23, 10-28, 11-25 (7)
2025: 02-24, 04-07, 04-28, 06-23, 09-29, 11-03, 12-15 (7)
2026: 02-23, 04-13, 06-15 (3, last within range)
All verified by PDF text: "PAJALA KOMMUN SAMMANTRÄDESPROTOKOLL / Kommunfullmäktige <date>".
Recorded confidence 0.97, source_page the protokoll URL.

## Duplicates / excerpts to NOT record (same meeting date as a main protocol)
- protokoll-2024-04-08.pdf "(1)" (parjodct) — identical 33-page protocol duplicate of vznpuqtq.
- protokoll-2024-04-08-kopia.pdf "Arvodesbestämmelser" — excerpt.
- protokoll-2024-06-17-omedelbar.pdf "Detaljplan Sahavaara" — excerpt (§).
- protokoll-2024-07-03-omedelbar.pdf — excerpt.
- protokoll-2023-02-27-omedelbar.pdf "§ 1 omedelbar just." — excerpt.
- protokoll-kf-260223-3-o-8.pdf "§ 3 o 8" — excerpt.
- protokoll-kf-251215-106.pdf "§ 106" — excerpt.
- Rule of thumb: one main protocol per meeting date; excerpts carry "omedelbar", "§", "kopia",
  "Arvodesbestämmelser" or "(1)" in title.

## Notes
- 2025-12-15 KF protocol is filed under the "Protokoll 2026" H3 subsection (filename
  protokoll-kf-251217.pdf, title "Kommunfullmäktige 2025-12-15", content date 2025-12-15) —
  do not be misled by the subsection or filename year.
- 2025-04-28 is a short extra meeting (09:00-09:15), still a real protocol (8 kB text).
- Kallelser (agendas) live under a separate "Kallelser" section / anslagstavla/kallelser — skip.
- Other bodies (KS, nämnder, utskott) have the same page/section layout; only the KF section
  matters for kf.
