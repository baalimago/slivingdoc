# Åsele (asele.se) — KF (Kommunfullmäktige) retrieval notes

## Site structure (as of 2026-08-20)
- Åsele kommun site is a JavaScript-rendered SPA (Umbraco delivery API backend).
  Plain slim_http GETs only show nav chrome; document listings load via the
  Umbraco Delivery API, so use the API directly.
- The "Politiska protokoll" page (https://asele.se/kommun-och-politik/protokoll-och-dokument/politiska-protokoll/)
  renders tabs per committee (Kommunfullmäktige, Kommunstyrelsen, Allmänna utskottet, ...),
  then per year (2014..2026), then a list of PDF links.
- The listing is fed by Umbraco Delivery API media queries:
  https://www.asele.se/umbraco/delivery/api/v2/media/?take=300&fields=properties[name]&fetch=children:{guid}

## Known GUIDs (media tree under /filer/protokoll/)
- Root "protokoll" folder:           9ddbc948-280b-411f-ae93-3aea89c4720d
- Kommunfullmäktige folder:          299074ea-a5a2-43cd-ba24-ebcda6e0cdfe
  - 2022: 5ef9798c-4fbd-44e6-92a6-330ae57218a0
  - 2023: 9cf7a742-a252-41f5-a73c-5ce65fa82861
  - 2024: 58b0c35e-d369-4554-a0c5-22ee291d763f
  - 2025: d0381c49-1520-4074-ae62-58bd33dcf6d4
  - 2026: 764102f6-f087-4369-86ca-f300d40881ff
- To find a committee folder GUID, query children of the protokoll root; each
  committee folder's GUID then feeds the year query above.

## KF minutes found (2022-01-01 .. 2026-08-20) — all recorded
PDFs live at https://www.asele.se/media/{slug}/protokoll-kf-YYMMDD-*.pdf
- 2022: 220228, 220509, 220926, 221114 (4)
- 2023: 230227, 230508, 231002, 231113, 231218 (5)
- 2024: 240226, 240408, 240506, 240902, 241111 (5)
- 2025: 250224, 250512, 250929, 251110 (4)
- 2026: 260223, 260504 (2)
Total 20. All verified by downloading + text extraction: each is
"ÅSELE KOMMUN SAMMANTRÄDESPROTOKOLL Kommunfullmäktige <date>".

## Dead ends / notes
- Anslagstavla (https://asele.se/anslagstavla/) holds "Kallelse Föredragningslista"
  (agenda/notice) PDFs and kungörelser — NOT minutes; skip for KF minutes harvest.
- Kommunfullmäktige page (politisk organisation) has no direct PDF links; it just
  points to Politiska protokoll.
- No separate e-diary (diarium) with KF protocols; the media tree is canonical.
