# harryda.se retrieval log (Härryda kommun), kf = Kommunfullmäktige

## kf — round 1 (2026-08-20): SUCCESS, 46 protocols recorded (2022-03-03 .. 2026-06-17)

- harryda.se is SiteVision CMS. KF meeting minutes live on per-meeting pages under
  /kommunochpolitik/politikochdemokrati/motenhandlingarochprotokoll/kf/kommunfullmaktige<NN><month><year>.<node>.html
  Each page has sections "Webbsändning" / "Protokoll" / "Handlingar" (handlingar = dagordning,
  ärendelista, motionssvar etc. — skip those; only the "Protokoll" link(s) are minutes).
- Main listing page: https://harryda.se/kommunochpolitik/politikochdemokrati/motenhandlingarochprotokoll.4.1439a0061817fd1d9405498.html
  - KF 2026 meetings: table of links (22 jan, 29 jan, 26 feb, 26 mars, 29 april, 28 maj, 17 juni,
    17 sept, 15 okt, 12 nov, 10 dec).
  - "Äldre webbsändningar, protokoll och handlingar" -> 2025/2024 link:
    .../kf/webbsandningarprotokollochhandlingar20242025.4.7df5c6ed182e8f937ad6d1f.html
    (lists 10 meetings for 2025 and 10 for 2024, each linking to a per-meeting page).
  - "Före 2024? Kontakta kansli@harryda.se" — but the 2022/2023 per-meeting pages STILL EXIST
    live (unlinked). Enumerate them via the site search or Wayback CDX (see below).

## Where the 2022-2023 meeting pages live (not linked from the listing anymore)
- 2022 pages: node prefix 4.7df5c6ed182e8f937ad... (same parent as the 2024-2025 page), e.g.
  .../kf/kommunfullmaktige3mars2022.4.7df5c6ed182e8f937ad6f4f.html
- 2023 pages: node prefix 4.5b317ffb1864b8f60728... (one exception: 2februari2023 is
  4.2ad5154218529a08d0613902), e.g. .../kf/kommunfullmaktige2februari2023.4.2ad5154218529a08d0613902.html
- Wayback CDX is a reliable enumerator:
  http://web.archive.org/cdx/search/cdx?url=harryda.se&matchType=domain&filter=urlkey:.*fullmaktige.*2022.*&collapse=urlkey
  (same for 2023). Also kultur.harryda.se mirrors the same pages (2025 captures).
- 2022 schedule (old page, captured): sammantraden2022 listed 3 feb (INSTÄLLT/cancelled),
  3 mars, 31 mars, 28 april, 19 maj, 16 juni, 22 sept, 20 okt, 17 nov, 15 dec -> 9 protocols.
- 2023: 2 feb, 2 mars, 30 mars, 27 april, 24 maj, 15 juni, 21 sept, 19 okt, 16 nov, 14 dec -> 10.
- Site search also finds these pages (server-side, works with slim_http):
  https://www.harryda.se/funktionssidor/sokresultatsida.4.5e92e42e181262dec291baa8.html?query=kommunfullm%C3%A4ktige+protokoll+2022
  pagination via &startAtHit=10. Relevance-ranked, not exhaustive — use CDX for completeness.

## Protocol document URL shape and gotchas
- Minutes are PDFs of the shape https://www.harryda.se/download/<18.nodeid>/<unixms>/<filename>.pdf
  (SiteVision download, opaque node id + publish timestamp; date only in the filename, e.g.
  "Protokoll Kf 2022-03-03_web.pdf", "Protokoll Kf 2024-02-01 justerad.pdf",
  "Protokoll Kf 2026-03-26.pdf"). Filenames/§-ranges vary; never guess, scrape the page.
- One meeting = one protocol, but several meetings split the minutes across TWO PDFs, e.g.
  2026-01-22 ("Protokoll § 3" + "Protokoll §§ 1-2,4-16"), 2026-02-26 (§§52,55 + §§49-51,53-54,56-64),
  2025-10-16 (§162 + §§156-161,163-176), 2024-11-14 (main + "§§142-144 (rättad)"). Record ONE
  per date — the main file covering the most paragraphs; skip the separate §-file / rättad file.
- 2022-05-19 live page has the protocol link REMOVED (plain text "Protokoll §§ 74-90" only), but
  the PDF still exists at the Wayback-recovered URL:
  https://www.harryda.se/download/18.7df5c6ed182e8f937ad10ccf/1663753135790/Protokoll%202022-05-19_web.pdf
  (verified live download 200, SAMMANTRÄDESPROTOKOLL 2022-05-19 §§74-90). If a live page lacks the
  protocol link, check a Wayback capture of the same page for the download URL.
- download_document + document_to_text verified samples from 2022/2024/2025/2026: all genuine
  "SAMMANTRÄDESPROTOKOLL Kommunfullmäktige Sammanträdesdatum <date>".
- Dates recorded = meeting date (sammanträdesdatum), titles = link text ("Protokoll §§ 1-27" etc.),
  source_page = the per-meeting page. Confidence 0.95.

## Counts per year
2022: 9 (03-03..12-15), 2023: 10 (02-02..12-14), 2024: 10 (02-01..12-12),
2025: 10 (01-30..12-11), 2026 in range (<=2026-08-20): 7 (01-22..06-17; 17 sept+ out of range).
Total 46.
