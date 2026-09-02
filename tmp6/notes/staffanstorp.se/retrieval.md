# staffanstorp.se retrieval log (kf = Kommunfullmäktige)

## Run 2026-08-20 — SUCCESS: 31 KF protocol documents recorded (2022-03-09 .. 2026-06-08)

## Entry / site structure
- Staffanstorp.se is WordPress (Municipio). The page
  https://staffanstorp.se/kommun-och-politik/demokrati-och-paverkan/kallelser-handlingar-och-protokoll/
  ("Kallelser, handlingar, protokoll") states "kallelser ... justerade protokoll ... från 2020 och framåt"
  and links OUT to a NetPublicator reader: **https://www.netpublicator.com/reader/r99471870**.
- The reader is JS (np-publicreader) but the underlying API is plain JSON — no Playwright needed once
  the pattern is known.

## NetPublicator API (docs.netpublicator.com, reader r99471870)
- Channel/meeting listing: `https://docs.netpublicator.com/api/public/r99471870/read?hash=<hash>&isr=false`
  - root hash: `2fdb323dd6eb149` (isr=true returns root channels)
  - KF channel: `2fdb323dd6eb149-193c192c90ec404061` → subchannels per year 2020..2026
  - year channels (root-KF-year): 2022=...-9b6fbe9a0ba66067067, 2023=...-5c7f30e196428242695,
    2024=...-0f5ae3738a058242698, 2025=...-3a6df34db70c9096473, 2026=...-4e79e183133b9644036
  - meeting read: hash = root-KF-year-<meetingId>; response has top-level "documents" (type document:
    the KF protocol + Kallelse) and agenda-item channels (skip those).
- Document download (WORKS directly with download_document, GET 200 application/pdf, no session):
  `https://docs.netpublicator.com/api/public/r99471870/document/<docId>?hash=<meetingHash>`
  (cache param optional). This is NOT like Hörby (no HEAD 404 trap).
- Search endpoint `/search?query=<q>&hash=2fdb323dd6eb149` exists but is capped (~200 results,
  dominated by other nämnder) — useless for full enumeration; use year-channel reads.
- slim_http can query the read API directly (raw JSON, one line). The API tolerates hash
  `root-yearId` but use the full `root-KF-yearId[-meetingId]` form.

## Recorded (31, one per meeting date; 2022-03-09 has TWO portal meeting records — see note)
- 2022 (7): 03-09, 04-06, 05-04, 06-01, 10-19, 11-16, 12-14.
- 2023 (7): 03-08, 04-05, 05-03, 06-07, 10-18, 11-20, 12-13.
- 2024 (7): 03-11, 04-15, 05-13, 06-10, 10-14, 11-18, 12-16.
- 2025 (7): 03-03, 04-14, 05-12, 06-09, 10-13, 11-10, 12-15.
- 2026 (3): 04-13, 05-11, 06-08.
- Meeting pattern: KF meets Mar/Apr/May/Jun/Oct/Nov/Dec only (no Jan/Feb/Jul/Aug/Sep) in 2022–2025;
  2026 has Apr/May/Jun. Nothing between 2026-06-08 and range end 2026-08-20.

## Verification / gotchas
- 2025-03-03 .. 2026-06-08 protocols have a text layer: page 1 = "MÖTESPROTOKOLL Kommunfullmäktige
  Mötesdatum <date>" (conf 0.95). 2022-04-06 and 2022-12-14 also text-readable (conf 0.95).
- 2022-03-09, 2022-05-04, 2022-06-01, 2022-10-19, 2022-11-16 and ALL 2023 + ALL 2024 protocols are
  scanned images (pdftotext → "ocr required", no text layer). Verified only by portal title + valid
  PDF + page count → conf 0.9. To read them you'd need OCR.
- 2022-03-09 appears TWICE in the 2022 channel: 17:00 (Rådhuset, "Kallelse fortsättning på KF
  2021-12-15", protocol §§1-6) and 19:00 (sal Vallby, main agenda, protocol §§7-22). One date = one
  record: recorded the §§7-22 main protocol only.
- Per-meeting extras to SKIP (not the KF minutes): "Kallelse ..." docs; "omedelbar justering" partials
  (2025-05-12 §39, 2025-06-09 §55); duplicate split "§§63-67" for 2024-06-10 (main = §§51-72);
  agenda attachments like Valberedningens protokoll, MBL-protokoll, Styrelsemotesprotokoll_*,
  Protokoll Sydarkivera, Protokoll från revisionens sammanträde, FINSAM, Protokoll från styrelsemöte.
- 2025-12-15 protocol portal title is auto-generated ("Protokoll skapad KF 2025-12-19 08.34.10...");
  text-verified as the 2025-12-15 KF protocol.

## Tips for next run
- Enumerate: read KF year channels 2022..2026 (no pagination — single JSON array per channel),
  then read each meeting, take the FIRST top-level document whose text ~ "Protokoll" (skip Kallelse),
  download with the no-session GET form, verify page 1 when text layer exists.
- source_page used: https://www.netpublicator.com/reader/r99471870 (the reader the municipality links to).
